// Package app wires the stores together and provides the shared Recorder used
// by every capture listener.
package app

import (
	"log/slog"

	"github.com/ToaderMatei/hooktrap/internal/capture"
	"github.com/ToaderMatei/hooktrap/internal/config"
	"github.com/ToaderMatei/hooktrap/internal/events"
	"github.com/ToaderMatei/hooktrap/internal/files"
	"github.com/ToaderMatei/hooktrap/internal/model"
	"github.com/ToaderMatei/hooktrap/internal/store"
	"github.com/ToaderMatei/hooktrap/internal/tunnel"
)

// App bundles the long-lived components of a running HookTrap instance.
type App struct {
	Store   *store.Store
	Files   *files.Store
	Broker  *events.Broker
	Tunnel  *tunnel.Manager
	Log     *slog.Logger
	Version string

	// CaptureHandler is set by the runtime so settings changes can be applied live.
	CaptureHandler *capture.Handler

	cfg config.Settings
}

// New builds an App around already-opened stores.
func New(st *store.Store, fs *files.Store, cfg config.Settings, log *slog.Logger, version string) *App {
	a := &App{
		Store:   st,
		Files:   fs,
		Broker:  events.NewBroker(),
		Log:     log,
		Version: version,
		cfg:     cfg,
	}
	a.Tunnel = tunnel.NewManager(func(url string) {
		a.Broker.Publish(model.Event{Type: "tunnel", Message: url})
	})
	return a
}

// Config returns a copy of the current settings.
func (a *App) Config() config.Settings { return a.cfg }

// SetConfig replaces the in-memory settings.
func (a *App) SetConfig(c config.Settings) { a.cfg = c }

// Record stores a captured request and fans a "request" event out to the dashboard.
// It satisfies capture.Recorder and listeners.Recorder.
func (a *App) Record(r model.Request) (model.Request, error) {
	stored, err := a.Store.Add(r)
	if err != nil {
		return stored, err
	}
	ev := stored
	a.Broker.Publish(model.Event{Type: "request", Request: &ev, BinID: ev.BinID})
	return stored, nil
}
