package capture

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ToaderMatei/hooktrap/internal/model"
	"github.com/ToaderMatei/hooktrap/internal/store"
)

type recorder struct{ last model.Request }

func (r *recorder) Record(req model.Request) (model.Request, error) { r.last = req; return req, nil }

type noFiles struct{}

func (noFiles) Open(string) ([]byte, string, bool) { return nil, "", false }

func testHandler(t *testing.T) (*Handler, *store.Store, *recorder) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "d"), 100)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rec := &recorder{}
	h := NewHandler(st, rec, noFiles{}, Options{CatchAll: true, MaxBodyKB: 64}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return h, st, rec
}

func TestCaptureRecordsRequest(t *testing.T) {
	h, _, rec := testHandler(t)
	req := httptest.NewRequest("POST", "/default/submit?a=1", strings.NewReader("hello=world"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if rec.last.Method != "POST" || rec.last.Path != "/submit" {
		t.Fatalf("captured %s %s", rec.last.Method, rec.last.Path)
	}
	if string(rec.last.Body) != "hello=world" {
		t.Fatalf("body = %q", rec.last.Body)
	}
	if w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Fatalf("response %d %q", w.Code, w.Body.String())
	}
}

func TestRedirectResponseKeepsRawLocation(t *testing.T) {
	h, st, _ := testHandler(t)
	st.UpdateBin(store.DefaultBinID, "", model.Response{Mode: model.ModeRedirect, Status: 302, RedirectURL: "gopher://127.0.0.1:6379/_test"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/default/go", nil))
	if w.Code != http.StatusFound {
		t.Fatalf("status %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "gopher://127.0.0.1:6379/_test" {
		t.Fatalf("location %q", loc)
	}
}

func TestEchoMode(t *testing.T) {
	h, st, _ := testHandler(t)
	st.UpdateBin(store.DefaultBinID, "", model.Response{Mode: model.ModeEcho, Status: 200})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/default/x", nil))
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type %q", ct)
	}
	if !strings.Contains(w.Body.String(), `"method": "GET"`) {
		t.Fatalf("echo body missing method: %s", w.Body.String())
	}
}

func TestTemplateExpansion(t *testing.T) {
	h, st, _ := testHandler(t)
	st.UpdateBin(store.DefaultBinID, "", model.Response{Mode: model.ModeStatic, Status: 200, Body: "ip={{ip}} token={{q:token}}"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/default/x?token=abc", nil)
	r.RemoteAddr = "203.0.113.7:9999"
	h.ServeHTTP(w, r)
	if got := w.Body.String(); got != "ip=203.0.113.7 token=abc" {
		t.Fatalf("expanded %q", got)
	}
}

func TestCatchAllDisabled404(t *testing.T) {
	h, _, _ := testHandler(t)
	h.SetOptions(Options{CatchAll: false, MaxBodyKB: 64})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/unknown-path", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status %d want 404", w.Code)
	}
}

func TestBodyTruncation(t *testing.T) {
	h, _, rec := testHandler(t)
	h.SetOptions(Options{CatchAll: true, MaxBodyKB: 1})
	big := strings.Repeat("A", 4096)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/default/x", strings.NewReader(big)))
	if !rec.last.Truncated || len(rec.last.Body) != 1024 {
		t.Fatalf("truncation failed: truncated=%v len=%d", rec.last.Truncated, len(rec.last.Body))
	}
}
