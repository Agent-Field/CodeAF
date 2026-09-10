package workspaceview

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// A TASK NUMBER IS NOT AN ADDRESS ON ITS OWN. Numbering restarts in every
// conversation, so two chats on one machine both have a work 1, and the whole
// reason internal/workspace carries a session id beside a task id is that these
// two must not be able to answer for each other.
func TestOneWorkNumberInTwoConversationsResolvesToItsOwnWork(t *testing.T) {
	home := newHome(t)
	first := home.conversation("aaaaaaaaaaaaaaa1", "the tariff table", time.Now().Add(-time.Hour))
	second := home.conversation("aaaaaaaaaaaaaaa2", "the invoice writer", time.Now().Add(-2*time.Hour))
	home.work(first, taskRow{ID: "1", Title: "read the tariff table", Status: "running"})
	home.work(second, taskRow{ID: "1", Title: "port the parser", Status: "done", Ended: time.Now().Add(-time.Minute)})
	home.live(first, "working", presenceWork{ID: "1", Title: "read the tariff table", State: "running"})

	records := home.resolve(t, nil,
		workspace.Ref{Kind: workspace.TaskKind, ID: "1", SessionID: first},
		workspace.Ref{Kind: workspace.TaskKind, ID: "1", SessionID: second},
	)

	if got := records[0]; !got.Available || got.Title != "read the tariff table" || got.State != "working" {
		t.Fatalf("the live conversation's work 1 read as %+v", got)
	}
	if got := records[1]; !got.Available || got.Title != "port the parser" || got.State != "done" {
		t.Fatalf("the other conversation's work 1 read as %+v", got)
	}
}

// A LIVE-LOOKING ROW IS NOT A LIVE ROW. The project's record says `running`
// forever about work whose window was killed, and the word a person must read
// for it is the one the runtime's own reading already gives: incomplete.
func TestWorkLeftBehindByAClosedWindowReadsAsIncomplete(t *testing.T) {
	home := newHome(t)
	id := home.conversation("bbbbbbbbbbbbbbb1", "the reconciler", time.Now().Add(-time.Hour))
	home.work(id, taskRow{ID: "3", Title: "fix the reconciler", Status: "running"})
	// A presence file three heartbeats old is a window that went away, which is
	// exactly the reading session/world.go refuses to believe.
	home.stale(id, "working", presenceWork{ID: "3", Title: "fix the reconciler", State: "running"})

	got := home.resolve(t, nil, workspace.Ref{Kind: workspace.TaskKind, ID: "3", SessionID: id})[0]
	if !got.Available {
		t.Fatalf("work nobody finished lost its record: %+v", got)
	}
	if got.State != "incomplete" {
		t.Fatalf("work left running reads as %q, want incomplete", got.State)
	}
	if got.Phase != "" {
		t.Fatalf("a row nothing live is behind narrated a phase %q", got.Phase)
	}
}

// A phase is a fact about a running node and belongs to the conversation that
// is holding it, so a check in flight reads as finishing rather than working.
func TestAConversationHoldingWorkSaysWhichLifeItIsIn(t *testing.T) {
	home := newHome(t)
	id := home.conversation("ccccccccccccccc1", "the parser", time.Now().Add(-time.Minute))
	home.work(id, taskRow{ID: "2", Title: "port the parser", Status: "running"})
	home.live(id, "working", presenceWork{ID: "2", Title: "port the parser", State: "running", Phase: session.TaskPhaseChecking})

	got := home.resolve(t, nil, workspace.Ref{Kind: workspace.TaskKind, ID: "2", SessionID: id})[0]
	if got.State != "finishing" || got.Phase != session.TaskPhaseChecking {
		t.Fatalf("a node under its check read as state %q phase %q", got.State, got.Phase)
	}
}

// EVERY MISSING SOURCE KEEPS ITS REFERENCE. A collection that quietly dropped
// the membership of a record it could not open would lose the person's
// organization to a temporary absence — and the sentence about work must never
// harden "I cannot find it" into "it was deleted", because the project's record
// is append-only, keeps only its newest rows, and has no row at all for work
// that has not landed.
func TestAMissingSourceKeepsItsReferenceAndSaysWhatWasFound(t *testing.T) {
	home := newHome(t)
	known := home.conversation("ddddddddddddddd1", "the ledger", time.Now().Add(-time.Hour))
	gone := filepath.Join(home.root, "no-such-report.md")

	refs := []workspace.Ref{
		{Kind: workspace.ConversationKind, ID: "eeeeeeeeeeeeeee9"},
		{Kind: workspace.TaskKind, ID: "9", SessionID: known},
		{Kind: workspace.ArtifactKind, ID: gone},
		{Kind: workspace.StandingKind, ID: "nosuchitem"},
	}
	records := home.resolve(t, nil, refs...)

	for at, got := range records {
		if got.Available || got.Unavailable == "" {
			t.Fatalf("%v came back available or silent: %+v", refs[at], got)
		}
		if got.Ref != refs[at] {
			t.Fatalf("the reference was not kept whole: %+v", got.Ref)
		}
	}
	if !strings.Contains(records[0].Unavailable, "not on this machine") {
		t.Fatalf("a missing conversation says %q", records[0].Unavailable)
	}
	work := records[1].Unavailable
	if !strings.Contains(work, "work 9") || !strings.Contains(work, "not landed") || !strings.Contains(work, "newest rows") {
		t.Fatalf("a missing row says %q, which does not say why it might be missing", work)
	}
	if !strings.Contains(records[2].Unavailable, "nothing at that path") {
		t.Fatalf("a missing file says %q", records[2].Unavailable)
	}
	if !strings.Contains(records[3].Unavailable, "standing order") {
		t.Fatalf("a missing standing order says %q", records[3].Unavailable)
	}
}

// The standing store is the owner of an ongoing thing, and this reading asks it
// for the one item it was asked about rather than listing every item a person
// owns to answer a question about one.
func TestAStandingOrderResolvesThroughItsOwnStore(t *testing.T) {
	home := newHome(t)
	item, err := home.standing.Create(standing.Item{
		Words:     "always use tabs in this repository",
		Workspace: home.workspace,
		When:      standing.When{Kind: standing.WhenHold},
	})
	if err != nil {
		t.Fatalf("the fixture item would not be made: %v", err)
	}

	got := home.resolve(t, nil, workspace.Ref{Kind: workspace.StandingKind, ID: item.ID})[0]
	switch {
	case !got.Available:
		t.Fatalf("a kept standing order read as unavailable: %+v", got)
	case got.Title != "always use tabs in this repository":
		t.Fatalf("the standing order is titled %q", got.Title)
	case got.State != string(standing.StatusActive):
		t.Fatalf("the standing order reads as %q, want active", got.State)
	case got.Location != home.workspace:
		t.Fatalf("the standing order sits at %q, want %q", got.Location, home.workspace)
	}
}

// ONE READING PER CALL, AND NOTHING WRITTEN. A folder full of references must
// cost one walk of the machine however many conversations it names, and asking
// what is in a folder must not be an act that changes anything.
func TestOneCallReadsTheWorldOnceAndChangesNothing(t *testing.T) {
	home := newHome(t)
	first := home.conversation("fffffffffffffff1", "the ledger", time.Now().Add(-time.Hour))
	second := home.conversation("fffffffffffffff2", "the tariff table", time.Now().Add(-2*time.Hour))
	home.work(first, taskRow{ID: "1", Title: "read the ledger", Status: "done", Ended: time.Now().Add(-time.Minute)})
	home.work(second, taskRow{ID: "1", Title: "read the tariff table", Status: "done", Ended: time.Now().Add(-time.Minute)})
	item, err := home.standing.Create(standing.Item{
		Words:     "keep an eye on the release notes",
		Workspace: home.workspace,
		When:      standing.When{Kind: standing.WhenHold},
	})
	if err != nil {
		t.Fatalf("the fixture item would not be made: %v", err)
	}
	folder := home.collection(t, "Pricing")
	report := filepath.Join(home.root, "report.md")
	if err := os.WriteFile(report, []byte("the report\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The two file-backed owners are snapshotted whole. The collection database
	// is deliberately not: SQLite is entitled to touch its own file, and what is
	// being proved here is that reading a folder does not disturb the
	// CONVERSATIONS, THE PROJECT'S RECORD or the standing orders it names.
	beforePlaces, beforeStanding := snapshot(t, home.places), snapshot(t, home.standing.Root())
	records := home.resolve(t, nil,
		workspace.Ref{Kind: workspace.CollectionKind, ID: folder},
		workspace.Ref{Kind: workspace.ConversationKind, ID: first},
		workspace.Ref{Kind: workspace.ConversationKind, ID: second},
		workspace.Ref{Kind: workspace.TaskKind, ID: "1", SessionID: first},
		workspace.Ref{Kind: workspace.TaskKind, ID: "1", SessionID: second},
		workspace.Ref{Kind: workspace.StandingKind, ID: item.ID},
		workspace.Ref{Kind: workspace.ArtifactKind, ID: report},
	)

	if home.reads != 1 {
		t.Fatalf("one call read the machine %d times", home.reads)
	}
	for _, got := range records {
		if !got.Available {
			t.Fatalf("%v resolved to nothing: %+v", got.Ref, got)
		}
	}
	if after := snapshot(t, home.places); !reflect.DeepEqual(beforePlaces, after) {
		t.Fatal("reading a folder changed a conversation or the project's record")
	}
	if after := snapshot(t, home.standing.Root()); !reflect.DeepEqual(beforeStanding, after) {
		t.Fatal("reading a folder changed a standing order")
	}
}

// A folder's order is the person's own, and a view of it is the same list with
// more said about each row — never a different arrangement.
func TestMembersAnswerInTheCollectionsOwnOrder(t *testing.T) {
	home := newHome(t)
	chat := home.conversation("aaaaaaaaaaaaaaab", "the ledger", time.Now().Add(-time.Hour))
	report := filepath.Join(home.root, "pricing.md")
	if err := os.WriteFile(report, []byte("pricing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outer, inner := home.collection(t, "Startup"), home.collection(t, "Marketing")
	order := []workspace.Ref{
		{Kind: workspace.ArtifactKind, ID: report},
		{Kind: workspace.CollectionKind, ID: inner},
		{Kind: workspace.ConversationKind, ID: chat},
	}
	for _, ref := range order {
		if err := home.collections.Add(context.Background(), outer, ref); err != nil {
			t.Fatalf("adding %v: %v", ref, err)
		}
	}

	records, err := home.resolver().Members(context.Background(), home.collections, outer)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	if len(records) != len(order) {
		t.Fatalf("a folder of %d references answered %d records", len(order), len(records))
	}
	for at, ref := range order {
		if records[at].Ref != ref {
			t.Fatalf("position %d holds %v, want %v", at, records[at].Ref, ref)
		}
		if !records[at].Available {
			t.Fatalf("%v resolved to nothing: %+v", ref, records[at])
		}
	}
	if records[1].Title != "Marketing" {
		t.Fatalf("the nested folder is called %q", records[1].Title)
	}
	if records[1].State != "" || records[1].Location != "" {
		t.Fatalf("a folder claimed a state or a place: %+v", records[1])
	}

	if _, err := home.resolver().Members(context.Background(), home.collections, "0123456789abcdef"); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("a folder that is not there answered %v", err)
	}
}

// A malformed address is refused BEFORE anything is opened: one bad reference
// must not cost a walk of the machine, and it must not come back dressed as a
// record that merely could not be found.
func TestAMalformedReferenceIsRefusedBeforeAnythingIsRead(t *testing.T) {
	home := newHome(t)
	_, err := home.resolver().Resolve(context.Background(), home.collections, []workspace.Ref{
		{Kind: workspace.ConversationKind, ID: "aaaaaaaaaaaaaaa1"},
		{Kind: workspace.TaskKind, ID: "4"},
	})
	if !errors.Is(err, workspace.ErrInvalid) {
		t.Fatalf("a task with no conversation answered %v", err)
	}
	if home.reads != 0 {
		t.Fatalf("a refused call still read the machine %d times", home.reads)
	}
}

// A capability with nothing behind it is absent rather than quietly wrong: a
// resolver that was given no way to read conversations says so instead of
// reporting the person's chat as missing.
func TestAskingForSomethingThisReadingCannotOpenIsAFault(t *testing.T) {
	var bare Resolver
	if _, err := bare.Resolve(context.Background(), nil, []workspace.Ref{
		{Kind: workspace.ConversationKind, ID: "aaaaaaaaaaaaaaa1"},
	}); !errors.Is(err, ErrNoConversations) {
		t.Fatalf("an unwired reading answered %v", err)
	}
	if _, err := bare.Resolve(context.Background(), nil, []workspace.Ref{
		{Kind: workspace.StandingKind, ID: "somestandingid"},
	}); !errors.Is(err, ErrNoStandingStore) {
		t.Fatalf("an unwired reading answered %v", err)
	}
	if _, err := bare.Resolve(context.Background(), nil, []workspace.Ref{
		{Kind: workspace.CollectionKind, ID: "0123456789abcdef"},
	}); !errors.Is(err, ErrNoCollections) {
		t.Fatalf("an unwired reading answered %v", err)
	}
}

// A cancelled reading stops where it stands rather than finishing a walk
// nobody is waiting for.
func TestACancelledReadingStops(t *testing.T) {
	home := newHome(t)
	id := home.conversation("aaaaaaaaaaaaaaac", "the ledger", time.Now().Add(-time.Hour))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := home.resolver().Resolve(ctx, home.collections, []workspace.Ref{
		{Kind: workspace.ConversationKind, ID: id},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled reading answered %v", err)
	}
	if home.reads != 0 {
		t.Fatalf("a cancelled reading still read the machine %d times", home.reads)
	}
}

// A conversation answers with its own title, the word it uses for itself, and
// the project it was held in — and nothing at all when it is not live.
func TestAConversationReadsAsItsOwnProjectAndWord(t *testing.T) {
	home := newHome(t)
	quiet := home.conversation("aaaaaaaaaaaaaaad", "the ledger", time.Now().Add(-time.Hour))
	asking := home.conversation("aaaaaaaaaaaaaaae", "the tariff table", time.Now().Add(-time.Minute))
	home.live(asking, "waiting on you")

	records := home.resolve(t, nil,
		workspace.Ref{Kind: workspace.ConversationKind, ID: quiet},
		workspace.Ref{Kind: workspace.ConversationKind, ID: asking},
	)
	if got := records[0]; got.Title != "the ledger" || got.State != "" || got.Location != home.workspace {
		t.Fatalf("a conversation nobody is holding read as %+v", got)
	}
	if got := records[1]; got.State != "waiting on you" {
		t.Fatalf("a conversation stopped on a question reads as %q", got.State)
	}
}

// ── the fixture ─────────────────────────────────────────────────────────────

// fixture is a places root with real session folders, a real project record, a
// real standing store and a real collection database — the four owners this
// package reads, none of them stubbed, because what is being tested is whether
// their own answers are carried faithfully.
type fixture struct {
	root        string
	places      string
	bucket      string
	workspace   string
	standing    *standing.Store
	collections *workspace.Store
	reads       int
}

func newHome(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	home := &fixture{
		root:      root,
		places:    filepath.Join(root, "v3", "projects"),
		workspace: filepath.Join(root, "code", "pricing"),
	}
	home.bucket = filepath.Join(home.places, "-tmp-code-pricing")
	if err := os.MkdirAll(home.bucket, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := standing.Open(filepath.Join(root, "v3", "standing"))
	if err != nil {
		t.Fatal(err)
	}
	home.standing = store
	collections, err := workspace.Open(filepath.Join(root, "v3", "collections.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = collections.Close() })
	home.collections = collections
	return home
}

// resolver counts its readings so a test can assert that one call walks the
// machine once, or not at all.
func (f *fixture) resolver() Resolver {
	return Resolver{
		World: func() session.World {
			f.reads++
			return session.ReadWorld(f.places)
		},
		Standing: f.standing,
	}
}

func (f *fixture) resolve(t *testing.T, ctx context.Context, refs ...workspace.Ref) []Record {
	t.Helper()
	if ctx == nil {
		ctx = context.Background()
	}
	records, err := f.resolver().Resolve(ctx, f.collections, refs)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(records) != len(refs) {
		t.Fatalf("%d references answered %d records", len(refs), len(records))
	}
	return records
}

// conversation writes one session folder the way a real one is written: a
// journal, and a meta.json saying when the person last spoke.
func (f *fixture) conversation(id, title string, spoke time.Time) string {
	dir := filepath.Join(f.bucket, id)
	must(os.MkdirAll(dir, 0o700))
	must(os.WriteFile(filepath.Join(dir, "transcript.jsonl"), []byte("{}\n"), 0o600))
	meta := map[string]any{
		"id":         id,
		"title":      title,
		"workspace":  f.workspace,
		"created":    spoke.Add(-time.Hour).Format(time.RFC3339Nano),
		"lastUserAt": spoke.Format(time.RFC3339Nano),
	}
	raw, err := json.Marshal(meta)
	must(err)
	must(os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0o600))
	return id
}

// taskRow is one row of the project's record, in the fields a test cares about.
type taskRow struct {
	ID     string
	Title  string
	Status string
	Ground string
	Ended  time.Time
}

func (f *fixture) work(sessionID string, rows ...taskRow) {
	file, err := os.OpenFile(filepath.Join(f.bucket, "tasks.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	must(err)
	defer file.Close()
	for _, row := range rows {
		entry := map[string]any{
			"id":        row.ID,
			"title":     row.Title,
			"label":     row.Title,
			"status":    row.Status,
			"sessionId": sessionID,
			"endedAt":   row.Ended.Format(time.RFC3339Nano),
		}
		if row.Ended.IsZero() {
			entry["endedAt"] = time.Time{}.Format(time.RFC3339Nano)
		}
		if row.Ground != "" {
			entry["ground"] = row.Ground
		}
		raw, err := json.Marshal(entry)
		must(err)
		_, err = file.Write(append(raw, '\n'))
		must(err)
	}
}

// presenceWork is one node a conversation says it has out.
type presenceWork struct {
	ID    string
	Title string
	State string
	Phase string
}

// live writes a presence file a reader will believe, and stale one three
// heartbeats old — the difference between a window that is there and one that
// was killed with work in flight.
func (f *fixture) live(sessionID, state string, out ...presenceWork) {
	f.presence(sessionID, state, time.Now(), out...)
}

func (f *fixture) stale(sessionID, state string, out ...presenceWork) {
	f.presence(sessionID, state, time.Now().Add(-10*time.Minute), out...)
}

func (f *fixture) presence(sessionID, state string, at time.Time, out ...presenceWork) {
	running := make([]map[string]any, 0, len(out))
	for _, one := range out {
		row := map[string]any{"id": one.ID, "title": one.Title, "state": one.State}
		if one.Phase != "" {
			row["phase"] = one.Phase
		}
		running = append(running, row)
	}
	presence := map[string]any{
		"schema":    1,
		"sessionId": sessionID,
		"workspace": f.workspace,
		"pid":       os.Getpid(),
		"updatedAt": at.Format(time.RFC3339Nano),
		"state":     state,
	}
	if len(running) > 0 {
		presence["runningTasks"] = running
	}
	raw, err := json.Marshal(presence)
	must(err)
	must(os.WriteFile(filepath.Join(f.bucket, sessionID, "presence.json"), raw, 0o600))
}

func (f *fixture) collection(t *testing.T, name string) string {
	t.Helper()
	created, err := f.collections.Create(context.Background(), name)
	if err != nil {
		t.Fatalf("creating %q: %v", name, err)
	}
	return created.ID
}

// snapshot is every file under a root with its bytes, which is how a read-only
// claim is proved rather than asserted.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	found := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		found[relative] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatalf("reading the fixture back: %v", err)
	}
	return found
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
