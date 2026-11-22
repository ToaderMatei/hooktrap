package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ToaderMatei/hooktrap/internal/tunnel"
)

func tunnelAvailable() bool { return tunnel.Available() }

// handleEvents streams dashboard events over Server-Sent Events.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, errNoStreaming)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")

	ch, cancel := s.app.Broker.Subscribe()
	defer cancel()

	_, _ = w.Write([]byte("event: ready\ndata: {}\n\n"))
	flusher.Flush()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if _, err := w.Write([]byte("event: " + ev.Type + "\ndata: ")); err != nil {
				return
			}
			_, _ = w.Write(data)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
		}
	}
}

type streamingError struct{ msg string }

func (e streamingError) Error() string { return e.msg }

var errNoStreaming = streamingError{"streaming unsupported by this connection"}
