package direction

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// world is a small organization to resolve against.
type world struct {
	t   *testing.T
	s   *Store
	ctx context.Context
}

func newWorld(t *testing.T) world { return world{t: t, s: openTest(t), ctx: context.Background()} }

func (w world) folder(name string) string { return folder(w.t, w.s, name) }

// nest places child inside parent: a governing edge.
func (w world) nest(parent, child string) {
	w.t.Helper()
	if err := w.s.Workspace().AddPlacement(w.ctx, parent, workspace.Ref{Kind: workspace.CollectionKind, ID: child}); err != nil {
		w.t.Fatal(err)
	}
}

func (w world) place(folder string, ref workspace.Ref) {
	w.t.Helper()
	if err := w.s.Workspace().AddPlacement(w.ctx, folder, ref); err != nil {
		w.t.Fatal(err)
	}
}

func (w world) unplace(folder string, ref workspace.Ref) {
	w.t.Helper()
	if err := w.s.Workspace().RemovePlacement(w.ctx, folder, ref); err != nil {
		w.t.Fatal(err)
	}
}

// reference files a ref in a folder as a reference membership: relevance, not authority.
func (w world) reference(folder string, ref workspace.Ref) {
	w.t.Helper()
	if err := w.s.Workspace().Add(w.ctx, folder, ref); err != nil {
		w.t.Fatal(err)
	}
}

// accept proposes a draft and has the person accept it.
func (w world) accept(d Draft) Revision {
	w.t.Helper()
	must := musts(w.t)
	return must(w.s.Accept(w.ctx, must(w.s.Propose(w.ctx, d, model)).Fence(), card(w.t, "yes")))
}

func (w world) resolve(refs ...workspace.Ref) Effective {
	w.t.Helper()
	eff, err := w.s.Resolve(w.ctx, Subject{Refs: refs, Phase: PhaseChat})
	if err != nil {
		w.t.Fatal(err)
	}
	return eff
}

func chat(id string) workspace.Ref { return workspace.Ref{Kind: workspace.ConversationKind, ID: id} }

func ids(list []Applied) []string {
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, a.Rev.ID)
	}
	return out
}

func titled(title, text string, targets ...Target) Draft {
	d := rule(text, targets...)
	d.Title = title
	return d
}

func governs(eff Effective, id string) *Applied {
	for i := range eff.Governing {
		if eff.Governing[i].Rev.ID == id {
			return &eff.Governing[i]
		}
	}
	return nil
}

// R1 — GOVERNING REACH WALKS PLACEMENTS ONLY. A reference membership in the
// folder a rule is on gives the rule no reach; a placement does.
func TestR1GoverningReachWalksPlacementsOnly(t *testing.T) {
	w := newWorld(t)
	launch := w.folder("Launch")
	r := w.accept(rule("reports never include phone numbers", folderTarget(launch, Subtree)))
	w.reference(launch, chat("w"))
	if eff := w.resolve(chat("w")); len(eff.Governing) != 0 {
		t.Fatalf("a reference gave a rule authority: %v", ids(eff.Governing))
	}
	w.place(launch, chat("w"))
	if eff := w.resolve(chat("w")); governs(eff, r.ID) == nil {
		t.Fatalf("a placement did not: %v", ids(eff.Governing))
	}
}

// R2 and C23 — A FOLDER'S DIRECTION REACHES DESCENDANTS ONLY WITH SUBTREE.
func TestR2AndC23DescendantsOnlyWhenTheRuleSaysSubtree(t *testing.T) {
	w := newWorld(t)
	launch, copyFolder := w.folder("Launch"), w.folder("Copy")
	w.nest(launch, copyFolder)
	w.place(copyFolder, chat("w"))
	w.place(launch, chat("direct"))
	r := w.accept(rule("formal tone", folderTarget(launch, Direct)))
	if eff := w.resolve(chat("w")); governs(eff, r.ID) != nil {
		t.Fatal("a direct rule reached a descendant")
	}
	if eff := w.resolve(chat("direct")); governs(eff, r.ID) == nil {
		t.Fatal("a direct rule did not reach work placed in its folder")
	}
	d := r.draft()
	d.Targets = []Target{folderTarget(launch, Subtree)}
	musts(t)(w.s.Revise(w.ctx, r.Fence(), d, AsPerson(card(t, "include subfolders"))))
	a := governs(w.resolve(chat("w")), r.ID)
	if a == nil || len(a.Via) != 1 || !reflect.DeepEqual(a.Via[0].Chain, []string{copyFolder, launch}) {
		t.Fatalf("after the person chose subtree: %+v", a)
	}
}

// R3 — COMPATIBLE DIRECTION IS A UNION, deduplicated by record, across every
// parent; order is display order and carries no precedence.
func TestR3DirectionIsAUnionAcrossParentsInDisplayOrder(t *testing.T) {
	w := newWorld(t)
	trips, europe, conference := w.folder("Trips"), w.folder("Europe"), w.folder("Conference")
	w.nest(trips, europe)
	w.nest(trips, conference)
	w.place(europe, chat("w"))
	w.place(conference, chat("w"))
	budget := w.accept(titled("Budget", "at most $200 a night", folderTarget(trips, Subtree)))
	near := w.accept(titled("Tone", "formal tone", folderTarget(europe, Direct)))
	own := w.accept(titled("Own", "send the draft by Friday", chatTarget("w")))
	everywhere := w.accept(titled("Everywhere", "never share passwords", Target{Kind: TargetEverywhere, Ref: Everywhere}))
	eff := w.resolve(chat("w"))
	want := []string{own.ID, near.ID, budget.ID, everywhere.ID}
	if got := ids(eff.Governing); !reflect.DeepEqual(got, want) {
		t.Fatalf("display order %v, want subject, folder by depth, everywhere: %v", got, want)
	}
	b := governs(eff, budget.ID)
	if len(b.Via) != 1 {
		// One path per subject ref: the shortest; the second parent is the same
		// record reached again, not a second record.
		t.Fatalf("the union repeated a record or lost its path: %+v", b.Via)
	}
	if eff.Placements[europe] != 0 || eff.Placements[conference] != 0 || eff.Placements[trips] != 1 {
		t.Fatalf("placements %v", eff.Placements)
	}
}

// R4 and C16 — THE CONFERENCE EXCEPTION. R2 on Conference overrides R1 on
// Trips; conference work gets both, annotated, and R1's compatible part (the
// formal tone) still applies. Work elsewhere in Trips gets R1 unannotated.
func TestR4AndC16AnOverrideDeliversBothAndAnnotatesThem(t *testing.T) {
	w := newWorld(t)
	trips, conference, europe := w.folder("Trips"), w.folder("Conference"), w.folder("Europe")
	w.nest(trips, conference)
	w.nest(trips, europe)
	w.place(conference, chat("conference-work"))
	w.place(europe, chat("europe-work"))
	r1 := w.accept(rule("at most $200 a night, formal tone", folderTarget(trips, Subtree)))
	d := rule("at most $400 a night", folderTarget(conference, Subtree))
	d.Links = []Link{{Kind: Overrides, To: r1.ID}}
	r2 := w.accept(d)
	eff := w.resolve(chat("conference-work"))
	a1, a2 := governs(eff, r1.ID), governs(eff, r2.ID)
	if a1 == nil || a2 == nil {
		t.Fatalf("both must be delivered: %v", ids(eff.Governing))
	}
	if !reflect.DeepEqual(a2.Overrides, []string{r1.ID}) || !reflect.DeepEqual(a1.OverriddenBy, []string{r2.ID}) {
		t.Fatalf("annotations: r1 %+v, r2 %+v", a1.OverriddenBy, a2.Overrides)
	}
	elsewhere := w.resolve(chat("europe-work"))
	if a := governs(elsewhere, r1.ID); a == nil || len(a.OverriddenBy) != 0 || governs(elsewhere, r2.ID) != nil {
		t.Fatalf("outside Conference: %+v", elsewhere.Governing)
	}
}

// R5 — AN EXCLUSION OF THE SUBJECT REMOVES THE RECORD, whether it names the
// work itself or its legacy workspace.
func TestR5ExcludingTheSubjectRemovesTheRecord(t *testing.T) {
	w := newWorld(t)
	launch := w.folder("Launch")
	w.place(launch, chat("w"))
	w.place(launch, chat("other"))
	d := rule("formal tone", folderTarget(launch, Subtree))
	d.Exclusions = []Exclusion{{Kind: TargetConversation, Ref: "w"}}
	r := w.accept(d)
	if governs(w.resolve(chat("w")), r.ID) != nil {
		t.Fatal("an excluded subject was governed")
	}
	if governs(w.resolve(chat("other")), r.ID) == nil {
		t.Fatal("the exclusion reached work it did not name")
	}
}

// R6 — MULTI-PARENT EXCLUSIONS REMOVE PATHS, NOT RECORDS. W reaches Trips
// through Europe (not excluded) and Conference (excluded): the rule applies,
// and the path through Conference is shown as blocked. Work reachable only
// through Conference is not governed.
func TestR6AnExcludedParentBlocksOnlyThePathsThroughIt(t *testing.T) {
	w := newWorld(t)
	trips, europe, conference := w.folder("Trips"), w.folder("Europe"), w.folder("Conference")
	w.nest(trips, europe)
	w.nest(trips, conference)
	w.place(europe, chat("w"))
	w.place(conference, chat("w"))
	w.place(conference, chat("only-conference"))
	d := rule("formal tone", folderTarget(trips, Subtree))
	d.Exclusions = []Exclusion{{Kind: TargetCollection, Ref: conference}}
	r := w.accept(d)
	a := governs(w.resolve(chat("w")), r.ID)
	if a == nil {
		t.Fatal("a record with a surviving path was removed")
	}
	if len(a.Via) != 1 || !reflect.DeepEqual(a.Via[0].Chain, []string{europe, trips}) {
		t.Fatalf("via %+v", a.Via)
	}
	if len(a.Blocked) != 1 || !reflect.DeepEqual(a.Blocked[0].Chain, []string{conference, trips}) {
		t.Fatalf("blocked %+v", a.Blocked)
	}
	if governs(w.resolve(chat("only-conference")), r.ID) != nil {
		t.Fatal("work reachable only through the excluded folder was governed")
	}
}

// R7 and C16 — MOVE VERSUS REFERENCE. Moving W out of Launch stops Launch's
// rules governing it from the next resolve; a reference left behind adds
// nothing; direction aimed at W itself survives the move; the revision an
// earlier run saw is still readable by its (id, revision).
func TestR7AndC16AMoveChangesFutureResolutionAndKeepsWhatWasSeen(t *testing.T) {
	w := newWorld(t)
	launch, archive := w.folder("Launch"), w.folder("Archive")
	w.place(launch, chat("w"))
	launchRule := w.accept(rule("launch copy is upbeat", folderTarget(launch, Direct)))
	own := w.accept(rule("W is always signed by Dana", chatTarget("w")))
	before := w.resolve(chat("w"))
	if governs(before, launchRule.ID) == nil || governs(before, own.ID) == nil {
		t.Fatalf("before the move: %v", ids(before.Governing))
	}
	w.unplace(launch, chat("w"))
	w.place(archive, chat("w"))
	w.reference(launch, chat("w"))
	after := w.resolve(chat("w"))
	if governs(after, launchRule.ID) != nil {
		t.Fatal("Launch's rule still governs moved work, or the reference carried it")
	}
	if governs(after, own.ID) == nil {
		t.Fatal("direction aimed at the work itself did not survive the move")
	}
	if before.Snapshot == after.Snapshot {
		t.Fatal("two different reads share a snapshot identity")
	}
	seen := governs(before, launchRule.ID).Rev
	if got := musts(t)(w.s.At(w.ctx, seen.ID, seen.Revision)); got.Text != seen.Text || got.State != Accepted {
		t.Fatalf("the revision an earlier run saw: %+v", got)
	}
}

// R8 and C23 — AN UNRESOLVED CONFLICT IS RETURNED, NEVER DECIDED. It is a
// conflict only where both apply; an overrides link between the two is the
// person's resolution.
func TestR8AConflictWhereBothApplyIsReturnedNotDecided(t *testing.T) {
	w := newWorld(t)
	launch, other := w.folder("Launch"), w.folder("Other")
	w.place(launch, chat("w"))
	w.place(other, chat("w"))
	w.place(launch, chat("launch-only"))
	a := w.accept(rule("ship Friday", folderTarget(launch, Direct)))
	d := rule("ship Monday", folderTarget(other, Direct))
	d.Links = []Link{{Kind: ConflictsWith, To: a.ID}}
	b := w.accept(d)
	eff := w.resolve(chat("w"))
	if governs(eff, a.ID) == nil || governs(eff, b.ID) == nil || len(eff.Conflicts) != 1 {
		t.Fatalf("both must be delivered with one conflict: %v %+v", ids(eff.Governing), eff.Conflicts)
	}
	if got := eff.Conflicts[0]; got != (Conflict{A: min(a.ID, b.ID), B: max(a.ID, b.ID)}) {
		t.Fatalf("conflict %+v", got)
	}
	if only := w.resolve(chat("launch-only")); len(only.Conflicts) != 0 {
		t.Fatalf("a conflict was reported where one side does not apply: %+v", only.Conflicts)
	}
	cur := musts(t)(w.s.Current(w.ctx, b.ID))
	musts(t)(w.s.Link(w.ctx, cur.Fence(), Link{Kind: Overrides, To: a.ID}, card(t, "Monday wins here")))
	if resolved := w.resolve(chat("w")); len(resolved.Conflicts) != 0 {
		t.Fatalf("the person's override did not resolve the conflict: %+v", resolved.Conflicts)
	}
}

// R9 — A PROPOSAL NEVER GOVERNS. It rides along labelled as proposed for a
// window, a page at a time, and says when there are more.
func TestR9ProposalsArePendingNeverGoverning(t *testing.T) {
	w := newWorld(t)
	launch := w.folder("Launch")
	w.place(launch, chat("w"))
	p := musts(t)(w.s.Propose(w.ctx, rule("never include phone numbers", folderTarget(launch, Direct)), model))
	eff := w.resolve(chat("w"))
	if len(eff.Governing) != 0 || len(eff.Pending) != 1 || eff.Pending[0].Rev.ID != p.ID || eff.Pending[0].Rev.State != Proposed {
		t.Fatalf("a proposal: governing %v, pending %v", ids(eff.Governing), ids(eff.Pending))
	}
	for i := 0; i < MaxPending+2; i++ {
		musts(t)(w.s.Propose(w.ctx, rule(fmt.Sprint("proposal ", i), folderTarget(launch, Direct)), model))
	}
	eff = w.resolve(chat("w"))
	if len(eff.Pending) != MaxPending || !eff.PendingMore {
		t.Fatalf("pending page %d, more %v", len(eff.Pending), eff.PendingMore)
	}
	// Past the window a proposal stops riding along; it is still proposed.
	w.s.now = func() time.Time { return time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC) }
	if late := w.resolve(chat("w")); len(late.Pending) != 0 {
		t.Fatalf("stale proposals still ride along: %d", len(late.Pending))
	}
	if cur := musts(t)(w.s.Current(w.ctx, p.ID)); cur.State != Proposed {
		t.Fatalf("aging changed the record: %s", cur.State)
	}
}

// R10 — A FINDING NEVER GOVERNS. It is informational: the subject's own places
// and the folders it is a member of, one hop — never two.
func TestR10FindingsAreInformationalOneMembershipHop(t *testing.T) {
	w := newWorld(t)
	venue, parent := w.folder("Venue"), w.folder("Parent")
	w.reference(venue, chat("w"))
	w.nest(parent, venue)
	direct := musts(t)(w.s.Note(w.ctx, finding("the room holds 40", chatTarget("w")), model))
	hop := musts(t)(w.s.Note(w.ctx, finding("parking is on level 2", folderTarget(venue, Direct)), model))
	far := musts(t)(w.s.Note(w.ctx, finding("the city has a tram", folderTarget(parent, Direct)), model))
	eff := w.resolve(chat("w"))
	if len(eff.Governing) != 0 || len(eff.Pending) != 0 {
		t.Fatalf("a finding governed or pended: %v %v", ids(eff.Governing), ids(eff.Pending))
	}
	got := ids(eff.Informational.Items)
	if len(got) != 2 || !contains(got, direct.ID) || !contains(got, hop.ID) || contains(got, far.ID) {
		t.Fatalf("informational %v; want the direct and one-hop findings only", got)
	}
	for i := 0; i < MaxInformational; i++ {
		musts(t)(w.s.Note(w.ctx, finding(fmt.Sprint("fact ", i), chatTarget("w")), model))
	}
	page, err := w.s.Informational(w.ctx, Subject{Refs: []workspace.Ref{chat("w")}, Phase: PhaseChat}, 0, 10)
	if err != nil || len(page.Items) != 10 || !page.More {
		t.Fatalf("page %d more %v: %v", len(page.Items), page.More, err)
	}
	rest, err := w.s.Informational(w.ctx, Subject{Refs: []workspace.Ref{chat("w")}, Phase: PhaseChat}, 50, 10)
	if err != nil || len(rest.Items) != 2 || rest.More {
		t.Fatalf("last page %d more %v: %v", len(rest.Items), rest.More, err)
	}
}

func contains(list []string, id string) bool {
	for _, x := range list {
		if x == id {
			return true
		}
	}
	return false
}

// R11 — A LEGACY WORKSPACE MATCHES ONLY IMPORTED RECORDS, by cleaned path.
func TestR11ALegacyWorkspaceMatchesImportedRecordsByCleanedPath(t *testing.T) {
	w := newWorld(t)
	item := ImportItem{
		Legacy: Legacy{Store: LegacyStanding, ID: "hold-1", Version: "1/1", SHA256: strings.Repeat("a", 64)},
		Revisions: []ImportRevision{{Draft: rule("reports never include phone numbers",
			Target{Kind: TargetLegacyWorkspace, Ref: "/work/launch"}), State: Accepted,
			Receipt: Receipt{Actor: ActorPerson, Door: DoorCard, Ref: "proposal-1"}}},
	}
	res, err := w.s.Import(w.ctx, ImportRun{ID: "run", Mode: "apply", Binary: "test"}, item)
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(root string) Effective {
		eff, err := w.s.Resolve(w.ctx, Subject{Refs: []workspace.Ref{chat("w")}, Workspace: root, Phase: PhaseTask})
		if err != nil {
			t.Fatal(err)
		}
		return eff
	}
	a := governs(resolve("/work/launch/"), res.Record)
	if a == nil || a.Legacy == nil || a.Legacy.ID != "hold-1" {
		t.Fatalf("the imported hold at its cleaned workspace: %+v", a)
	}
	if governs(resolve("/work/launch/sub"), res.Record) != nil || governs(resolve(""), res.Record) != nil {
		t.Fatal("a legacy workspace matched something other than its own path")
	}
	if _, err := w.s.Propose(w.ctx, rule("x", Target{Kind: TargetLegacyWorkspace, Ref: "/work/launch"}), model); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a new write created a legacy workspace target: %v", err)
	}
}

// R12 — SUPERSEDED RECORDS ARE NOT LIVE.
func TestR12ASupersededRecordNoLongerReaches(t *testing.T) {
	w := newWorld(t)
	old := w.accept(rule("at most $200", chatTarget("w")))
	replacement := musts(t)(w.s.Supersede(w.ctx, rule("at most $250", chatTarget("w")), []Fence{old.Fence()}, card(t, "raise")))
	if got := ids(w.resolve(chat("w")).Governing); !reflect.DeepEqual(got, []string{replacement.ID}) {
		t.Fatalf("governing %v", got)
	}
}

// C15 — THE TRIP UNLINK. A budget aimed at trip T survives removing T's
// discovered association with Europe, and never governs another trip.
func TestC15ATripsOwnBudgetSurvivesUnlinkingAndReachesNoOtherTrip(t *testing.T) {
	w := newWorld(t)
	europe := w.folder("Europe")
	w.reference(europe, chat("trip-t"))
	w.place(europe, chat("trip-u"))
	budget := w.accept(rule("trip T is at most $150 a night", chatTarget("trip-t")))
	if err := w.s.Workspace().Remove(w.ctx, europe, chat("trip-t")); err != nil {
		t.Fatal(err)
	}
	if governs(w.resolve(chat("trip-t")), budget.ID) == nil {
		t.Fatal("the trip lost its own budget when an association was removed")
	}
	if governs(w.resolve(chat("trip-u")), budget.ID) != nil {
		t.Fatal("trip T's budget governed another trip")
	}
}

// E1 — "for Launch, reports never include phone numbers" is one record: a
// rule on Launch with the reach the card chose, governing a report task placed
// under Launch.
func TestE1OneSentenceIsOneGoverningRecord(t *testing.T) {
	w := newWorld(t)
	launch, reports := w.folder("Launch"), w.folder("Reports")
	w.nest(launch, reports)
	task := workspace.Ref{Kind: workspace.TaskKind, ID: "4", SessionID: "chat"}
	w.place(reports, task)
	r := w.accept(Draft{Kind: Rule, Title: "Reports never include phone numbers",
		Text: "for Launch, reports never include phone numbers", Quote: "for Launch, reports never include phone numbers",
		QuoteOrigin: AdoptedWording, Source: Source{Class: SourceConversation, ID: "chat"},
		Targets: []Target{folderTarget(launch, Subtree)}})
	eff := w.resolve(task, chat("chat"))
	if got := ids(eff.Governing); !reflect.DeepEqual(got, []string{r.ID}) {
		t.Fatalf("governing %v", got)
	}
	if a := eff.Governing[0]; a.Via[0].From != task || !reflect.DeepEqual(a.Via[0].Chain, []string{reports, launch}) {
		t.Fatalf("provenance %+v", a.Via)
	}
}

// THE BOUNDS STOP THE WORK AND SAY WHERE TO NARROW. More than 64 governing
// records, or more than 64 KiB of their wording, is ErrGoverningTooLarge
// naming the heaviest target; ancestry beyond 256 edges is ErrClosureTooLarge.
func TestResolveStopsAtItsBoundsAndNamesTheHeaviestTarget(t *testing.T) {
	w := newWorld(t)
	busy := w.folder("Busy")
	w.place(busy, chat("w"))
	for i := 0; i <= MaxGoverning; i++ {
		w.accept(rule(fmt.Sprint("rule ", i), folderTarget(busy, Direct)))
	}
	_, err := w.s.Resolve(w.ctx, Subject{Refs: []workspace.Ref{chat("w")}, Phase: PhaseChat})
	var large *GoverningTooLargeError
	if !errors.As(err, &large) || !errors.Is(err, ErrGoverningTooLarge) || large.Heaviest.Ref != busy || large.Records != MaxGoverning+1 {
		t.Fatalf("over the record bound: %v", err)
	}

	w2 := newWorld(t)
	for i := 0; i < 2; i++ {
		w2.accept(rule(fmt.Sprint(i, strings.Repeat("x", 40*1024)), chatTarget("w")))
	}
	if _, err := w2.s.Resolve(w2.ctx, Subject{Refs: []workspace.Ref{chat("w")}, Phase: PhaseChat}); !errors.Is(err, ErrGoverningTooLarge) {
		t.Fatalf("over the byte bound: %v", err)
	}

	w3 := newWorld(t)
	prev := w3.folder("F0")
	w3.place(prev, chat("deep"))
	for i := 1; i <= MaxClosureEdges; i++ {
		next := w3.folder(fmt.Sprint("F", i))
		w3.nest(next, prev)
		prev = next
	}
	if _, err := w3.s.Resolve(w3.ctx, Subject{Refs: []workspace.Ref{chat("deep")}, Phase: PhaseChat}); !errors.Is(err, ErrClosureTooLarge) {
		t.Fatalf("over the closure bound: %v", err)
	}
}

// A subject is validated before anything is read.
func TestResolveRefusesAMalformedSubject(t *testing.T) {
	w := newWorld(t)
	for name, subj := range map[string]Subject{
		"no refs":         {Phase: PhaseChat},
		"unknown phase":   {Refs: []workspace.Ref{chat("w")}, Phase: "lunch"},
		"a relative root": {Refs: []workspace.Ref{chat("w")}, Workspace: "work", Phase: PhaseChat},
		"a malformed ref": {Refs: []workspace.Ref{{Kind: workspace.TaskKind, ID: "1"}}, Phase: PhaseChat},
		"too many refs":   {Refs: manyChats(MaxSubjectRefs + 1), Phase: PhaseChat},
	} {
		if _, err := w.s.Resolve(w.ctx, subj); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func manyChats(n int) []workspace.Ref {
	out := make([]workspace.Ref, n)
	for i := range out {
		out[i] = chat(fmt.Sprint("chat-", i))
	}
	return out
}
