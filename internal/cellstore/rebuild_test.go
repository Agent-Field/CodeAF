package cellstore

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/inventory"
)

// look is one screening of the rig's tree, answering what it left out.
func (r *guardRig) look(changed ...string) Screened {
	r.t.Helper()
	got, err := r.guard.Look(r.cell, r.tree, r.state(), changed, nil)
	if err != nil {
		r.t.Fatal(err)
	}
	return got
}

// folders is the paths a screening left out.
func folders(s Screened) []string { return append([]string(nil), s.paths()...) }

// commit makes the rig's tree a git repository holding everything in it.
func (r *guardRig) commit() {
	r.t.Helper()
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A", "-f"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "x"}} {
		if out, err := gitIn(r.tree, args...); err != nil {
			r.t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func (r *guardRig) npmProject() {
	r.write("package-lock.json", `{"lockfileVersion":3}`)
	r.write("node_modules/left-pad/index.js", "module.exports = 1\n")
}

func TestWithholdInstallFolderWithLockfile(t *testing.T) {
	r := newRig(t)
	r.npmProject()
	got := r.look()
	if want := []Left{{Path: "node_modules", Lock: "package-lock.json"}}; !reflect.DeepEqual(got.Folders, want) {
		t.Fatalf("left out %+v, want %+v", got.Folders, want)
	}
	if p := r.policy(); !reflect.DeepEqual(p.rebuilt, []string{"node_modules"}) || len(p.withheld) != 0 {
		t.Fatalf("policy %+v: the folder belongs in its own block and not the secrets block", p)
	}
	text, _ := os.ReadFile(filepath.Join(r.state(), policyName))
	if !strings.Contains(string(text), rebuiltTag+"\nexclude node_modules\n") {
		t.Fatalf("policy file:\n%s", text)
	}
}

func TestNoLockfileTravels(t *testing.T) {
	r := newRig(t)
	r.write("package.json", `{"name":"x"}`)
	r.write("node_modules/left-pad/index.js", "module.exports = 1\n")
	if got := r.look(); len(got.Folders) != 0 || len(r.policy().rebuilt) != 0 {
		t.Fatalf("a folder with no lockfile was left out: %+v", got.Folders)
	}
}

func TestLockfileNotCarriedFolderTravels(t *testing.T) {
	t.Run("the person excluded the lockfile", func(t *testing.T) {
		r := newRig(t)
		r.npmProject()
		r.write(".furrowpolicy", "exclude package-lock.json\n")
		if got := r.look(); len(got.Folders) != 0 {
			t.Fatalf("left out %+v though its lockfile does not travel", got.Folders)
		}
	})
	t.Run("the secret guard holds the lockfile back", func(t *testing.T) {
		r := newRig(t)
		r.npmProject()
		r.write("package-lock.json", `{"resolved":"https://x/`+fakeKey+`"}`)
		if got := r.look(); len(got.Folders) != 0 {
			t.Fatalf("left out %+v though its lockfile is held back as a secret", got.Folders)
		}
	})
}

func TestTrackedFilesUnderFolderTravel(t *testing.T) {
	r := newRig(t)
	r.npmProject()
	r.commit()
	if got := r.look(); len(got.Folders) != 0 {
		t.Fatalf("left out %+v though the project tracks files inside it", got.Folders)
	}
}

func TestNearestLockfileWins(t *testing.T) {
	r := newRig(t)
	r.write("package-lock.json", "{}")
	r.write("node_modules/a/i.js", "1")
	r.write("desktop/package-lock.json", "{}")
	r.write("desktop/node_modules/b/i.js", "1")
	r.write("tools/node_modules/c/i.js", "1")
	want := []Left{
		{"desktop/node_modules", "desktop/package-lock.json"},
		{"node_modules", "package-lock.json"},
		{"tools/node_modules", "package-lock.json"},
	}
	if got := r.look(); !reflect.DeepEqual(got.Folders, want) {
		t.Fatalf("left out %+v, want %+v", got.Folders, want)
	}
}

func TestWithholdWithoutGit(t *testing.T) {
	r := newRig(t)
	r.npmProject()
	if _, err := os.Stat(filepath.Join(r.tree, ".git")); err == nil {
		t.Fatal("the rig's tree is a repository")
	}
	if got := r.look(); !reflect.DeepEqual(folders(got), []string{"node_modules"}) {
		t.Fatalf("left out %v in a tree with no repository", folders(got))
	}
}

func TestRequirementsCountsOnlyWhenPinned(t *testing.T) {
	r := newRig(t)
	r.write("requirements.txt", "requests\nflask==3.0.0\n")
	r.write(".venv/lib/x.py", "1")
	if got := r.look(); len(got.Folders) != 0 {
		t.Fatalf("an unpinned requirements file licensed %+v", got.Folders)
	}
	r.write("requirements.txt", "# pins\nrequests==2.32.0\nflask==3.0.0  # web\n")
	if got := r.look(); !reflect.DeepEqual(got.Folders, []Left{{".venv", "requirements.txt"}}) {
		t.Fatalf("a fully pinned requirements file left out %+v", got.Folders)
	}
}

func TestLockChangeKeepsFolderOut(t *testing.T) {
	r := newRig(t)
	r.npmProject()
	first := r.look()
	r.write("package-lock.json", `{"lockfileVersion":3,"packages":{"node_modules/left-pad":{}}}`)
	again := r.look("package-lock.json")
	if !reflect.DeepEqual(first.Folders, again.Folders) {
		t.Fatalf("a dependency added between seals moved the folder: %+v then %+v", first.Folders, again.Folders)
	}
}

// A folder that a previous seal left out stays out even where it is not on the
// disk: that is the machine a chat moved to, and the folder a setup turn is
// about to bring back.
func TestAbsentFolderStaysKnownUntilItsLockIsGone(t *testing.T) {
	r := newRig(t)
	r.npmProject()
	r.look()
	if err := os.RemoveAll(filepath.Join(r.tree, "node_modules")); err != nil {
		t.Fatal(err)
	}
	if got := r.look("app.js"); !reflect.DeepEqual(folders(got), []string{"node_modules"}) {
		t.Fatalf("left out %v after the folder left the machine", folders(got))
	}
	if err := os.Remove(filepath.Join(r.tree, "package-lock.json")); err != nil {
		t.Fatal(err)
	}
	if got := r.look("package-lock.json"); len(got.Folders) != 0 {
		t.Fatalf("still left out %+v with no lockfile to rebuild it", got.Folders)
	}
}

func TestPersonExcludeIsNotRecorded(t *testing.T) {
	c := newCell(t)
	tree := t.TempDir()
	g := Guard{Home: t.TempDir(), Notify: func(string) {}}
	for rel, body := range map[string]string{".furrowpolicy": "exclude node_modules\n", "package-lock.json": "{}", "node_modules/x/i.js": "1"} {
		writeTree(t, tree, map[string]string{rel: body}, 0o644)
	}
	got, err := g.Look(c, tree, stateDir(c), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := noteLeftOut(c, c.Root, got, nil); err != nil {
		t.Fatal(err)
	}
	inv, _ := inventory.Open(c.Root)
	if p, _ := readPolicy(tree, stateDir(c)); len(p.rebuilt) != 0 || len(got.Folders) != 0 || len(inv.Snapshot().Withheld) != 0 {
		t.Fatalf("the person's own exclusion was taken over: policy %+v, folders %+v, record %+v", p, got.Folders, inv.Snapshot().Withheld)
	}
}

func TestFailedInstallStillLeftOut(t *testing.T) {
	c := newCell(t)
	folders := []Left{{Path: "node_modules", Lock: "package-lock.json"}}
	failed := Executed{Call: Call{Exit: 1}, Command: "npm ci"}
	if err := noteLeftOut(c, c.Root, Screened{Folders: folders}, []Executed{failed}); err != nil {
		t.Fatal(err)
	}
	inv, _ := inventory.Open(c.Root)
	want := []inventory.Withheld{{Path: "node_modules", Lock: "package-lock.json"}}
	if got := inv.Snapshot().Withheld; !reflect.DeepEqual(got, want) {
		t.Fatalf("record %+v, want the folder named with no maker: a half-finished install is rebuilt from the lock as well", got)
	}
}

func TestMadeByNamesTheCommandThatWroteTheFolder(t *testing.T) {
	c := newCell(t)
	left := []Left{{Path: "web/node_modules", Lock: "web/package-lock.json"}}
	install := Executed{Call: Call{Tool: "bash"}, Command: "cd web && API_KEY=sk-abcdefghijklmnopqrstuvwx npm ci"}
	read := Executed{Call: Call{Tool: "read"}}
	if err := noteLeftOut(c, c.Root, Screened{Folders: left}, []Executed{install, read}); err != nil {
		t.Fatal(err)
	}
	inv, _ := inventory.Open(c.Root)
	got := inv.Snapshot().Withheld[0]
	if got.MadeBy != "cd web && API_KEY=… npm ci" {
		t.Fatalf("made_by %q: the newest command that could have written it, cleaned of secrets", got.MadeBy)
	}
	// A later seal that names no maker keeps the first one.
	if err := noteLeftOut(c, c.Root, Screened{Folders: left}, []Executed{read}); err != nil {
		t.Fatal(err)
	}
	inv2, _ := inventory.Open(c.Root)
	if again := inv2.Snapshot().Withheld[0]; again.MadeBy != got.MadeBy {
		t.Fatalf("made_by %q after a seal that saw nothing", again.MadeBy)
	}
}

// One seal through the whole path: a folder is never left out without the record
// saying so in the same seal.
func TestWithheldAlwaysRecorded(t *testing.T) {
	c := newCell(t)
	fake := &fakeEngine{}
	e := fake.engine(t)
	writeTree(t, c.Root, map[string]string{
		"package-lock.json": "{}", "node_modules/a/i.js": "1",
		"web/package-lock.json": "{}", "web/node_modules/b/i.js": "1",
	}, 0o644)
	if _, err := e.Seal(context.Background(), c, TurnInfo{Calls: []Executed{exec1("bash", "")}}); err != nil {
		t.Fatal(err)
	}
	policy, err := readPolicyAt(c.Root)
	if err != nil {
		t.Fatal(err)
	}
	inv, _ := inventory.Open(c.Root)
	var recorded []string
	for _, w := range inv.Snapshot().Withheld {
		recorded = append(recorded, w.Path)
	}
	if len(policy.rebuilt) != 2 || !reflect.DeepEqual(policy.rebuilt, recorded) {
		t.Fatalf("left out %v but the record names %v", policy.rebuilt, recorded)
	}
	if locks := inv.Snapshot().Lockfiles; !reflect.DeepEqual(locks, []string{"package-lock.json", "web/package-lock.json"}) {
		t.Fatalf("lockfiles %v", locks)
	}
}

// The engine's own behaviour, on the real binary: a folder left out is not in
// the snapshot, and a local copy of it is never deleted by a restore or a rewind.
func TestTakeBackLeavesLocalInstallFolder(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		e, c := realEngine(t), newCell(t)
		first := sealStep(t, e, c, map[string]string{"package-lock.json": "{}", "node_modules/a/i.js": "mine\n", "src/a.txt": "one\n"}, `{"n":1}`)
		writeTree(t, c.Root, map[string]string{"src/a.txt": "damaged\n"}, 0o644)
		if err := e.Restore(context.Background(), c, first.Turn.ID, nil); err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(filepath.Join(c.Root, "node_modules/a/i.js")); string(got) != "mine\n" {
			t.Fatalf("the machine's own install folder was changed by a restore: %q", got)
		}
		if got, _ := os.ReadFile(filepath.Join(c.Root, "src/a.txt")); string(got) != "one\n" {
			t.Fatalf("the restore did not restore: %q", got)
		}
	})
}

func TestRewindKeepsLeftOutFolder(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		e, c := realEngine(t), newCell(t)
		first := sealStep(t, e, c, map[string]string{"package-lock.json": "{}", "node_modules/a/i.js": "mine\n", "a.txt": "one\n"}, `{"n":1}`)
		sealStep(t, e, c, map[string]string{"a.txt": "two\n"}, `{"n":2}`)
		mustRewind(t, e, c, first.Turn.ID)
		if got, _ := os.ReadFile(filepath.Join(c.Root, "node_modules/a/i.js")); string(got) != "mine\n" {
			t.Fatalf("a rewind changed the left-out folder: %q", got)
		}
		if got, _ := os.ReadFile(filepath.Join(c.Root, "a.txt")); string(got) != "one\n" {
			t.Fatalf("the rewind did not rewind: %q", got)
		}
	})
}

// What a snapshot holds is what a machine that receives it has: a tree damaged
// down to nothing and restored comes back without the folder that was left out.
func TestSnapshotDoesNotHoldTheLeftOutFolder(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		e, c := realEngine(t), newCell(t)
		first := sealStep(t, e, c, map[string]string{"package-lock.json": "{}", "node_modules/a/i.js": "mine\n", "src/a.txt": "one\n"}, `{"n":1}`)
		for _, p := range []string{"node_modules", "src"} {
			if err := os.RemoveAll(filepath.Join(c.Root, p)); err != nil {
				t.Fatal(err)
			}
		}
		if err := e.Restore(context.Background(), c, first.Turn.ID, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(c.Root, "node_modules")); err == nil {
			t.Fatal("the folder that was left out came back from the snapshot")
		}
		if _, err := os.Stat(filepath.Join(c.Root, "src/a.txt")); err != nil {
			t.Fatalf("what was not left out did not come back: %v", err)
		}
	})
}

// A setup turn that brings the folder back does not put it into the next seal.
func TestRestoredFolderStaysOut(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		e, c := realEngine(t), newCell(t)
		sealStep(t, e, c, map[string]string{"package-lock.json": "{}", "node_modules/a/i.js": "1", "a.txt": "1"}, `{"n":1}`)
		if err := os.RemoveAll(filepath.Join(c.Root, "node_modules")); err != nil {
			t.Fatal(err)
		}
		sealStep(t, e, c, map[string]string{"a.txt": "2"}, `{"n":2}`)
		sealStep(t, e, c, map[string]string{"node_modules/a/i.js": "rebuilt", "node_modules/b/i.js": "rebuilt"}, `{"n":3}`)
		policy, _ := readPolicyAt(c.Root)
		if !reflect.DeepEqual(policy.rebuilt, []string{"node_modules"}) {
			t.Fatalf("policy %+v: the rebuilt folder was taken back into the seal", policy)
		}
	})
}

// folderAged makes a folder in the tree and dates its last change.
func folderAged(t *testing.T, tree, folder string, at time.Time) {
	t.Helper()
	dir := filepath.Join(tree, folder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dir, at, at); err != nil {
		t.Fatal(err)
	}
}

func madeByAfterOneCall(t *testing.T, folderAt time.Time, call Executed) inventory.Withheld {
	t.Helper()
	c := newCell(t)
	folderAged(t, c.Root, "node_modules", folderAt)
	left := []Left{{Path: "node_modules", Lock: "package-lock.json"}}
	if err := noteLeftOut(c, c.Root, Screened{Folders: left}, []Executed{call}); err != nil {
		t.Fatal(err)
	}
	inv, _ := inventory.Open(c.Root)
	return inv.Snapshot().Withheld[0]
}

func TestFolderThatPredatesTheChatNamesNoMaker(t *testing.T) {
	started := time.Now()
	first := Executed{Call: Call{Tool: "bash", Started: started.UnixMilli()}, Command: "echo edited >> README.md"}
	got := madeByAfterOneCall(t, started.Add(-time.Hour), first)
	if got.MadeBy != "" || got.Cwd != "" {
		t.Fatalf("record %+v: the folder was there before the first command, so no command made it", got)
	}
}

func TestFolderCreatedDuringTheChatNamesItsCommand(t *testing.T) {
	started := time.Now().Add(-time.Minute)
	install := Executed{Call: Call{Tool: "bash", Started: started.UnixMilli()}, Command: "npm ci"}
	got := madeByAfterOneCall(t, started.Add(time.Second), install)
	if got.MadeBy != "npm ci" {
		t.Fatalf("made_by %q, want the command that ran while the folder came to be", got.MadeBy)
	}
}
