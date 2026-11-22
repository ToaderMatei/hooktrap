// Package api serves the local dashboard: a REST API, a live SSE event stream
// and the embedded web UI. It is always bound to loopback.
package api

import (
	"context"
	"io/fs"
	"net/http"
	"time"

	"github.com/ToaderMatei/hooktrap/internal/app"
)

// ReloadFunc re-applies settings to the running listeners. It returns a
// human-readable status (or an error) after attempting to apply cfg.
type ReloadFunc func() (string, error)

// Server is the admin/dashboard HTTP server.
type Server struct {
	app    *app.App
	static fs.FS
	reload ReloadFunc
	mux    *http.ServeMux
	srv    *http.Server
}

// NewServer builds the admin server. staticFS serves the web UI at /.
func NewServer(a *app.App, staticFS fs.FS, reload ReloadFunc) *Server {
	s := &Server{app: a, static: staticFS, reload: reload, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Handler exposes the router (useful for tests).
func (s *Server) Handler() http.Handler { return s.mux }

// Start binds the admin server to addr (expected loopback) until ctx is done.
func (s *Server) Start(ctx context.Context, addr string) error {
	s.srv = &http.Server{Addr: addr, Handler: s.mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(shutCtx)
	}()
	return s.srv.ListenAndServe()
}

func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("GET /api/state", s.handleState)
	m.HandleFunc("GET /api/events", s.handleEvents)

	m.HandleFunc("GET /api/bins", s.handleListBins)
	m.HandleFunc("POST /api/bins", s.handleCreateBin)
	m.HandleFunc("GET /api/bins/{id}", s.handleGetBin)
	m.HandleFunc("PUT /api/bins/{id}", s.handleUpdateBin)
	m.HandleFunc("DELETE /api/bins/{id}", s.handleDeleteBin)
	m.HandleFunc("POST /api/bins/{id}/clear", s.handleClearBin)
	m.HandleFunc("GET /api/bins/{id}/requests", s.handleListRequests)
	m.HandleFunc("GET /api/bins/{id}/export", s.handleExportBin)

	m.HandleFunc("GET /api/requests/{id}", s.handleGetRequest)
	m.HandleFunc("PATCH /api/requests/{id}", s.handleAnnotateRequest)
	m.HandleFunc("DELETE /api/requests/{id}", s.handleDeleteRequest)
	m.HandleFunc("GET /api/requests/{id}/export", s.handleExportRequest)
	m.HandleFunc("POST /api/requests/{id}/replay", s.handleReplay)

	m.HandleFunc("GET /api/files", s.handleListFiles)
	m.HandleFunc("POST /api/files", s.handleUploadFile)
	m.HandleFunc("DELETE /api/files/{name}", s.handleDeleteFile)

	m.HandleFunc("GET /api/payloads", s.handlePayloads)

	m.HandleFunc("POST /api/config", s.handleSaveConfig)
	m.HandleFunc("POST /api/tunnel/start", s.handleTunnelStart)
	m.HandleFunc("POST /api/tunnel/stop", s.handleTunnelStop)

	m.Handle("/", s.staticHandler())
}

func (s *Server) staticHandler() http.Handler {
	fileServer := http.FileServer(http.FS(s.static))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		// Dashboard is local-only; relax nothing to the network.
		fileServer.ServeHTTP(w, r)
	})
}
