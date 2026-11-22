package capture

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ToaderMatei/hooktrap/internal/model"
)

// MaxDelay caps the configurable response delay.
const MaxDelay = 30 * time.Second

func writeResponse(w http.ResponseWriter, r *http.Request, resp model.Response, req model.Request) {
	if resp.DelayMs > 0 {
		d := min(time.Duration(resp.DelayMs)*time.Millisecond, MaxDelay)
		select {
		case <-time.After(d):
		case <-r.Context().Done():
			return
		}
	}
	if resp.CORS {
		setCORS(w, r)
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	w.Header().Set("X-HookTrap-Request-Id", req.ID)

	switch resp.Mode {
	case model.ModeRedirect:
		writeRedirect(w, resp, req)
	case model.ModeEcho:
		writeEcho(w, req)
	default:
		writeStatic(w, r, resp, req)
	}
}

func writeStatic(w http.ResponseWriter, r *http.Request, resp model.Response, req model.Request) {
	for _, h := range resp.Headers {
		if h.Name == "" {
			continue
		}
		w.Header().Add(h.Name, expand(h.Value, req))
	}
	if resp.ContentType != "" && w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", resp.ContentType)
	}
	status := validStatus(resp.Status, http.StatusOK)
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(expand(resp.Body, req)))
	}
}

func writeRedirect(w http.ResponseWriter, resp model.Response, req model.Request) {
	status := resp.Status
	switch status {
	case 301, 302, 303, 307, 308:
	default:
		status = http.StatusFound
	}
	for _, h := range resp.Headers {
		if h.Name != "" {
			w.Header().Add(h.Name, expand(h.Value, req))
		}
	}
	w.Header().Set("Location", expand(resp.RedirectURL, req))
	w.WriteHeader(status)
}

func writeEcho(w http.ResponseWriter, req model.Request) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(struct {
		model.Request
		BodyText string `json:"bodyText"`
	}{req, string(req.Body)})
}

func setCORS(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = "*"
	} else {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Add("Vary", "Origin")
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD")
	if reqH := r.Header.Get("Access-Control-Request-Headers"); reqH != "" {
		w.Header().Set("Access-Control-Allow-Headers", reqH)
	} else {
		w.Header().Set("Access-Control-Allow-Headers", "*")
	}
	w.Header().Set("Access-Control-Expose-Headers", "*")
}

func validStatus(code, fallback int) int {
	if code < 100 || code > 999 {
		return fallback
	}
	return code
}

// expand substitutes {{placeholders}} with values from the captured request,
// so a bin can reflect data back to the client (handy for testing templating).
func expand(s string, req model.Request) string {
	if !strings.Contains(s, "{{") {
		return s
	}
	q, _ := url.ParseQuery(req.Query)
	repl := []string{
		"{{request_id}}", req.ID,
		"{{ip}}", req.RemoteAddr,
		"{{method}}", req.Method,
		"{{path}}", req.Path,
		"{{host}}", req.Host,
		"{{query}}", req.Query,
		"{{body}}", string(req.Body),
		"{{time}}", req.Time.UTC().Format(time.RFC3339),
		"{{unix}}", strconv.FormatInt(req.Time.Unix(), 10),
	}
	out := strings.NewReplacer(repl...).Replace(s)
	for strings.Contains(out, "{{q:") {
		start := strings.Index(out, "{{q:")
		end := strings.Index(out[start:], "}}")
		if end < 0 {
			break
		}
		name := out[start+4 : start+end]
		out = out[:start] + q.Get(name) + out[start+end+2:]
	}
	return out
}
