package listeners

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"

	"github.com/ToaderMatei/hooktrap/internal/model"
	"github.com/ToaderMatei/hooktrap/internal/store"
)

// TCPServer accepts raw TCP connections, optionally writes a banner and logs
// whatever bytes the client sends. It makes generic out-of-band TCP callbacks
// visible on the dashboard.
type TCPServer struct {
	addr   string
	banner string
	rec    Recorder
	log    *slog.Logger
}

// NewTCPServer builds a raw TCP capture listener.
func NewTCPServer(addr, banner string, rec Recorder, log *slog.Logger) *TCPServer {
	return &TCPServer{addr: addr, banner: banner, rec: rec, log: log}
}

// Start accepts connections until ctx is cancelled.
func (t *TCPServer) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", t.addr)
	if err != nil {
		return fmt.Errorf("listen tcp: %w", err)
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		go t.handle(conn)
	}
}

func (t *TCPServer) handle(conn net.Conn) {
	defer conn.Close()
	remote := conn.RemoteAddr().String()
	host, _, _ := net.SplitHostPort(remote)
	if t.banner != "" {
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_, _ = conn.Write([]byte(t.banner))
	}
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	data, _ := io.ReadAll(io.LimitReader(bufio.NewReader(conn), 64<<10))

	req := model.Request{
		BinID:      store.DefaultBinID,
		Kind:       model.KindTCP,
		Time:       time.Now(),
		RemoteAddr: host,
		Summary:    fmt.Sprintf("TCP connection from %s (%d bytes)", host, len(data)),
		Body:       data,
		BodySize:   len(data),
	}
	if _, err := t.rec.Record(req); err != nil {
		t.log.Error("record tcp", "err", err)
	}
}
