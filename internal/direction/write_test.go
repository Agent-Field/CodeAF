package direction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A STALE WRITER IS REFUSED WHOLE (L1). Every change to an existing record
// names the revision its writer read; a writer holding an older one gets
// ErrConflict and the winner's revision is what stays.
func TestAStaleFenceIsRefusedAndTheWinnersRevisionStays(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	must := musts(t)
	p := must(s.Propose(ctx, rule("reports never include phone numbers"), model))
	stale := p.Fence()
	winner := must(s.Accept(ctx, stale, card(t, "first answer")))
	d := p.draft()
	d.Text = "reports may include phone numbers"
	for name, change := range map[string]func() error{
		"accept":   func() error { _, err := s.Accept(ctx, stale, card(t, "second answer")); return err },
		"reject":   func() error { _, err := s.Reject(ctx, stale, card(t, "c")); return err },
		"withdraw": func() error { _, err := s.Withdraw(ctx, stale, "paused", card(t, "c")); return err },
		"revise":   func() error { _, err := s.Revise(ctx, stale, d, AsPerson(card(t, "c"))); return err },
		"link": func() error {
			_, err := s.Link(ctx, stale, Link{Kind: DerivedFrom, To: "memory:7"}, card(t, "c"))
			return err
		},
		"supersede": func() error { _, err := s.Supersede(ctx, d, []Fence{stale}, card(t, "c")); return err },
	} {
		if err := change(); !errors.Is(err, ErrConflict) {
			t.Errorf("%s with a stale fence: %v", name, err)
		}
	}
	now := must(s.Current(ctx, p.ID))
	if now.Revision != winner.Revision || now.Text != winner.Text || now.Receipt.Ref != "first answer" {
		t.Fatalf("a stale writer changed the record: %+v", now)
	}
	if err := s.Verify(ctx); err != nil {
		t.Fatal(err)
	}
}

// Two handles race to answer one card. Exactly one wins, the other is told the
// record changed, and nothing is lost or doubled.
func TestRacingAnswersToOneCardProduceExactlyOneAcceptance(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "race.db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	p, err := first.Propose(ctx, rule("the budget is at most $200 a night"), model)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make([]error, 2)
	for i, s := range []*Store{first, second} {
		wg.Add(1)
		go func(i int, s *Store) {
			defer wg.Done()
			_, results[i] = s.Accept(ctx, p.Fence(), card(t, fmt.Sprint("answer ", i)))
		}(i, s)
	}
	wg.Wait()
	wins, conflicts := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrConflict):
			conflicts++
		default:
			t.Fatalf("a racing answer failed another way: %v", err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("wins %d, conflicts %d", wins, conflicts)
	}
	now, err := first.Current(ctx, p.ID)
	if err != nil || now.Revision != 2 || now.State != Accepted {
		t.Fatalf("after the race: %+v, %v", now, err)
	}
}

// SUPERSEDE IS ALL OR NOTHING. When one of the records it replaces moved, no
// replacement is written and the other old record is left current.
func TestASupersedeWithOneMovedRecordWritesNothing(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	must := musts(t)
	a := must(s.Accept(ctx, must(s.Propose(ctx, rule("formal tone"), model)).Fence(), card(t, "c")))
	b := must(s.Accept(ctx, must(s.Propose(ctx, rule("concise tone"), model)).Fence(), card(t, "c")))
	edit := b.draft()
	edit.Text = "concise tone, always"
	moved := must(s.Revise(ctx, b.Fence(), edit, AsPerson(card(t, "c"))))
	_, err := s.Supersede(ctx, rule("formal and concise tone"), []Fence{a.Fence(), b.Fence()}, card(t, "merge"))
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("superseded past a moved record: %v", err)
	}
	if got := must(s.Current(ctx, a.ID)); got.Revision != a.Revision || got.State != Accepted {
		t.Fatalf("the unmoved record changed: %+v", got)
	}
	if got := must(s.Current(ctx, b.ID)); got.Revision != moved.Revision {
		t.Fatalf("the moved record changed: %+v", got)
	}
	var records int
	if err := s.ws.ReadSnapshot(ctx, func(tx *sqlTx) error {
		return tx.QueryRowContext(ctx, "SELECT count(*) FROM direction_records").Scan(&records)
	}); err != nil {
		t.Fatal(err)
	}
	if records != 2 {
		t.Fatalf("a refused supersede left %d records", records)
	}
	// With both fences current it replaces both in one step.
	merged := must(s.Supersede(ctx, rule("formal and concise tone"), []Fence{a.Fence(), moved.Fence()}, card(t, "merge")))
	if merged.State != Accepted || len(merged.Links) != 2 {
		t.Fatalf("the replacement: %+v", merged)
	}
	for _, id := range []string{a.ID, b.ID} {
		if got := must(s.Current(ctx, id)); got.State != Superseded || got.Receipt.Ref != "merge" {
			t.Fatalf("%s after the replacement: %+v", id, got)
		}
	}
}

// A proposal that names what it replaces replaces it when — and only when —
// the person accepts it, fenced at the revision the proposal named.
func TestAcceptingAReplacementProposalSupersedesWhatItNamed(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	must := musts(t)
	old := must(s.Accept(ctx, must(s.Propose(ctx, rule("at most $200 a night"), model)).Fence(), card(t, "c")))
	d := rule("at most $250 a night")
	d.Links = []Link{{Kind: Supersedes, To: old.ID, ToRevision: old.Revision}}
	proposal := must(s.Propose(ctx, d, model))
	if got := must(s.Current(ctx, old.ID)); got.State != Accepted {
		t.Fatalf("a proposal replaced a rule before it was accepted: %+v", got)
	}
	must(s.Accept(ctx, proposal.Fence(), card(t, "yes")))
	if got := must(s.Current(ctx, old.ID)); got.State != Superseded {
		t.Fatalf("an accepted replacement left the old rule %s", got.State)
	}
	// A supersedes link cannot be added to a record that is no longer a
	// proposal: nothing would perform the replacement it claims.
	other := must(s.Accept(ctx, must(s.Propose(ctx, rule("another rule"), model)).Fence(), card(t, "c")))
	target := must(s.Accept(ctx, must(s.Propose(ctx, rule("a third rule"), model)).Fence(), card(t, "c")))
	_, err := s.Link(ctx, other.Fence(), Link{Kind: Supersedes, To: target.ID, ToRevision: target.Revision}, card(t, "c"))
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("a supersedes link was added to an accepted record: %v", err)
	}
}

// REJECTED WORDING STAYS REJECTED (C16). Reprocessing unchanged history —
// the extractor, a model, an unattended run reading the same conversation
// again — cannot propose it back, as a new record or as a revision; the person
// can, because it is their own act.
func TestRejectedWordingIsNotProposedAgainByAnyoneButThePerson(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	must := musts(t)
	text := "always copy the whole team on every email"
	must(s.Reject(ctx, must(s.Propose(ctx, rule(text), model)).Fence(), card(t, "no")))
	// Each writer asks in the one shape §4.1 lets it propose, so the refusal
	// is the rejected wording and nothing else.
	extracted := rule(text)
	extracted.Kind, extracted.QuoteOrigin = Decision, ModelExtracted
	occurrence := rule(text)
	occurrence.Source = Source{Class: SourceOccurrence, ID: "occurrence-9"}
	for by, d := range map[Actor]Draft{As(AuthorExtractor, "tidy"): extracted, model: rule(text),
		As(AuthorRun, "occurrence-9"): occurrence, As(AuthorSteward, "s"): rule(text)} {
		if _, err := s.Propose(ctx, d, by); !errors.Is(err, ErrRejectedText) {
			t.Errorf("%s proposed rejected wording again: %v", by.author.Class, err)
		}
	}
	other := must(s.Propose(ctx, rule("copy only the owner"), model))
	d := other.draft()
	d.Text = text
	if _, err := s.Revise(ctx, other.Fence(), d, model); !errors.Is(err, ErrRejectedText) {
		t.Errorf("a revision restored rejected wording: %v", err)
	}
	if _, err := s.Propose(ctx, rule(text), AsPerson(card(t, "I changed my mind"))); err != nil {
		t.Errorf("the person could not propose their own wording again: %v", err)
	}
}

// Every bound refuses the request whole, and a refused write stores nothing.
// The person writes here, so every refusal is a bound and not §4.1.
func TestBoundsRefuseTheWholeRecord(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	person := AsPerson(card(t, "bounds"))
	targets := make([]Target, MaxTargets+1)
	for i := range targets {
		targets[i] = chatTarget(fmt.Sprint("chat-", i))
	}
	exclusions := make([]Exclusion, MaxExclusions+1)
	for i := range exclusions {
		exclusions[i] = Exclusion{Kind: TargetConversation, Ref: fmt.Sprint("chat-", i)}
	}
	links := make([]Link, MaxLinks+1)
	for i := range links {
		links[i] = Link{Kind: DerivedFrom, To: fmt.Sprint("memory:", i)}
	}
	for name, d := range map[string]Draft{
		"empty title":           func() Draft { d := rule("x"); d.Title = ""; return d }(),
		"title too long":        func() Draft { d := rule("x"); d.Title = strings.Repeat("t", 257); return d }(),
		"text too long":         rule(strings.Repeat("x", 65537)),
		"text with an escape":   rule("red \x1b[31m text"),
		"quote too long":        func() Draft { d := rule("x"); d.Quote = strings.Repeat("q", MaxQuote+1); return d }(),
		"too many targets":      rule("x", targets...),
		"too many exclusions":   func() Draft { d := rule("x"); d.Exclusions = exclusions; return d }(),
		"too many links":        func() Draft { d := rule("x"); d.Links = links; return d }(),
		"subtree on a chat":     rule("x", Target{Kind: TargetConversation, Ref: "c", Reach: Subtree}),
		"one place two reaches": rule("x", folderTarget("f", Direct), folderTarget("f", Subtree)),
		"a legacy workspace":    rule("x", Target{Kind: TargetLegacyWorkspace, Ref: "/work/repo"}),
		"everywhere misspelled": rule("x", Target{Kind: TargetEverywhere, Ref: "all"}),
		"excluding everywhere": func() Draft {
			d := rule("x")
			d.Exclusions = []Exclusion{{Kind: TargetEverywhere, Ref: Everywhere}}
			return d
		}(),
		"unknown target kind":    rule("x", Target{Kind: "workspace", Ref: "w"}),
		"a task without session": rule("x", Target{Kind: TargetTask, Ref: "3"}),
		"an unsourced artifact":  func() Draft { d := rule("x"); d.Source = Source{Class: SourceArtifact, ID: "/a.md"}; return d }(),
		"unknown source class":   func() Draft { d := rule("x"); d.Source.Class = "rumour"; return d }(),
		"unknown quote origin":   func() Draft { d := rule("x"); d.QuoteOrigin = ""; return d }(),
		"a link to a position":   func() Draft { d := rule("x"); d.Links = []Link{{Kind: Overrides, To: "3"}}; return d }(),
		"an unfenced supersedes": func() Draft {
			d := rule("x")
			d.Links = []Link{{Kind: Supersedes, To: strings.Repeat("a", 32)}}
			return d
		}(),
	} {
		if _, err := s.Propose(ctx, d, person); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Exactly at each bound is accepted.
	d := rule(strings.Repeat("x", 65536), targets[:MaxTargets]...)
	d.Exclusions, d.Links, d.Quote = exclusions[:MaxExclusions], links[:MaxLinks], strings.Repeat("q", MaxQuote)
	if _, err := s.Propose(ctx, d, person); err != nil {
		t.Fatalf("a record exactly at every bound: %v", err)
	}
	// A folder that does not exist, and a link to a record that does not, are
	// refused too — this store owns both and can say so.
	if _, err := s.Propose(ctx, rule("x", folderTarget(strings.Repeat("f", 32), Direct)), person); err == nil {
		t.Error("proposed onto a folder that does not exist")
	}
	missing := rule("x")
	missing.Links = []Link{{Kind: Overrides, To: strings.Repeat("b", 32)}}
	if _, err := s.Propose(ctx, missing, person); !errors.Is(err, ErrNotFound) {
		t.Errorf("linked to a record that does not exist: %v", err)
	}
	var records int
	if err := s.ws.ReadSnapshot(ctx, func(tx *sqlTx) error {
		return tx.QueryRowContext(ctx, "SELECT count(*) FROM direction_records").Scan(&records)
	}); err != nil || records != 1 {
		t.Fatalf("refused writes left %d records: %v", records, err)
	}
}

// A STATEMENT RECEIPT IS VERIFIED AGAINST THE PERSON'S OWN LINE (§1.4). A
// harness note, a peer line and a line that does not contain the quote yield
// no receipt; a receipt authorizes only a revision that quotes the verified
// words and cites the hash of that line.
func TestAStatementReceiptComesOnlyFromThePersonsOwnWords(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	must := musts(t)
	typed := "for Launch, reports never include phone numbers"
	for name, line := range map[string]JournalLine{
		"a wake note":           {Role: "user", Note: true, Input: true, Text: "the user decided: " + typed},
		"a peer or tool line":   {Role: "assistant", Input: true, Text: typed},
		"not an input door":     {Role: "user", Input: false, Text: typed},
		"quote not in the line": {Role: "user", Input: true, Text: "please draft the launch report"},
	} {
		if _, err := FromVerifiedStatement(line, typed); err == nil {
			t.Errorf("%s produced a statement receipt", name)
		}
	}
	line := JournalLine{Role: "user", Input: true, Text: "ok — " + typed + ", thanks"}
	r, err := FromVerifiedStatement(line, typed)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(line.Text))
	anchored := rule(typed)
	anchored.Quote, anchored.QuoteOrigin = typed, PersonSaid
	anchored.Source = Source{Class: SourceConversation, ID: "chat", SHA256: hex.EncodeToString(sum[:])}
	unanchored := anchored
	unanchored.Source.SHA256 = strings.Repeat("0", 64)
	bad := must(s.Propose(ctx, unanchored, model))
	if _, err := s.Accept(ctx, bad.Fence(), r); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a statement accepted wording anchored to another line: %v", err)
	}
	good := must(s.Propose(ctx, anchored, model))
	accepted := must(s.Accept(ctx, good.Fence(), r))
	if accepted.Receipt.Door != DoorStatement || accepted.Receipt.Ref != hex.EncodeToString(sum[:]) {
		t.Fatalf("the statement receipt: %+v", accepted.Receipt)
	}
}

// Imports are idempotent by content, keep the old identity when told to, copy
// the old receipt class without promoting it, append when a newer version of
// the source arrives, and may create what no other writer can: a legacy
// workspace place.
func TestImportIsIdempotentByContentAndNeverMintsAPerson(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	must := musts(t)
	run := ImportRun{ID: "run-1", Mode: "apply", Binary: "test"}
	hash := func(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	hold := ImportItem{
		Legacy: Legacy{Store: LegacyStanding, ID: "hold-1", Version: "3/1", SHA256: hash("v1")},
		Revisions: []ImportRevision{{
			Draft: Draft{Kind: Rule, Title: "No phone numbers", Text: "Reports never include phone numbers.",
				QuoteOrigin: AdoptedWording, Source: Source{Class: SourceConversation, ID: "chat"},
				Targets:    []Target{{Kind: TargetLegacyWorkspace, Ref: "/work/launch"}},
				Exclusions: []Exclusion{{Kind: TargetConversation, Ref: "side-chat", At: created}}},
			State:     Accepted,
			Receipt:   Receipt{Actor: ActorLegacyDelegated, Door: DoorCard, Ref: "proposal-9", At: created},
			WrittenAt: created,
		}},
	}
	first, err := s.Import(ctx, run, hold)
	if err != nil || first.Outcome != Imported || first.Revision != 1 {
		t.Fatalf("first import: %+v, %v", first, err)
	}
	again, err := s.Import(ctx, run, hold)
	if err != nil || again.Outcome != Unchanged || again.Record != first.Record {
		t.Fatalf("second import: %+v, %v", again, err)
	}
	rec := must(s.Current(ctx, first.Record))
	if rec.Receipt.Actor != ActorLegacyDelegated || rec.Author.Class != AuthorMigration || !rec.WrittenAt.Equal(created) || rec.Lane() != LaneGoverning {
		t.Fatalf("the imported hold: %+v", rec)
	}
	changed := hold
	changed.Legacy.Version, changed.Legacy.SHA256 = "4/1", hash("v2")
	changed.Revisions = []ImportRevision{hold.Revisions[0]}
	changed.Revisions[0].Draft.Text = "Reports never include phone numbers or emails."
	appended, err := s.Import(ctx, run, changed)
	if err != nil || appended.Outcome != Appended || appended.Record != first.Record || appended.Revision != 2 {
		t.Fatalf("a changed source: %+v, %v", appended, err)
	}
	// A shared-context record keeps its id and every revision number, so an
	// exposure receipt citing (id, revision) still resolves.
	contextID := strings.Repeat("c", 32)
	ctxItem := ImportItem{Legacy: Legacy{Store: LegacyContexts, ID: contextID, Version: "2", SHA256: hash("ctx")}, ID: contextID,
		Revisions: []ImportRevision{
			{Draft: finding("Venue holds 40 people.", chatTarget("chat")), State: Informational, WrittenAt: created},
			{Draft: finding("Venue holds 40 people.", chatTarget("chat")), State: Withdrawn, WrittenAt: created.Add(time.Hour),
				Receipt: Receipt{Actor: ActorLegacyUnknown, Door: DoorMigration, Ref: run.ID}},
		}}
	got, err := s.Import(ctx, run, ctxItem)
	if err != nil || got.Record != contextID || got.Revision != 2 {
		t.Fatalf("context import: %+v, %v", got, err)
	}
	if r := must(s.At(ctx, contextID, 1)); r.State != Informational {
		t.Fatalf("revision 1 of the kept identity: %+v", r)
	}
	// An import cannot dress a missing receipt up, nor carry one where no
	// authority was given.
	for name, item := range map[string]ImportItem{
		"accepted without a receipt": {Legacy: Legacy{Store: LegacyStanding, ID: "h2", Version: "1", SHA256: hash("h2")},
			Revisions: []ImportRevision{{Draft: rule("x"), State: Accepted}}},
		"a statement it cannot verify": {Legacy: Legacy{Store: LegacyStanding, ID: "h3", Version: "1", SHA256: hash("h3")},
			Revisions: []ImportRevision{{Draft: rule("x"), State: Accepted, Receipt: Receipt{Actor: ActorPerson, Door: DoorStatement, Ref: "x"}}}},
		"a receipt on a proposal": {Legacy: Legacy{Store: LegacyMemory, ID: "m1", Version: "1", SHA256: hash("m1")},
			Revisions: []ImportRevision{{Draft: rule("x"), State: Proposed, Receipt: Receipt{Actor: ActorLegacyUnknown, Door: DoorMigration, Ref: "x"}}}},
	} {
		if _, err := s.Import(ctx, run, item); err == nil {
			t.Errorf("%s was imported", name)
		}
	}
	if err := s.FinishImportRun(ctx, run.ID, "", `{"imported":2}`); err != nil {
		t.Fatal(err)
	}
	if err := s.Verify(ctx); err != nil {
		t.Fatal(err)
	}
	// The legacy workspace place the import created survives a person's
	// revision of the record; the person cannot add a new one.
	cur := must(s.Current(ctx, first.Record))
	d := cur.draft()
	d.Title = "No phone numbers in reports"
	must(s.Revise(ctx, cur.Fence(), d, AsPerson(card(t, "c"))))
	cur = must(s.Current(ctx, first.Record))
	d = cur.draft()
	d.Targets = append(d.Targets, Target{Kind: TargetLegacyWorkspace, Ref: "/work/other"})
	if _, err := s.Revise(ctx, cur.Fence(), d, AsPerson(card(t, "c"))); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a person's revision added a legacy workspace: %v", err)
	}
}
