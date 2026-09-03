package tui3

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/filedoor"
	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── the fakes ───────────────────────────────────────────────────────────────

// fakeWire is the far machine, as a map. It counts calls because half of what
// this file asserts is about HOW MANY of them there were.
type fakeWire struct {
	mu sync.Mutex

	statCalls    [][]string
	fetchCalls   []string
	listCalls    []string
	depositCalls []depositCall

	facts    map[string]remote.PathFact
	listings map[string]remote.DirListing
	files    map[string]remote.FetchedFile

	statErr    error
	listErr    error
	fetchErr   error
	depositErr error
}

// depositCall is one drop as the far machine saw it, so a test can say what
// crossed and not merely that something did.
type depositCall struct {
	name string
	mime string
	body string
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

// DepositFile is the far session's attachments folder, as a map would keep it:
// the engine chooses the directory and answers with the path, which is the one
// thing about a deposit the surface may not derive.
func (w *fakeWire) DepositFile(name, mime string, data []byte) (string, error) {
	w.mu.Lock()
	w.depositCalls = append(w.depositCalls, depositCall{name: name, mime: mime, body: string(data)})
	w.mu.Unlock()
	if w.depositErr != nil {
		return "", w.depositErr
	}
	return "/srv/app/.aforge-v3/sessions/9f3c/attachments/20260824-141233-a1b2c3d4-" + name, nil
}

func (w *fakeWire) fetched() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.fetchCalls...)
}

func (w *fakeWire) stats() [][]string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([][]string(nil), w.statCalls...)
}

// farFile puts one file on the fake far disk, with the facts a stat answers
// about it. THE TWO HAVE TO BE COHERENT: the freshness rule compares what the
// stat says now against what the stat said when the bytes crossed, so a fake
// whose bytes and whose numbers disagree is a fake testing nothing.
func (w *fakeWire) farFile(target, kind, body string, mtime int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.files[target] = remote.FetchedFile{
		Name: path.Base(target), MIME: kind, Size: int64(len(body)), Bytes: []byte(body),
	}
	w.facts[target] = remote.PathFact{Exists: true, Size: int64(len(body)), ModTime: mtime}
}

// testClock is the two ages in remotefiles.go — how long a NO is believed, and
// how long a dropped call buys quiet — moved by hand. Proving either by
// sleeping would be twenty seconds of suite for what an assignment says.
type testClock struct{ at time.Time }

func (c *testClock) now() time.Time { return c.at }

func (c *testClock) pass(d time.Duration) { c.at = c.at.Add(d) }

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

func (d *fakeDoor) OpenURL(path string) (string, error) { return d.FileURL(path) }

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

// clockOf pins one surface's clock and hands back the handle that moves it, so
// a test can say "twenty seconds later" without waiting twenty seconds.
func clockOf(a *app) *testClock {
	c := &testClock{at: time.Unix(1_700_000_000, 0)}
	a.rfiles.now = c.now
	return c
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

// A candidate the engine ANSWERED about — "there is nothing there" — is written
// down as absent, or it is a question this surface asks on every frame for the
// rest of the session.
func TestRemoteUnansweredCandidateIsNotAskedForever(t *testing.T) {
	a, wire, _ := hostedFixture(t)
	clockOf(a)
	a.linkPaths([]string{"see pkg/one.go"})
	a.remoteFactsBack(run(a.remoteStatKick()).(remoteFactsMsg))
	a.linkPaths([]string{"see pkg/one.go"})
	if a.remoteStatKick() != nil || len(wire.statCalls) != 1 {
		t.Fatalf("a clean not-there was asked about again: %v", wire.statCalls)
	}
}

// A CALL THAT DID NOT GET THROUGH IS NOT AN ANSWER, and this is the difference
// between the two. Recording a dropped frame as absence blackholes up to a
// whole batch of REAL files for the rest of the session — a confirmed word is
// never re-asked, so the words that most deserve another question would be
// precisely the ones that never got one.
func TestADroppedStatDoesNotBlackholeThePathsItCarried(t *testing.T) {
	a, wire, _ := hostedFixture(t)
	clock := clockOf(a)
	wire.statErr = errors.New("engine: the connection to devbox has gone")
	wire.facts["/srv/app/pkg/one.go"] = remote.PathFact{Exists: true, Size: 12, ModTime: 1}

	a.linkPaths([]string{"see pkg/one.go"})
	a.remoteFactsBack(run(a.remoteStatKick()).(remoteFactsMsg))
	if fact, known := a.rfiles.facts["/srv/app/pkg/one.go"]; known {
		t.Fatalf("a broken pipe was written down as a fact about the disk: %#v", fact)
	}
	// AND NOT ON THE VERY NEXT FRAME EITHER. The quiet window is what keeps a
	// released word from becoming a question every frame while the link is down.
	a.linkPaths([]string{"see pkg/one.go"})
	if a.remoteStatKick() != nil {
		t.Fatal("the surface went straight back at a wire that just failed")
	}

	// Past the window, and with the link back, the same word is asked again and
	// this time it is a door.
	clock.pass(remoteStatQuiet + time.Second)
	wire.statErr = nil
	a.linkPaths([]string{"see pkg/one.go"})
	msg, ok := run(a.remoteStatKick()).(remoteFactsMsg)
	if !ok {
		t.Fatal("the word a dropped call carried was never asked again")
	}
	a.remoteFactsBack(msg)
	rows := a.linkPaths([]string{"see pkg/one.go"})
	if !strings.Contains(rows[0], "\x1b]8;;http://127.0.0.1:9999/f/1") {
		t.Fatalf("the file the dropped call lost never became a link: %q", rows[0])
	}
}

// A PATH MENTIONED BEFORE IT EXISTS IS THE ORDINARY CASE, not the exotic one:
// the model says where it is going to put something and then puts it there. A
// NO is believed for [remoteAbsentFor] and then asked again, which is the
// expiry the local pass gets from the turn boundary (pathlink.go's memo) and
// this table cannot.
func TestARemotePathThatWasAbsentLinksOnceItExists(t *testing.T) {
	a, wire, _ := hostedFixture(t)
	clock := clockOf(a)

	// "I'll write dist/app" — said before anything is there.
	a.linkPaths([]string{"I'll write dist/app when the build finishes"})
	a.remoteFactsBack(run(a.remoteStatKick()).(remoteFactsMsg))
	rows := a.linkPaths([]string{"I'll write dist/app when the build finishes"})
	if strings.Contains(rows[0], "\x1b]8;;") {
		t.Fatalf("a name that is not a file became a link: %q", rows[0])
	}

	// The build runs under bash, which is not a trigger for anything — nothing
	// on this surface saw the file appear.
	wire.farFile("/srv/app/dist/app", "", "binary", 1)
	clock.pass(remoteAbsentFor + time.Second)

	a.linkPaths([]string{"I'll write dist/app when the build finishes"})
	msg, ok := run(a.remoteStatKick()).(remoteFactsMsg)
	if !ok {
		t.Fatal("the word was never asked about again")
	}
	a.remoteFactsBack(msg)
	rows = a.linkPaths([]string{"I'll write dist/app when the build finishes"})
	if !strings.Contains(rows[0], "\x1b]8;;http://127.0.0.1:9999/f/1") {
		t.Fatalf("a path that now exists is still plain text: %q", rows[0])
	}
	// And a YES is not on a clock: the confirmed word is never asked again.
	before := len(wire.stats())
	clock.pass(remoteAbsentFor * 10)
	a.linkPaths([]string{"I'll write dist/app when the build finishes"})
	if a.remoteStatKick() != nil || len(wire.stats()) != before {
		t.Fatal("a confirmed path was put back on the wire by the clock")
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
	wire.facts["/srv/app/out/big.bin"] = remote.PathFact{Exists: true, Size: prefetchMax + 1, ModTime: 7}
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
	wire.farFile("/srv/app/out/report.md", "text/markdown", "hello world\n", 1700)

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

// A picture tool ending is the remote preview's transfer trigger. The surface
// decodes the cached bytes while continuing to label and link the far path.
func TestPrefetchCachesAPictureForTheHostedTerminalPreview(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	a, wire, _ := hostedFixture(t)
	local := writePicture(t, t.TempDir(), "far.png", tinyPicture())
	data, err := os.ReadFile(local)
	if err != nil {
		t.Fatal(err)
	}
	wire.farFile("/srv/app/out/far.png", "image/png", string(data), 1700)
	ev := session.Event{Kind: session.EventToolEnd, Tool: "generate_image",
		Args:   `{"path":"out/far.png","prompt":"harbour"}`,
		Output: "out/far.png — 2×2 png, generated on paint/model"}
	msg := run(a.prefetchWritten(ev)).(remotePrefetchedMsg)
	if !msg.ok {
		t.Fatalf("the picture did not cross: %#v", msg)
	}
	a.remotePrefetched(msg)
	e := &entry{tool: ev.Tool, detail: toolDetail{Args: ev.Args, Output: ev.Output}}
	if _, _, drawn := a.drawPicture(e, 40, pictureRowsMax); !drawn {
		t.Fatal("the cached far picture was not painted")
	}
}

func TestAPictureThatCannotCrossNamesTheMachineHoldingIt(t *testing.T) {
	a, wire, _ := hostedFixture(t)
	wire.fetchErr = errors.New("engine: far.png is over the file limit")
	ev := session.Event{Kind: session.EventToolEnd, Tool: "view_image", Args: `{"path":"out/far.png"}`}
	msg := run(a.prefetchWritten(ev)).(remotePrefetchedMsg)
	if !msg.required || msg.err == nil {
		t.Fatalf("the required picture failure was lost: %#v", msg)
	}
	a.remotePrefetched(msg)
	if said := a.entries[len(a.entries)-1].text; said != "engine: far.png is over the file limit · the picture remains on devbox" {
		t.Fatalf("the refusal reads %q", said)
	}
}

func TestAReplayedHostedPictureStartsItsFetchAtInit(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	a, wire, _ := hostedFixture(t)
	wire.farFile("/srv/app/out/far.png", "image/png", "picture bytes", 1700)
	a.entries = []entry{{kind: entryTool, tool: "view_image", status: toolOK,
		detail: toolDetail{Args: `{"path":"out/far.png"}`}}}
	answer := run(a.prefetchReplayedPictures())
	if batch, ok := answer.(tea.BatchMsg); ok && len(batch) == 1 {
		answer = batch[0]()
	}
	msg, ok := answer.(remotePrefetchedMsg)
	if !ok || !msg.ok || !msg.required {
		t.Fatalf("the replay fetch answered %#v", msg)
	}
}

// A REPLAY PREFETCH SPENDS ITS THREE SLOTS FROM THE LIVE EDGE. Old user
// attachments may share the budget, but they must not crowd a later generated
// picture out before the conversation opens.
func TestReplayPrefetchSpendsItsBudgetAtTheLiveEdge(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	a, wire, _ := hostedFixture(t)
	users := []string{"/srv/app/in/one.png", "/srv/app/in/two.png", "/srv/app/in/three.png"}
	generated := "/srv/app/out/latest.png"
	for _, target := range append(append([]string(nil), users...), generated) {
		wire.farFile(target, "image/png", "picture bytes", 1700)
	}
	a.entries = []entry{
		{kind: entryUser, pictures: []string{users[0]}},
		{kind: entryUser, pictures: []string{users[1]}},
		{kind: entryUser, pictures: []string{users[2]}},
		{kind: entryTool, tool: "view_image", status: toolOK,
			detail: toolDetail{Args: `{"path":"/srv/app/out/latest.png"}`}},
	}
	batch, ok := run(a.prefetchReplayedPictures()).(tea.BatchMsg)
	if !ok || len(batch) != prefetchAtOnce {
		t.Fatalf("the replay started %#v, want a batch of %d", batch, prefetchAtOnce)
	}
	for _, cmd := range batch {
		cmd()
	}
	fetched := wire.fetched()
	if !slices.Contains(fetched, generated) {
		t.Fatalf("the live-edge generated picture was crowded out: %v", fetched)
	}
}

// The three-slot limit is a concurrency cap, not a replay cutoff. Every older
// picture already has a row waiting for it, so each completion starts the next
// transfer until the visible journal has been mirrored.
func TestReplayPrefetchContinuesPastTheFirstThreePictures(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	a, wire, _ := hostedFixture(t)
	for i := 0; i < prefetchAtOnce+2; i++ {
		target := fmt.Sprintf("/srv/app/in/picture-%d.png", i)
		wire.farFile(target, "image/png", "picture bytes", int64(i+1))
		a.entries = append(a.entries, entry{
			kind: entryUser, pictures: []string{target}, picturesHere: false,
		})
	}

	drainReplayPrefetches(a.prefetchReplayedPictures(), a)
	if fetched := wire.fetched(); len(fetched) != prefetchAtOnce+2 {
		t.Fatalf("replay fetched %d pictures and left older rows blank: %v", len(fetched), fetched)
	}
	if len(a.rfiles.replay) != 0 || len(a.rfiles.prefetching) != 0 {
		t.Fatalf("replay did not drain: queued=%d active=%d", len(a.rfiles.replay), len(a.rfiles.prefetching))
	}
}

func drainReplayPrefetches(first tea.Cmd, a *app) {
	commands := []tea.Cmd{first}
	for len(commands) > 0 {
		cmd := commands[0]
		commands = commands[1:]
		if cmd == nil {
			continue
		}
		switch msg := cmd().(type) {
		case tea.BatchMsg:
			commands = append(commands, msg...)
		case remotePrefetchedMsg:
			commands = append(commands, a.remotePrefetched(msg))
		}
	}
}

// A person's accepted picture may be larger than the two-megabyte heuristic
// for speculative model output. Replay follows the actual ten-megabyte image
// contract so a valid attachment does not become a permanent blank row.
func TestReplayPrefetchUsesTheImageLimitForUserPictures(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	a, wire, _ := hostedFixture(t)
	target := "/srv/app/in/large.png"
	body := strings.Repeat("x", prefetchMax+1)
	wire.farFile(target, "image/png", body, 1700)
	a.entries = []entry{{kind: entryUser, pictures: []string{target}, picturesHere: false}}

	drainReplayPrefetches(a.prefetchReplayedPictures(), a)
	if fetched := wire.fetched(); len(fetched) != 1 || fetched[0] != target {
		t.Fatalf("a valid replayed image over the speculation heuristic stayed blank: %v", fetched)
	}
}

// Task-room journals load after the conversation's Init and keep their own row
// cache. They therefore need both a fetch trigger and a redraw when it lands.
func TestAHostedTaskRoomPrefetchesAndRestylesItsPictures(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	a, wire, _ := hostedFixture(t)
	target := "/srv/app/in/room.png"
	wire.farFile(target, "image/png", "picture bytes", 1700)
	a.room = a.newRoom(0, "")
	a.room.entries = []entry{{
		kind: entryUser, pictures: []string{target}, picturesHere: false,
	}}

	cmd := a.prefetchRoomPictures()
	answer := run(cmd)
	if batch, ok := answer.(tea.BatchMsg); ok && len(batch) == 1 {
		answer = run(batch[0])
	}
	msg := answer.(remotePrefetchedMsg)
	a.room.entries[0].stale = false
	a.room.dirty = false
	a.remotePrefetched(msg)
	if !a.room.entries[0].stale || !a.room.dirty {
		t.Fatal("the mirrored task-room picture did not invalidate the room row cache")
	}
}

// A marker shows a basename but opens the full journal identity. Resolving the
// visible name against the workspace root would point at a different file.
func TestAHostedPictureMarkerKeepsItsFullRemotePath(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	a, wire, door := hostedFixture(t)
	target := "/srv/app/in/nested/chart.png"
	wire.farFile(target, "image/png", "picture bytes", 1700)
	a.entries = []entry{{kind: entryUser, text: userLine("look", []chip{{path: target}}, a.pal),
		pictures: []string{target}, picturesHere: false}}
	drainReplayPrefetches(a.prefetchReplayedPictures(), a)

	rows := userEntryRows(t, a, 80)
	if !strings.Contains(strings.Join(rows, "\n"), "\x1b]8;;http://127.0.0.1:9999/f/1") {
		t.Fatalf("the replay marker is not a door:\n%s", strings.Join(rows, "\n"))
	}
	if len(door.minted) != 1 || door.minted[0] != target {
		t.Fatalf("the basename marker opened %v, want the full journal path", door.minted)
	}
}

// A long marker may wrap through the private mask one fragment at a time. No
// mask rune reaches the terminal, and every fragment keeps the one exact door.
func TestAWrappedHostedPictureMarkerRestoresEveryFragment(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	a, wire, door := hostedFixture(t)
	target := "/srv/app/in/nested/a-very-long-chart-name.png"
	wire.farFile(target, "image/png", "picture bytes", 1700)
	a.entries = []entry{{kind: entryUser, text: userLine("look", []chip{{path: target}}, a.pal),
		pictures: []string{target}, picturesHere: false}}
	drainReplayPrefetches(a.prefetchReplayedPictures(), a)

	body := strings.Join(userEntryRows(t, a, 12), "\n")
	for _, r := range body {
		if r >= 0xE000 && r <= 0xF8FF {
			t.Fatalf("a wrapped marker leaked its private mask: %q", body)
		}
	}
	if !strings.Contains(body, "\x1b]8;;http://127.0.0.1:9999/f/1") {
		t.Fatalf("the wrapped marker fragments are not doors:\n%s", body)
	}
	if len(door.minted) != 1 || door.minted[0] != target {
		t.Fatalf("the wrapped marker opened %v, want the full journal path", door.minted)
	}
}

// Q10: a live hosted send records the person's local path and reads it instead
// of the mirror. This drives the attachment and enter doors that set the flag.
func TestALiveHostedUserPictureDrawsFromTheLocalDisk(t *testing.T) {
	a, _, dir := attachLab(t, nil)
	a.host, a.localRoot = "devbox", dir
	a.rfiles = newRemoteFilesOver("devbox", newFakeWire())
	a.pal = newPalette(tokens.TrueColor, false)
	path := writePicture(t, dir, "local.png", wideTestPicture())
	a.attach(path)
	typeLine(t, a, "look")
	found := false
	for _, e := range a.entries {
		if e.kind == entryUser {
			found = true
			if !e.picturesHere || len(e.pictures) != 1 || e.pictures[0] != path {
				t.Fatalf("the live send recorded the wrong disk: %+v", e)
			}
		}
	}
	if !found {
		t.Fatal("the live send left no user entry")
	}
	if rows := userEntryRows(t, a, 80); paintedRows(rows) == 0 {
		t.Fatal("the live hosted attachment was sent through the empty mirror")
	}
}

// Q10: a replayed hosted user picture fetches optionally and appears only after the mirror holds it.
func TestAReplayedHostedUserPictureFetchesSilentlyIntoTheMirror(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	a, wire, _ := hostedFixture(t)
	a.pal = newPalette(tokens.TrueColor, false)
	local := writePicture(t, t.TempDir(), "far.png", tinyPicture())
	data, err := os.ReadFile(local)
	if err != nil {
		t.Fatal(err)
	}
	far := "/srv/app/out/far.png"
	wire.farFile(far, "image/png", string(data), 1700)
	a.entries = []entry{{kind: entryUser, text: "look [#1 far.png]",
		pictures: []string{far}, picturesHere: false}}
	if rows := userEntryRows(t, a, 80); paintedRows(rows) != 0 {
		t.Fatal("the replay drew before the mirror held the file")
	}
	answer := run(a.prefetchReplayedPictures())
	if batch, ok := answer.(tea.BatchMsg); ok && len(batch) == 1 {
		answer = batch[0]()
	}
	msg, ok := answer.(remotePrefetchedMsg)
	if !ok || !msg.ok || msg.required {
		t.Fatalf("the optional replay fetch answered %#v", answer)
	}
	before := len(a.entries)
	a.remotePrefetched(msg)
	if len(a.entries) != before {
		t.Fatal("the user-picture prefetch wrote a note")
	}
	if rows := userEntryRows(t, a, 80); paintedRows(rows) == 0 {
		t.Fatal("the replay did not redraw after the mirror received the file")
	}

	missing := "/srv/app/out/missing.png"
	wire.farFile(missing, "image/png", string(data), 1800)
	wire.fetchErr = errors.New("engine: missing.png moved")
	a.entries = []entry{{kind: entryUser, text: "look [#1 missing.png]",
		pictures: []string{missing}, picturesHere: false}}
	answer = run(a.prefetchReplayedPictures())
	if batch, ok := answer.(tea.BatchMsg); ok && len(batch) == 1 {
		answer = batch[0]()
	}
	failed, ok := answer.(remotePrefetchedMsg)
	if !ok || failed.ok || failed.required || failed.err == nil {
		t.Fatalf("the optional failed fetch answered %#v", answer)
	}
	before = len(a.entries)
	a.remotePrefetched(failed)
	if len(a.entries) != before {
		t.Fatal("a failed user-picture prefetch wrote a note")
	}
}

// A prefetch that failed is a click that will fetch later. It says NOTHING.
func TestPrefetchFailsSilently(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	a, wire, _ := hostedFixture(t)
	wire.statErr = errors.New("engine: no such file")
	before := len(a.entries)
	msg := run(a.prefetchWritten(writeEvent("out/report.md"))).(remotePrefetchedMsg)
	if msg.ok {
		t.Fatal("a failed stat reported a prefetch")
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

// ONE FETCH PER PATH AT A TIME, and the mark comes off when it lands — because
// a SECOND write to the same path is the one moment the cached copy is known to
// be wrong, and a guard that skipped it would be the guard that pins the stale
// bytes in place.
func TestPrefetchDoesNotFetchTheSamePathTwiceAtOnce(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	a, wire, _ := hostedFixture(t)
	wire.farFile("/srv/app/out/report.md", "text/markdown", "first draft\n", 100)

	first := a.prefetchWritten(writeEvent("out/report.md"))
	if first == nil {
		t.Fatal("the first write started nothing")
	}
	if a.prefetchWritten(writeEvent("out/report.md")) != nil {
		t.Fatal("the same path went out twice with the first fetch still in flight")
	}
	a.remotePrefetched(run(first).(remotePrefetchedMsg))

	// The model writes the same file again, and this time the bytes on that
	// machine are different.
	wire.farFile("/srv/app/out/report.md", "text/markdown", "second draft\n", 200)
	second := a.prefetchWritten(writeEvent("out/report.md"))
	if second == nil {
		t.Fatal("a rewrite of a path already held did not re-prime the cache")
	}
	a.remotePrefetched(run(second).(remotePrefetchedMsg))
	if len(wire.fetched()) != 2 {
		t.Fatalf("the rewritten file crossed %d times: %v", len(wire.fetched()), wire.fetched())
	}
	_, file, err := a.rfiles.fetch("/srv/app/out/report.md")
	if err != nil || string(file.Bytes) != "second draft\n" {
		t.Fatalf("the cache is holding %q: %v", file.Bytes, err)
	}
}

// SPECULATIVE WORK MUST NEVER OUTRUN THE PERSON'S NEXT MESSAGE. A turn writing
// forty files is an ordinary turn, and forty transfers fanned out on one ssh
// pipe ahead of a click nobody has made is the connection spent on a guess.
func TestPrefetchStopsAtTheInFlightCap(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	a, wire, _ := hostedFixture(t)

	started := make([]tea.Cmd, 0, prefetchAtOnce+3)
	for i := range prefetchAtOnce + 3 {
		name := fmt.Sprintf("out/file%d.txt", i)
		wire.farFile("/srv/app/"+name, "", "x", int64(i+1))
		started = append(started, a.prefetchWritten(writeEvent(name)))
	}
	out := 0
	for _, cmd := range started {
		if cmd != nil {
			out++
		}
	}
	if out != prefetchAtOnce {
		t.Fatalf("%d speculative fetches went out at once, and the cap is %d", out, prefetchAtOnce)
	}
	// WHAT DID NOT FIT IS DROPPED AND NOT QUEUED: the cost of not having
	// prefetched is one round trip on a click that will probably never happen.
	landed := make([]remotePrefetchedMsg, 0, out)
	for _, cmd := range started {
		if cmd != nil {
			landed = append(landed, run(cmd).(remotePrefetchedMsg))
		}
	}
	if len(wire.fetched()) != prefetchAtOnce {
		t.Fatalf("%d files crossed: %v", len(wire.fetched()), wire.fetched())
	}
	// And a landing frees the pipe for the next one.
	a.remotePrefetched(landed[0])
	wire.farFile("/srv/app/out/late.txt", "", "y", 99)
	if a.prefetchWritten(writeEvent("out/late.txt")) == nil {
		t.Fatal("the cap never lifted after a prefetch landed")
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

// A file dropped onto the browse page crosses on the deposit door and comes
// back as THE PATH THE ENGINE PUT IT AT. Nothing here derives that path: the
// far machine stamped the name and chose the folder, and this side shows what
// it was told (internal/remote's [remote.DepositedFile]).
func TestDepositCrossesAndAnswersWithTheEnginesOwnPath(t *testing.T) {
	a, wire, _ := hostedFixture(t)
	source := &hostSource{files: a.rfiles}
	landed, err := source.Deposit("notes.txt", []byte("hello"))
	if err != nil {
		t.Fatalf("a deposit has to cross: %v", err)
	}
	if landed != "/srv/app/.aforge-v3/sessions/9f3c/attachments/20260824-141233-a1b2c3d4-notes.txt" {
		t.Fatalf("the path is the engine's answer and came back as %q", landed)
	}
	wire.mu.Lock()
	calls := append([]depositCall(nil), wire.depositCalls...)
	wire.mu.Unlock()
	if len(calls) != 1 || calls[0].name != "notes.txt" || calls[0].body != "hello" {
		t.Fatalf("what crossed the wire was %+v", calls)
	}
	// THE NAME CROSSES AS THE PAGE GAVE IT. A name that is really a path is the
	// engine's to refuse, and a surface that pre-empted that would be taking a
	// permission decision on the wrong machine.
	if calls[0].mime != "" {
		t.Fatalf("the door carries no type and the surface must not invent one: %q", calls[0].mime)
	}
}

// The engine's refusal — a name that is a path, a file over the ceiling — is
// passed through with nothing softening it, which is [remote.Client.FetchFile]'s
// law and applies in this direction for the same reason.
func TestADepositRefusedByTheEngineKeepsTheEnginesSentence(t *testing.T) {
	a, wire, _ := hostedFixture(t)
	wire.depositErr = errors.New("engine: \"../../.ssh/authorized_keys\" is a path and not a name")
	source := &hostSource{files: a.rfiles}
	landed, err := source.Deposit("../../.ssh/authorized_keys", []byte("x"))
	if err == nil || landed != "" {
		t.Fatalf("a deposit the engine refused was accepted: %q", landed)
	}
	if err.Error() != wire.depositErr.Error() {
		t.Fatalf("the engine's sentence was reworded: %q", err)
	}
}

// And a drop with no wire under it at all says so rather than panicking. It is
// a state the door should never be open in — a session whose agent bears no
// client gets no [remoteFiles] and therefore no door — so the sentence is about
// the connection and not about what the person did.
func TestADepositWithNoConnectionSaysSo(t *testing.T) {
	source := &hostSource{files: &remoteFiles{}}
	landed, err := source.Deposit("notes.txt", []byte("x"))
	if err == nil || landed != "" {
		t.Fatalf("a deposit with no wire was accepted: %q", landed)
	}
	if err.Error() != depositUnreachableWord {
		t.Fatalf("the sentence is %q", err)
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
