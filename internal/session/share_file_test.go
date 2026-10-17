package session

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestSharedFileInspectionKeepsGenericBytesAndUnknownProduction(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "report.bin")
	data := []byte("a person's exact result\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	budget := shareFileBytes
	got, err := inspectSharedFile(context.Background(), []string{root}, path, &budget)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if got.Kind != "file" || got.File.SHA256 != hex.EncodeToString(sum[:]) || got.File.Bytes != int64(len(data)) || budget != shareFileBytes-int64(len(data)) {
		t.Fatalf("facts=%+v budget=%d", got, budget)
	}
	if got.Media.Origin != MediaPublished || got.Media.Change != ShellUnknown || got.Media.Model != "" {
		t.Fatalf("invented production %+v", got.Media)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(data) {
		t.Fatalf("original changed %q %v", after, err)
	}
}

func TestSharedFileInspectionRefusesOutsideAndLinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "file")
	if err := os.WriteFile(target, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	for _, path := range []string{target, link, root, "relative"} {
		budget := shareFileBytes
		if _, err := inspectSharedFile(context.Background(), []string{root}, path, &budget); err == nil {
			t.Fatalf("unowned source accepted %q", path)
		}
	}
	if err := os.Symlink(outside, filepath.Join(root, "dirlink")); err != nil {
		t.Fatal(err)
	}
	budget := shareFileBytes
	if _, err := inspectSharedFile(context.Background(), []string{root}, filepath.Join(root, "dirlink", "file"), &budget); err == nil {
		t.Fatal("intermediate link followed")
	}
}

func TestSharedFileInspectionRefusesSizeBeforeReadingAndHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "huge")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(shareFileBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	budget := shareFileBytes
	if _, err := inspectSharedFile(context.Background(), []string{root}, path, &budget); err == nil {
		t.Fatal("oversized source accepted")
	}
	if budget != shareFileBytes {
		t.Fatal("oversized file was admitted for read")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := inspectSharedFile(ctx, []string{root}, path, &budget); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled inspection=%v", err)
	}
}

func TestSharedFileKindsComeFromBytesNotNames(t *testing.T) {
	for _, row := range []struct{ mime, want string }{{"image/png", "image"}, {"audio/wav", "audio"}, {"video/mp4", "video"}, {"application/pdf", "document"}, {"text/plain; charset=utf-8", "file"}} {
		if got := sharedFileKind(row.mime); got != row.want {
			t.Fatalf("%s => %s", row.mime, got)
		}
	}
}

func TestSharedFileInspectionReadsActualPictureAndDocumentHeaders(t *testing.T) {
	root := t.TempDir()
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		name string
		data []byte
		kind string
	}{{"misleading.txt", picture.Bytes(), "image"}, {"misleading.png", []byte("%PDF-1.7\n%%EOF\n"), "document"}} {
		path := filepath.Join(root, row.name)
		if err := os.WriteFile(path, row.data, 0600); err != nil {
			t.Fatal(err)
		}
		budget := shareFileBytes
		got, err := inspectSharedFile(context.Background(), []string{root}, path, &budget)
		if err != nil {
			t.Fatal(err)
		}
		if got.Kind != row.kind {
			t.Fatalf("name=%s kind=%s", row.name, got.Kind)
		}
		if row.kind == "image" && (got.Media.Width != 3 || got.Media.Height != 2) {
			t.Fatalf("dimensions=%dx%d", got.Media.Width, got.Media.Height)
		}
	}
}
