// Package files stores operator-provided static files that the capture
// listener serves back at /f/{name}. This mirrors webhook.site's "hosted
// file" feature: it lets a tester point a client at a small text/binary
// response (for example an XML document, a JSON fixture or a script the
// client under test is expected to fetch) and confirm the fetch happened.
package files

import (
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// MaxFileSize caps a single hosted file.
const MaxFileSize = 25 << 20

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// ValidName reports whether name is a safe flat filename (no traversal, no separators).
func ValidName(name string) bool {
	return namePattern.MatchString(name) && !strings.Contains(name, "..")
}

// Info describes a hosted file.
type Info struct {
	Name        string    `json:"name"`
	Size        int64     `json:"size"`
	ContentType string    `json:"contentType"`
	Modified    time.Time `json:"modified"`
}

// Store is a flat directory of hosted files, guarded by a mutex.
type Store struct {
	mu  sync.RWMutex
	dir string
}

// New opens (creating if needed) the hosted file directory.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create files dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// ContentType guesses a MIME type from the file extension.
func ContentType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".dtd":
		return "application/xml-dtd"
	case ".svg":
		return "image/svg+xml"
	case ".xml":
		return "application/xml"
	case ".js":
		return "application/javascript"
	case ".json":
		return "application/json"
	case ".txt":
		return "text/plain; charset=utf-8"
	}
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

// Open returns file contents for the capture handler.
func (s *Store) Open(name string) ([]byte, string, bool) {
	if !ValidName(name) {
		return nil, "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, err := os.ReadFile(filepath.Join(s.dir, name))
	if err != nil {
		return nil, "", false
	}
	return data, ContentType(name), true
}

// Put writes (or replaces) a hosted file.
func (s *Store) Put(name string, data []byte) (Info, error) {
	if !ValidName(name) {
		return Info{}, fmt.Errorf("invalid file name %q: use letters, digits, '.', '_' or '-'", name)
	}
	if len(data) > MaxFileSize {
		return Info{}, fmt.Errorf("file exceeds %d MB limit", MaxFileSize>>20)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p := filepath.Join(s.dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return Info{}, fmt.Errorf("write hosted file: %w", err)
	}
	return Info{Name: name, Size: int64(len(data)), ContentType: ContentType(name), Modified: time.Now()}, nil
}

// Delete removes a hosted file.
func (s *Store) Delete(name string) error {
	if !ValidName(name) {
		return errors.New("invalid file name")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(filepath.Join(s.dir, name)); err != nil {
		return fmt.Errorf("delete hosted file: %w", err)
	}
	return nil
}

// List returns all hosted files sorted by name.
func (s *Store) List() ([]Info, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("list hosted files: %w", err)
	}
	out := make([]Info, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !ValidName(e.Name()) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Info{Name: e.Name(), Size: fi.Size(), ContentType: ContentType(e.Name()), Modified: fi.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
