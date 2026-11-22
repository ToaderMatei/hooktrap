package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ToaderMatei/hooktrap/internal/app"
	"github.com/ToaderMatei/hooktrap/internal/config"
	"github.com/ToaderMatei/hooktrap/internal/files"
	"github.com/ToaderMatei/hooktrap/internal/model"
	"github.com/ToaderMatei/hooktrap/internal/store"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "data"), 100)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	fs, err := files.New(filepath.Join(dir, "files"))
	if err != nil {
		t.Fatal(err)
	}
	a := app.New(st, fs, config.Defaults(), slog.New(slog.NewTextHandler(io.Discard, nil)), "test")
	static := fstest.MapFS{"index.html": {Data: []byte("<html>ok</html>")}}
	return NewServer(a, static, nil)
}

func do(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestStateAndStaticIndex(t *testing.T) {
	s := testServer(t)
	if w := do(t, s, "GET", "/api/state", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"version":"test"`) {
		t.Fatalf("state: %d %s", w.Code, w.Body.String())
	}
	if w := do(t, s, "GET", "/", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "ok") {
		t.Fatalf("index: %d", w.Code)
	}
}

func TestBinLifecycle(t *testing.T) {
	s := testServer(t)
	w := do(t, s, "POST", "/api/bins", `{"id":"x1","name":"X1"}`)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	// invalid id rejected
	if w := do(t, s, "POST", "/api/bins", `{"id":"BAD ID"}`); w.Code != 400 {
		t.Fatalf("expected 400 for bad id, got %d", w.Code)
	}
	if w := do(t, s, "DELETE", "/api/bins/x1", ""); w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}
}

func TestRequestsAndExport(t *testing.T) {
	s := testServer(t)
	s.app.Record(model.Request{BinID: store.DefaultBinID, Kind: model.KindHTTP, Method: "GET", Path: "/hi", Host: "h"})
	w := do(t, s, "GET", "/api/bins/default/requests", "")
	var reqs []model.Request
	if err := json.Unmarshal(w.Body.Bytes(), &reqs); err != nil || len(reqs) != 1 {
		t.Fatalf("list: %v %s", err, w.Body.String())
	}
	id := reqs[0].ID
	if w := do(t, s, "GET", "/api/requests/"+id+"/export?format=curl", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "curl -X GET") {
		t.Fatalf("curl export: %d %s", w.Code, w.Body.String())
	}
	// annotate
	if w := do(t, s, "PATCH", "/api/requests/"+id, `{"starred":true}`); w.Code != 200 {
		t.Fatalf("annotate: %d", w.Code)
	}
}

func TestPayloadsEndpoint(t *testing.T) {
	s := testServer(t)
	w := do(t, s, "GET", "/api/payloads?bin=default", "")
	var out struct {
		Groups []struct {
			Category string `json:"category"`
		} `json:"groups"`
		Base string `json:"base"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Groups) == 0 || !strings.Contains(out.Base, "/default") {
		t.Fatalf("payloads wrong: %+v", out)
	}
}

func TestFilesEndpoint(t *testing.T) {
	s := testServer(t)
	if w := do(t, s, "POST", "/api/files?name=oob.dtd", "content"); w.Code != 201 {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	w := do(t, s, "GET", "/api/files", "")
	if !strings.Contains(w.Body.String(), "oob.dtd") {
		t.Fatalf("list files: %s", w.Body.String())
	}
}
