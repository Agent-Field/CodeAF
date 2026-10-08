package main

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// factoryDoors is every door a seam can carry, by the name [factory.Seam.Has]
// answers to.
var factoryDoors = []string{
	"load", "launch", "stop", "pause", "dismiss", "answer", "steer", "signoff",
	"sendback", "reverify", "bank", "new", "sync", "askauthor", "setstage",
	"addstage", "setgate", "setcap", "seteffort",
}

// A launch with no store behind it hands the page the zero seam, whose page is
// the one dim line saying nothing is connected yet.
func TestFactorySeamWithNoStoreIsTheZeroSeam(t *testing.T) {
	t.Setenv(factoryFixtureEnv, "")
	seam := factorySeam(nil, "", "")
	for _, door := range factoryDoors {
		if seam.Has(door) {
			t.Fatalf("a seam over no store carries %q", door)
		}
	}
}

// The person's own floor has the doors the local seam can keep. In a process
// that does not run the floor (no runner here), the eight runner doors are
// asks to the process that does (factory_run.go's mailbox), and the doors
// nothing on this machine can keep are absent, so the page offers nothing it
// cannot do.
func TestFactorySeamOverAStoreIsTheLocalFloor(t *testing.T) {
	t.Setenv(factoryFixtureEnv, "")
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seam := factorySeam(st, "", "")
	for _, door := range []string{"load", "new", "dismiss", "setgate", "setstage", "setcap"} {
		if !seam.Has(door) {
			t.Fatalf("the floor over a store is missing %q", door)
		}
	}
	for _, door := range []string{"launch", "stop", "pause", "answer", "steer", "signoff", "sendback", "reverify"} {
		if !seam.Has(door) {
			t.Fatalf("the floor over a store has no %q to ask the runner through", door)
		}
	}
	for _, door := range []string{"sync", "askauthor"} {
		if seam.Has(door) {
			t.Fatalf("the floor over a store carries the engine door %q with nothing behind it", door)
		}
	}
	snap, err := seam.Load()
	if err != nil {
		t.Fatalf("an empty store did not load: %v", err)
	}
	if len(snap.Items) != 0 {
		t.Fatalf("an empty store loaded %d items", len(snap.Items))
	}
	// A floor with no items has no repos, so the store refuses work on none,
	// and the window's own workspace is the answer the page's `n` falls to.
	if _, err := seam.New("", "fix the ledger double count"); err == nil {
		t.Fatal("the store took work on no repository")
	}
	seam = factoryHere(seam, "/home/you/ledger")
	// And what `n` writes is what the next load reads back, on that repo.
	id, err := seam.New("", "fix the ledger double count, $8, plan first")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	snap, err = seam.Load()
	if err != nil || len(snap.Items) != 1 || snap.Items[0].ID != id {
		t.Fatalf("the item `n` made is not on the next load: %+v, %v", snap.Items, err)
	}
	if got := snap.Items[0].Repo; got != "ledger" {
		t.Fatalf("new work on an empty floor landed on %q, want the workspace's name", got)
	}
	// A repo the floor named is kept.
	if _, err := seam.New("acme/api", "tidy the logs"); err != nil {
		t.Fatalf("new on a named repo: %v", err)
	}
	snap, _ = seam.Load()
	repos := map[string]bool{}
	for _, it := range snap.Items {
		repos[it.Repo] = true
	}
	if !repos["acme/api"] {
		t.Fatalf("a named repo was overwritten by the workspace: %v", repos)
	}
}

// The fixture switch still wins over a store, so the page can be looked at
// with a full floor on a machine whose own floor is empty.
func TestFactoryFixtureWinsOverAStore(t *testing.T) {
	t.Setenv(factoryFixtureEnv, "1")
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seam := factorySeam(st, "", "")
	if seam.Has("new") {
		t.Fatal("the fixture switch was on and the store's own `new` door answered")
	}
	snap, err := seam.Load()
	if err != nil || len(snap.Items) == 0 {
		t.Fatalf("the fixture switch was on and the floor is not the fixture: %d items, %v", len(snap.Items), err)
	}
}

// A store that could not be opened is a nil door, spelled as the interface's
// own nil, so a typed-nil pointer never puts `factory_add` on the belt.
func TestFactoryDoorOfANilStoreIsNil(t *testing.T) {
	if door := factoryDoor(nil); door != nil {
		t.Fatalf("a nil store became a non-nil door: %#v", door)
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if factoryDoor(st) == nil {
		t.Fatal("an open store became no door")
	}
}

// The store lives in the v3 factory folder under the codeaf home, which is
// what the manual tells a person who asks where their items are saved.
func TestFactoryStoreIsUnderTheCodeafHome(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	st := v3Factory()
	if st == nil {
		t.Fatal("the factory store did not open under a writable home")
	}
	if got, want := st.Root(), home.Join("v3", "factory"); got != want {
		t.Fatalf("the factory store is at %q, want %q", got, want)
	}
}

// THE TOOL WAITS FOR A PERSON, so the shared assembly never hands it out: a
// headless launch, an engine's launch and a --once run all come out of
// [openV3Launch] with no factory door. Two doors set one afterwards: the
// in-process interactive door (chatv3.go), and the engine when the hello is a
// window on this machine ([engineFactoryHere], asserted below).
func TestFactoryDoorIsAbsentFromEveryAssembledConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv(config.ProfileDirEnv, "")
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	for _, door := range []string{"chat", "engine"} {
		proc, err := openV3Process(door)
		if err != nil {
			t.Fatalf("%s: the process did not open: %v", door, err)
		}
		t.Cleanup(proc.closeAll)
		for _, interactive := range []bool{false, true} {
			launch, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: t.TempDir(), Interactive: interactive, NoStandingTicks: true})
			if err != nil {
				t.Fatalf("%s: the launch did not open: %v", door, err)
			}
			if launch.Config.Factory != nil {
				t.Fatalf("%s (interactive %v): the shared assembly handed out a factory door", door, interactive)
			}
		}
	}
}

// THE ORDINARY LAUNCH CARRIES THE TOOL. Bare `codeaf` with a key runs its
// conversation in the session host, so the engine is where `factory_add` has to
// be: a window on this machine (the local dial's interactive shape) gets it, and
// a window over --host or --at (no shape), a --once probe (a shape that is not
// interactive) and a headless caller do not, because none of them can see the
// floor the tool would write to.
func TestTheEngineHandsTheFactoryDoorOnlyToAWindowOnThisMachine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEAF_HOME", filepath.Join(home, "state"))
	t.Setenv(config.ProfileDirEnv, "")
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv(factoryFixtureEnv, "")
	t.Chdir(home)
	freshEngineProcess(t)

	for _, c := range []struct {
		name  string
		hello remote.Hello
		want  bool
	}{
		{"a window on this machine", remote.Hello{Version: remote.Version, Launch: &remote.LaunchShape{Interactive: true}}, true},
		{"a window over --host or --at", remote.Hello{Version: remote.Version}, false},
		{"a --once probe", remote.Hello{Version: remote.Version, Launch: &remote.LaunchShape{Yolo: true}}, false},
		{"a headless caller", remote.Hello{Version: remote.Version, Headless: true, Launch: &remote.LaunchShape{Interactive: true}}, false},
	} {
		if got := engineFactoryHere(c.hello); got != c.want {
			t.Fatalf("%s: engineFactoryHere is %v, want %v", c.name, got, c.want)
		}
		c.hello.New = true
		engine, err := bootEngine(c.hello, "", "")
		if err != nil {
			t.Fatalf("%s: the engine door did not open: %v", c.name, err)
		}
		agent, ok := engine.Agent.(*session.Agent)
		if !ok {
			t.Fatalf("%s: the engine serves a %T, not a session agent", c.name, engine.Agent)
		}
		got := agent.ToolOnBelt("factory_add")
		gotRecipe := agent.ToolOnBelt("factory_recipe")
		gotItem := agent.ToolOnBelt("factory_item")
		_ = agent.Close()
		if got != c.want {
			t.Fatalf("%s: factory_add on the belt is %v, want %v", c.name, got, c.want)
		}
		if gotRecipe != c.want {
			t.Fatalf("%s: factory_recipe on the belt is %v, want %v", c.name, gotRecipe, c.want)
		}
		if gotItem != c.want {
			t.Fatalf("%s: factory_item on the belt is %v, want %v", c.name, gotItem, c.want)
		}
	}
	// And the store it writes to is this machine's own floor, the one the
	// page reads ([v3FactoryRoot]).
	if st := engineFactory(""); st == nil || st.Root() != v3FactoryRoot() {
		t.Fatalf("the engine's factory store is not this machine's floor: %v", st)
	}
}

// The dir function answers the workspace for its own folder name, a recorded
// checkout for a watched repository, and nothing for a stranger.
func TestFactoryRepoDirsRules(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetCheckout("acme/billing", "/src/billing"); err != nil {
		t.Fatal(err)
	}
	dir := factoryRepoDirs(st, "/home/you/ledger")
	if got := dir("ledger"); got != "/home/you/ledger" {
		t.Fatalf("workspace: %q", got)
	}
	if got := dir("billing"); got != "/src/billing" {
		t.Fatalf("recorded: %q", got)
	}
	if got := dir("acme/billing"); got != "/src/billing" {
		t.Fatalf("recorded full name: %q", got)
	}
	if got := dir("stranger"); got != "" {
		t.Fatalf("unknown: %q", got)
	}
}

// A workspace whose origin names a watched repository is recorded; one that
// names another repository is not.
func TestRecordWorkspaceCheckoutFromOrigin(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"acme/ledger"}); err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "git@github.com:acme/ledger.git"}} {
		if out, err := exec.Command("git", append([]string{"-C", ws}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if err := recordWorkspaceCheckoutNow(st, ws); err != nil {
		t.Fatal(err)
	}
	if got := st.CheckoutDir("ledger"); got != filepath.Clean(ws) {
		t.Fatalf("recorded %q, want %q", got, ws)
	}
	other := t.TempDir()
	exec.Command("git", "-C", other, "init", "-q").Run()
	exec.Command("git", "-C", other, "remote", "add", "origin", "https://github.com/acme/else").Run()
	_ = recordWorkspaceCheckoutNow(st, other)
	if got := st.CheckoutDir("else"); got != "" {
		t.Fatalf("unwatched repo recorded: %q", got)
	}
}

// The bank door is on the shipped seam only when a workspace gives it a folder.
func TestFactorySeamBankStagesDoor(t *testing.T) {
	t.Setenv(factoryFixtureEnv, "")
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !factorySeam(st, "/home/you/ledger", "").Has("bankstages") {
		t.Fatal("no bankstages with a workspace")
	}
	// The door is on whenever the dir function is wired, because a recorded
	// checkout can answer for a repository with no workspace; with neither,
	// the function answers "" and the bank refuses by name.
	if err := factorySeam(st, "", "").BankStages(1); err == nil {
		t.Fatal("bank accepted with no folder known")
	}
}
