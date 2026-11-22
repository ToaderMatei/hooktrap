// Package model holds the data types shared across HookTrap.
package model

import "time"

// Kind identifies what captured an event.
const (
	KindHTTP = "http"
	KindDNS  = "dns"
	KindTCP  = "tcp"
)

// Response modes for a bin.
const (
	ModeStatic   = "static"
	ModeRedirect = "redirect"
	ModeEcho     = "echo"
)

// Header is an ordered HTTP header pair (order matters when inspecting requests).
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// FilePart describes an uploaded multipart file inside a captured request.
type FilePart struct {
	Field       string `json:"field"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
}

// Request is a single captured interaction (HTTP request, DNS query or TCP session).
type Request struct {
	ID         string     `json:"id"`
	BinID      string     `json:"binId"`
	Kind       string     `json:"kind"`
	Time       time.Time  `json:"time"`
	RemoteAddr string     `json:"remoteAddr"`
	Method     string     `json:"method,omitempty"`
	Scheme     string     `json:"scheme,omitempty"`
	Host       string     `json:"host,omitempty"`
	Path       string     `json:"path,omitempty"`
	Query      string     `json:"query,omitempty"`
	Proto      string     `json:"proto,omitempty"`
	Headers    []Header   `json:"headers,omitempty"`
	Body       []byte     `json:"body,omitempty"`
	BodySize   int        `json:"bodySize"`
	Truncated  bool       `json:"truncated,omitempty"`
	Files      []FilePart `json:"files,omitempty"`
	Summary    string     `json:"summary,omitempty"`
	Note       string     `json:"note,omitempty"`
	Starred    bool       `json:"starred,omitempty"`
}

// Response configures what a bin answers with.
type Response struct {
	Mode        string   `json:"mode"`
	Status      int      `json:"status"`
	ContentType string   `json:"contentType"`
	Headers     []Header `json:"headers"`
	Body        string   `json:"body"`
	RedirectURL string   `json:"redirectUrl"`
	DelayMs     int      `json:"delayMs"`
	CORS        bool     `json:"cors"`
}

// Bin is a named capture endpoint reachable at /{id}/... on the capture listener.
type Bin struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Created  time.Time `json:"created"`
	Response Response  `json:"response"`
}

// DefaultResponse is what a freshly created bin answers.
func DefaultResponse() Response {
	return Response{
		Mode:        ModeStatic,
		Status:      200,
		ContentType: "text/plain; charset=utf-8",
		Headers:     []Header{},
		Body:        "ok",
		CORS:        true,
	}
}

// Event is pushed to live dashboard subscribers.
type Event struct {
	Type    string   `json:"type"`
	Request *Request `json:"request,omitempty"`
	BinID   string   `json:"binId,omitempty"`
	ID      string   `json:"id,omitempty"`
	Message string   `json:"message,omitempty"`
}
