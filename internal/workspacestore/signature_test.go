package workspacestore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSignatureReadOnlyAndInvalidatesRemovedFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-created")
	s := open(t, dir)
	if sig, err := s.Signature("now"); err != nil || sig != (FileSignature{}) {
		t.Fatal(sig, err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("signature created files: %v %v", entries, err)
	}
	if _, err := s.Signature("../escape"); !errors.Is(err, ErrInvalidKey) {
		t.Fatal(err)
	}
	if _, err := s.Put("now", 0, "writer", doc("a")); err != nil {
		t.Fatal(err)
	}
	first, err := s.Signature("now")
	if err != nil || first.Bytes == 0 || first.Modified == 0 {
		t.Fatal(first, err)
	}
	if _, err := s.Put("now", 1, "writer", doc("a", "longer-tab")); err != nil {
		t.Fatal(err)
	}
	second, err := s.Signature("now")
	if err != nil || second == first {
		t.Fatal(first, second, err)
	}
	if err := os.Remove(s.path("now")); err != nil {
		t.Fatal(err)
	}
	if sig, err := s.Signature("now"); err != nil || sig != (FileSignature{}) {
		t.Fatal(sig, err)
	}
}
