package cellstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissingPackCapturePreservesEvidenceBeforeOneFreshPlan(t *testing.T) {
	for _, recover := range []bool{false, true} {
		t.Run(map[bool]string{false: "persistent", true: "recovered"}[recover], func(t *testing.T) {
			c := newCell(t)
			e := Engine{Binary: "scratch-furrow", DataRoot: t.TempDir()}
			store := filepath.Join(e.StoreDir(c), "store-v1")
			if err := os.MkdirAll(filepath.Join(store, "packs"), 0700); err != nil {
				t.Fatal(err)
			}
			pack := filepath.Join(store, "packs", "active.pack")
			for path, body := range map[string]string{pack: "PRIVATE PACK BYTES", filepath.Join(store, "catalog.sqlite3"): "catalog metadata", filepath.Join(store, "group-commit.json"): "frontier metadata"} {
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Rename(pack, filepath.Join(t.TempDir(), "aside.pack")); err != nil {
				t.Fatal(err)
			}
			calls := 0
			original := errors.New("engine capture-plan: durable object pack is missing")
			e.Run = func(_ context.Context, _ string, _ []string, argv ...string) ([]byte, error) {
				args := strings.Join(argv, " ")
				if strings.Contains(args, "--version") {
					return []byte("furrow scratch-version"), nil
				}
				if strings.Contains(args, "capture-plan") {
					calls++
					if calls == 2 {
						bundles, _ := filepath.Glob(filepath.Join(e.LocalDir(c), "capture-evidence", "*.json"))
						if len(bundles) != 1 {
							t.Fatal("retry ran before evidence was written")
						}
					}
					if calls == 1 || !recover {
						return nil, original
					}
					return []byte(`{"token":"fresh-plan","full":true,"paths":[]}`), nil
				}
				return []byte(`{"snapshot":"saved"}`), nil
			}
			_, captureErr := e.snapshotReply(context.Background(), c, "receipt", nil)
			if recover && captureErr != nil {
				t.Fatal(captureErr)
			}
			if !recover && !errors.Is(captureErr, original) {
				t.Fatalf("lost original: %v", captureErr)
			}
			if calls != 2 {
				t.Fatalf("plans=%d, want 2", calls)
			}
			bundles, _ := filepath.Glob(filepath.Join(e.LocalDir(c), "capture-evidence", "*.json"))
			if len(bundles) != 1 {
				t.Fatalf("bundles=%v", bundles)
			}
			if !recover && !strings.Contains(captureErr.Error(), bundles[0]) {
				t.Fatalf("failure does not name bundle: %v", captureErr)
			}
			raw, err := os.ReadFile(bundles[0])
			if err != nil {
				t.Fatal(err)
			}
			var bundle captureEvidence
			if err := json.Unmarshal(raw, &bundle); err != nil {
				t.Fatal(err)
			}
			for name, want := range map[string]string{"catalog.sqlite3": "catalog metadata", "group-commit.json": "frontier metadata"} {
				if string(bundle.Metadata[name]) != want {
					t.Fatalf("metadata %s=%q", name, bundle.Metadata[name])
				}
			}
			if len(bundle.Metadata) != 2 {
				t.Fatalf("unexpected copied files: %v", bundle.Metadata)
			}
			for _, want := range []string{"scratch-furrow", "scratch-version", "catalog.sqlite3", "group-commit.json", "durable object pack is missing", "mtime", "size"} {
				if !strings.Contains(string(raw), want) {
					t.Errorf("missing %s: %s", want, raw)
				}
			}
			if strings.Contains(string(raw), "PRIVATE PACK BYTES") {
				t.Fatal("copied pack contents")
			}

		})
	}
}

func TestOtherCaptureErrorsDoNotRetryOrWriteEvidence(t *testing.T) {
	for _, message := range []string{"engine capture-plan: permission failure", "engine capture-plan: durable object pack is missing index", "engine capture-plan: not durable object pack is missing"} {
		t.Run(message, func(t *testing.T) {
			c := newCell(t)
			e := Engine{Binary: "scratch-furrow", DataRoot: t.TempDir()}
			calls := 0
			e.Run = func(context.Context, string, []string, ...string) ([]byte, error) {
				calls++
				return nil, errors.New(message)
			}
			_, err := e.snapshotReply(context.Background(), c, "receipt", nil)
			if err == nil || calls != 1 {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
			if _, err := os.Stat(filepath.Join(e.LocalDir(c), "capture-evidence")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unexpected evidence: %v", err)
			}
		})
	}
}

func TestCaptureEvidenceBoundsAndRetainsOnlyNewestBundles(t *testing.T) {
	c := newCell(t)
	e := Engine{Binary: "scratch-furrow", DataRoot: t.TempDir()}
	root := filepath.Join(e.StoreDir(c), "store-v1")
	if err := os.MkdirAll(filepath.Join(root, "keys"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"catalog.sqlite3": strings.Repeat("x", captureMetadataLimit+1), "group-commit.json": "frontier", "keys/secret": "DO NOT COPY"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("keys/secret", filepath.Join(root, "catalog.sqlite3-wal")); err != nil {
		t.Fatal(err)
	}
	e.Run = func(context.Context, string, []string, ...string) ([]byte, error) {
		return []byte("scratch-version"), nil
	}
	for i := 0; i < 5; i++ {
		if _, err := e.captureEvidence(context.Background(), c, fmt.Errorf("durable object pack is missing")); err != nil {
			t.Fatal(err)
		}
	}
	paths, _ := filepath.Glob(filepath.Join(e.LocalDir(c), "capture-evidence", "*.json"))
	if len(paths) != captureEvidenceKeep {
		t.Fatalf("retained %d", len(paths))
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) > captureEvidenceLimit {
			t.Fatal("bundle exceeds cap")
		}
		var bundle captureEvidence
		if err := json.Unmarshal(raw, &bundle); err != nil {
			t.Fatal(err)
		}
		if len(bundle.Metadata) != 1 || string(bundle.Metadata["group-commit.json"]) != "frontier" {
			t.Fatalf("unexpected metadata: %v", bundle.Metadata)
		}
		if strings.Contains(string(raw), "DO NOT COPY") || strings.Contains(string(raw), "keys/secret") {
			t.Fatal("credential evidence leaked")
		}
		if len(bundle.Notes) == 0 {
			t.Fatal("omission not explained")
		}
	}
}
