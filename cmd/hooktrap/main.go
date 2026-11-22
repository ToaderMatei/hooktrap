// Command hooktrap runs the HookTrap local request-capture server and dashboard.
//
// HookTrap is a self-hosted request inspector in the spirit of webhook.site,
// built for authorised security testing, lab work and CTFs. It captures HTTP
// (and optionally DNS / raw TCP) interactions, shows them live, and lets you
// script the response. Run it only against systems you own or are permitted to
// test.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/ToaderMatei/hooktrap/internal/api"
	"github.com/ToaderMatei/hooktrap/internal/app"
	"github.com/ToaderMatei/hooktrap/internal/capture"
	"github.com/ToaderMatei/hooktrap/internal/config"
	"github.com/ToaderMatei/hooktrap/internal/files"
	"github.com/ToaderMatei/hooktrap/internal/listeners"
	"github.com/ToaderMatei/hooktrap/internal/store"
	"github.com/ToaderMatei/hooktrap/web"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hooktrap:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		dataFlag    = flag.String("data", "", "data directory (default: per-user app data)")
		httpPort    = flag.Int("http-port", 0, "override capture HTTP port")
		adminPort   = flag.Int("admin-port", 0, "override dashboard port")
		noBrowser   = flag.Bool("no-browser", false, "do not open the dashboard in a browser")
		showVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()
	if *showVersion {
		fmt.Println("HookTrap", version)
		return nil
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	dataDir := *dataFlag
	if dataDir == "" {
		d, err := config.DataDir()
		if err != nil {
			return err
		}
		dataDir = d
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	cfg, err := config.Load(dataDir)
	if err != nil {
		log.Warn("config", "msg", err)
	}
	if *httpPort != 0 {
		cfg.HTTPPort = *httpPort
	}
	if *adminPort != 0 {
		cfg.AdminPort = *adminPort
	}
	if *noBrowser {
		cfg.OpenBrowser = false
	}
	cfg = cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("configuration: %w", err)
	}

	st, err := store.Open(filepath.Join(dataDir, "data"), cfg.MaxPerBin)
	if err != nil {
		return err
	}
	defer st.Close()

	fs, err := files.New(filepath.Join(dataDir, "files"))
	if err != nil {
		return err
	}

	application := app.New(st, fs, cfg, log, version)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rt := &appRuntime{app: application, dataDir: dataDir, log: log}
	if err := rt.startCapture(ctx); err != nil {
		return err
	}
	rt.startExtras(ctx)

	adminAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.AdminPort))
	server := api.NewServer(application, web.FS(), rt.reload)

	dashURL := "http://" + adminAddr + "/"
	printBanner(cfg, dashURL)
	if cfg.OpenBrowser {
		go openBrowser(dashURL)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- server.Start(ctx, adminAddr) }()

	select {
	case <-ctx.Done():
		log.Info("shutting down")
		time.Sleep(200 * time.Millisecond)
		return nil
	case err := <-errCh:
		if err != nil && ctx.Err() == nil {
			return fmt.Errorf("dashboard server: %w", err)
		}
		return nil
	}
}

// runtime owns the capture listeners so they can be (re)started.
type appRuntime struct {
	app     *app.App
	dataDir string
	log     *slog.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
}

func (rt *appRuntime) startCapture(ctx context.Context) error {
	cfg := rt.app.Config()
	h := capture.NewHandler(rt.app.Store, rt.app, rt.app.Files,
		capture.Options{CatchAll: cfg.CatchAll, MaxBodyKB: cfg.MaxBodyKB}, rt.log)
	rt.app.CaptureHandler = h
	srv := capture.NewServer(h)
	if err := srv.StartHTTP(ctx, net.JoinHostPort(cfg.BindAddr, strconv.Itoa(cfg.HTTPPort))); err != nil {
		return err
	}
	if cfg.HTTPSEnabled {
		if err := srv.StartHTTPS(ctx, net.JoinHostPort(cfg.BindAddr, strconv.Itoa(cfg.HTTPSPort)), rt.dataDir); err != nil {
			rt.log.Warn("https listener", "err", err)
		}
	}
	return nil
}

func (rt *appRuntime) startExtras(ctx context.Context) {
	cfg := rt.app.Config()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.cancel != nil {
		rt.cancel()
	}
	exCtx, cancel := context.WithCancel(ctx)
	rt.cancel = cancel

	if cfg.DNSEnabled {
		dns, err := listeners.NewDNSServer(net.JoinHostPort(cfg.BindAddr, strconv.Itoa(cfg.DNSPort)), cfg.DNSAnswerIP, rt.app, rt.log)
		if err != nil {
			rt.log.Warn("dns listener", "err", err)
		} else {
			go func() {
				if err := dns.Start(exCtx); err != nil {
					rt.log.Warn("dns listener", "err", err)
				}
			}()
		}
	}
	if cfg.TCPEnabled {
		tcp := listeners.NewTCPServer(net.JoinHostPort(cfg.BindAddr, strconv.Itoa(cfg.TCPPort)), cfg.TCPBanner, rt.app, rt.log)
		go func() {
			if err := tcp.Start(exCtx); err != nil {
				rt.log.Warn("tcp listener", "err", err)
			}
		}()
	}
}

// reload applies live-changeable settings (catch-all, body limit) to the
// running capture handler. Port and listener changes need a restart.
func (rt *appRuntime) reload() (string, error) {
	cfg := rt.app.Config()
	if rt.app.CaptureHandler != nil {
		rt.app.CaptureHandler.SetOptions(capture.Options{CatchAll: cfg.CatchAll, MaxBodyKB: cfg.MaxBodyKB})
	}
	return "Saved. Capture behaviour updated live; port/listener changes apply on restart.", nil
}

func printBanner(cfg config.Settings, dashURL string) {
	fmt.Printf(`
  HookTrap %s
  â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
  Dashboard : %s
  Capture   : http://%s:%d/   (bind %s)
  Data      : per-user app data

  For authorised testing, labs and CTFs only.
  Press Ctrl+C to stop.

`, version, dashURL, "<this-host>", cfg.HTTPPort, cfg.BindAddr)
}

