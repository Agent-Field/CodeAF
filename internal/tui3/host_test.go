package tui3

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// hostLab is the surface opened on a session that is running somewhere else:
// the workspace is the FAR machine's path, and the host is the name the person
// typed after --host.
func hostLab(t *testing.T) (*app, *fakeAgent) {
	t.Helper()
	agent := &fakeAgent{model: "vendor/model"}
	a := newApp(context.Background(), Options{
		Agent:     agent,
		Host:      "devbox",
		Workspace: "/srv/code/app",
	})
	// Wide, for exportLab's reason: what these tests read back is a line with a
	// path in it, and an assertion that had to know where it wrapped would be a
	// test of the renderer's fold.
	a.width, a.height = 160, 24
	a.pal = newPalette(tokens.ANSI256, false)
	a.tmux, a.remote = false, false
	a.tilde = "/home/dev"
	a.entries = nil
	a.welcome = welcome{spent: true}
	a.touch()
	return a, agent
}

// ── 1. THE CONNECTION IS THE PLACE ──────────────────────────────────────────

func TestThePlaceNamesTheMachineInFrontOfThePath(t *testing.T) {
	a, _ := hostLab(t)
	// The path is abbreviated exactly as a local one is — leading segments to
	// their initials — and the machine rides in front of the result.
	if got := a.placePath(0); got != "devbox:/s/c/app" {
		t.Fatalf("placePath = %q", got)
	}
	// The abbreviation eats the PATH and never the machine: which machine is the
	// half a person cannot reconstruct from anything else on the screen.
	if got := a.placePath(2); got != "devbox:app" {
		t.Fatalf("placePath at the hardest strength = %q", got)
	}
	// And the sheet's own place row is where a person now reads it, since the
	// legend keeps the machine while the status line owns the conversation name.
	if got := deckValue(a.deckItems(), "place"); got != "devbox:/s/c/app" {
		t.Fatalf("the sheet's place row = %q", got)
	}
}

// TestTheLegendNamesTheMachineAsItsOwnSegment pins how a connection reaches the
// border under the input now that the path has left it: the machine leads, and
// it leads with the legend's own separator rather than with the path's colon.
func TestTheLegendNamesTheMachineAsItsOwnSegment(t *testing.T) {
	a, _ := hostLab(t)
	a.title = "porting the parser"
	line := plain(a.legend(a.width))
	if !strings.Contains(line, "devbox") || strings.Contains(line, "porting the parser") {
		t.Fatalf("the legend does not carry only the machine: %q", line)
	}
	if strings.Contains(line, "/s/c/app") {
		t.Fatalf("the legend is still carrying the path: %q", line)
	}
	// Unnamed, the machine is still the whole of what the border can say — the
	// branch probe is off over a connection, so there is nothing else true.
	a.title = ""
	if got, _ := a.legendLeft(a.width, legendRoom(a.width, "")); got != "devbox" {
		t.Fatalf("an unnamed remote legend = %q", got)
	}
}

func TestTheStatusLinesPlaceCarriesTheMachine(t *testing.T) {
	a, _ := hostLab(t)
	if a.place != "devbox:app" {
		t.Fatalf("place = %q, want the machine and the directory", a.place)
	}
	name, _ := a.identityParts()
	if !strings.HasPrefix(plain(name), "devbox:app") {
		t.Fatalf("the status line's identity = %q", plain(name))
	}
}

func TestALocalSessionSaysNothingAboutAMachine(t *testing.T) {
	agent := &fakeAgent{model: "vendor/model"}
	a := newApp(context.Background(), Options{Agent: agent, Workspace: "/home/dev/src/app"})
	a.width, a.height = 160, 24
	a.pal = newPalette(tokens.ANSI256, false)
	a.tilde = "/home/dev"
	if a.hosted() {
		t.Fatal("a local session thinks it is hosted")
	}
	if got := a.placePath(0); got != "~/s/app" {
		t.Fatalf("placePath = %q — a local session must render exactly as it always did", got)
	}
	if a.place != "app" {
		t.Fatalf("place = %q", a.place)
	}
	a.title = "porting the parser"
	// The legend gave the name to the status line (render.go): a local session
	// with no machine and no branch has a legend with nothing on its left.
	if got, _ := a.legendLeft(a.width, legendRoom(a.width, "")); got != "" {
		t.Fatalf("a local legend = %q — neither a machine nor a name belongs on it", got)
	}
}

func TestStatusCarriesTheWholeTruthAboutWhichDiskIsWhose(t *testing.T) {
	a, _ := hostLab(t)
	a.file = "/srv/.sessions/20260817-150405_a3f2.jsonl"
	said := a.statusText()
	if !strings.Contains(said, "devbox:/srv/code/app") {
		t.Fatalf("/status does not carry the full place: %s", said)
	}
	if !strings.Contains(said, "devbox:/srv/.sessions/20260817-150405_a3f2.jsonl") {
		t.Fatalf("/status does not say whose disk the journal is on: %s", said)
	}
}

// ── 2. THE BRANCH PROBE IS OFF ──────────────────────────────────────────────

func TestTheBranchProbeDoesNotRunAgainstAPathOnAnotherMachine(t *testing.T) {
	a, _ := hostLab(t)
	if a.gitProbe != nil {
		t.Fatal("the git probe is still wired over --host")
	}
	if cmd := a.probeGit(); cmd != nil {
		t.Fatal("probeGit produced work over --host")
	}
	a.title = "porting the parser"
	// The machine stays on the legend; the name lives on the status line now.
	if got, _ := a.legendLeft(a.width, legendRoom(a.width, "")); got != "devbox" {
		t.Fatalf("the legend grew a branch or a name: %q", got)
	}
}

// deckValue is one row of the status sheet, by its label — the sheet is where
// the workspace path went when the legend gave its left end to the
// conversation's name.
func deckValue(items []deckItem, label string) string {
	for _, item := range items {
		if item.label == label {
			return item.value
		}
	}
	return ""
}

// ── 3. WHAT CANNOT WORK SAYS SO ─────────────────────────────────────────────

func TestConnectSaysWhyItCannotOverHost(t *testing.T) {
	a, _ := hostLab(t)
	a.openConnect()
	said := strings.Join(plainRows(a), "\n")
	if !strings.Contains(said, "not available over --host") {
		t.Fatalf("/connect did not state the fact: %s", said)
	}
	if a.connPanel.open {
		t.Fatal("the accounts panel opened over --host")
	}
}

func TestABrowserSignInOffersOnlyNotNow(t *testing.T) {
	a, _ := hostLab(t)
	a.askConnect(session.Event{Kind: session.EventConnectAsk, ConnectID: "1", Service: "google", ServiceName: "Google"})
	rows := a.connectAskRows(a.width)
	block := plain(strings.Join(rows, "\n"))
	if !strings.Contains(block, connectAskRemoteWord) {
		t.Fatalf("the card does not say what is wrong: %s", block)
	}
	if strings.Contains(block, "[enter]") {
		t.Fatalf("the card still offers a key that cannot work: %s", block)
	}
	// And the key itself does nothing rather than answering in somebody's name.
	a.connectAskKey(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if len(a.connAsks) != 1 {
		t.Fatal("y answered a browser sign-in over --host")
	}
	a.connectAskKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if len(a.connAsks) != 0 {
		t.Fatal("esc did not decline")
	}
}

func TestAKeySignInStillWorksOverHost(t *testing.T) {
	a, _ := hostLab(t)
	a.askConnect(session.Event{Kind: session.EventConnectAsk, ConnectID: "1", Service: "notion", ServiceName: "Notion", NeedsKey: true})
	block := plain(strings.Join(a.connectAskRows(a.width), "\n"))
	if strings.Contains(block, connectAskRemoteWord) {
		t.Fatalf("a key sign-in was refused, and a key needs no browser: %s", block)
	}
	if !strings.Contains(block, "[enter]") {
		t.Fatalf("a key sign-in lost its offer: %s", block)
	}
	a.connectAskKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !a.entering() {
		t.Fatal("enter did not open the key box over --host")
	}
}

func TestABrowserSignInWithAnAddressAnswerIsStillRefusedOverHost(t *testing.T) {
	a, _ := hostLab(t)
	a.askConnect(askDatadogEvent("1"))
	block := plain(strings.Join(a.connectAskRows(a.width), "\n"))
	if !strings.Contains(block, connectAskRemoteWord) {
		t.Fatalf("the browser sign-in was offered over --host: %s", block)
	}
	if strings.Contains(block, "[enter]") {
		t.Fatalf("the refused trip still offers enter: %s", block)
	}
	a.connectAskKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if a.entering() {
		t.Fatal("enter opened the site box for a browser trip over --host")
	}
}

func TestSettingsSaysWhoseRowsTheseAre(t *testing.T) {
	a, _ := hostLab(t)
	a.openSettings()
	said := strings.Join(plainRows(a), "\n")
	if !strings.Contains(said, "belong to this machine") {
		t.Fatalf("the settings panel opened without saying whose rows it edits: %s", said)
	}
	if !a.at(pageSettings) {
		t.Fatal("the panel refused to open, taking the rows that DO work with it")
	}
}

func TestCommandsWithoutAFarDoorNameTheMachineAndTouchNoLocalState(t *testing.T) {
	a, _ := hostLab(t)
	profile := t.TempDir()
	a.profileDir = profile

	checks := []struct {
		name string
		run  func()
	}{
		{"cache", func() { a.runCacheCommand("clean now") }},
		{"permissions", a.openPermissions},
		{"crew", func() { a.runCrew("frugal") }},
		{"memories", func() { a.runMemories("") }},
		{"memory query", func() { a.runMemories("Ada") }},
		{"remember", func() { a.runRemember("Ada likes tea") }},
		{"forget", func() { a.runForget("Ada") }},
		{"subharness", func() { a.openSubharness("") }},
		{"harness", a.openHarness},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			before, err := os.ReadDir(profile)
			if err != nil {
				t.Fatal(err)
			}
			check.run()
			said := strings.Join(plainRows(a), "\n")
			if !strings.Contains(said, "devbox") {
				t.Fatalf("the refusal did not name the machine: %s", said)
			}
			after, err := os.ReadDir(profile)
			if err != nil {
				t.Fatal(err)
			}
			if len(after) != len(before) {
				t.Fatalf("the remote refusal changed this machine's profile: before %d files, after %d", len(before), len(after))
			}
		})
	}
	if a.permPanel.open || a.harnPanel.open || a.subPage.open {
		t.Fatal("a list backed by this machine opened over --host")
	}
}

func TestTheYoloBadgeNamesTheEnginesPostureNotThisMachines(t *testing.T) {
	// This laptop's own profile says "allow" — if approvalPosture ever fell
	// back to reading it over --host, this is the test that would catch it.
	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "config.json"), []byte(`{"tools.approvalMode":"allow"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a, _ := hostLab(t)
	a.profileDir = local
	if got := a.approvalPosture(); got != "" {
		t.Fatalf("approvalPosture = %q over --host, want nothing — this laptop's \"allow\" must not leak into a remote badge", got)
	}

	// The engine's own posture, carried once on the welcome, is what a remote
	// badge draws instead.
	a.hostApproval = "allow"
	if got := a.approvalPosture(); got != "allow" {
		t.Fatalf("approvalPosture = %q, want the engine's carried posture %q", got, "allow")
	}
}

// ── 4. WHAT MUST KEEP WORKING ───────────────────────────────────────────────

func TestAPictureIsFoundOnTheMachineThePersonIsSittingAt(t *testing.T) {
	a, _ := hostLab(t)
	dir := t.TempDir()
	a.localRoot = dir
	path := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := a.resolvePath("shot.png"); got != path {
		t.Fatalf("resolvePath = %q, want the local file — not %q joined onto a path on another machine", got, "shot.png")
	}
	a.attachPath("shot.png")
	if len(a.chips) != 1 {
		t.Fatalf("the picture did not attach: %s", strings.Join(plainRows(a), "\n"))
	}
}

func TestTheCompletionWalksADirectoryThisProcessCanOpen(t *testing.T) {
	a, _ := hostLab(t)
	dir := t.TempDir()
	a.localRoot = dir
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := a.loadFiles()
	if cmd == nil {
		t.Fatal("the walk did not start")
	}
	loaded, ok := cmd().(filesLoadedMsg)
	if !ok {
		t.Fatalf("the walk answered %T", cmd())
	}
	if len(loaded.paths) != 1 || loaded.paths[0] != "notes.md" {
		t.Fatalf("paths = %v, want this machine's own directory", loaded.paths)
	}
}

func TestExportSaysWhichMachineTheFileLandedOn(t *testing.T) {
	a, agent := hostLab(t)
	dir := t.TempDir()
	a.localRoot = dir
	a.file = "/srv/.sessions/20260817-150405_a3f2.jsonl"
	a.title = "port the resume picker"
	agent.past = []session.DisplayEntry{{Role: "user", Text: "hello"}}

	cmd := a.exportTranscript("")
	if cmd == nil {
		t.Fatalf("/export did nothing: %s", strings.Join(plainRows(a), "\n"))
	}
	msg, ok := cmd().(exportedMsg)
	if !ok {
		t.Fatalf("/export answered %T", cmd())
	}
	if msg.err != nil {
		t.Fatalf("/export failed: %v", msg.err)
	}
	if filepath.Dir(msg.path) != dir {
		t.Fatalf("the file landed at %q, want it under this machine's own directory", msg.path)
	}
	a.exportDone(msg)
	said := strings.Join(plainRows(a), "\n")
	if !strings.Contains(said, "on this machine") {
		t.Fatalf("the note does not say where the file went: %s", said)
	}
}

func TestARemoteJournalIsNamedWithItsMachineWhereverItIsShown(t *testing.T) {
	a, _ := hostLab(t)
	a.file = "/srv/j.jsonl"
	if got := a.hostedPath(a.file); got != "devbox:/srv/j.jsonl" {
		t.Fatalf("hostedPath = %q", got)
	}
	if got := a.hostedPath(""); got != "" {
		t.Fatalf("hostedPath on nothing = %q, want nothing", got)
	}
}

// A local surface must be byte-identical, so the one function every path above
// goes through is checked to be a no-op without a host.
func TestHostedPathIsANoOpWithoutAHost(t *testing.T) {
	a := &app{}
	if got := a.hostedPath("/srv/app"); got != "/srv/app" {
		t.Fatalf("hostedPath = %q on a local surface", got)
	}
	if a.pathRoot() != "" {
		t.Fatalf("pathRoot = %q on a surface with no workspace", a.pathRoot())
	}
	a.workspace = "/home/dev/src/app"
	if a.pathRoot() != "/home/dev/src/app" {
		t.Fatalf("pathRoot = %q, want the workspace on a local session", a.pathRoot())
	}
}
