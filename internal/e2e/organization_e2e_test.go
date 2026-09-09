//go:build e2e

// Shared fixture helpers and the owner-resolution journey. The domain journeys
// live in organization_journeys_e2e_test.go; task workers use the existing family
// harness. All cases use disposable homes and actual model/tool execution.
package e2e

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
	"github.com/Agent-Field/aforge-v2/internal/workspaceview"
)

const (
	// orgCollectionsTool is the tool a model reaches for to find and read a
	// collection, and orgContextTool the one it records and reads a shared fact
	// with. They are the registered names on the belt — the strings
	// internal/session/manual_test.go checks the manual spells.
	orgCollectionsTool = "collections"
	orgContextTool     = "shared_context"
)

// orgResolve is the narrow read-only callback the runtime is handed so that a
// collection's references answer with their owners' current state.
//
// IT IS A CALLBACK AND NOT AN IMPORT, and that is the whole architecture of this
// slice in one line: internal/workspaceview reads session.World, so a session
// that imported it would close a cycle. The door injects; the engine calls.
func orgResolve(w *world) func(context.Context, *workspace.Store, []workspace.Ref) ([]workspace.ResolvedRef, error) {
	view := workspaceview.Resolver{World: session.ReadHome, Standing: w.store}
	return view.Resolve
}

// orgStorePath is the collection database the binary itself uses — the same
// expression cmd/aforge's `collections --db` defaults to — resolved under the
// throwaway AFORGE_HOME this lane installed.
func orgStorePath() string { return home.Join("v3", "collections.db") }

// orgSeam is what a door wires: where the organization lives, and how a
// reference is resolved. A conversation opened without it has neither tool on
// its belt, which is this codebase's law for a capability with nothing behind it.
func orgSeam(w *world) func(*session.Config) {
	return func(cfg *session.Config) {
		cfg.Organization = &session.Organization{
			Path:    orgStorePath(),
			Resolve: orgResolve(w),
		}
		// MEMORY STAYS NIL, and it is an assertion rather than an omission:
		// essential organization must work with learned memory disabled
		// (HANDOFF.md decision 10). [world.conversationConfig] never sets it, and
		// this line is here so that a future change to that helper cannot quietly
		// give this lane a brain to lean on.
		cfg.Memory = nil
		cfg.OneModel = true
		cfg.Interactive = true
		cfg.SpendRailUSD = 0.2
		// The conversation writes its answer into the person's folder, which the
		// ambient lane's read-only rules would stop at a question nobody is
		// standing at a keyboard to answer.
		policy, err := approval.Load(map[string]any{"default": "allow"})
		if err != nil {
			panic("approval.Load: " + err.Error())
		}
		cfg.ApprovalPolicy = &policy
	}
}

// ── bounds ──────────────────────────────────────────────────────────────────

const (
	// orgCap is what the whole file may spend before this is a fault in the run
	// rather than in the work. Eight short turns on a cheap model are cents.
	orgCap = 0.75
	// orgRequireLive turns this lane's skip into a failure. A scheduled run that
	// meant to buy real evidence and silently bought a skip is the one outcome
	// worse than a red, so a caller that intends a live run says so and gets an
	// error when the key is missing.
	orgRequireLive = "AFORGE_E2E_REQUIRE_LIVE"
)

// ── the run ─────────────────────────────────────────────────────────────────

// Owner resolution reads authoritative state without starting those owners.
func testOrganizationOwnerRead(t *testing.T) {
	if strings.TrimSpace(os.Getenv(orgRequireLive)) != "" &&
		strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")) == "" {
		t.Fatalf("%s is set and OPENROUTER_API_KEY is not: this lane was asked for real evidence and has no way to buy it", orgRequireLive)
	}
	w := newWorld(t)
	pinEveryTextModel(t)
	started := time.Now()

	fix := newOrgFixture(t, w)
	defer fix.close()

	// The two conversations, both on the fixture's folder, both wired to the
	// organization. reader is the one every journey asks; source is the one that
	// records the shared fact.
	reader, _ := w.open(fix.folder, orgSeam(w))

	t.Run("a named collection answers with its members' real titles and states", func(t *testing.T) {
		fix.aNamedCollectionResolvesItsMembers(t, reader)
	})

	// THE PINNING IS CHECKED RATHER THAN ASSUMED. A second model id in the
	// ledger is a row this file does not know about choosing a model, and the
	// figure this run cost would then be evidence about somebody else's profile.
	usd, models := ledgerSince(t, started)
	t.Logf("RUN wall=%s spend=$%.6f models=%v", time.Since(started).Round(time.Second), usd, models)
	w.bill("organization run (off the machine's own ledger)", usd)
	if usd > orgCap {
		t.Errorf("this file spent $%.4f, past the $%.2f a handful of short turns should cost", usd, orgCap)
	}
	for _, model := range models {
		if model != e2eModel {
			t.Errorf("%q answered in this run; every text row is pinned at %q, so a second id means a row this file does not know about", model, e2eModel)
		}
	}
}

// ── the fixture ─────────────────────────────────────────────────────────────

// orgFixture is one collection, its two members, and the nonces that make every
// answer in this file unforgeable.
type orgFixture struct {
	t      *testing.T
	world  *world
	store  *workspace.Store
	folder string

	name       string          // the collection's name, nonced
	collection workspace.Ref   // the collection itself, as a target
	chatTitle  string          // the seeded conversation's title, nonced
	taskTitle  string          // the seeded task's title, nonced
	taskState  string          // and the state its owner holds for it
	members    []workspace.Ref //  what was put in the collection, in order

}

// newOrgFixture seeds the world: two real records with real owners, and one
// collection naming both.
func newOrgFixture(t *testing.T, w *world) *orgFixture {
	t.Helper()
	fix := &orgFixture{
		t:         t,
		world:     w,
		folder:    t.TempDir(),
		name:      "Work-" + orgNonce(t),
		chatTitle: "Login investigation " + orgNonce(t),
		taskTitle: "Repair request retries " + orgNonce(t),
		taskState: string(session.TaskDone),
	}

	// A REAL CONVERSATION, with a real folder and a real meta.json. The title
	// only reaches a reader through session.ReadWorld, and world.go skips a
	// meta.json with no lastUserAt on purpose — an empty shell is not a
	// conversation somebody has had — so the seed sets it.
	bucket := w.projectBucket(fix.folder)
	seeded := w.place(bucket, fix.folder)
	seededID := session.PlaceSession(seeded)
	if err := session.SaveMeta(seeded.Dir, session.Meta{
		ID:         seededID,
		Title:      fix.chatTitle,
		Workspace:  fix.folder,
		Created:    time.Now().Add(-2 * time.Hour),
		LastUserAt: time.Now().Add(-1 * time.Hour),
		Model:      e2eModel,
	}); err != nil {
		t.Fatalf("seed a conversation: %v", err)
	}

	// AND A REAL TASK, in the project's own index — the record that outlives the
	// conversation that commissioned it, and the only place a landed task's state
	// can be read once its graph is gone.
	if err := os.WriteFile(seeded.Transcript(), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fix.seedTaskRow(seeded, seededID)
	second := w.place(bucket, fix.folder)
	secondID := session.PlaceSession(second)
	if err := session.SaveMeta(second.Dir, session.Meta{ID: secondID, Title: "Second issue", Workspace: fix.folder, Created: time.Now().Add(-time.Hour), LastUserAt: time.Now().Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second.Transcript(), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	other := *fix
	other.taskTitle = "Interrupted retry investigation " + orgNonce(t)
	other.taskState = string(session.TaskRunning)
	other.seedTaskRow(second, secondID)

	store, err := workspace.Open(orgStorePath())
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}
	fix.store = store

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	made, err := store.Create(ctx, fix.name)
	if err != nil {
		t.Fatalf("create the collection %q: %v", fix.name, err)
	}
	fix.collection = workspace.Ref{Kind: workspace.CollectionKind, ID: made.ID}
	ongoing, err := w.store.Create(standing.Item{Words: "Review dependency upgrades", Workspace: fix.folder, When: standing.When{Kind: standing.WhenEvery, Every: "24h"}, Does: standing.Action{Kind: standing.ActionSay, Say: "Report upgrades; patch releases only"}})
	if err != nil {
		t.Fatal(err)
	}
	fix.members = []workspace.Ref{
		{Kind: workspace.ConversationKind, ID: seededID},
		{Kind: workspace.TaskKind, ID: "1", SessionID: seededID},
		{Kind: workspace.TaskKind, ID: "1", SessionID: secondID},
		{Kind: workspace.StandingKind, ID: ongoing.ID},
	}
	for _, ref := range fix.members {
		if err := store.Add(ctx, made.ID, ref); err != nil {
			t.Fatalf("add %+v to %q: %v", ref, fix.name, err)
		}
	}
	t.Logf("FIXTURE collection=%q id=%s members=%+v", fix.name, made.ID, fix.members)
	t.Logf("FIXTURE conversation title=%q  task title=%q state=%q", fix.chatTitle, fix.taskTitle, fix.taskState)
	return fix
}

// seedTaskRow appends one landed node to the project's task index, the way the
// engine appends one when a node settles. It is written as a raw line rather
// than through the engine because this lane is about READING a task's state, and
// running a real task to produce one would buy minutes and dollars of evidence
// about a road that has its own file.
func (f *orgFixture) seedTaskRow(place session.Place, sessionID string) {
	t := f.t
	t.Helper()
	row := session.TaskIndexEntry{
		ID:            "1",
		Name:          "reconcile-berth-ledger",
		Label:         f.taskTitle,
		Title:         f.taskTitle,
		Status:        f.taskState,
		Outcome:       "The berth ledger was reconciled against the harbour manifest.",
		SessionID:     sessionID,
		EndedAt:       time.Now().Add(-30 * time.Minute),
		TranscriptURI: place.Transcript(),
	}
	line, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("encode a task index row: %v", err)
	}
	path := session.TaskIndexPath(place.Transcript())
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		t.Fatalf("append to %s: %v", path, err)
	}
	// Read it straight back through the engine's own reader, so a seed the
	// product cannot see fails here rather than three turns later as "the model
	// did not find it".
	found := false
	for _, back := range session.ReadTaskIndex(path) {
		if back.SessionID == sessionID && back.ID == "1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the seeded task row is not readable through session.ReadTaskIndex(%s)", path)
	}
}

func (f *orgFixture) close() {
	if f.store != nil {
		_ = f.store.Close()
	}
}

// ── journey one ─────────────────────────────────────────────────────────────

// aNamedCollectionResolvesItsMembers is the first journey: a person names a
// collection out loud, and what comes back is what its members ACTUALLY are.
//
// THE NEGATIVE IS THE HALF THAT MATTERS. Both titles are nonces minted this run,
// so a model that answered without calling the tool cannot have them; and the
// tool call must come BEFORE the file was written, or the file is a guess that
// happened to be checked afterwards.
func (f *orgFixture) aNamedCollectionResolvesItsMembers(t *testing.T, reader *session.Agent) {
	ask := fmt.Sprintf(`Look up the collection named %q with your tools, and read what is in it.

Then write the file collection.json in the current folder. Its whole content must be one JSON object and nothing else:

  {"collection": "<the collection's name>", "titles": [...], "states": [...]}

titles is the exact title of every member, copied from what the tool returned. states is the current state of every member the tool gave a state for. Copy the tool's own words; do not invent a title or a state, and if the tool returned none, write an empty list.`, f.name)

	out := f.w().say(reader, ask, answerNo)
	if out.Err != nil {
		t.Fatalf("the turn ended in an error: %v", out.Err)
	}

	lookup, at := orgActionReceipt(out, orgCollectionsTool, "show")
	if at < 0 {
		t.Fatalf("no %s receipt in %v; the conversation answered about a collection without opening one", orgCollectionsTool, out.names())
	}
	if lookup.Failed {
		t.Fatalf("the %s call failed: %s", orgCollectionsTool, shorten(lookup.Output, 400))
	}
	if !strings.Contains(lookup.Output, f.chatTitle) && !strings.Contains(lookup.Output, f.taskTitle) {
		t.Errorf("the %s receipt named neither member's title; a resolved collection answers with its members' own words, and this one said: %s",
			orgCollectionsTool, shorten(lookup.Output, 600))
	}

	var projection struct {
		Items []workspace.ResolvedRef `json:"items"`
	}
	if err := json.Unmarshal([]byte(lookup.Output), &projection); err != nil {
		t.Fatal(err)
	}
	standingSeen := false
	for _, row := range projection.Items {
		if row.Ref.Kind == workspace.StandingKind {
			standingSeen = row.Available && row.State == "active"
		}
	}
	if !standingSeen {
		t.Fatal("ongoing responsibility did not resolve through its owner")
	}
	tasks := map[string]string{}
	for _, row := range projection.Items {
		if row.Ref.Kind == workspace.TaskKind {
			tasks[row.Ref.SessionID+"/"+row.Ref.ID] = row.State
		}
	}
	for i, ref := range f.members[1:3] {
		want := "done"
		if i == 1 {
			want = "incomplete"
		}
		if got := tasks[ref.SessionID+"/"+ref.ID]; got != want {
			t.Fatalf("task %+v resolved to %q, want %q", ref, got, want)
		}
	}

	if wrote := orgCallAt(out, "write"); wrote >= 0 && wrote < at {
		t.Errorf("collection.json was written before %s was ever called; the answer cannot have come from the store", orgCollectionsTool)
	}

	answer := orgArtifact(t, f.folder, "collection.json")
	if !strings.Contains(answer.Collection, f.name) {
		t.Errorf("collection.json names %q, not the collection that was asked for, %q", answer.Collection, f.name)
	}
	for _, want := range []string{f.chatTitle, f.taskTitle} {
		if !orgAnyContains(answer.Titles, want) {
			t.Errorf("collection.json lists titles %v and none of them is %q — a member resolved to a reference rather than to what its owner calls it",
				answer.Titles, want)
		}
	}
	if !orgAnyContains(answer.States, f.taskState) {
		t.Errorf("collection.json lists states %v and none of them is %q — the task's state came from nowhere, or the reference was never resolved through its owner",
			answer.States, f.taskState)
	}
}

// w is the throwaway machine the fixture was built on. It is kept on the
// fixture so a journey method reads like one sentence.
func (f *orgFixture) w() *world { return f.world }

// ── small readers ───────────────────────────────────────────────────────────

// orgAnswer is every JSON artifact this file asks a model for, in one shape. The
// keys a given ask does not name simply stay zero.
type orgAnswer struct {
	Collection string   `json:"collection"`
	Titles     []string `json:"titles"`
	States     []string `json:"states"`
}

// orgArtifact reads one of those files off disk.
//
// IT IS LENIENT ABOUT THE WRAPPER AND STRICT ABOUT THE CONTENT. A model that
// fenced its JSON in backticks or wrote a sentence above it has still answered
// the question; a model that wrote the wrong titles has not. So the object is
// cut out between the first brace and the last, and everything after that is
// asserted exactly.
func orgArtifact(t *testing.T, folder, name string) orgAnswer {
	t.Helper()
	path := filepath.Join(folder, name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the turn was asked to write %s and did not: %v", path, err)
	}
	text := string(raw)
	opens := strings.Index(text, "{")
	shuts := strings.LastIndex(text, "}")
	if opens < 0 || shuts <= opens {
		t.Fatalf("%s holds no JSON object: %s", path, shorten(text, 400))
	}
	var answer orgAnswer
	if err := json.Unmarshal([]byte(text[opens:shuts+1]), &answer); err != nil {
		t.Fatalf("%s is not the object that was asked for (%v): %s", path, err, shorten(text, 400))
	}
	return answer
}

// orgReceipt is the first call of a name and where it stood in the order, or an
// index of -1 when the turn never made one.
func orgReceipt(out turn, name string) (call, int) {
	for at, one := range out.Calls {
		if one.Name == name {
			return one, at
		}
	}
	return call{}, -1
}

// orgCallAt is that index alone.
func orgCallAt(out turn, name string) int {
	_, at := orgReceipt(out, name)
	return at
}

// orgTurnText is everything one turn put in front of a person OR in front of the
// model: the reply and every tool receipt. It is what a negative assertion about
// a token has to search, because an automatic snapshot would arrive in a receipt
// nobody asked for — or in no receipt at all.
func orgTurnText(out turn) string {
	var all strings.Builder
	all.WriteString(out.Reply)
	for _, one := range out.Calls {
		all.WriteString("\n")
		all.WriteString(one.Args)
		all.WriteString("\n")
		all.WriteString(one.Output)
	}
	return all.String()
}

// orgAnyContains reports whether any of the strings carries the needle. The
// model copies a token into a list of its own making, so the test asks whether
// the token is THERE rather than whether the list is spelled a particular way.
func orgAnyContains(list []string, want string) bool {
	for _, one := range list {
		if strings.Contains(one, want) {
			return true
		}
	}
	return false
}

// orgNonce is a fresh token for this run and this run only.
//
// IT IS THE WHOLE DEFENCE AGAINST PRIOR KNOWLEDGE. Every title, every collection
// name and every shared fact in this file carries one, so a model that produced
// the right answer produced it from a tool call made seconds earlier and from
// nothing it could have been trained on or remembered from a previous run.
func orgNonce(t *testing.T) string {
	t.Helper()
	var raw [5]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatalf("mint a nonce: %v", err)
	}
	return strings.ToUpper(hex.EncodeToString(raw[:]))
}

func orgActionReceipt(out turn, name, action string) (call, int) {
	for i, c := range out.Calls {
		var p struct {
			Action string `json:"action"`
		}
		_ = json.Unmarshal([]byte(c.Args), &p)
		if c.Name == name && p.Action == action && !c.Failed {
			return c, i
		}
	}
	return call{}, -1
}
