package api

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"
)

// replayClient is used to re-send captured requests. It follows no redirects so
// the operator sees the exact response, and has a short timeout.
var replayClient = &http.Client{
	Timeout:       20 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// handleReplay re-sends a captured request to a target URL chosen by the
// operator (Burp-Repeater style) and returns the target's response. This is an
// explicit, operator-initiated outbound request — use it only against systems
// you are authorised to test.
func (s *Server) handleReplay(w http.ResponseWriter, r *http.Request) {
	req, err := s.app.Store.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	var in struct {
		URL    string `json:"url"`
		Method string `json:"method"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	target := strings.TrimSpace(in.URL)
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		writeErr(w, http.StatusBadRequest, errBadTarget)
		return
	}
	method := in.Method
	if method == "" {
		method = req.Method
	}
	if method == "" {
		method = http.MethodGet
	}

	outReq, err := http.NewRequestWithContext(r.Context(), method, target, bytes.NewReader(req.Body))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	for _, h := range req.Headers {
		switch strings.ToLower(h.Name) {
		case "host", "content-length", "connection":
			continue
		}
		outReq.Header.Add(h.Name, h.Value)
	}

	start := time.Now()
	resp, err := replayClient.Do(outReq)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))

	headers := make([]map[string]string, 0, len(resp.Header))
	for name, vals := range resp.Header {
		for _, v := range vals {
			headers = append(headers, map[string]string{"name": name, "value": v})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"status":   resp.StatusCode,
		"ms":       time.Since(start).Milliseconds(),
		"headers":  headers,
		"body":     string(body),
		"bodySize": len(body),
	})
}

type badTargetError struct{}

func (badTargetError) Error() string { return "target must be an absolute http:// or https:// URL" }

var errBadTarget = badTargetError{}
