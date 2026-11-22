// Package config loads, validates and persists HookTrap settings.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Settings is the persisted user configuration.
type Settings struct {
	BindAddr     string `json:"bindAddr"`     // interface for capture listeners, e.g. 0.0.0.0
	HTTPPort     int    `json:"httpPort"`     // capture HTTP listener
	HTTPSEnabled bool   `json:"httpsEnabled"` // self-signed HTTPS capture listener
	HTTPSPort    int    `json:"httpsPort"`
	AdminPort    int    `json:"adminPort"` // dashboard, always bound to 127.0.0.1
	PublicURL    string `json:"publicUrl"` // e.g. https://abc.trycloudflare.com (used by payload generator)
	CatchAll     bool   `json:"catchAll"`  // log unknown paths into the default bin
	MaxPerBin    int    `json:"maxPerBin"`
	MaxBodyKB    int    `json:"maxBodyKb"`

	DNSEnabled  bool   `json:"dnsEnabled"`
	DNSPort     int    `json:"dnsPort"`
	DNSAnswerIP string `json:"dnsAnswerIp"`

	TCPEnabled bool   `json:"tcpEnabled"`
	TCPPort    int    `json:"tcpPort"`
	TCPBanner  string `json:"tcpBanner"`

	OpenBrowser bool `json:"openBrowser"`
}

// Defaults returns the out-of-the-box settings.
func Defaults() Settings {
	return Settings{
		BindAddr:     "0.0.0.0",
		HTTPPort:     8080,
		HTTPSEnabled: false,
		HTTPSPort:    8443,
		AdminPort:    8099,
		CatchAll:     true,
		MaxPerBin:    2000,
		MaxBodyKB:    10240,
		DNSEnabled:   false,
		DNSPort:      5353,
		DNSAnswerIP:  "127.0.0.1",
		TCPEnabled:   false,
		TCPPort:      9001,
		TCPBanner:    "",
		OpenBrowser:  true,
	}
}

// Validate checks settings and returns a descriptive error for the first problem.
func (s Settings) Validate() error {
	if net.ParseIP(s.BindAddr) == nil {
		return fmt.Errorf("bindAddr %q is not an IP address", s.BindAddr)
	}
	ports := map[string]int{"httpPort": s.HTTPPort, "adminPort": s.AdminPort}
	if s.HTTPSEnabled {
		ports["httpsPort"] = s.HTTPSPort
	}
	if s.DNSEnabled {
		ports["dnsPort"] = s.DNSPort
	}
	if s.TCPEnabled {
		ports["tcpPort"] = s.TCPPort
	}
	seen := map[int]string{}
	for name, p := range ports {
		if p < 1 || p > 65535 {
			return fmt.Errorf("%s %d is out of range 1-65535", name, p)
		}
		// DNS is UDP, so it may share a number with a TCP port.
		if other, dup := seen[p]; dup && name != "dnsPort" && other != "dnsPort" {
			return fmt.Errorf("%s and %s both use port %d", name, other, p)
		}
		seen[p] = name
	}
	if s.MaxPerBin < 10 || s.MaxPerBin > 100000 {
		return errors.New("maxPerBin must be between 10 and 100000")
	}
	if s.MaxBodyKB < 1 || s.MaxBodyKB > 102400 {
		return errors.New("maxBodyKb must be between 1 and 102400")
	}
	if s.DNSEnabled && net.ParseIP(s.DNSAnswerIP).To4() == nil {
		return fmt.Errorf("dnsAnswerIp %q must be an IPv4 address", s.DNSAnswerIP)
	}
	if s.PublicURL != "" {
		u, err := url.Parse(s.PublicURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("publicUrl %q must be an absolute http(s) URL", s.PublicURL)
		}
	}
	if len(s.TCPBanner) > 4096 {
		return errors.New("tcpBanner is limited to 4096 bytes")
	}
	return nil
}

// Normalize trims user-provided strings.
func (s Settings) Normalize() Settings {
	out := s
	out.BindAddr = strings.TrimSpace(out.BindAddr)
	out.PublicURL = strings.TrimRight(strings.TrimSpace(out.PublicURL), "/")
	out.DNSAnswerIP = strings.TrimSpace(out.DNSAnswerIP)
	return out
}

// DataDir returns the per-user data directory (%APPDATA%\HookTrap on Windows).
func DataDir() (string, error) {
	if d := os.Getenv("HOOKTRAP_DATA"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate config dir: %w", err)
	}
	return filepath.Join(base, "HookTrap"), nil
}

// Load reads settings.json from dir, falling back to defaults for a missing file.
func Load(dir string) (Settings, error) {
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return Defaults(), nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("read settings: %w", err)
	}
	s := Defaults()
	if err := json.Unmarshal(data, &s); err != nil {
		return Settings{}, fmt.Errorf("parse settings.json: %w", err)
	}
	s = s.Normalize()
	if err := s.Validate(); err != nil {
		return Defaults(), fmt.Errorf("invalid settings.json, using defaults: %w", err)
	}
	return s, nil
}

// Save writes settings.json atomically.
func Save(dir string, s Settings) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	p := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(p+".tmp", data, 0o600); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	return os.Rename(p+".tmp", p)
}
