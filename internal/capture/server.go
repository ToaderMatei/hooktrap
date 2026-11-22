package capture

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Server runs the capture HTTP (and optional HTTPS) listener.
type Server struct {
	handler http.Handler
	http    *http.Server
	https   *http.Server
}

// NewServer wraps a capture handler.
func NewServer(h http.Handler) *Server { return &Server{handler: h} }

// StartHTTP binds the plaintext capture listener.
func (s *Server) StartHTTP(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen http %s: %w", addr, err)
	}
	s.http = &http.Server{Handler: s.handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		_ = s.http.Close()
	}()
	go func() { _ = s.http.Serve(ln) }()
	return nil
}

// StartHTTPS binds a TLS capture listener using a persisted self-signed cert.
func (s *Server) StartHTTPS(ctx context.Context, addr, certDir string) error {
	cert, err := loadOrCreateCert(certDir)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen https %s: %w", addr, err)
	}
	s.https = &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{cert}},
	}
	go func() {
		<-ctx.Done()
		_ = s.https.Close()
	}()
	go func() { _ = s.https.ServeTLS(ln, "", "") }()
	return nil
}

func loadOrCreateCert(dir string) (tls.Certificate, error) {
	certPath := filepath.Join(dir, "capture-cert.pem")
	keyPath := filepath.Join(dir, "capture-key.pem")
	if cert, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		return cert, nil
	}
	cert, certPEM, keyPEM, err := generateCert()
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := os.MkdirAll(dir, 0o700); err == nil {
		_ = os.WriteFile(certPath, certPEM, 0o600)
		_ = os.WriteFile(keyPath, keyPEM, 0o600)
	}
	return cert, nil
}

func generateCert() (tls.Certificate, []byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, nil, fmt.Errorf("generate key: %w", err)
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "HookTrap Local Capture", Organization: []string{"HookTrap"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(5, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, nil, nil, fmt.Errorf("create cert: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, nil, nil, fmt.Errorf("marshal key: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	return cert, certPEM, keyPEM, err
}
