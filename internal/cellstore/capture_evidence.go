package cellstore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
)

const captureEvidenceLimit = 1 << 20
const captureEvidenceKeep = 3
const captureMetadataLimit = 64 << 10

// missingPackPlan matches the engine's whole diagnostic, rather than treating
// unrelated pack errors as permission to repeat a capture.
func missingPackPlan(err error) bool {
	if err == nil {
		return false
	}
	text := strings.TrimSpace(err.Error())
	return text == "durable object pack is missing" || strings.HasSuffix(text, ": durable object pack is missing")
}

type captureEvidenceEntry struct {
	Name  string    `json:"name"`
	Size  int64     `json:"size"`
	Mtime time.Time `json:"mtime"`
	Mode  string    `json:"mode"`
}

type captureEvidence struct {
	Binary   string                 `json:"binary"`
	Version  string                 `json:"version"`
	Store    string                 `json:"store"`
	Error    string                 `json:"error"`
	Entries  []captureEvidenceEntry `json:"entries"`
	Metadata map[string][]byte      `json:"metadata"`
	Notes    []string               `json:"notes,omitempty"`
}

// captureEvidence writes only an explicit metadata allowlist. A recursive copy
// could collect object bytes or credentials precisely when a store is suspect.
func (e Engine) captureEvidence(ctx context.Context, c cell.Cell, problem error) (string, error) {
	bin := e.Binary
	if bin == "" {
		var err error
		bin, err = sealingEngine()
		if err != nil {
			return "", err
		}
	}
	bundle := captureEvidence{Binary: bin, Store: e.StoreDir(c), Error: problem.Error(), Metadata: map[string][]byte{}}
	root := filepath.Join(bundle.Store, "store-v1")
	// Directory metadata is collected first, before a version probe or retry can
	// cause the engine to open the store again. Symlinks are never followed.
	visited := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			bundle.Notes = append(bundle.Notes, walkErr.Error())
			return nil
		}
		visited++
		if visited > 1024 {
			return fs.SkipAll
		}
		if entry.Name() == "keys" || entry.Name() == "credentials" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			bundle.Notes = append(bundle.Notes, err.Error())
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		bundle.Entries = append(bundle.Entries, captureEvidenceEntry{rel, info.Size(), info.ModTime(), info.Mode().String()})
		return nil
	})
	if err != nil {
		bundle.Notes = append(bundle.Notes, err.Error())
	}
	if visited > 1024 {
		bundle.Notes = append(bundle.Notes, "directory listing limited to 1024 entries")
	}
	// OpenRoot confines metadata reads even if a damaged store contains links.
	// A raw catalog and its SQLite sidecars preserve evidence without opening a
	// database, checkpointing it, or changing its durability frontier.
	confined, err := os.OpenRoot(root)
	if err == nil {
		defer confined.Close()
		for _, name := range []string{"catalog.sqlite3", "catalog.sqlite3-wal", "catalog.sqlite3-shm", "group-commit.json"} {
			info, err := confined.Lstat(name)
			if err != nil {
				bundle.Notes = append(bundle.Notes, name+": "+err.Error())
				continue
			}
			if !info.Mode().IsRegular() {
				bundle.Notes = append(bundle.Notes, name+": not a regular metadata file")
				continue
			}
			if info.Size() > captureMetadataLimit {
				bundle.Notes = append(bundle.Notes, name+": exceeds metadata limit; omitted")
				continue
			}
			f, err := confined.Open(name)
			if err != nil {
				bundle.Notes = append(bundle.Notes, name+": "+err.Error())
				continue
			}
			raw, readErr := io.ReadAll(io.LimitReader(f, captureMetadataLimit+1))
			_ = f.Close()
			if readErr != nil || len(raw) > captureMetadataLimit {
				bundle.Notes = append(bundle.Notes, name+": could not copy within metadata limit")
				continue
			}
			bundle.Metadata[name] = raw
		}
	} else {
		bundle.Notes = append(bundle.Notes, err.Error())
	}
	versionCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	if e.Run != nil {
		raw, err := e.Run(versionCtx, "", nil, bin, "--version")
		bundle.Version = string(raw)
		if err != nil {
			bundle.Notes = append(bundle.Notes, "version: "+err.Error())
		}
	} else {
		out := &limitedVersion{}
		cmd := exec.CommandContext(versionCtx, bin, "--version") //codeaf:plumbing The evidence names the actual engine, including configured binaries.
		cmd.Stdout = out
		cmd.Stderr = out
		if err := cmd.Run(); err != nil {
			bundle.Notes = append(bundle.Notes, "version: "+err.Error())
		}
		bundle.Version = string(out.raw)
	}
	if len(bundle.Version) > 4096 {
		bundle.Version = bundle.Version[:4096]
	}
	raw, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return "", err
	}
	if len(raw) > captureEvidenceLimit {
		bundle.Notes = append(bundle.Notes, "directory listing shortened to fit bundle limit")
		for len(raw) > captureEvidenceLimit && len(bundle.Entries) > 0 {
			bundle.Entries = bundle.Entries[:len(bundle.Entries)/2]
			raw, err = json.MarshalIndent(bundle, "", "  ")
			if err != nil {
				return "", err
			}
		}
		if len(raw) > captureEvidenceLimit {
			return "", fmt.Errorf("capture evidence exceeds %d bytes", captureEvidenceLimit)
		}
	}
	dir := filepath.Join(e.LocalDir(c), "capture-evidence")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(dir, e.now().UTC().Format("20060102T150405.000000000")+"-*.json")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if _, err = file.Write(raw); err != nil {
		file.Close()
		os.Remove(path)
		return "", err
	}
	if err = file.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	sort.Strings(paths)
	retained := paths[:0]
	for _, other := range paths {
		if other != path {
			retained = append(retained, other)
		}
	}
	paths = retained
	for len(paths) >= captureEvidenceKeep {
		old := paths[0]
		paths = paths[1:]
		if old != path {
			if err := os.Remove(old); err != nil {
				log.Printf("capture evidence retention: %v", err)
			}
		}
	}
	log.Printf("capture evidence: %s", path)
	return path, nil
}

// limitedVersion drains excessive subprocess output without keeping it in RAM.
// Version discovery must not turn an engine failure into an unbounded bundle.
type limitedVersion struct{ raw []byte }

func (w *limitedVersion) Write(p []byte) (int, error) {
	n := len(p)
	if room := 4096 - len(w.raw); room > 0 {
		if len(p) > room {
			p = p[:room]
		}
		w.raw = append(w.raw, p...)
	}
	return n, nil
}
