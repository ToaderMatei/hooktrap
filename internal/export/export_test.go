package export

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ToaderMatei/hooktrap/internal/model"
)

func sample() model.Request {
	return model.Request{
		ID: "abc", BinID: "default", Kind: model.KindHTTP, Method: "POST",
		Scheme: "http", Host: "example.test", Path: "/submit", Query: "a=1",
		Proto: "HTTP/1.1", Headers: []model.Header{{Name: "Content-Type", Value: "application/json"}},
		Body: []byte(`{"k":"v"}`), BodySize: 9,
	}
}

func TestCurl(t *testing.T) {
	c := Curl(sample())
	if !strings.Contains(c, "curl -X POST") || !strings.Contains(c, "http://example.test/submit?a=1") {
		t.Fatalf("curl missing parts: %s", c)
	}
	if !strings.Contains(c, "--data-raw") {
		t.Fatalf("curl missing body: %s", c)
	}
}

func TestRaw(t *testing.T) {
	raw := Raw(sample())
	if !strings.HasPrefix(raw, "POST /submit?a=1 HTTP/1.1\r\n") {
		t.Fatalf("raw request line wrong: %q", raw)
	}
	if !strings.Contains(raw, "Content-Type: application/json") {
		t.Fatalf("raw missing header: %s", raw)
	}
}

func TestHARValid(t *testing.T) {
	data, err := HAR([]model.Request{sample()})
	if err != nil {
		t.Fatalf("har: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("har not valid json: %v", err)
	}
	log, ok := parsed["log"].(map[string]any)
	if !ok {
		t.Fatal("har missing log")
	}
	if entries, ok := log["entries"].([]any); !ok || len(entries) != 1 {
		t.Fatalf("har entries wrong: %v", log["entries"])
	}
}

func TestJSON(t *testing.T) {
	data, err := JSON([]model.Request{sample()})
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	if !strings.Contains(string(data), `"method": "POST"`) {
		t.Fatalf("json missing method: %s", data)
	}
}
