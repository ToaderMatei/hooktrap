package store

import (
	"path/filepath"
	"testing"

	"github.com/ToaderMatei/hooktrap/internal/model"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "data"), 5)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestDefaultBinExists(t *testing.T) {
	s := newStore(t)
	if _, ok := s.Bin(DefaultBinID); !ok {
		t.Fatal("default bin missing")
	}
}

func TestValidBinID(t *testing.T) {
	cases := map[string]bool{"ab": true, "oast-1": true, "A": false, "x": false, "has space": false, "..": false, "a_b": false}
	for in, want := range cases {
		if got := ValidBinID(in); got != want {
			t.Errorf("ValidBinID(%q)=%v want %v", in, got, want)
		}
	}
}

func TestCreateBinReservedAndDuplicate(t *testing.T) {
	s := newStore(t)
	if _, err := s.CreateBin("api", ""); err == nil {
		t.Error("expected reserved id to be rejected")
	}
	if _, err := s.CreateBin("box", "Box"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.CreateBin("box", "again"); err == nil {
		t.Error("expected duplicate id to be rejected")
	}
}

func TestAddListAndEvictPerBin(t *testing.T) {
	s := newStore(t)
	for i := 0; i < 8; i++ {
		if _, err := s.Add(model.Request{BinID: DefaultBinID, Method: "GET", Path: "/p"}); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	if n := s.CountFor(DefaultBinID); n != 5 {
		t.Fatalf("retained %d want 5 (maxPerBin cap)", n)
	}
	list := s.List(DefaultBinID, "", 10)
	if len(list) != 5 {
		t.Fatalf("list len %d want 5", len(list))
	}
}

func TestSearchFilter(t *testing.T) {
	s := newStore(t)
	s.Add(model.Request{BinID: DefaultBinID, Method: "POST", Path: "/login", Body: []byte("secret=42")})
	s.Add(model.Request{BinID: DefaultBinID, Method: "GET", Path: "/health"})
	if got := s.List(DefaultBinID, "secret", 10); len(got) != 1 {
		t.Fatalf("body search got %d want 1", len(got))
	}
	if got := s.List(DefaultBinID, "health", 10); len(got) != 1 || got[0].Path != "/health" {
		t.Fatalf("path search failed: %+v", got)
	}
}

func TestAnnotateAndDelete(t *testing.T) {
	s := newStore(t)
	r, _ := s.Add(model.Request{BinID: DefaultBinID, Method: "GET"})
	note, star := "flagged", true
	if _, err := s.Annotate(r.ID, &note, &star); err != nil {
		t.Fatalf("annotate: %v", err)
	}
	got, err := s.Get(r.ID)
	if err != nil || got.Note != "flagged" || !got.Starred {
		t.Fatalf("annotate not persisted: %+v err=%v", got, err)
	}
	if err := s.Delete(r.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(r.ID); err == nil {
		t.Error("expected not found after delete")
	}
}

func TestPersistenceReload(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	s1, err := Open(dir, 100)
	if err != nil {
		t.Fatal(err)
	}
	s1.CreateBin("keep", "Keep")
	s1.Add(model.Request{BinID: "keep", Method: "GET", Path: "/a"})
	_ = s1.Close()

	s2, err := Open(dir, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if _, ok := s2.Bin("keep"); !ok {
		t.Fatal("bin not persisted")
	}
	if n := s2.CountFor("keep"); n != 1 {
		t.Fatalf("requests not persisted: %d", n)
	}
}

func TestDefaultBinCannotBeDeleted(t *testing.T) {
	s := newStore(t)
	if err := s.DeleteBin(DefaultBinID); err == nil {
		t.Error("expected default bin delete to fail")
	}
}
