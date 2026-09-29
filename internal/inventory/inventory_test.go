package inventory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/lawcheck"
)

// rig is a cell root, a directory holding one fake tool, and an observer whose
// version probe counts its calls.
type rig struct {
	root, bin string
	store     *Store
	obs       *Observer
	probes    atomic.Int32
}

func newRig(t *testing.T, tools ...string) *rig {
	t.Helper()
	r := &rig{root: t.TempDir(), bin: t.TempDir()}
	for _, name := range tools {
		if err := os.WriteFile(filepath.Join(r.bin, name), []byte("#!/bin/sh\n"+name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var err error
	if r.store, err = Open(r.root); err != nil {
		t.Fatal(err)
	}
	probes := Probes{"node": {"--version"}}
	r.obs = NewObserver(r.store, probes, func(string, []string) string {
		r.probes.Add(1)
		return "v22.4.0"
	})
	return r
}

func (r *rig) call(argv0 string, env ...string) (executor.ExecRequest, executor.ExecResult) {
	return executor.ExecRequest{Argv: []string{argv0, "x"}, Env: append([]string{"PATH=" + r.bin}, env...)}, executor.ExecResult{}
}

func (r *rig) observe(argv0 string, res func(*executor.ExecResult), env ...string) {
	req, out := r.call(argv0, env...)
	if res != nil {
		res(&out)
	}
	r.obs.Observe(req, out)
}

func TestObserverRecordsToolOnce(t *testing.T) {
	r := newRig(t, "node")
	r.observe("node", nil)
	r.observe("node", nil)
	tools := r.store.Snapshot().Tools
	if len(tools) != 1 || tools[0].Name != "node" || tools[0].BinaryHash == "" || tools[0].VersionString != "v22.4.0" {
		t.Fatalf("tools = %+v", tools)
	}
}

func TestVersionProbeRunsOncePerBinaryHash(t *testing.T) {
	r := newRig(t, "node")
	for i := 0; i < 3; i++ {
		r.observe("node", nil)
	}
	if n := r.probes.Load(); n != 1 {
		t.Fatalf("probe ran %d times, want 1", n)
	}
	// A restarted observer learns the probed hashes from the file.
	again := NewObserver(r.store, Probes{"node": {"--version"}}, func(string, []string) string {
		r.probes.Add(1)
		return "other"
	})
	req, res := r.call("node")
	again.Observe(req, res)
	if n := r.probes.Load(); n != 1 {
		t.Fatalf("probe ran again after reopen: %d", n)
	}
	// New bytes are a new hash and are probed again.
	if err := os.WriteFile(filepath.Join(r.bin, "node"), []byte("#!/bin/sh\nnew"), 0o755); err != nil {
		t.Fatal(err)
	}
	again.Observe(req, res)
	if n := r.probes.Load(); n != 2 {
		t.Fatalf("changed binary not probed: %d", n)
	}
}

func TestToolWithoutProbeKeepsHashOnly(t *testing.T) {
	r := newRig(t, "make")
	r.observe("make", nil)
	tools := r.store.Snapshot().Tools
	if len(tools) != 1 || tools[0].VersionString != "" || r.probes.Load() != 0 {
		t.Fatalf("tools = %+v probes = %d", tools, r.probes.Load())
	}
}

func TestServicesAreMerged(t *testing.T) {
	r := newRig(t, "node")
	r.observe("node", func(res *executor.ExecResult) {
		res.Services = []executor.Service{{PGID: 7, Ports: []int{3000}}}
	})
	r.observe("node", func(res *executor.ExecResult) {
		res.Services = []executor.Service{{PGID: 8, Ports: []int{3001}, DataDir: "var/data"}}
	})
	svcs := r.store.Snapshot().Services
	if len(svcs) != 1 {
		t.Fatalf("services = %+v", svcs)
	}
	s := svcs[0]
	if s.Name != "node" || s.Version != "v22.4.0" || len(s.Ports) != 2 || !s.Stateful() {
		t.Fatalf("service = %+v", s)
	}
}

func TestAbsoluteDataDirIsDropped(t *testing.T) {
	r := newRig(t, "node")
	r.observe("node", func(res *executor.ExecResult) {
		res.Services = []executor.Service{{PGID: 7, DataDir: "/var/lib/pg"}}
	})
	if s := r.store.Snapshot().Services[0]; s.Stateful() {
		t.Fatalf("absolute data dir kept: %+v", s)
	}
}

func TestEnvNamesOnly(t *testing.T) {
	r := newRig(t, "node")
	r.observe("node", nil, "DATABASE_URL=postgres://u:hunter2@h/db", "TOKEN=s3cret")
	raw := readFile(t, r)
	if strings.Contains(raw, "hunter2") || strings.Contains(raw, "s3cret") {
		t.Fatalf("a value reached the file:\n%s", raw)
	}
	names := r.store.Snapshot().EnvVarNames
	if strings.Join(names, ",") != "DATABASE_URL,PATH,TOKEN" {
		t.Fatalf("names = %v", names)
	}
}

func TestWriteIsAtomicAndUnchangedIsNotRewritten(t *testing.T) {
	r := newRig(t, "node")
	r.observe("node", nil)
	dir := filepath.Dir(filepath.Join(r.root, filepath.FromSlash(Path)))
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "inventory.json" {
		t.Fatalf("temp files left behind: %v", entries)
	}
	before, _ := os.Stat(filepath.Join(dir, "inventory.json"))
	r.observe("node", nil)
	after, _ := os.Stat(filepath.Join(dir, "inventory.json"))
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("an identical observation rewrote the file")
	}
}

func TestPersistedFileHasNoAbsolutePathsAndVersionFirst(t *testing.T) {
	r := newRig(t, "node")
	r.observe("node", func(res *executor.ExecResult) {
		res.Services = []executor.Service{{PGID: 7, Ports: []int{1}, DataDir: "var/pg"}}
	})
	raw := []byte(readFile(t, r))
	// The fake tool's own PATH entries live in the test, not in the file.
	if bad := lawcheck.NoAbsolutePaths(raw); len(bad) > 0 {
		t.Fatalf("absolute paths: %q", bad)
	}
	if !lawcheck.VersionFirst([]byte(strings.Join(strings.Fields(string(raw)), ""))) {
		t.Fatalf("V not first:\n%s", raw)
	}
}

func TestRelativeScriptIsNotATool(t *testing.T) {
	r := newRig(t)
	r.observe("./run.sh", nil)
	if n := len(r.store.Snapshot().Tools); n != 0 {
		t.Fatalf("recorded %d tools for a cell script", n)
	}
}

func TestAnnotateTouchesOnlyAnnotations(t *testing.T) {
	r := newRig(t, "node")
	r.observe("node", nil)
	before := r.store.Snapshot()
	if err := r.store.Annotate("postgres", map[string]any{"optional": true}); err != nil {
		t.Fatal(err)
	}
	after := r.store.Snapshot()
	if after.Annotations["postgres"]["optional"] != true {
		t.Fatalf("annotation lost: %+v", after.Annotations)
	}
	after.Annotations = nil
	before.Annotations = nil
	if !equal(before, after) {
		t.Fatalf("Annotate changed another field:\n%+v\n%+v", before, after)
	}
}

func readFile(t *testing.T, r *rig) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.root, filepath.FromSlash(Path)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A tool run inside `bash -c` is the shell's child, and the inventory learns it
// from the shell call's command line at the seat's boundary.
func TestAToolRunByBashIsObserved(t *testing.T) {
	r := newRig(t, "jq")
	t.Setenv("PATH", r.bin)
	seat := executor.Watching(executor.Host, r.obs)
	args, _ := json.Marshal(map[string]string{"command": "cd sub && jq . a.json | wc -l"})
	err := seat.Around(context.Background(), executor.Call{Tool: "bash", Args: args}, func() ([]byte, bool) { return nil, false })
	if err != nil {
		t.Fatal(err)
	}
	tools := r.store.Snapshot().Tools
	if len(tools) != 1 || tools[0].Name != "jq" {
		t.Fatalf("the inventory holds %+v, want jq alone", tools)
	}
}
