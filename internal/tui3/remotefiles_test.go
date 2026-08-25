package tui3

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/filedoor"
	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── the fakes ───────────────────────────────────────────────────────────────

// fakeWire is the far machine, as a map. It counts calls because half of what
// this file asserts is about HOW MANY of them there were.
type fakeWire struct {
	mu sync.Mutex

	statCalls  [][]string
	fetchCalls []string
	listCalls  []string

	facts    map[string]remote.PathFact
	listings map[string]remote.DirListing
	files    map[string]remote.FetchedFile

	statErr  error
	listErr  error
	fetchErr error
}

func newFakeWire() *fakeWire {
	return &fakeWire{
		facts:    map[string]remote.PathFact{},
		listings: map[string]remote.DirListing{},
		files:    map[string]remote.FetchedFile{},
	}
}

func (w *fakeWire) StatPaths(paths []string) ([]remote.PathFact, error) {
	w.mu.Lock()
	w.statCalls = append(w.statCalls, append([]string(nil), paths...))
	w.mu.Unlock()
	if w.statErr != nil {
		return nil, w.statErr
	}
	out := make([]remote.PathFact, 0, len(paths))
	for _, p := range paths {
		if fact, ok := w.facts[p]; ok {
			fact.Path = p
			out = append(out, fact)
			continue
		}
		out = append(out, remote.PathFact{Path: p})
	}
	return out, nil
}

func (w *fakeWire) ListDir(path string) (remote.DirListing, error) {
	w.mu.Lock()
	w.listCalls = append(w.listCalls, path)
	w.mu.Unlock()
	if w.listErr != nil {
		return remote.DirListing{}, w.listErr
	}
	return w.listings[path], nil
}

func (w *fakeWire) FetchFile(path string) (remote.FetchedFile, error) {
	w.mu.Lock()
	w.fetchCalls = append(w.fetchCalls, path)
	w.mu.Unlock()
	if w.fetchErr != nil {
		return remote.FetchedFile{}, w.fetchErr
	}
	file, ok := w.files[path]
	if !ok {
		return remote.FetchedFile{}, fmt.Errorf("engine: no such file: %s", path)
	}
	return file, nil
}

func (w *fakeWire) fetched() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.fetchCalls...)
}

// fakeDoor is the loopback listener without a listener: it mints ids the way
// the real one does and remembers what it was asked for.
type fakeDoor struct {
	minted []string
	browse string
	closed bool
	err    error
}

func (d *fakeDoor) FileURL(path string) (string, error) {
	if d.err != nil {
		return "", d.err
	}
	d.minted = append(d.minted, path)
	return fmt.Sprintf("http://127.0.0.1:9999/f/%d", len(d.minted)), nil
}

func (d *fakeDoor) BrowseURL() string { return d.browse }

func (d *fakeDoor) Close() error { d.closed = true; return nil }

// hostedFixture is a surface with a far disk under it, with no connection and
// no listener anywhere near it.
func hostedFixture(t *testing.T) (*app, *fakeWire, *fakeDoor) {
	t.Helper()
	a := newTestApp(nil)
	a.workspace = "/srv/app"
	a.host = "devbox"
	wire := newFakeWire()
	a.rfiles = newRemoteFilesOver("devbox", wire)
	// The gate is a fact about the terminal the suite happens to be running in
	// (pathlink.go's [terminalTakesLinks]), so it is pinned here for the reason
	// [newTestApp] pins the colour profile.
	a.pathLinks = true
	door := &fakeDoor{browse: "http://127.0.0.1:9999/browse/tok"}
	restore := openDoor
	openDoor = func(_ filedoor.Source) (doorLinker, error) { return door, nil }
	t.Cleanup(func() { openDoor = restore })
	return a, wire, door
}

// run turns one tea.Cmd into the message it produced, so a batched wire question
// can be asserted without a program loop.
func run(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

// ── the honesty rule, over a connection ─────────────────────────────────────

// The whole law in one test: a word that looks like a path is NOT a link until
// the other machine has said the file is there.
func TestRemoteRowsDoNotLinkBeforeTheEngineAnswers(t *testing.T) {
	a, _, _ := hostedFixture(t)
	rows := a.linkPaths([]string{"wrote it to internal/api/handler.go"})
	if strings.Contains(rows[0], "\x1b]8;;") {
		t.Fatalf("a word linked before anything confirmed it: %q", rows[0])
	}
	if len(a.rfiles.want) != 1 || a.rfiles.want[0] != "/srv/app/internal/api/handler.go" {
		t.Fatalf("the word was not queued for the engine: %v", a.rfiles.want)
	}
}

// And once it HAS said so, the anchor is the door's URL and never a file URI —
// which is the whole reason this pass was off over a connection for a wave.
func TestRemoteLinkPointsAtTheDoorAndNotAtThisMachine(t *testing.T) {
	a, wire, door := hostedFixture(t)
	wire.facts["/srv/app/internal/api/handler.go"] = remote.PathFact{Exists: true}

	a.linkPaths([]string{"wrote it to internal/api/handler.go"})
	msg := run(a.remoteStatKick())
	facts, ok := msg.(remoteFactsMsg)
	if !ok {
		t.Fatalf("the batch produced %T", msg)
	}
	a.remoteFactsBack(facts)

	rows := a.linkPaths([]string{"wrote it to internal/api/handler.go"})
	if !strings.Contains(rows[0], "\x1b]8;;http://127.0.0.1:9999/f/1") {
		t.Fatalf("the row does not link through the door: %q", rows[0])
	}
	if strings.Contains(rows[0], "file://") {
		t.Fatalf("a remote path was linked to THIS machine's disk: %q", rows[0])
	}
	if len(door.minted) != 1 || door.minted[0] != "/srv/app/internal/api/handler.go" {
		t.Fatalf("the door was asked for %v", door.minted)
	}
}

// A directory is confirmed and still not linked: the door serves one file per
// id, and this wave does not point the browse page at a folder.
func TestRemoteDirectoryIsConfirmedAndStillNotLinked(t *testing.T) {
	a, wire, _ := hostedFixture(t)
	wire.facts["/srv/app/internal"] = remote.PathFact{Exists: true, Dir: true}

	a.linkPaths([]string{"look in internal/ for it"})
	a.remoteFactsBack(run(a.remoteStatKick()).(remoteFactsMsg))
	rows := a.linkPaths([]string{"look in internal/ for it"})
	if strings.Contains(rows[0], "\x1b]8;;") {
		t.Fatalf("a directory became a link: %q", rows[0])
	}
	if !a.rfiles.facts["/srv/app/internal"].dir {
		t.Fatalf("the directory was not recorded as one")
	}
}

// A tilde on a hosted session names THIS machine's home and the file is on the
// other one. There is no substitution to make, so there is no question to ask.
func TestRemotePassNeverResolvesATilde(t *testing.T) {
	a, _, _ := hostedFixture(t)
	a.linkPaths([]string{"it is at ~/notes/today.md"})
	for _, want := range a.rfiles.want {
		if strings.Contains(want, "notes/today.md") {
			t.Fatalf("a tilde was resolved against this machine: %v", a.rfiles.want)
		}
	}
}

// ── the batch ───────────────────────────────────────────────────────────────

// ONE CALL PER BURST, NEVER ONE PER WORD, and never more than the engine will
// accept in one frame.
func TestRemoteStatIsBatchedAndBounded(t *testing.T) {
	a, wire, _ := hostedFixture(t)
	rows := make([]string, 0, 100)
	for i := 0; i < 100; i++ {
		// The rows deliberately do not END on the path: a word that finishes a
		// row is a candidate head for the wrap-join ([linker.join]), and the
		// concatenation it tries is a question of its own. That is correct
		// behaviour — a path broken across a wrap is the reason the join exists —
		// and it is not what this test is counting.
		rows = append(rows, fmt.Sprintf("made pkg/file%03d.go just now", i))
	}
	a.linkPaths(rows)
	if len(a.rfiles.want) != 100 {
		t.Fatalf("collected %d candidates, wanted 100", len(a.rfiles.want))
	}
	msg := run(a.remoteStatKick()).(remoteFactsMsg)
	if len(wire.statCalls) != 1 {
		t.Fatalf("%d calls for one burst of rows", len(wire.statCalls))
	}
	if len(wire.statCalls[0]) != statBatchMax {
		t.Fatalf("the batch carried %d paths, over the engine's own ceiling of %d",
			len(wire.statCalls[0]), statBatchMax)
	}
	// And the debounce: nothing else goes out while that call is in flight.
	if a.remoteStatKick() != nil {
		t.Fatal("a second batch went out with the first still on the wire")
	}
	// And the tail goes out on the NEXT frame, as one call and not thirty-six.
	a.remoteFactsBack(msg)
	run(a.remoteStatKick())
	if len(wire.statCalls) != 2 || len(wire.statCalls[1]) != 100-statBatchMax {
		t.Fatalf("the tail was not asked as one call: %v", wire.statCalls[1:])
	}
}

// A word is asked ONCE. Drawing the same rows every frame for a minute must not
// be a wire question every frame for a minute.
func TestRemoteWordIsAskedOncePerSession(t *testing.T) {
	a, wire, _ := hostedFixture(t)
	rows := []string{"see pkg/one.go and pkg/two.go"}
	for i := 0; i < 5; i++ {
		a.linkPaths(rows)
	}
	if len(a.rfiles.want) != 2 {
		t.Fatalf("five renders queued %d questions", len(a.rfiles.want))
	}
	a.remoteFactsBack(run(a.remoteStatKick()).(remoteFactsMsg))
	for i := 0; i < 5; i++ {
		a.linkPaths(rows)
	}
	if a.remoteStatKick() != nil || len(wire.statCalls) != 1 {
		t.Fatalf("answered words were asked about again: %v", wire.statCalls)
	}
}

// A candidate the engine said nothing about is still written down as absent, or
// it is a question this surface asks on every frame for the rest of the session.
func TestRemoteUnansweredCandidateIsNotAskedForever(t *testing.T) {
	a, wire, _ := hostedFixture(t)
	wire.statErr = errors.New("engine: the connection to devbox has gone")
	a.linkPaths([]string{"see pkg/one.go"})
	a.remoteFactsBack(run(a.remoteStatKick()).(remoteFactsMsg))
	a.linkPaths([]string{"see pkg/one.go"})
	if a.remoteStatKick() != nil {
		t.Fatal("a word a failed call carried is being asked again")
	}
}

// ── prefetch on write ───────────────────────────────────────────────────────

func writeEvent(path string) session.Event {
	return session.Event{
		Kind: session.EventToolEnd,
		Tool: "write",
		Args: fmt.Sprintf(`{"path":%q,"content":"x"}`, path),
	}
}

// The size is asked BEFORE the bytes are, so the ceiling is a ceiling and not a
// receipt for a transfer that already happened.
func TestPrefetchRefusesAFileOverTheCeilingWithoutFetchingIt(t *testing.T) {
	a, wire, _ := hostedFixture(t)
	wire.listings["/srv/app/out"] = remote.DirListing{
		Path:    "/srv/app/out",
		Entries: []remote.DirEntry{{Name: "big.bin", Size: prefetchMax + 1}},
	}
	msg := run(a.prefetchWritten(writeEvent("out/big.bin")))
	got, ok := msg.(remotePrefetchedMsg)
	if !ok || got.ok {
		t.Fatalf("a file over the ceiling was prefetched: %#v", msg)
	}
	if len(wire.fetched()) != 0 {
		t.Fatalf("the bytes crossed anyway: %v", wire.fetched())
	}
}

// Under the ceiling it lands in the cache, and the row it names becomes a link
// without anybody asking StatPaths about it.
func TestPrefetchCachesAndLinksWhatTheModelWrote(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	a, wire, door := hostedFixture(t)
	wire.listings["/srv/app/out"] = remote.DirListing{
		Path:    "/srv/app/out",
		Entries: []remote.DirEntry{{Name: "report.md", Size: 12}},
	}
	wire.files["/srv/app/out/report.md"] = remote.FetchedFile{Name: "report.md", Bytes: []byte("hello world\n")}

	msg := run(a.prefetchWritten(writeEvent("out/report.md"))).(remotePrefetchedMsg)
	if !msg.ok {
		t.Fatalf("the prefetch did not land: %#v", msg)
	}
	a.remotePrefetched(msg)
	if fact := a.rfiles.facts["/srv/app/out/report.md"]; !fact.file || fact.uri == "" {
		t.Fatalf("the written file is not a confirmed link: %#v", fact)
	}
	if len(door.minted) != 1 {
		t.Fatalf("the door was asked %d times", len(door.minted))
	}
	if _, held := a.rfiles.ref("/srv/app/out/report.md"); !held {
		t.Fatal("the bytes are not in the cache")
	}
}

// A prefetch that failed is a click that will fetch later. It says NOTHING.
func TestPrefetchFailsSilently(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	a, wire, _ := hostedFixture(t)
	wire.listErr = errors.New("engine: no such file")
	before := len(a.entries)
	msg := run(a.prefetchWritten(writeEvent("out/report.md"))).(remotePrefetchedMsg)
	if msg.ok {
		t.Fatal("a failed listing reported a prefetch")
	}
	if cmd := a.remotePrefetched(msg); cmd != nil {
		t.Fatal("a failed prefetch asked the loop to do something")
	}
	if len(a.entries) != before {
		t.Fatalf("a failed prefetch wrote %d rows on the screen", len(a.entries)-before)
	}
}

// A read is not a trigger, and neither is a local session.
func TestPrefetchOnlyFollowsAWriteOnAConnection(t *testing.T) {
	a, _, _ := hostedFixture(t)
	read := session.Event{Kind: session.EventToolEnd, Tool: "read", Args: `{"path":"main.go"}`}
	if a.prefetchWritten(read) != nil {
		t.Fatal("a read started a prefetch")
	}
	local := newTestApp(nil)
	if local.prefetchWritten(writeEvent("main.go")) != nil {
		t.Fatal("a local session started a prefetch")
	}
}

// ── the door, and /files ────────────────────────────────────────────────────

// The listener is opened by the first NEED and not by the session starting: a
// healthy connection nobody names a file on runs none.
func TestTheDoorOpensOnFirstNeedAndClosesWithTheSurface(t *testing.T) {
	a, wire, door := hostedFixture(t)
	if a.rfiles.door != nil {
		t.Fatal("a door was opened before anything needed one")
	}
	wire.facts["/srv/app/main.go"] = remote.PathFact{Exists: true}
	a.linkPaths([]string{"see main.go"})
	a.remoteFactsBack(run(a.remoteStatKick()).(remoteFactsMsg))
	if a.rfiles.door == nil {
		t.Fatal("the first confirmed link did not open the door")
	}
	a.closeFileDoor()
	if !door.closed {
		t.Fatal("the door outlived the surface")
	}
}

// Bare /files over a connection is the far workspace as a page: handed to the
// platform AND written down, because neither is trusted with the other's job.
func TestFilesOpensTheBrowsePageOnAConnection(t *testing.T) {
	a, _, door := hostedFixture(t)
	opened := ""
	restore := processOpener
	processOpener = func(target string) error { opened = target; return nil }
	t.Cleanup(func() { processOpener = restore })

	a.slash("/files")
	if opened != door.browse {
		t.Fatalf("the platform was handed %q", opened)
	}
	last := a.entries[len(a.entries)-1]
	if last.kind != entryNote || !strings.Contains(last.text, door.browse) {
		t.Fatalf("the address was not written down: %q", last.text)
	}
}

// And the argument form on a session that is not on another machine says so
// rather than doing something surprising with a local path.
func TestFilesWithAPathRefusesHonestlyOnALocalSession(t *testing.T) {
	a := newTestApp(nil)
	a.slash("/files internal/tui3/app.go")
	last := a.entries[len(a.entries)-1]
	if last.kind != entryNote || last.text != filesLocalWord {
		t.Fatalf("a local session said %q", last.text)
	}
}

// Dropping a file onto the browse page is refused, and the refusal names the
// lane that works. It is a wave-1 fact and the manual says the same thing.
func TestDepositIsRefusedAndNamesTheLaneThatWorks(t *testing.T) {
	a, _, _ := hostedFixture(t)
	source := &hostSource{files: a.rfiles}
	landed, err := source.Deposit("notes.txt", []byte("x"))
	if err == nil || landed != "" {
		t.Fatalf("a deposit was accepted: %q", landed)
	}
	if !strings.Contains(err.Error(), "/attach") {
		t.Fatalf("the refusal does not name the lane that works: %q", err)
	}
}

// ── the shapes ──────────────────────────────────────────────────────────────

// The mirror is one directory per machine and the engine's own path under it.
func TestMirrorPathIsOneTreePerMachine(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(home.EnvVar, dir)
	got := mirrorPath("me@devbox:22", "/srv/code/app/main.go")
	want := filepath.Join(dir, "v3", "remote", "mirror", "me-devbox-22", "srv", "code", "app", "main.go")
	if got != want {
		t.Fatalf("mirror path is %q, wanted %q", got, want)
	}
}

// A BOUNDARY THAT TRUSTS ITS INPUT IS NOT A BOUNDARY: the path came off the
// wire, and nothing that arrives on it may name a place outside the mirror.
func TestMirrorPathRefusesToBeWalkedOutOf(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(home.EnvVar, dir)
	root := filepath.Join(dir, "v3", "remote", "mirror")
	// A name that is not an absolute path on the engine's disk is not a name
	// this side will write anything under.
	for _, bad := range []string{"srv/code/main.go", "", "/", "  "} {
		if got := mirrorPath("devbox", bad); got != "" {
			t.Fatalf("%q was mirrored at %q", bad, got)
		}
	}
	// And one that climbs cannot climb out: it is cleaned before it is joined,
	// so the worst it can do is name a different file INSIDE the mirror.
	for _, climb := range []string{"/srv/../../etc/passwd", "/srv/app/../../../x"} {
		got := mirrorPath("devbox", climb)
		if got == "" {
			continue
		}
		if !strings.HasPrefix(got, root+string(filepath.Separator)) {
			t.Fatalf("%q was mirrored outside the mirror, at %q", climb, got)
		}
	}
	if got := mirrorPath("", "/srv/main.go"); got != "" {
		t.Fatalf("a machine with no name got a directory: %q", got)
	}
	if got := mirrorPath("../..", "/srv/main.go"); got != "" {
		t.Fatalf("a machine named %q got a directory: %q", "../..", got)
	}
}
