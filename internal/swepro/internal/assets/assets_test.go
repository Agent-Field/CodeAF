package assets

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"testing"
)

type manifestEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

func loadManifest(t *testing.T) []manifestEntry {
	t.Helper()
	raw, err := os.ReadFile("testdata/assets-manifest.json")
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var entries []manifestEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	return entries
}

func TestEmbeddedAssetManifest(t *testing.T) {
	entries := loadManifest(t)
	if len(entries) != 48 {
		t.Fatalf("manifest contains %d assets, want 48", len(entries))
	}

	manifestPaths := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if _, duplicate := manifestPaths[entry.Path]; duplicate {
			t.Fatalf("duplicate manifest path %q", entry.Path)
		}
		manifestPaths[entry.Path] = struct{}{}

		text, ok := Get(entry.Path)
		if !ok {
			t.Errorf("manifest asset %q is not embedded", entry.Path)
			continue
		}
		sum := sha256.Sum256([]byte(text))
		if got := hex.EncodeToString(sum[:]); got != entry.SHA256 {
			t.Errorf("%s sha256 = %s, want %s", entry.Path, got, entry.SHA256)
		}
	}

	embeddedCount := 0
	err := fs.WalkDir(files, "src", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		embeddedCount++
		if _, ok := manifestPaths[path]; !ok {
			t.Errorf("embedded asset %q is missing from the manifest", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded assets: %v", err)
	}
	if embeddedCount != len(entries) {
		t.Errorf("embedded asset count = %d, manifest count = %d", embeddedCount, len(entries))
	}
}

func TestGetRejectsUnknownPaths(t *testing.T) {
	for _, path := range []string{"", "src/tool/plan-enter.txt", "../go.mod", "src/not-present.txt"} {
		if text, ok := Get(path); ok {
			t.Errorf("Get(%q) = %q, true; want false", path, text)
		}
	}
}
