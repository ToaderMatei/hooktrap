// Package capture implements the public-facing listener that records every
// incoming HTTP request and answers with the bin's configured response.
package capture

import (
	"bytes"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ToaderMatei/hooktrap/internal/model"
	"github.com/ToaderMatei/hooktrap/internal/store"
)

// Recorder persists a captured request and notifies listeners.
type Recorder interface {
	Record(r model.Request) (model.Request, error)
}

// FileServer serves operator-hosted payload files (/f/{name}).
type FileServer interface {
	Open(name string) (data []byte, contentType string, ok bool)
}

// Options tune the handler at runtime.
type Options struct {
	CatchAll  bool
	MaxBodyKB int
}

// Handler is the capture http.Handler.
type Handler struct {
	store    *store.Store
	recorder Recorder
	files    FileServer
	opts     atomic.Pointer[Options]
	log      *slog.Logger
}

// NewHandler builds a capture handler.
func NewHandler(st *store.Store, rec Recorder, files FileServer, opts Options, log *slog.Logger) *Handler {
	h := &Handler{store: st, recorder: rec, files: files, log: log}
	h.SetOptions(opts)
	return h
}

// SetOptions swaps runtime options atomically.
func (h *Handler) SetOptions(o Options) {
	cp := o
	if cp.MaxBodyKB <= 0 {
		cp.MaxBodyKB = 10240
	}
	h.opts.Store(&cp)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	opts := *h.opts.Load()
	body, truncated := readBody(r, int64(opts.MaxBodyKB)*1024)

	if name, ok := strings.CutPrefix(r.URL.Path, "/f/"); ok && name != "" {
		h.serveHostedFile(w, r, name, body, truncated)
		return
	}

	binID, rest := h.resolveBin(r)
	if binID == "" {
		if !opts.CatchAll || r.URL.Path == "/favicon.ico" {
			http.NotFound(w, r)
			return
		}
		binID = store.DefaultBinID
	}
	bin, _ := h.store.Bin(binID)
	req := buildRequest(r, binID, rest, body, truncated)
	stored, err := h.recorder.Record(req)
	if err != nil {
		h.log.Error("record request", "err", err)
	}
	writeResponse(w, r, bin.Response, stored)
}

// resolveBin maps a request to a bin by subdomain (<bin>.host) or first path segment.
func (h *Handler) resolveBin(r *http.Request) (binID, rest string) {
	host := r.Host
	if hh, _, err := net.SplitHostPort(host); err == nil {
		host = hh
	}
	if net.ParseIP(host) == nil {
		if label, _, found := strings.Cut(host, "."); found {
			label = strings.ToLower(label)
			if _, ok := h.store.Bin(label); ok && label != store.DefaultBinID {
				return label, r.URL.Path
			}
		}
	}
	trimmed := strings.TrimPrefix(r.URL.Path, "/")
	seg, after, _ := strings.Cut(trimmed, "/")
	seg = strings.ToLower(seg)
	if seg != "" {
		if _, ok := h.store.Bin(seg); ok {
			return seg, "/" + after
		}
	}
	return "", r.URL.Path
}

func (h *Handler) serveHostedFile(w http.ResponseWriter, r *http.Request, name string, body []byte, truncated bool) {
	data, ctype, ok := h.files.Open(name)
	req := buildRequest(r, store.DefaultBinID, r.URL.Path, body, truncated)
	if ok {
		req.Summary = "hosted file served: " + name
	} else {
		req.Summary = "hosted file missing: " + name
	}
	if _, err := h.recorder.Record(req); err != nil {
		h.log.Error("record file hit", "err", err)
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	setCORS(w, r)
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func readBody(r *http.Request, limit int64) ([]byte, bool) {
	if r.Body == nil {
		return nil, false
	}
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil && len(data) == 0 {
		return nil, false
	}
	if int64(len(data)) > limit {
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 64<<20))
		return data[:limit], true
	}
	return data, false
}

func buildRequest(r *http.Request, binID, path string, body []byte, truncated bool) model.Request {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = p
	}
	req := model.Request{
		BinID:      binID,
		Kind:       model.KindHTTP,
		Time:       time.Now(),
		RemoteAddr: clientIP(r),
		Method:     r.Method,
		Scheme:     scheme,
		Host:       r.Host,
		Path:       path,
		Query:      r.URL.RawQuery,
		Proto:      r.Proto,
		Headers:    collectHeaders(r),
		Body:       body,
		BodySize:   len(body),
		Truncated:  truncated,
	}
	req.Files = multipartFiles(r.Header.Get("Content-Type"), body)
	return req
}

func collectHeaders(r *http.Request) []model.Header {
	names := make([]string, 0, len(r.Header))
	for n := range r.Header {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]model.Header, 0, len(names)+1)
	out = append(out, model.Header{Name: "Host", Value: r.Host})
	for _, n := range names {
		for _, v := range r.Header[n] {
			out = append(out, model.Header{Name: n, Value: v})
		}
	}
	return out
}

// clientIP prefers proxy headers only when the socket peer is loopback (i.e. a local tunnel).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return host
	}
	for _, h := range []string{"CF-Connecting-IP", "True-Client-IP", "X-Real-IP"} {
		if v := strings.TrimSpace(r.Header.Get(h)); v != "" {
			return v
		}
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		return strings.TrimSpace(first)
	}
	return host
}

func multipartFiles(contentType string, body []byte) []model.FilePart {
	mt, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(mt, "multipart/") || params["boundary"] == "" {
		return nil
	}
	mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	var out []model.FilePart
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		if p.FileName() != "" {
			n, _ := io.Copy(io.Discard, p)
			out = append(out, model.FilePart{
				Field:       p.FormName(),
				Filename:    p.FileName(),
				ContentType: p.Header.Get("Content-Type"),
				Size:        int(n),
			})
		}
		_ = p.Close()
	}
	return out
}
