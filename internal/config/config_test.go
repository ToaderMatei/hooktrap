package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultsValidate(t *testing.T) {
	if err := Defaults().Validate(); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
}

func TestValidateRejectsPortClash(t *testing.T) {
	c := Defaults()
	c.AdminPort = c.HTTPPort
	if err := c.Validate(); err == nil {
		t.Error("expected port clash error")
	}
}

func TestValidateDNSSharesPortWithTCP(t *testing.T) {
	c := Defaults()
	c.DNSEnabled, c.TCPEnabled = true, true
	c.DNSPort, c.TCPPort = 9001, 9001 // UDP vs TCP: allowed
	if err := c.Validate(); err != nil {
		t.Errorf("DNS/TCP same port should be allowed: %v", err)
	}
}

func TestValidateBadPublicURL(t *testing.T) {
	c := Defaults()
	c.PublicURL = "not a url"
	if err := c.Validate(); err == nil {
		t.Error("expected invalid public url error")
	}
	c.PublicURL = "https://x.trycloudflare.com"
	if err := c.Validate(); err != nil {
		t.Errorf("valid url rejected: %v", err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := Defaults()
	c.HTTPPort = 12345
	c.PublicURL = "https://abc.trycloudflare.com"
	if err := Save(dir, c); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.HTTPPort != 12345 || got.PublicURL != c.PublicURL {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}

func TestLoadMissingReturnsDefaults(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("load missing: %v", err)
	}
	if got.HTTPPort != Defaults().HTTPPort {
		t.Error("expected defaults for missing file")
	}
}
