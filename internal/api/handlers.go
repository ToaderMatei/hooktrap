package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ToaderMatei/hooktrap/internal/config"
	"github.com/ToaderMatei/hooktrap/internal/export"
	"github.com/ToaderMatei/hooktrap/internal/files"
	"github.com/ToaderMatei/hooktrap/internal/model"
	"github.com/ToaderMatei/hooktrap/internal/store"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 8<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

// ---------- state ----------

func (s *Server) handleState(w http.ResponseWriter, _ *http.Request) {
	cfg := s.app.Config()
	bins := s.app.Store.Bins()
	type binInfo struct {
		model.Bin
		Count int `json:"count"`
	}
	infos := make([]binInfo, 0, len(bins))
	for _, b := range bins {
		infos = append(infos, binInfo{Bin: b, Count: s.app.Store.CountFor(b.ID)})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":         s.app.Version,
		"config":          cfg,
		"bins":            infos,
		"tunnel":          s.app.Tunnel.State(),
		"tunnelAvailable": tunnelAvailable(),
		"subscribers":     s.app.Broker.Count(),
	})
}

// ---------- bins ----------

func (s *Server) handleListBins(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.app.Store.Bins())
}

func (s *Server) handleCreateBin(w http.ResponseWriter, r *http.Request) {
	var in struct{ ID, Name string }
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	bin, err := s.app.Store.CreateBin(in.ID, in.Name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, bin)
}

func (s *Server) handleGetBin(w http.ResponseWriter, r *http.Request) {
	bin, ok := s.app.Store.Bin(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, store.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, bin)
}

func (s *Server) handleUpdateBin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string         `json:"name"`
		Response model.Response `json:"response"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	bin, err := s.app.Store.UpdateBin(r.PathValue("id"), in.Name, in.Response)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeErr(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, bin)
}

func (s *Server) handleDeleteBin(w http.ResponseWriter, r *http.Request) {
	if err := s.app.Store.DeleteBin(r.PathValue("id")); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeErr(w, status, err)
		return
	}
	s.app.Broker.Publish(model.Event{Type: "bins"})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleClearBin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.app.Store.Clear(id); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeErr(w, status, err)
		return
	}
	s.app.Broker.Publish(model.Event{Type: "cleared", BinID: id})
	w.WriteHeader(http.StatusNoContent)
}

// ---------- requests ----------

func (s *Server) handleListRequests(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.app.Store.Bin(id); !ok {
		writeErr(w, http.StatusNotFound, store.ErrNotFound)
		return
	}
	limit := 200
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 {
		limit = min(l, 2000)
	}
	reqs := s.app.Store.List(id, r.URL.Query().Get("q"), limit)
	writeJSON(w, http.StatusOK, reqs)
}

func (s *Server) handleGetRequest(w http.ResponseWriter, r *http.Request) {
	req, err := s.app.Store.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) handleAnnotateRequest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Note    *string `json:"note"`
		Starred *bool   `json:"starred"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	req, err := s.app.Store.Annotate(r.PathValue("id"), in.Note, in.Starred)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) handleDeleteRequest(w http.ResponseWriter, r *http.Request) {
	if err := s.app.Store.Delete(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleExportRequest(w http.ResponseWriter, r *http.Request) {
	req, err := s.app.Store.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	switch r.URL.Query().Get("format") {
	case "curl":
		writeText(w, export.Curl(req))
	case "raw":
		writeText(w, export.Raw(req))
	case "har":
		data, _ := export.HAR([]model.Request{req})
		writeDownload(w, "application/json", "request.har", data)
	default:
		data, _ := export.JSON([]model.Request{req})
		writeDownload(w, "application/json", "request.json", data)
	}
}

func (s *Server) handleExportBin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.app.Store.Bin(id); !ok {
		writeErr(w, http.StatusNotFound, store.ErrNotFound)
		return
	}
	reqs := s.app.Store.List(id, r.URL.Query().Get("q"), 0)
	// List returns newest-first; export oldest-first for readability.
	for i, j := 0, len(reqs)-1; i < j; i, j = i+1, j-1 {
		reqs[i], reqs[j] = reqs[j], reqs[i]
	}
	if r.URL.Query().Get("format") == "har" {
		data, _ := export.HAR(reqs)
		writeDownload(w, "application/json", id+".har", data)
		return
	}
	data, _ := export.JSON(reqs)
	writeDownload(w, "application/json", id+".json", data)
}

func writeText(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, s)
}

func writeDownload(w http.ResponseWriter, ctype, filename string, data []byte) {
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	_, _ = w.Write(data)
}

// ---------- files ----------

func (s *Server) handleListFiles(w http.ResponseWriter, _ *http.Request) {
	list, err := s.app.Files.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	ctype := r.Header.Get("Content-Type")
	var name string
	var data []byte
	if strings.HasPrefix(ctype, "multipart/form-data") {
		if err := r.ParseMultipartForm(files.MaxFileSize); err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("parse upload: %w", err))
			return
		}
		f, hdr, err := r.FormFile("file")
		if err != nil {
			writeErr(w, http.StatusBadRequest, errors.New("missing 'file' field"))
			return
		}
		defer f.Close()
		name = hdr.Filename
		if n := r.FormValue("name"); n != "" {
			name = n
		}
		data, _ = io.ReadAll(io.LimitReader(f, files.MaxFileSize+1))
	} else {
		name = r.URL.Query().Get("name")
		data, _ = io.ReadAll(io.LimitReader(r.Body, files.MaxFileSize+1))
	}
	info, err := s.app.Files.Put(name, data)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, info)
}

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	if err := s.app.Files.Delete(r.PathValue("name")); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- config & tunnel ----------

func (s *Server) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	in := s.app.Config()
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	in = in.Normalize()
	if err := in.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	dir, err := config.DataDir()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if err := config.Save(dir, in); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.app.SetConfig(in)
	msg := "Saved. Some changes (ports, extra listeners) apply after a restart."
	if s.reload != nil {
		if status, err := s.reload(); err != nil {
			msg = "Saved, but applying live failed: " + err.Error()
		} else if status != "" {
			msg = status
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": in, "message": msg})
}

func (s *Server) handleTunnelStart(w http.ResponseWriter, _ *http.Request) {
	if err := s.app.Tunnel.Start(s.app.Config().HTTPPort); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, s.app.Tunnel.State())
}

func (s *Server) handleTunnelStop(w http.ResponseWriter, _ *http.Request) {
	s.app.Tunnel.Stop()
	writeJSON(w, http.StatusOK, s.app.Tunnel.State())
}
