package api

import (
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// lanIPs returns the host's non-loopback IPv4 addresses (private and public),
// so the operator can pick a LAN- or internet-reachable capture address instead
// of 127.0.0.1. Enumerating interfaces is local and makes no network request.
func lanIPs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			ip4 := ip.To4()
			if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
				continue
			}
			out = append(out, ip4.String())
		}
	}
	return out
}

// publicIPCache memoises the detected public IP for the session.
type publicIPCache struct {
	mu sync.Mutex
	ip string
}

var pubIP publicIPCache

// publicIPServices are plain-text IPv4 echo endpoints tried in order. They
// receive only the request's own source IP — no user or project data.
var publicIPServices = []string{
	"https://api.ipify.org",
	"https://ifconfig.me/ip",
	"https://icanhazip.com",
}

// handlePublicIP detects the host's public IPv4 via an outbound echo request.
// It is explicit (the operator clicks "Public IP"), never automatic, because it
// reaches out to the internet. The result is cached for the session.
func (s *Server) handlePublicIP(w http.ResponseWriter, r *http.Request) {
	pubIP.mu.Lock()
	cached := pubIP.ip
	pubIP.mu.Unlock()
	if cached != "" && r.URL.Query().Get("refresh") != "1" {
		writeJSON(w, http.StatusOK, map[string]string{"ip": cached})
		return
	}

	client := &http.Client{Timeout: 6 * time.Second}
	ctx := r.Context()
	for _, svc := range publicIPServices {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, svc, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "HookTrap")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
		_ = resp.Body.Close()
		ip := strings.TrimSpace(string(body))
		if net.ParseIP(ip) != nil {
			pubIP.mu.Lock()
			pubIP.ip = ip
			pubIP.mu.Unlock()
			writeJSON(w, http.StatusOK, map[string]string{"ip": ip})
			return
		}
	}
	writeErr(w, http.StatusBadGateway, errPublicIP)
}

type publicIPError struct{}

func (publicIPError) Error() string {
	return "could not determine public IP (no echo service reachable)"
}

var errPublicIP = publicIPError{}
