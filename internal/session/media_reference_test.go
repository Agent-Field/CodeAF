package session

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func referenceFixture(t *testing.T) (MediaRow, string, string, []byte) {
	t.Helper()
	root := t.TempDir()
	dest := t.TempDir()
	path := filepath.Join(root, "original.png")
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	fact := GeneratedMedia(path, data, "")
	fact.ID = NewMediaID()
	return MediaRow{Fact: fact, Kind: "image", Recorded: true}, root, dest, data
}
func TestMediaReferenceCopyIsOwnedAndOriginalIsUnchanged(t *testing.T) {
	row, root, dest, data := referenceFixture(t)
	got, err := CopyMediaReference(context.Background(), row, []string{root}, row.Version(), dest)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path == row.Fact.Canonical || !strings.HasPrefix(got.Path, dest+string(os.PathSeparator)) {
		t.Fatal("not a separate owned snapshot")
	}
	if err := os.WriteFile(row.Fact.Canonical, []byte("source edited after staging"), 0o600); err != nil {
		t.Fatal(err)
	}
	copied, err := os.ReadFile(got.Path)
	if err != nil || string(copied) != string(data) {
		t.Fatalf("snapshot changed: %q %v", copied, err)
	}
	if err := got.Remove(); err != nil {
		t.Fatal(err)
	}
	if err := got.Remove(); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(row.Fact.Canonical)
	if err != nil || string(original) != "source edited after staging" {
		t.Fatal("cleanup touched original")
	}
	entries, _ := os.ReadDir(dest)
	if len(entries) != 0 {
		t.Fatal("snapshot directory leaked")
	}
}
func TestMediaReferenceRefusesUnownedChangedAndStaleSources(t *testing.T) {
	for _, name := range []string{"legacy", "audio", "version", "outside", "changed", "symlink", "hash"} {
		t.Run(name, func(t *testing.T) {
			row, root, dest, _ := referenceFixture(t)
			version := row.Version()
			roots := []string{root}
			switch name {
			case "legacy":
				row.Recorded = false
			case "audio":
				row.Kind = "audio"
			case "version":
				version = "old"
			case "outside":
				roots = []string{dest}
			case "changed":
				if err := os.WriteFile(row.Fact.Canonical, []byte("different"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				moved := row.Fact.Canonical + ".moved"
				if err := os.Rename(row.Fact.Canonical, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(moved, row.Fact.Canonical); err != nil {
					t.Fatal(err)
				}
			case "hash":
				row.Fact.SHA256 = strings.Repeat("0", 64)
				version = row.Version()
			}
			_, err := CopyMediaReference(context.Background(), row, roots, version, dest)
			if !errors.Is(err, ErrMediaReference) {
				t.Fatalf("wanted authoritative refusal: %v", err)
			}
			entries, _ := os.ReadDir(dest)
			if len(entries) != 0 {
				t.Fatal("refused snapshot leaked")
			}
		})
	}
}
func TestMediaReferenceCancellationAndByteLimitLeaveNothing(t *testing.T) {
	row, root, dest, _ := referenceFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CopyMediaReference(ctx, row, []string{root}, row.Version(), dest); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	file, err := os.OpenFile(row.Fact.Canonical, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxImageBytes + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	row.Fact = DerivedMedia(row.Fact.Canonical)
	row.Fact.ID = NewMediaID()
	if _, err := CopyMediaReference(context.Background(), row, []string{root}, row.Version(), dest); err == nil {
		t.Fatal("oversize accepted")
	}
	entries, _ := os.ReadDir(dest)
	if len(entries) != 0 {
		t.Fatal("cancelled/oversize copy leaked")
	}
}
