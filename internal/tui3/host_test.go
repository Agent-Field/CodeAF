package tui3

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"

	"github.com/Agent-Field/codeaf/internal/session"
)

// hostLab is the surface opened on a session that is running somewhere else:
// the workspace is the FAR machine's path, and the host is the name the person
// typed after --host.
func hostLab(t *testing.T) (*app, *fakeAgent) {
	t.Helper()
	agent := &fakeAgent{model: "vendor/model"}
	a := newApp(context.Background(), Options{
		Agent:      agent,
		Host:       "devbox",
		Workspace:  "/srv/code/app",
		ProfileDir: t.TempDir(),
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
// border under the input now that the path has left it: the machine LEADS the
// cluster, with the legend's own separator rather than with the path's colon,
// and the model follows it (foot.go's [app.seamIdentity]). The conversation's
// name is not on the seam since 2026-09-17 — the tab strip says it.
func TestTheLegendNamesTheMachineAsItsOwnSegment(t *testing.T) {
	a, _ := hostLab(t)
	a.title = "porting the parser"
	if got, _ := a.legendLeft(a.width, legendRoom(a.width, "")); got != "devbox · vendor/model" {
		t.Fatalf("the remote legend = %q, want the machine leading the model", got)
	}
	line := plain(a.legend(a.width))
	if strings.Contains(line, "devbox:vendor/model") {
		t.Fatalf("the machine is spelled with the path's colon: %q", line)
	}
	// The project is on the keys row since 2026-09-22 (footswap.go), and it
	// carries the machine there the way the seam did.
	if strings.Contains(line, targetProjectLead) {
		t.Fatalf("the legend still carries the project: %q", line)
	}
	if keys := plain(a.hintRow(a.width)); !strings.Contains(keys, "project: devbox:/srv/code/app") {
		t.Fatalf("the keys row lost the remote project path: %q", keys)
	}
	// AND THE MACHINE IS NAMED ONCE. An unnamed conversation draws the same
	// line: nothing stands in for a name the seam does not carry, and
	// [app.place]'s `devbox:app` spelling never reaches it.
	a.title = ""
	if got, _ := a.legendLeft(a.width, legendRoom(a.width, "")); got != "devbox · vendor/model" {
		t.Fatalf("an unnamed remote legend = %q", got)
	}
}

func TestTheStatusLinesPlaceCarriesTheMachine(t *testing.T) {
	a, _ := hostLab(t)
	if a.place != "devbox:app" {
		t.Fatalf("place = %q, want the machine and the directory", a.place)
	}
	name, _ := a.identityParts(0)
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
	// The seam carries the model (foot.go), and a local session with no
	// machine says nothing about one: no host segment, and no colon that would
	// read as scp syntax.
	got, _ := a.legendLeft(a.width, legendRoom(a.width, ""))
	if got != "vendor/model" {
		t.Fatalf("a local legend = %q — nothing about a machine belongs on it", got)
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
	// The machine and the identity are on the legend, and NO BRANCH is: the
	// probe would read this machine's repository at the other one's path, so
	// nothing is shown rather than something possibly wrong.
	got, _ := a.legendLeft(a.width, legendRoom(a.width, ""))
	if got != "devbox · vendor/model" {
		t.Fatalf("the remote legend = %q", got)
	}
	if strings.Contains(got, "*") || strings.Contains(got, "main") {
		t.Fatalf("the legend grew a branch over --host: %q", got)
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
	block := strings.Join(connectBlock(a), "\n")
	if !strings.Contains(block, connectAskRemoteWord) {
		t.Fatalf("the card does not say what is wrong: %s", block)
	}
	if strings.Contains(block, "1 connect") {
		t.Fatalf("the card still offers an answer that cannot work: %s", block)
	}
	if !strings.Contains(block, "2 not now") {
		t.Fatalf("the card left no way out at all: %s", block)
	}
}

func TestAKeySignInStillWorksOverHost(t *testing.T) {
	a, _ := hostLab(t)
	a.askConnect(session.Event{Kind: session.EventConnectAsk, ConnectID: "1", Service: "notion", ServiceName: "Notion", NeedsKey: true})
	block := strings.Join(connectBlock(a), "\n")
	if strings.Contains(block, connectAskRemoteWord) {
		t.Fatalf("a key sign-in was refused, and a key needs no browser: %s", block)
	}
	if !a.entering() {
		t.Fatal("a key sign-in over --host is not collecting a key")
	}
}

func TestABrowserSignInWithAnAddressAnswerIsStillRefusedOverHost(t *testing.T) {
	a, _ := hostLab(t)
	a.askConnect(askDatadogEvent("1"))
	block := strings.Join(connectBlock(a), "\n")
	if !strings.Contains(block, connectAskRemoteWord) {
		t.Fatalf("the browser sign-in was offered over --host: %s", block)
	}
	if strings.Contains(block, "1 connect") {
		t.Fatalf("the refused trip still offers a way to start it: %s", block)
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
		{"crew", func() { a.runCrew("cap 5") }},
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
	a.handedApproval = "allow"
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
	a.attachFilePath("shot.png")
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

// movedFolder is a chat's own folder with the record a moved chat has.
func movedFolder(t *testing.T, meta string) string {
	t.Helper()
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
	return work
}

func movedKeys(t *testing.T, work string) string {
	t.Helper()
	a, _ := hostLab(t)
	a.host = ""
	a.takeUp(Conversation{Agent: &fakeAgent{model: "m"}, Workspace: work}, true)
	return plain(a.hintRow(a.width))
}

// A chat moved here from a machine whose project is not on this one works in
// its own work/ folder. The keys row names the project it came from, marked as
// a copy of ANOTHER MACHINE'S folder — never the state-root path of the chat's
// own folder, never the product, and never a bare "proj-a" that would be
// claiming a folder this machine does not have.
func TestAMovedChatIsNamedByTheProjectItCameFrom(t *testing.T) {
	work := movedFolder(t, `{"id":"x","title":"fix it","origin":"/srv/other/proj-a"}`)
	keys := movedKeys(t, work)
	if !strings.Contains(keys, "project: proj-a (copy from another machine)") {
		t.Fatalf("the keys row = %q", keys)
	}
	for _, claim := range []string{"/work", "(copy here)", "proj-a ·"} {
		if strings.Contains(keys, claim) {
			t.Fatalf("the keys row claims %q: %q", claim, keys)
		}
	}
}

// With no origin recorded the chat's own title stands in, marked the same way,
// never the product.
func TestAMovedChatWithNoOriginIsNamedByItsTitle(t *testing.T) {
	keys := movedKeys(t, movedFolder(t, `{"id":"x","title":"fix it"}`))
	if !strings.Contains(keys, "project: fix it (copy from another machine)") || strings.Contains(keys, ownedWord) {
		t.Fatalf("the keys row = %q", keys)
	}
}

// A chat taken back to the machine and folder it came from is named by that
// folder plainly: it works in the project's own directory again — the folder
// is on this machine, as it is on the machine a chat comes home to — and a
// copy marker would be claiming a copy there is no reason for.
func TestAChatBackInItsOwnFolderIsNamedByIt(t *testing.T) {
	home := t.TempDir()
	proj := filepath.Join(home, "proj-a")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	a, _ := hostLab(t)
	a.host = ""
	a.takeUp(Conversation{Agent: &fakeAgent{model: "m"}, Workspace: proj}, true)
	keys := plain(a.hintRow(a.width))
	if !strings.Contains(keys, proj) {
		t.Fatalf("the keys row = %q", keys)
	}
	for _, claim := range []string{"copy"} {
		if strings.Contains(keys, claim) {
			t.Fatalf("a chat back in its own folder is marked %q: %q", claim, keys)
		}
	}
}

// The home screen agrees with the footer about the same taken chat: a project
// every conversation of which is a copy taken here from another machine is
// named for where it came from — the origin folder's name, marked as a copy —
// and the projects panel shows that name instead of the recorded path, which
// is a folder on the machine the chat left and would be claiming one here.
func TestHomeNamesACopyProjectForWhereItCameFrom(t *testing.T) {
	lab := newHomeLab(t)
	spoke := lab.pin(time.Now())
	dir := filepath.Join(lab.project("-tmp-taken"), "cell000000000001")
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"),
		[]byte(`{"type":"session","version":1,"id":"cell000000000001"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(dir, session.Meta{
		ID: "cell000000000001", Title: "fix it",
		Workspace: "/srv/other/proj-a", LaunchDir: "/srv/other/proj-a",
		Origin:  "/srv/other/proj-a",
		Created: spoke.Add(-time.Hour), LastUserAt: spoke,
	}); err != nil {
		t.Fatal(err)
	}
	world := session.ReadWorld(lab.root)
	if len(world.Projects) != 1 {
		t.Fatalf("read %d projects, want 1", len(world.Projects))
	}
	project := world.Projects[0]
	if !project.Moved {
		t.Fatalf("project %q is not marked as a copy", project.Name)
	}
	if project.Name != "proj-a (copy from another machine)" {
		t.Fatalf("project is called %q", project.Name)
	}
	a := lab.app("")
	a.world = func() (session.World, bool) { return world, true }
	a.openHome()
	text := homeText(a)
	// The panel truncates the marker at its width, so the assertion takes the
	// untruncated head of the name and not its full spelling.
	if !strings.Contains(text, "proj-a (copy from") {
		t.Fatalf("home does not name the copy project:\n%s", text)
	}
	for _, claim := range []string{"/srv/other/proj-a", "(copy here)"} {
		if strings.Contains(text, claim) {
			t.Fatalf("home claims %q for a copy project:\n%s", claim, text)
		}
	}
}

// A chat that came home — its folder is on this machine again — is not a copy
// whatever a note from an earlier hop says: the world names its project by the
// folder that is there.
func TestHomeNamesAChatBackInItsOwnFolderByIt(t *testing.T) {
	lab := newHomeLab(t)
	spoke := lab.pin(time.Now())
	proj := lab.workspace("proj-a") // a real folder on this machine
	dir := filepath.Join(lab.project("-tmp-home"), "cell000000000002")
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"),
		[]byte(`{"type":"session","version":1,"id":"cell000000000002"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(dir, session.Meta{
		ID: "cell000000000002", Title: "fix it",
		Workspace: proj, LaunchDir: proj,
		// A note written on a machine the chat hopped through, carried home
		// with the record: the folder it names is HERE now.
		Origin:  proj,
		Created: spoke.Add(-time.Hour), LastUserAt: spoke,
	}); err != nil {
		t.Fatal(err)
	}
	world := session.ReadWorld(lab.root)
	if len(world.Projects) != 1 {
		t.Fatalf("read %d projects, want 1", len(world.Projects))
	}
	project := world.Projects[0]
	if project.Moved {
		t.Fatalf("a chat back in its own folder is marked a copy: %q", project.Name)
	}
	if project.Name != "proj-a" {
		t.Fatalf("project is called %q, want %q", project.Name, "proj-a")
	}
}
