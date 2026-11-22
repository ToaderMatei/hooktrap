// Package store keeps bins and captured requests in memory and persists them
// to a data directory (bins.json + requests.jsonl).
package store

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ToaderMatei/hooktrap/internal/model"
)

// ErrNotFound is returned when a bin or request does not exist.
var ErrNotFound = errors.New("not found")

// DefaultBinID is the catch-all bin that always exists.
const DefaultBinID = "default"

var binIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,39}$`)

// ValidBinID reports whether id can be used as a bin identifier (URL and subdomain safe).
func ValidBinID(id string) bool { return binIDPattern.MatchString(id) }

// Store is a concurrency-safe, persisted repository of bins and requests.
type Store struct {
	mu        sync.RWMutex
	dir       string
	maxPerBin int
	bins      map[string]model.Bin
	reqs      map[string][]model.Request // binID -> requests (oldest first)
	byID      map[string]string          // requestID -> binID
	appendF   *os.File
	dirty     int
}

// Open loads (or initialises) a store in dir. maxPerBin caps retained requests per bin.
func Open(dir string, maxPerBin int) (*Store, error) {
	if maxPerBin <= 0 {
		maxPerBin = 2000
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	s := &Store{
		dir:       dir,
		maxPerBin: maxPerBin,
		bins:      map[string]model.Bin{},
		reqs:      map[string][]model.Request{},
		byID:      map[string]string{},
	}
	if err := s.loadBins(); err != nil {
		return nil, err
	}
	if _, ok := s.bins[DefaultBinID]; !ok {
		s.bins[DefaultBinID] = model.Bin{ID: DefaultBinID, Name: "default", Created: time.Now(), Response: model.DefaultResponse()}
		if err := s.saveBinsLocked(); err != nil {
			return nil, err
		}
	}
	if err := s.loadRequests(); err != nil {
		return nil, err
	}
	if err := s.compactLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

// Close flushes and closes the append log.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.appendF != nil {
		err := s.appendF.Close()
		s.appendF = nil
		return err
	}
	return nil
}

// NewID returns n random bytes hex-encoded.
func NewID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}

// ---------- bins ----------

// Bins returns all bins sorted by creation time.
func (s *Store) Bins() []model.Bin {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Bin, 0, len(s.bins))
	for _, b := range s.bins {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}

// Bin returns a bin by id.
func (s *Store) Bin(id string) (model.Bin, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.bins[id]
	return b, ok
}

// CountFor returns how many requests are stored for a bin.
func (s *Store) CountFor(id string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.reqs[id])
}

// CreateBin creates a bin. An empty id generates a random one.
func (s *Store) CreateBin(id, name string) (model.Bin, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		id = NewID(4)
	}
	if !ValidBinID(id) {
		return model.Bin{}, fmt.Errorf("invalid bin id %q: use 2-40 chars of a-z, 0-9 and -", id)
	}
	if reservedBinID(id) {
		return model.Bin{}, fmt.Errorf("bin id %q is reserved", id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.bins[id]; exists {
		return model.Bin{}, fmt.Errorf("bin %q already exists", id)
	}
	if strings.TrimSpace(name) == "" {
		name = id
	}
	b := model.Bin{ID: id, Name: name, Created: time.Now(), Response: model.DefaultResponse()}
	s.bins[id] = b
	return b, s.saveBinsLocked()
}

// UpdateBin replaces name and response of an existing bin.
func (s *Store) UpdateBin(id, name string, resp model.Response) (model.Bin, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.bins[id]
	if !ok {
		return model.Bin{}, ErrNotFound
	}
	updated := model.Bin{ID: b.ID, Name: b.Name, Created: b.Created, Response: resp}
	if strings.TrimSpace(name) != "" {
		updated.Name = name
	}
	s.bins[id] = updated
	return updated, s.saveBinsLocked()
}

// DeleteBin removes a bin and its requests. The default bin can only be cleared.
func (s *Store) DeleteBin(id string) error {
	if id == DefaultBinID {
		return errors.New("the default bin cannot be deleted")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.bins[id]; !ok {
		return ErrNotFound
	}
	delete(s.bins, id)
	s.dropRequestsLocked(id)
	if err := s.saveBinsLocked(); err != nil {
		return err
	}
	return s.compactLocked()
}

func reservedBinID(id string) bool {
	switch id {
	case "f", "api", "static", "favicon.ico", "robots.txt", "files":
		return true
	}
	return false
}

// ---------- requests ----------

// Add stores a captured request, assigning ID/time if missing. Returns the stored copy.
func (s *Store) Add(r model.Request) (model.Request, error) {
	if r.ID == "" {
		r.ID = NewID(8)
	}
	if r.Time.IsZero() {
		r.Time = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.bins[r.BinID]; !ok {
		r.BinID = DefaultBinID
	}
	list := append(s.reqs[r.BinID], r)
	if over := len(list) - s.maxPerBin; over > 0 {
		for _, old := range list[:over] {
			delete(s.byID, old.ID)
		}
		list = append([]model.Request(nil), list[over:]...)
		s.dirty += over
	}
	s.reqs[r.BinID] = list
	s.byID[r.ID] = r.BinID
	if err := s.appendLocked(r); err != nil {
		return r, err
	}
	if s.dirty > s.maxPerBin {
		if err := s.compactLocked(); err != nil {
			return r, err
		}
	}
	return r, nil
}

// List returns requests of a bin, newest first, optionally filtered by a
// case-insensitive substring query over method/path/query/headers/body.
func (s *Store) List(binID, q string, limit int) []model.Request {
	s.mu.RLock()
	defer s.mu.RUnlock()
	src := s.reqs[binID]
	q = strings.ToLower(strings.TrimSpace(q))
	out := make([]model.Request, 0, min(len(src), max(limit, 0)))
	for i := len(src) - 1; i >= 0; i-- {
		if q != "" && !matches(src[i], q) {
			continue
		}
		out = append(out, src[i])
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func matches(r model.Request, q string) bool {
	if strings.Contains(strings.ToLower(r.Method+" "+r.Path+"?"+r.Query+" "+r.RemoteAddr+" "+r.Kind+" "+r.Summary+" "+r.Note), q) {
		return true
	}
	for _, h := range r.Headers {
		if strings.Contains(strings.ToLower(h.Name+": "+h.Value), q) {
			return true
		}
	}
	return strings.Contains(strings.ToLower(string(r.Body)), q)
}

// Get returns a request by id.
func (s *Store) Get(id string) (model.Request, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	binID, ok := s.byID[id]
	if !ok {
		return model.Request{}, ErrNotFound
	}
	for _, r := range s.reqs[binID] {
		if r.ID == id {
			return r, nil
		}
	}
	return model.Request{}, ErrNotFound
}

// Annotate sets note/starred on a request.
func (s *Store) Annotate(id string, note *string, starred *bool) (model.Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	binID, ok := s.byID[id]
	if !ok {
		return model.Request{}, ErrNotFound
	}
	list := s.reqs[binID]
	for i, r := range list {
		if r.ID != id {
			continue
		}
		updated := r
		if note != nil {
			updated.Note = *note
		}
		if starred != nil {
			updated.Starred = *starred
		}
		newList := append([]model.Request(nil), list...)
		newList[i] = updated
		s.reqs[binID] = newList
		return updated, s.compactLocked()
	}
	return model.Request{}, ErrNotFound
}

// Delete removes a single request.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	binID, ok := s.byID[id]
	if !ok {
		return ErrNotFound
	}
	list := s.reqs[binID]
	newList := make([]model.Request, 0, len(list))
	for _, r := range list {
		if r.ID != id {
			newList = append(newList, r)
		}
	}
	s.reqs[binID] = newList
	delete(s.byID, id)
	return s.compactLocked()
}

// Clear removes all requests of a bin.
func (s *Store) Clear(binID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.bins[binID]; !ok {
		return ErrNotFound
	}
	s.dropRequestsLocked(binID)
	return s.compactLocked()
}

func (s *Store) dropRequestsLocked(binID string) {
	for _, r := range s.reqs[binID] {
		delete(s.byID, r.ID)
	}
	delete(s.reqs, binID)
}

// ---------- persistence ----------

func (s *Store) binsPath() string { return filepath.Join(s.dir, "bins.json") }
func (s *Store) reqsPath() string { return filepath.Join(s.dir, "requests.jsonl") }

func (s *Store) loadBins() error {
	data, err := os.ReadFile(s.binsPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read bins: %w", err)
	}
	var bins []model.Bin
	if err := json.Unmarshal(data, &bins); err != nil {
		return fmt.Errorf("parse bins.json: %w", err)
	}
	for _, b := range bins {
		s.bins[b.ID] = b
	}
	return nil
}

func (s *Store) saveBinsLocked() error {
	bins := make([]model.Bin, 0, len(s.bins))
	for _, b := range s.bins {
		bins = append(bins, b)
	}
	sort.Slice(bins, func(i, j int) bool { return bins[i].Created.Before(bins[j].Created) })
	data, err := json.MarshalIndent(bins, "", "  ")
	if err != nil {
		return fmt.Errorf("encode bins: %w", err)
	}
	return writeAtomic(s.binsPath(), data)
}

func (s *Store) loadRequests() error {
	f, err := os.Open(s.reqsPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open requests log: %w", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		var r model.Request
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			continue // skip a corrupt/partial line instead of refusing to start
		}
		if _, ok := s.bins[r.BinID]; !ok {
			continue
		}
		s.reqs[r.BinID] = append(s.reqs[r.BinID], r)
		s.byID[r.ID] = r.BinID
	}
	for id, list := range s.reqs {
		if over := len(list) - s.maxPerBin; over > 0 {
			for _, old := range list[:over] {
				delete(s.byID, old.ID)
			}
			s.reqs[id] = append([]model.Request(nil), list[over:]...)
		}
	}
	return sc.Err()
}

func (s *Store) appendLocked(r model.Request) error {
	if s.appendF == nil {
		f, err := os.OpenFile(s.reqsPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("open requests log: %w", err)
		}
		s.appendF = f
	}
	line, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	if _, err := s.appendF.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("append request: %w", err)
	}
	return nil
}

// compactLocked rewrites requests.jsonl with exactly the retained requests.
func (s *Store) compactLocked() error {
	if s.appendF != nil {
		_ = s.appendF.Close()
		s.appendF = nil
	}
	all := make([]model.Request, 0, len(s.byID))
	for _, list := range s.reqs {
		all = append(all, list...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Time.Before(all[j].Time) })
	var sb strings.Builder
	for _, r := range all {
		line, err := json.Marshal(r)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		sb.Write(line)
		sb.WriteByte('\n')
	}
	s.dirty = 0
	return writeAtomic(s.reqsPath(), []byte(sb.String()))
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace %s: %w", filepath.Base(path), err)
	}
	return nil
}
