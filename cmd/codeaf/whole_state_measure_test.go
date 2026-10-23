package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
)

// TestC29MeasureCopyCost measures C29.5 and creates isolated paired fifty-chat
// homes for C29.2's first-paint comparison. It runs only on explicit request.
func TestC29MeasureCopyCost(t *testing.T) {
	out := os.Getenv("C6_MEASURE_OUT")
	if out == "" {
		t.Skip("explicit lane measurement only")
	}
	root, err := os.MkdirTemp("/tmp", "c6m-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cellstore.StopSpawned)
	write := func(path string, raw []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	work := []string{filepath.Join(root, "small"), filepath.Join(root, "medium"), filepath.Join(root, "tenk")}
	for n, dir := range work {
		count := []int{10, 100, 10000}[n]
		for i := 0; i < count; i++ {
			write(filepath.Join(dir, fmt.Sprintf("file-%05d.txt", i)), []byte(fmt.Sprintf("current folder contents number %d\n", i)))
		}
	}
	fixture := func(name string, count int) []string {
		state := filepath.Join(root, name)
		if _, err := identity.Ensure(state); err != nil {
			t.Fatal(err)
		}
		var originals []string
		for i := 0; i < count; i++ {
			id := fmt.Sprintf("old-%02d", i)
			dir := filepath.Join(state, "v3", "projects", fmt.Sprintf("p%d", i%3), id)
			journal := fmt.Sprintf("{\"type\":\"session\",\"version\":1,\"id\":%q}\n{\"type\":\"message\",\"role\":\"user\",\"content\":%q}\n", id, "Earlier whole conversation "+id)
			write(filepath.Join(dir, "transcript.jsonl"), []byte(journal))
			if err := session.SaveMeta(dir, session.Meta{ID: id, Title: "Earlier conversation " + id, Workspace: work[i%3], LastUserAt: time.Now().Add(-time.Duration(i) * time.Minute)}); err != nil {
				t.Fatal(err)
			}
			originals = append(originals, dir)
		}
		return originals
	}
	originals := fixture("copy-home", 20)
	fixture("startup-before", 50)
	fixture("startup-after", 50)
	roots, _ := json.MarshalIndent(map[string]any{"root": root, "work": work, "before": filepath.Join(root, "startup-before"), "after": filepath.Join(root, "startup-after")}, "", "  ")
	write(filepath.Join(out, "measurement-fixture.json"), append(roots, '\n'))
	t.Setenv(home.EnvVar, filepath.Join(root, "copy-home"))
	disk := func(path string) int64 {
		raw, err := exec.Command("du", "-s", "-B1", path).Output()
		if err != nil {
			t.Fatal(err)
		}
		n, err := strconv.ParseInt(strings.Fields(string(raw))[0], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := disk(home.Dir())
	started := time.Now()
	var samples []map[string]any
	for _, dir := range originals {
		began := time.Now()
		if _, err := copyOlderChat(t.Context(), dir); err != nil {
			t.Fatal(err)
		}
		samples = append(samples, map[string]any{"original": filepath.Base(dir), "ms": float64(time.Since(began).Microseconds()) / 1000, "disk_bytes": disk(home.Dir()) - before})
	}
	result := map[string]any{"chats": 20, "folders": 3, "largest_folder_files": 10000, "disk_added": disk(home.Dir()) - before, "elapsed_ms": float64(time.Since(started).Microseconds()) / 1000, "samples": samples}
	raw, _ := json.MarshalIndent(result, "", "  ")
	write(filepath.Join(out, "copy-measurements.json"), append(raw, '\n'))
	t.Log(string(raw))
}
