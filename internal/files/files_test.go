package files

import "testing"

func TestValidName(t *testing.T) {
	cases := map[string]bool{"oob.dtd": true, "beacon.js": true, "a_b-c.txt": true, "../etc": false, "a/b": false, "": false, ".hidden": false}
	for in, want := range cases {
		if got := ValidName(in); got != want {
			t.Errorf("ValidName(%q)=%v want %v", in, got, want)
		}
	}
}

func TestPutOpenDelete(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put("oob.dtd", []byte("<!ENTITY x SYSTEM 'file:///etc/hostname'>")); err != nil {
		t.Fatalf("put: %v", err)
	}
	data, ct, ok := s.Open("oob.dtd")
	if !ok || ct != "application/xml-dtd" || len(data) == 0 {
		t.Fatalf("open failed ok=%v ct=%q", ok, ct)
	}
	if _, _, ok := s.Open("../secret"); ok {
		t.Fatal("traversal name should not open")
	}
	if err := s.Delete("oob.dtd"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, _, ok := s.Open("oob.dtd"); ok {
		t.Fatal("file should be gone")
	}
}

func TestPutRejectsBadName(t *testing.T) {
	s, _ := New(t.TempDir())
	if _, err := s.Put("../evil", []byte("x")); err == nil {
		t.Error("expected invalid name error")
	}
}
