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
	a.home = "/home/dev"
	a.entries = nil
	a.welcome = welcome{spent: true}
	a.touch()
	return a, agent
}

// ── 1. THE CONNECTION IS THE PLACE ──────────────────────────────────────────

func TestTheLegendNamesTheMachineInFrontOfThePath(t *testing.T) {
	a, _ := hostLab(t)
	// The path is abbreviated exactly as a local one is — leading segments to
	// their initials — and the machine rides in front of the result.
	if got := a.legendPath(0); got != "devbox:/s/c/app" {
		t.Fatalf("legendPath = %q", got)
	}
	// The abbreviation eats the PATH and never the machine: which machine is the
	// half a person cannot reconstruct from anything else on the screen.
	if got := a.legendPath(2); got != "devbox:app" {
		t.Fatalf("legendPath at the hardest strength = %q", got)
	}
	line := plain(a.legend(a.width))
	if !strings.Contains(line, "devbox:/s/c/app") {
		t.Fatalf("the legend line does not carry the place: %q", line)
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
	a.home = "/home/dev"
	if a.hosted() {
		t.Fatal("a local session thinks it is hosted")
	}
	if got := a.legendPath(0); got != "~/s/app" {
		t.Fatalf("legendPath = %q — a local session must render exactly as it always did", got)
	}
	if a.place != "app" {
		t.Fatalf("place = %q", a.place)
	}
	if strings.Contains(plain(a.legend(a.width)), ":") {
		t.Fatal("a local legend grew a colon")
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
	if got := a.legendLeft(a.width, 0); strings.Contains(got, "·") {
		t.Fatalf("the legend grew a branch: %q", got)
	}
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

func TestSettingsSaysWhoseRowsTheseAre(t *testing.T) {
	a, _ := hostLab(t)
	a.openSettings()
	said := strings.Join(plainRows(a), "\n")
	if !strings.Contains(said, "this machine's") {
		t.Fatalf("the settings panel opened without saying whose rows it edits: %s", said)
	}
	if !a.sheet.open {
		t.Fatal("the panel refused to open, taking the rows that DO work with it")
	}
}

func TestTheYoloBadgeIsNeverDrawnFromTheWrongMachinesProfile(t *testing.T) {
	a, _ := hostLab(t)
	a.profileDir = t.TempDir()
	if got := a.approvalPosture(); got != "" {
		t.Fatalf("approvalPosture = %q over --host, want nothing at all", got)
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
