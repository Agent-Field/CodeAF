package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/tui3"
)

// The remote agent IS the surface's agent, checked at compile time so a method
// added to tui3.Agent breaks the build here rather than at the first keystroke
// of a remote session.
var _ tui3.Agent = (*remote.Agent)(nil)

func TestParseHostTarget(t *testing.T) {
	cases := []struct {
		raw       string
		dest      string
		workspace string
		fails     bool
	}{
		{raw: "devbox", dest: "devbox"},
		{raw: "me@devbox", dest: "me@devbox"},
		{raw: "devbox:code/app", dest: "devbox", workspace: "code/app"},
		{raw: "devbox:/srv/app", dest: "devbox", workspace: "/srv/app"},
		{raw: "me@devbox:/srv/app", dest: "me@devbox", workspace: "/srv/app"},
		// A trailing colon is "the engine's own home", said out loud.
		{raw: "devbox:", dest: "devbox"},
		// localhost is a host like any other — it is also the rig this feature is
		// tested end to end on, and nothing may special-case it.
		{raw: "localhost", dest: "localhost"},
		{raw: "localhost:code/app", dest: "localhost", workspace: "code/app"},
		// A path with its own colon in it belongs to the workspace: the FIRST
		// colon is the split and the rest is the path as typed.
		{raw: "devbox:code/a:b", dest: "devbox", workspace: "code/a:b"},
		{raw: "  devbox:code/app  ", dest: "devbox", workspace: "code/app"},
		{raw: "", fails: true},
		{raw: ":/srv/app", fails: true},
	}
	for _, c := range cases {
		dest, workspace, err := parseHostTarget(c.raw)
		if c.fails {
			if err == nil {
				t.Fatalf("parseHostTarget(%q) was accepted", c.raw)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseHostTarget(%q): %v", c.raw, err)
		}
		if dest != c.dest || workspace != c.workspace {
			t.Fatalf("parseHostTarget(%q) = %q, %q; want %q, %q", c.raw, dest, workspace, c.dest, c.workspace)
		}
	}
}

// THE WORKSPACE IS NEVER RESOLVED HERE. A relative path stays relative, because
// the engine resolves it against its own home and this machine has no standing
// to make it absolute.
func TestParseHostTargetLeavesTheWorkspaceAsTyped(t *testing.T) {
	_, workspace, err := parseHostTarget("devbox:code/app")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(workspace, "/") {
		t.Fatalf("the workspace was made absolute: %q", workspace)
	}
}

func TestFlagsThatCannotTravelAreRefusedRatherThanIgnored(t *testing.T) {
	for _, launch := range []hostLaunch{
		{target: "devbox", yolo: true},
		{target: "devbox", noCompact: true},
		{target: "devbox:app", yolo: true, noCompact: true},
	} {
		err := launch.check()
		if err == nil {
			t.Fatalf("%+v was accepted silently", launch)
		}
		if !strings.Contains(err.Error(), "devbox") {
			t.Fatalf("the refusal does not name the machine to set it on: %v", err)
		}
	}
	if err := (hostLaunch{target: "devbox"}).check(); err != nil {
		t.Fatalf("an ordinary launch was refused: %v", err)
	}
}

func TestShellQuoteSurvivesAPathWithSpacesAndQuotes(t *testing.T) {
	if got := shellQuote("code/my app"); got != `'code/my app'` {
		t.Fatalf("shellQuote = %s", got)
	}
	if got := shellQuote("it's"); got != `'it'\''s'` {
		t.Fatalf("shellQuote = %s", got)
	}
}

func TestMissingCommandIsRecognizedInEveryShellsWording(t *testing.T) {
	for _, said := range []string{
		"bash: aforge: command not found",
		"sh: 1: aforge: not found",
		"zsh: command not found: aforge",
	} {
		if !mentionsMissingCommand(said) {
			t.Fatalf("not recognized as a missing aforge: %q", said)
		}
	}
	if mentionsMissingCommand("Permission denied (publickey).") {
		t.Fatal("an ssh refusal was read as a missing aforge")
	}
}

// The flag exists and is documented in exactly one place: `aforge chat -h`.
func TestHostFlagIsOnTheChatUsage(t *testing.T) {
	err := openChatV3("chat", []string{"--help"}, false)
	if err == nil {
		return
	}
	if !strings.Contains(err.Error(), "flag: help requested") {
		t.Fatalf("chat --help = %v", err)
	}
}

// ── the ambient side over a connection ──────────────────────────────────────

// THE ENGINE DOOR BUILDS THE AMBIENT SIDE, which is what puts the `stand` tool
// on the belt over --host: internal/session leaves it off entirely when
// Config.Standing has no store behind it (its tools_standing.go), so a nil here
// is a remote conversation that cannot be asked to keep an eye on anything and
// says so rather than trying.
//
// The two wire doors are asserted beside it because they are the same fact from
// the surface's side — the band and the pause key read the ENGINE machine's
// store, and a closure that was never filled is a refusal on the wire.
func TestTheEngineDoorKeepsTheAmbientSideOnOverAConnection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AFORGE_HOME", filepath.Join(home, "state"))
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Chdir(home)

	engine, err := bootEngine(remote.Hello{Version: remote.Version}, "", "")
	if err != nil {
		t.Fatalf("the engine door did not open: %v", err)
	}
	defer func() { _ = engine.Agent.Close() }()

	// The doors are built from the config's own seam ([engineStandingItems]
	// hands back nil for a nil one), so a door that is here is proof that
	// bootEngine's Config.Standing was filled and `stand` reached the belt.
	if engine.StandingItems == nil || engine.StandingSave == nil {
		t.Fatal("the engine serves no standing doors, so a remote surface has no band and no pause key")
	}
	// The seam the session itself proposes through. It is read back off the
	// launch the engine assembles from, because bootEngine hands the config to
	// the agent and keeps none of it. The engine's door word rides on the
	// PROCESS now rather than on the launch's options (chatv3_process.go).
	proc, err := openV3Process("engine")
	if err != nil {
		t.Fatalf("the process every v3 door builds once did not open: %v", err)
	}
	t.Cleanup(proc.closeAll)
	launch, err := openV3Launch(proc, v3Options{Workspace: home})
	if err != nil {
		t.Fatalf("the shared assembly did not open: %v", err)
	}
	if launch.Config.Standing == nil || launch.Config.Standing.Store == nil {
		t.Fatal("the engine door built no standing seam, so `stand` is off the belt over --host")
	}
	// AND THE STORE IS THE ENGINE MACHINE'S OWN, under its state root and not
	// under any path a surface could have sent it.
	if root := launch.Config.Standing.Store.Root(); root != v3StandingRoot() {
		t.Fatalf("the engine's store is at %q, want this machine's own %q", root, v3StandingRoot())
	}
	// The doors answer that same store: an item written through Save comes back
	// out of Items for its own workspace.
	item := standing.Item{
		Words:     "tell me when the build breaks",
		Workspace: home,
		When:      standing.When{Kind: standing.WhenEvery, Words: "every hour", Every: "1h"},
		Does:      standing.Action{Kind: standing.ActionSay, Say: "the build broke"},
		Rails:     standing.Rails{PerRunUSD: 0.02, MaxPerDay: 4},
	}
	stood, err := launch.Config.Standing.Store.Create(item)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	found, err := engine.StandingItems(home)
	if err != nil || len(found) != 1 || found[0].ID != stood.ID {
		t.Fatalf("the engine's items door answered %+v, %v", found, err)
	}
	stood.Status = standing.StatusPaused
	if err := engine.StandingSave(stood); err != nil {
		t.Fatalf("the engine's save door refused a paused item: %v", err)
	}
	if again, err := engine.StandingItems(home); err != nil || len(again) != 1 || again[0].Status != standing.StatusPaused {
		t.Fatalf("the write did not land: %+v, %v", again, err)
	}
}

// AND THE SURFACE IS HANDED IT, with the two fields that would be about the
// wrong machine left out. This is the door's half: what hostOptions wires is
// what a person over --host actually gets.
func TestTheHostDoorWiresTheStandingSeamAndNothingAboutThisMachine(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	options := hostOptions(nil, nil, "devbox", remote.Welcome{Version: remote.Version, Workspace: "/srv/app"}, false)
	if options.Standing.Items == nil || options.Standing.Save == nil {
		t.Fatal("the door hands over no standing seam, so a remote surface has no rows and no pause key")
	}
	// Running: nothing on the far machine's disk says "firing at this instant",
	// so nil answers no for everything and no row ever wears `●`.
	if options.Standing.Running != nil {
		t.Fatal("the door claims it can tell whether an item is firing on another machine")
	}
	// Watch: the OS timer is the engine's, and a status read off this laptop's
	// launchd would be a line about the wrong machine.
	if options.Standing.Watch != nil {
		t.Fatal("the door answers `keeping watch` from this machine's own timer")
	}
	// StandingRoot is the LOCAL errand and exchange folder, and there is no
	// errand over a connection.
	if options.StandingRoot != "" {
		t.Fatalf("the door pointed the surface at %q, a folder on the wrong machine", options.StandingRoot)
	}
}

// THE READ MUST NOT BLOCK, and that is the whole reason [hostStanding] exists.
// The reader over --host is the status line's `keeping an eye on` segment — home
// does not open on a remote session — and that segment is asked on every frame,
// while a wire call has a ten-second deadline behind it. So this holds the far
// end still and asks anyway.
func TestTheHostStandingSeamAnswersFromItsCacheWithoutBlocking(t *testing.T) {
	held := make(chan struct{})
	asked := make(chan string, 8)
	stands := &hostStanding{
		ask: func(workspace string) ([]standing.Item, error) {
			asked <- workspace
			<-held
			return []standing.Item{{ID: "01HQ", Words: "watch CI", Workspace: workspace}}, nil
		},
		put:      func(standing.Item) error { return nil },
		items:    map[string][]standing.Item{},
		read:     map[string]time.Time{},
		fetching: map[string]bool{},
	}

	// The first reading has nothing to answer with and answers nothing, at once.
	if items := stands.list("/srv/app"); items != nil {
		t.Fatalf("the first reading answered %+v, want nothing yet", items)
	}
	// An absent segment for one beat is the honest order: a surface that guessed
	// a count would have to guess wrong first.
	if !waitFor(func() bool { return len(asked) == 1 }) {
		t.Fatal("the first reading started no fetch at all")
	}
	// AND SO DOES EVERY READING WHILE THAT FETCH IS STILL OUT THERE. This is the
	// frame that would otherwise be a terminal that stopped repainting.
	start := time.Now()
	for range 50 {
		stands.list("/srv/app")
	}
	if waited := time.Since(start); waited > 2*time.Second {
		t.Fatalf("fifty readings took %s while the far end said nothing", waited)
	}
	// ONE FETCH AND NEVER A PILE. Fifty more readings of a workspace nobody has
	// answered for yet is still one question on the wire.
	if len(asked) != 1 {
		t.Fatalf("%d fetches were started for one workspace", len(asked))
	}
	close(held)
	if !waitFor(func() bool { return len(stands.list("/srv/app")) == 1 }) {
		t.Fatal("the answer never reached the cache")
	}

	// A workspace nobody asked about gets its own fetch, and an empty workspace
	// gets none: there is no such place to ask about.
	if items := stands.list("   "); items != nil {
		t.Fatalf("an empty workspace answered %+v", items)
	}
}

// A WRITE THAT LANDED IS NOT READ BACK STALE. The engine accepted this exact
// document, so what is held is corrected with it rather than left showing the
// version the key was pressed on — and the entry is aged out so the next beat
// still asks the store what it really thinks. (The keys that call this are
// home's, and home does not open over --host today; the door is proved here so
// that it is the surface's opening that is missing and not this.)
func TestSavingOneItemOverTheWireRefreshesWhatHomeDraws(t *testing.T) {
	item := standing.Item{ID: "01HQ", Words: "watch CI", Workspace: "/srv/app", Status: standing.StatusActive}
	var written []standing.Item
	stands := &hostStanding{
		ask:      func(string) ([]standing.Item, error) { return []standing.Item{item}, nil },
		put:      func(saved standing.Item) error { written = append(written, saved); return nil },
		items:    map[string][]standing.Item{"/srv/app": {item}},
		read:     map[string]time.Time{"/srv/app": time.Now()},
		fetching: map[string]bool{},
	}
	paused := item
	paused.Status = standing.StatusPaused
	if err := stands.save(paused); err != nil {
		t.Fatalf("save: %v", err)
	}
	if len(written) != 1 || written[0].Status != standing.StatusPaused {
		t.Fatalf("the item did not travel: %+v", written)
	}
	held := stands.list("/srv/app")
	if len(held) != 1 || held[0].Status != standing.StatusPaused {
		t.Fatalf("the row would have redrawn as %+v", held)
	}
}

// A REFUSED WRITE IS THE STORE'S OWN SENTENCE AND NOTHING IS CHANGED HERE. Home
// prints it on its message line, and a cache that had already recorded the pause
// would be this screen disagreeing with the other machine's disk.
func TestARefusedRemoteWriteLeavesTheRowAsItWas(t *testing.T) {
	item := standing.Item{ID: "01HQ", Words: "watch CI", Workspace: "/srv/app", Status: standing.StatusActive}
	stands := &hostStanding{
		ask:      func(string) ([]standing.Item, error) { return []standing.Item{item}, nil },
		put:      func(standing.Item) error { return errors.New("an item needs a per-run budget") },
		items:    map[string][]standing.Item{"/srv/app": {item}},
		read:     map[string]time.Time{"/srv/app": time.Now()},
		fetching: map[string]bool{},
	}
	paused := item
	paused.Status = standing.StatusPaused
	err := stands.save(paused)
	if err == nil || err.Error() != "an item needs a per-run budget" {
		t.Fatalf("save = %v, want the store's own refusal", err)
	}
	if held := stands.list("/srv/app"); len(held) != 1 || held[0].Status != standing.StatusActive {
		t.Fatalf("a refused write changed the row anyway: %+v", held)
	}
}

// A FAILING LINK KEEPS THE LAST LIST. The two ways a fetch fails are a
// connection that died and an engine with no ambient side; neither of them is
// the news "the things you set up are gone", so the count holds what it had
// rather than dropping to nothing and taking the segment off the line.
func TestALostConnectionDoesNotEmptyTheItemBand(t *testing.T) {
	item := standing.Item{ID: "01HQ", Words: "watch CI", Workspace: "/srv/app"}
	stands := &hostStanding{
		ask:      func(string) ([]standing.Item, error) { return nil, errors.New("the connection to devbox is gone") },
		put:      func(standing.Item) error { return nil },
		items:    map[string][]standing.Item{"/srv/app": {item}},
		read:     map[string]time.Time{},
		fetching: map[string]bool{},
	}
	if held := stands.list("/srv/app"); len(held) != 1 {
		t.Fatalf("the seam answered %+v before the fetch even failed", held)
	}
	if !waitFor(func() bool {
		stands.mu.Lock()
		defer stands.mu.Unlock()
		return !stands.fetching["/srv/app"] && !stands.read["/srv/app"].IsZero()
	}) {
		t.Fatal("the failing fetch never finished")
	}
	if held := stands.list("/srv/app"); len(held) != 1 || held[0].ID != "01HQ" {
		t.Fatalf("a failed round trip emptied the list: %+v", held)
	}
}

// waitFor polls a condition rather than sleeping on it: the fetch it is waiting
// for is a goroutine, and a fixed sleep is either a slow test or a flaky one.
func waitFor(done func() bool) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}
