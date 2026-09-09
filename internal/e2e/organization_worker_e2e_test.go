//go:build e2e

// Ordinary task workers read one sourced record in two separate repositories.
// Receipts, persisted provenance and artifacts establish the result; model prose
// is not the oracle. Both owners number their first task #1 without collapsing.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

const (
	// orgWorkerWall is how long ONE task may take from the moment it is admitted
	// until every node of it has come to rest. The work is a tool call and a
	// small file; five minutes is the far edge of that and still an answer rather
	// than a hang. The two tasks run one after the other, each on its own clock.
	orgWorkerWall = 5 * time.Minute
	// orgWorkerCap is what the whole scenario may spend before this is a fault in
	// the run rather than in the work. One turn and two tiny tasks on a cheap
	// model are cents; half a dollar is a runaway.
	orgWorkerCap = 0.50
)

// testOrganizationWorker is the scenario, on a throwaway machine of its own.
//
// IT BUILDS ITS OWN WORLD so that it can be called from anywhere — but [newWorld]
// moves AFORGE_HOME with t.Setenv, so a caller inside TestOrganizationE2E must
// call it under its own t.Run and not on the parent's t, or the parent's home
// moves under it. A caller that would rather share the parent's machine calls
// [organizationWorkerOn] with the world it already has.
func testOrganizationWorker(t *testing.T) {
	w := newWorld(t)
	pinEveryTextModel(t)
	organizationWorkerOn(t, w)
}

// organizationWorkerOn is the whole scenario against a machine somebody else
// built.
func organizationWorkerOn(t *testing.T, w *world) {
	started := time.Now()

	store, err := workspace.Open(orgStorePath())
	if err != nil {
		t.Fatalf("workspace.Open(%s): %v", orgStorePath(), err)
	}
	defer store.Close()

	name := "Berthing-" + orgNonce(t)
	key := "LEDGER-" + orgNonce(t)

	held, err := orgWorkerSeed(t, w, store, name, key)
	if err != nil {
		t.Fatalf("seed the shared record: %v", err)
	}
	collection := workspace.Ref{Kind: workspace.CollectionKind, ID: held.collection}
	t.Logf("SEED collection=%q record=%s revision=%d source=%+v key=%s",
		name, held.record.ID, held.record.Revision, held.record.Source, key)

	// ── the same work, in two conversations that both call it task #1 ──
	first := orgWorkerTask(t, w, name, "context-first.json")
	second := orgWorkerTask(t, w, name, "context-second.json")

	for _, one := range []*orgWorkerJourney{first, second} {
		orgWorkerReadsTheNote(t, one, held)
	}

	// ── and the note is exactly as it was ──
	orgWorkerLeftItAlone(t, store, collection, held)

	// ── two task ones, and they are two addresses ──
	orgWorkerTwoAddresses(t, w, store, held.collection, first, second)

	// THE PINNING IS CHECKED RATHER THAN ASSUMED, over this scenario's own window
	// of the machine's ledger. A second model id is a row this file does not know
	// about choosing a model, and the figure below would then be about a mixture.
	//
	// The spend is LOGGED AND NOT BILLED to the world: a caller that runs this
	// beside the conversation journeys reads the whole run off the same ledger at the
	// end, and billing here would count these calls twice in that total.
	usd, models := ledgerSince(t, started)
	t.Logf("WORKER RUN wall=%s spend=$%.6f models=%v", time.Since(started).Round(time.Second), usd, models)
	if usd > orgWorkerCap {
		t.Errorf("this scenario spent $%.4f, past the $%.2f a turn and two small tasks should cost", usd, orgWorkerCap)
	}
	for _, model := range models {
		if model != e2eModel {
			t.Errorf("%q answered in this scenario; every text row is pinned at %q, so a second id means a row this file does not know about", model, e2eModel)
		}
	}
}

// orgWorkerSeeded is the shared record as the STORE holds it, with the identity
// of the conversation that made it. Everything the worker writes down is checked
// against this and against nothing else.
type orgWorkerSeeded struct {
	collection string // the collection's own id
	source     string // the session id of the conversation that recorded the note
	key        string // the nonce in the note's text, which no brief ever says
	record     workspace.ContextRecord
}

// orgWorkerSeed makes the collection and records one note against it THROUGH THE
// PRODUCT'S OWN DOOR — a conversation reaching for its shared context tool — so
// that the record's source is the runtime's stamp rather than a value this test
// supplied. A record a test wrote straight into the store would prove nothing
// about provenance, which is half of what the worker is asked for.
func orgWorkerSeed(t *testing.T, w *world, store *workspace.Store, name, key string) (orgWorkerSeeded, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	holder, err := store.Create(ctx, name)
	if err != nil {
		return orgWorkerSeeded{}, fmt.Errorf("create the collection %q: %w", name, err)
	}

	source, sourcePlace := w.open(t.TempDir(), orgSeam(w))
	sourceID := session.PlaceSession(sourcePlace)
	t.Logf("SOURCE session=%s", sourceID)

	ask := fmt.Sprintf(`Record a shared note against the collection named %q, using your shared context tool. Title it "Berth rate finding". Its text must contain this exact token and nothing may change it: %s

Record it once. Do not write any file and do not start a task.`, name, key)

	out := w.say(source, ask, answerNo)
	if out.Err != nil {
		return orgWorkerSeeded{}, fmt.Errorf("the recording turn ended in an error: %w", out.Err)
	}
	receipt, at := orgReceipt(out, orgContextTool)
	if at < 0 {
		return orgWorkerSeeded{}, fmt.Errorf("no %s receipt in %v; nothing was recorded through the product's own door", orgContextTool, out.names())
	}
	if receipt.Failed {
		return orgWorkerSeeded{}, fmt.Errorf("the %s call failed: %s", orgContextTool, shorten(receipt.Output, 400))
	}

	// THE STORE IS THE WITNESS. What the worker is later checked against is read
	// here, from the store, including the SOURCE the runtime stamped — the one
	// value in this whole scenario that no model anywhere supplied.
	// Provider latency must not consume the later storage operation's deadline.
	ctx, readCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer readCancel()
	records, err := store.ContextFor(ctx, []workspace.Ref{{Kind: workspace.CollectionKind, ID: holder.ID}})
	if err != nil {
		return orgWorkerSeeded{}, fmt.Errorf("ContextFor the collection: %w", err)
	}
	if len(records) != 1 {
		return orgWorkerSeeded{}, fmt.Errorf("the collection holds %d shared records, want exactly 1: %+v", len(records), records)
	}
	only := records[0]
	if !strings.Contains(only.Text, key) {
		return orgWorkerSeeded{}, fmt.Errorf("the stored record does not carry %s; it says %q", key, shorten(only.Text, 300))
	}
	want := workspace.Ref{Kind: workspace.ConversationKind, ID: sourceID}
	if only.Source != want {
		return orgWorkerSeeded{}, fmt.Errorf("the record's source is %+v, want the conversation that spoke, %+v", only.Source, want)
	}
	return orgWorkerSeeded{collection: holder.ID, source: sourceID, key: key, record: only}, nil
}

// orgWorkerJourney is one owning conversation: its repository, its session, the
// file its task was asked for, and the graph the task left behind.
type orgWorkerJourney struct {
	run      *familyRun
	session  string
	artifact string
}

// orgWorkerConfig is what the door wires for a task that also stands in an
// organization: [familyConfig] for the work and [orgSeam] for the collection,
// in that order, so that a seam changing later has the last word on its own
// fields.
func orgWorkerConfig(w *world) func(*session.Config) {
	family := familyConfig(w)
	seam := orgSeam(w)
	return func(cfg *session.Config) {
		family(cfg)
		seam(cfg)
		cfg.Divide = false
		cfg.AskConsent = false
	}
}

// orgWorkerTask opens one owning conversation on a repository of its own, starts
// ONE task at the typed door, and waits for it to come to rest.
func orgWorkerTask(t *testing.T, w *world, collection, artifact string) *orgWorkerJourney {
	t.Helper()
	ground := newRepositoryGround(t)
	desk := filepath.Join(t.TempDir(), "desk")
	if err := os.MkdirAll(desk, 0o755); err != nil {
		t.Fatalf("make the conversation's own folder: %v", err)
	}

	// A REAL CONVERSATION, with a title and a lastUserAt: world.go skips a
	// meta.json with no lastUserAt on purpose, so an owning conversation seeded
	// without one would be invisible to anything that reads it back by reference.
	place := w.place(w.projectBucket(desk), desk)
	owner := session.PlaceSession(place)
	if err := session.SaveMeta(place.Dir, session.Meta{
		ID:         owner,
		Title:      "Berth ledger " + orgNonce(t),
		Workspace:  desk,
		Created:    time.Now().Add(-time.Hour),
		LastUserAt: time.Now().Add(-30 * time.Minute),
		Model:      e2eModel,
	}); err != nil {
		t.Fatalf("seed the owning conversation: %v", err)
	}

	agent := w.openAt(desk, place, orgWorkerConfig(w))
	if _, err := agent.ReferPlace(ground, session.PlaceSaid); err != nil {
		t.Fatalf("refer %s: %v", ground, err)
	}

	// THE KEY IS NOT IN THIS BRIEF, and that is the whole of what makes the answer
	// evidence. The worker is told which collection to look at and what shape to
	// write; every value it fills in has to come out of a tool call.
	ask := fmt.Sprintf(`Read one shared note and write down what it says. This is one small piece of work for one worker: do not hand any of it out and do not start further work.

Using your tools, read the shared context that applies right now to the collection named %q. Do not record a note, do not revise one and do not withdraw one: this work only reads.

Then write the file %s into the folder this work is about. Its whole content must be one JSON object and nothing else:

  {"key": "...", "record": "...", "revision": 1, "source": "..."}

  · key is the all-capitals hyphenated token in the note that applies now;
  · record is that note's own id, and revision is its revision number;
  · source is the conversation the tool says recorded the note — not this one.

Copy every value exactly from what the tool returned. Do not invent one and do not answer from anything you already believe. Write that one file and no other.`, collection, artifact)

	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), orgWorkerWall)
	defer cancel()

	root, title, _, err := agent.StartTask(ctx, ask)
	if err != nil {
		t.Fatalf("start the task: %v", err)
	}
	t.Logf("TASK #%d started in session %s: %q  (ground %s, artifact %s)", root, owner, title, ground, artifact)

	run := &familyRun{t: t, w: w, agent: agent, place: place, ground: ground, root: root}
	run.timedOut = !run.waitForRest(ctx)
	run.wall = time.Since(started)
	run.rows = readTaskRows(t, place.Tasks())
	run.divisions = readDivisions(t, place)
	run.usd, run.models = ledgerSince(t, started)
	t.Logf("  task #%d · wall %s · cost $%.6f · models %v\n%s",
		root, run.wall.Round(time.Second), run.usd, run.models, run.nodeLog())
	if run.timedOut {
		t.Fatalf("the task did not come to rest inside %s; the nodes are:\n%s", orgWorkerWall, run.nodeLog())
	}
	return &orgWorkerJourney{run: run, session: owner, artifact: artifact}
}

// orgWorkerReadsTheNote is the assertion this whole file exists for: the file the
// worker wrote carries the key, the record's own id, its revision, and the
// SOURCE of the note — which is the conversation that recorded it and is never
// the worker's own session.
func orgWorkerReadsTheNote(t *testing.T, journey *orgWorkerJourney, held orgWorkerSeeded) {
	t.Helper()
	run := journey.run
	root := run.rootRow()
	if root.State == string(session.TaskFailed) {
		t.Errorf("the task failed (%s: %s) at work that is one tool call and one file", root.Ending, shorten(root.Report, 400))
	}
	if parts := run.parts(); len(parts) > 0 {
		t.Errorf("%d part(s) were handed out under a task with no division road on its belt at all", len(parts))
	}
	t.Logf("  task #%d came home as %q, having written %v", root.ID, root.Merge, root.Changed)

	// THE ARTIFACT IS LOOKED FOR WHERE THE WORK CAN HAVE LEFT IT. A repository
	// family that merged put it in the person's own material; one whose branch was
	// kept still wrote it in the working copy it worked in, and this scenario is
	// about what the worker READ rather than about how its branch came home.
	where, body := run.ground, readGroundFile(t, run.ground, journey.artifact)
	if strings.TrimSpace(body) == "" && root.Worktree != "" {
		where, body = root.Worktree, readGroundFile(t, root.Worktree, journey.artifact)
	}
	if strings.TrimSpace(body) == "" {
		t.Fatalf("the task was asked to write %s and neither the person's repository (%s) nor the working copy it worked in (%s) holds it; it came home as %q and its nodes are:\n%s",
			journey.artifact, run.ground, root.Worktree, root.Merge, run.nodeLog())
	}
	answer := orgWorkerRead(t, filepath.Join(where, journey.artifact), body)

	// THE KEY IS THE UNFORGEABLE HALF. It is a nonce minted this run, put into the
	// note by a different conversation and named in no brief, so a worker that
	// wrote it down opened the note seconds earlier and a worker that did not
	// cannot have it — from training, from a previous run or from anywhere else.
	if got := orgWorkerText(answer, "key"); !strings.Contains(got, held.key) {
		t.Errorf("%s in %s says the key is %q, and the note that applies carries %s — the token is a nonce this run minted and no brief ever said, so this answer did not come out of the note (the note says %q)",
			journey.artifact, where, got, held.key, shorten(held.record.Text, 200))
	}
	if got := orgWorkerText(answer, "record"); !strings.Contains(got, held.record.ID) {
		t.Errorf("%s names the record %q; the note that applies is %s — a shared record read without its identity cannot be revised, withdrawn or traced",
			journey.artifact, got, held.record.ID)
	}
	if got, want := orgWorkerText(answer, "revision"), strconv.Itoa(held.record.Revision); got != want {
		t.Errorf("%s says the record is at revision %q, and the store holds revision %s — a reading with no revision on it cannot be told from a stale one",
			journey.artifact, got, want)
	}
	source := orgWorkerText(answer, "source")
	if !strings.Contains(source, held.source) {
		t.Errorf("%s says the note came from %q; it was recorded by the conversation %s — provenance a worker guessed is not provenance",
			journey.artifact, source, held.source)
	}
	if strings.Contains(source, journey.session) && journey.session != held.source {
		t.Errorf("%s names its OWN conversation %s as the note's source; a worker that only read a record does not become the source of it",
			journey.artifact, journey.session)
	}
	t.Logf("  %s in %s → key ✓ record=%s revision=%s source=%s", journey.artifact, where,
		orgWorkerText(answer, "record"), orgWorkerText(answer, "revision"), source)
}

// orgWorkerLeftItAlone is the read-only half: a worker that was handed sourced
// context did not get to change it by reading it. It is asserted on the STORE,
// after both tasks have settled, because a receipt saying nothing was written is
// not the same fact.
func orgWorkerLeftItAlone(t *testing.T, store *workspace.Store, collection workspace.Ref, held orgWorkerSeeded) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	after, err := store.Context(ctx, held.record.ID)
	if err != nil {
		t.Fatalf("Context(%s) after the work ran: %v", held.record.ID, err)
	}
	if after.Revision != held.record.Revision {
		t.Errorf("the record is at revision %d after two workers only read it, and was at %d before", after.Revision, held.record.Revision)
	}
	if after.Withdrawn {
		t.Errorf("the record is withdrawn after two workers only read it")
	}
	if !strings.Contains(after.Text, held.key) {
		t.Errorf("the record now says %q and no longer carries %s", shorten(after.Text, 300), held.key)
	}
	if after.Source != held.record.Source {
		t.Errorf("the record's source is now %+v, and was %+v — reading a record does not re-stamp who made it", after.Source, held.record.Source)
	}
	history, err := store.ContextHistory(ctx, held.record.ID)
	if err != nil {
		t.Fatalf("ContextHistory(%s): %v", held.record.ID, err)
	}
	if len(history) != held.record.Revision {
		t.Errorf("the record's history holds %d revision(s) after two workers only read it, want the %d it was seeded with", len(history), held.record.Revision)
	}
	records, err := store.ContextFor(ctx, []workspace.Ref{collection})
	if err != nil {
		t.Fatalf("ContextFor after the work ran: %v", err)
	}
	if len(records) != 1 || records[0].ID != held.record.ID {
		t.Errorf("the collection holds %d record(s) after two workers only read one: %+v", len(records), records)
	}
	t.Logf("  the note is untouched: revision %d, %d revision(s) of history, one record on the collection", after.Revision, len(history))
}

// orgWorkerTwoAddresses is the second half of the acceptance: both conversations
// numbered their work TASK #1, and the two are different addresses.
//
// A TASK NUMBER IS LOCAL TO ITS CONVERSATION (internal/workspace's Ref carries
// the session id for exactly this reason), so the claim is checked three ways: the
// two references are not equal, they resolve through their owners separately, and
// neither repository holds the other's file.
func orgWorkerTwoAddresses(t *testing.T, w *world, store *workspace.Store, collection string, first, second *orgWorkerJourney) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if first.session == second.session {
		t.Fatalf("both tasks ran in one conversation (%s); there is no second address for this to be about", first.session)
	}
	refs := make([]workspace.Ref, 0, 2)
	for _, journey := range []*orgWorkerJourney{first, second} {
		if journey.run.root != 1 {
			t.Errorf("the first task of the conversation %s is #%d, not #1; this scenario is about two conversations that both call their work task one",
				journey.session, journey.run.root)
		}
		ref := workspace.Ref{
			Kind:      workspace.TaskKind,
			ID:        strconv.FormatUint(journey.run.root, 10),
			SessionID: journey.session,
		}
		if err := ref.Validate(); err != nil {
			t.Fatalf("the reference to task %d of %s is not a reference at all: %v", journey.run.root, journey.session, err)
		}
		refs = append(refs, ref)
		if err := store.Add(ctx, collection, ref); err != nil {
			t.Fatalf("add %+v to the collection: %v", ref, err)
		}
	}
	if refs[0] == refs[1] {
		t.Fatalf("the two task ones are the same reference %+v; a task number without its conversation is not an address", refs[0])
	}

	// THEY RESOLVE SEPARATELY, through the same read-only callback a door injects.
	// What is asserted is that two references came back and that they are still
	// the two that were asked about; the titles and states are LOGGED, because
	// what a settled task is called on a given day is a fact about a model.
	resolved, err := orgResolve(w)(ctx, store, refs)
	if err != nil {
		t.Fatalf("resolve the two task ones: %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("resolving two task ones answered with %d record(s): %+v", len(resolved), resolved)
	}
	if resolved[0].Ref == resolved[1].Ref {
		t.Errorf("both task ones resolved to %+v; identical task numbers in different conversations collapsed into one record", resolved[0].Ref)
	}
	for _, one := range resolved {
		if one.Ref != refs[0] && one.Ref != refs[1] {
			t.Errorf("resolving answered about %+v, which is neither task one that was asked about", one.Ref)
		}
		t.Logf("  %+v → title=%q state=%v location=%v available=%v unavailable=%v",
			one.Ref, one.Title, one.State, one.Location, one.Available, one.Unavailable)
	}

	// AND NEITHER REPOSITORY HOLDS THE OTHER'S FILE, which is the same claim read
	// off the disk: two conversations that both called their work task one wrote
	// into two separate grounds and never into each other's.
	if body := readGroundFile(t, first.run.ground, second.artifact); strings.TrimSpace(body) != "" {
		t.Errorf("%s, which the second conversation's task was asked for, is in the first conversation's repository %s", second.artifact, first.run.ground)
	}
	if body := readGroundFile(t, second.run.ground, first.artifact); strings.TrimSpace(body) != "" {
		t.Errorf("%s, which the first conversation's task was asked for, is in the second conversation's repository %s", first.artifact, second.run.ground)
	}
}

// orgWorkerRead cuts the JSON object out of what a worker wrote and answers its
// fields.
func orgWorkerRead(t *testing.T, path, body string) map[string]any {
	t.Helper()
	opens := strings.Index(body, "{")
	// The close brace is held one PAST itself, so that the object is cut with two
	// plain indices — the same reading as the neighbouring file's, written the way
	// gofmt leaves alone.
	shuts := strings.LastIndex(body, "}") + 1
	if opens < 0 || shuts <= opens {
		t.Fatalf("%s holds no JSON object: %s", path, shorten(body, 400))
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(body[opens:shuts]), &fields); err != nil {
		t.Fatalf("%s is not the object that was asked for (%v): %s", path, err, shorten(body, 400))
	}
	return fields
}

// orgWorkerText is one of those fields as text, and the empty string where the
// worker wrote no such field at all.
func orgWorkerText(fields map[string]any, name string) string {
	value, found := fields[name]
	if !found || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	if number, ok := value.(float64); ok && number == float64(int64(number)) {
		// A JSON number decodes as a float, and "revision 1" written back as
		// "1e+00" would be a failure about formatting rather than about reading.
		return strconv.FormatInt(int64(number), 10)
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
