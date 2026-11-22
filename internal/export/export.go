// Package export renders captured requests into shareable formats.
package export

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ToaderMatei/hooktrap/internal/model"
)

// JSON returns pretty-printed JSON for a set of requests.
func JSON(reqs []model.Request) ([]byte, error) {
	data, err := json.MarshalIndent(reqs, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode json: %w", err)
	}
	return data, nil
}

// HAR renders requests as an HTTP Archive 1.2 log (importable into browsers / Burp).
func HAR(reqs []model.Request) ([]byte, error) {
	type nv struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	type entry map[string]any
	entries := make([]entry, 0, len(reqs))
	for _, r := range reqs {
		if r.Kind != model.KindHTTP {
			continue
		}
		headers := make([]nv, 0, len(r.Headers))
		for _, h := range r.Headers {
			headers = append(headers, nv{h.Name, h.Value})
		}
		url := fmt.Sprintf("%s://%s%s", orDefault(r.Scheme, "http"), r.Host, r.Path)
		if r.Query != "" {
			url += "?" + r.Query
		}
		postData := map[string]any{}
		if len(r.Body) > 0 {
			postData = map[string]any{"mimeType": contentType(r), "text": string(r.Body)}
		}
		entries = append(entries, entry{
			"startedDateTime": r.Time.Format("2006-01-02T15:04:05.000Z07:00"),
			"time":            0,
			"request": map[string]any{
				"method":      r.Method,
				"url":         url,
				"httpVersion": orDefault(r.Proto, "HTTP/1.1"),
				"headers":     headers,
				"queryString": []nv{},
				"cookies":     []nv{},
				"headersSize": -1,
				"bodySize":    r.BodySize,
				"postData":    postData,
			},
			"response": map[string]any{
				"status": 200, "statusText": "OK", "httpVersion": "HTTP/1.1",
				"headers": []nv{}, "cookies": []nv{},
				"content":     map[string]any{"size": 0, "mimeType": "text/plain"},
				"redirectURL": "", "headersSize": -1, "bodySize": 0,
			},
			"cache":           map[string]any{},
			"timings":         map[string]any{"send": 0, "wait": 0, "receive": 0},
			"serverIPAddress": r.RemoteAddr,
		})
	}
	har := map[string]any{
		"log": map[string]any{
			"version": "1.2",
			"creator": map[string]any{"name": "HookTrap", "version": "1.0"},
			"entries": entries,
		},
	}
	data, err := json.MarshalIndent(har, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode har: %w", err)
	}
	return data, nil
}

// Curl renders a single request as a reproducible curl command.
func Curl(r model.Request) string {
	var b strings.Builder
	scheme := orDefault(r.Scheme, "http")
	url := fmt.Sprintf("%s://%s%s", scheme, r.Host, r.Path)
	if r.Query != "" {
		url += "?" + r.Query
	}
	fmt.Fprintf(&b, "curl -X %s '%s'", orDefault(r.Method, "GET"), url)
	for _, h := range r.Headers {
		if strings.EqualFold(h.Name, "Content-Length") {
			continue
		}
		fmt.Fprintf(&b, " \\\n  -H '%s: %s'", h.Name, strings.ReplaceAll(h.Value, "'", "'\\''"))
	}
	if len(r.Body) > 0 {
		if isPrintable(r.Body) {
			fmt.Fprintf(&b, " \\\n  --data-raw '%s'", strings.ReplaceAll(string(r.Body), "'", "'\\''"))
		} else {
			fmt.Fprintf(&b, " \\\n  --data-binary @<(base64 -d <<< '%s')", base64.StdEncoding.EncodeToString(r.Body))
		}
	}
	return b.String()
}

// Raw renders the request the way it arrived on the wire.
func Raw(r model.Request) string {
	var b strings.Builder
	target := r.Path
	if r.Query != "" {
		target += "?" + r.Query
	}
	fmt.Fprintf(&b, "%s %s %s\r\n", orDefault(r.Method, "GET"), orDefault(target, "/"), orDefault(r.Proto, "HTTP/1.1"))
	for _, h := range r.Headers {
		fmt.Fprintf(&b, "%s: %s\r\n", h.Name, h.Value)
	}
	b.WriteString("\r\n")
	if len(r.Body) > 0 {
		if isPrintable(r.Body) {
			b.Write(r.Body)
		} else {
			fmt.Fprintf(&b, "<%d bytes of binary body omitted>", len(r.Body))
		}
	}
	return b.String()
}

func contentType(r model.Request) string {
	for _, h := range r.Headers {
		if strings.EqualFold(h.Name, "Content-Type") {
			return h.Value
		}
	}
	return "application/octet-stream"
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func isPrintable(b []byte) bool {
	if len(b) > 1<<20 {
		return false
	}
	for _, c := range b {
		if c == 0 {
			return false
		}
	}
	return true
}
