// Package tunnel optionally exposes the local capture listener through a
// Cloudflare quick tunnel so remote clients (another VM, a CTF target box)
// can reach it without port-forwarding. It shells out to `cloudflared` if it
// is on PATH; it never bundles or downloads it.
package tunnel

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"sync"
)

var urlRe = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

// State describes the current tunnel.
type State struct {
	Running bool   `json:"running"`
	URL     string `json:"url"`
	Err     string `json:"err,omitempty"`
}

// Manager controls a single cloudflared child process.
type Manager struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	state  State
	onURL  func(string)
}

// NewManager builds a tunnel manager. onURL (optional) fires when a URL appears.
func NewManager(onURL func(string)) *Manager {
	return &Manager{onURL: onURL}
}

// Available reports whether cloudflared is on PATH.
func Available() bool {
	_, err := exec.LookPath("cloudflared")
	return err == nil
}

// State returns a snapshot.
func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// Start launches a quick tunnel to http://127.0.0.1:port. It is a no-op if one runs.
func (m *Manager) Start(port int) error {
	m.mu.Lock()
	if m.state.Running {
		m.mu.Unlock()
		return errors.New("tunnel already running")
	}
	path, err := exec.LookPath("cloudflared")
	if err != nil {
		m.mu.Unlock()
		return errors.New("cloudflared is not installed or not on PATH")
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.state = State{Running: true}
	m.mu.Unlock()

	target := fmt.Sprintf("http://127.0.0.1:%d", port)
	cmd := exec.CommandContext(ctx, path, "tunnel", "--no-autoupdate", "--url", target)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		m.fail(cancel, err)
		return err
	}
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		m.fail(cancel, err)
		return fmt.Errorf("start cloudflared: %w", err)
	}
	go m.scan(stderr)
	go m.scan(stdout)
	go func() {
		err := cmd.Wait()
		m.mu.Lock()
		m.state.Running = false
		if err != nil && ctx.Err() == nil {
			m.state.Err = err.Error()
		}
		m.mu.Unlock()
	}()
	return nil
}

func (m *Manager) scan(r io.Reader) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if u := urlRe.FindString(sc.Text()); u != "" {
			m.mu.Lock()
			already := m.state.URL != ""
			m.state.URL = u
			cb := m.onURL
			m.mu.Unlock()
			if !already && cb != nil {
				cb(u)
			}
		}
	}
}

func (m *Manager) fail(cancel context.CancelFunc, err error) {
	cancel()
	m.mu.Lock()
	m.state = State{Running: false, Err: err.Error()}
	m.mu.Unlock()
}

// Stop terminates the tunnel.
func (m *Manager) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	m.cancel = nil
	m.state = State{Running: false}
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
