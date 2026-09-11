package direction

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// at brings a fresh record of this kind to this state, the way the product
// would: a model proposes or notes, and the person acts on it.
func at(t *testing.T, s *Store, kind Kind, state State) Revision {
	return atBy(t, s, kind, state, model)
}

// atBy is at with the writer who proposes or notes the record. A finding
// leaves the informational state only through an import (design §3.4): no
// §4.1 writer withdraws or replaces one.
func atBy(t *testing.T, s *Store, kind Kind, state State, author Actor) Revision {
	t.Helper()
	must := musts(t)
	ctx := context.Background()
	d := rule(fmt.Sprintf("a %s that is %s, %p", kind, state, t))
	d.Kind = kind
	switch author.author.Class {
	case AuthorExtractor:
		d.QuoteOrigin = ModelExtracted
	case AuthorRun:
		d.Source = Source{Class: SourceOccurrence, ID: "occurrence-9"}
	}
	if kind == Finding {
		switch state {
		case Informational:
			return must(s.Note(ctx, d, author))
		case Withdrawn, Superseded:
			var receipt Receipt
			if state == Withdrawn {
				receipt = Receipt{Actor: ActorLegacyUnknown, Door: DoorMigration, Ref: importRun.ID}
			}
			id, err := workspace.NewID()
			if err != nil {
				t.Fatal(err)
			}
			res, err := s.Import(ctx, importRun, ImportItem{
				Legacy: Legacy{Store: LegacyContexts, ID: id, Version: "2", SHA256: legacyHash(id)}, ID: id,
				Revisions: []ImportRevision{
					{Draft: d, State: Informational, WrittenAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
					{Draft: d, State: state, Receipt: receipt, WrittenAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
				}})
			if err != nil {
				t.Fatal(err)
			}
			return must(s.Current(ctx, res.Record))
		}
		t.Fatalf("no finding is %s", state)
	}
	r := must(s.Propose(ctx, d, author))
	switch state {
	case Proposed:
		return r
	case Rejected:
		return must(s.Reject(ctx, r.Fence(), card(t, "c")))
	}
	r = must(s.Accept(ctx, r.Fence(), card(t, "c")))
	switch state {
	case Accepted:
		return r
	case Withdrawn:
		return must(s.Withdraw(ctx, r.Fence(), "paused", card(t, "c")))
	case Superseded:
		d.Text += " replaced"
		must(s.Supersede(ctx, d, []Fence{r.Fence()}, card(t, "c")))
		return must(s.Current(ctx, r.ID))
	}
	t.Fatalf("no rule is %s", state)
	return Revision{}
}

// THE STATE MACHINE, WRITER BY TRANSITION, AS §4.1 WRITES IT. Each row starts
// a fresh record in a state, asks for one change as one writer, and says
// whether the store must allow it or which refusal it must give. The person's
// transitions are exactly the design's list — proposed → accepted / rejected,
// accepted → withdrawn / superseded, withdrawn → accepted — plus revising a
// live record in place (§1.1), which is how exclusions and links are written.
// The only other writer with a transition is the model: it revises its own
// still-proposed record. A person-only change asked for by anyone else arrives
// with no receipt, because nobody else can construct one; ErrNoReceipt is the
// refusal.
func TestEveryWriterAndTransitionIsAllowedOrRefusedAsDesigned(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	person := card(t, "card-1")
	other := As(AuthorModel, "session-2")
	steward := As(AuthorSteward, "steward")
	extractor, run := As(AuthorExtractor, "tidy"), As(AuthorRun, "occurrence-9")
	var none PersonReceipt

	type change func(Revision) (Revision, error)
	revise := func(by Actor) change {
		return func(cur Revision) (Revision, error) {
			d := cur.draft()
			d.Text += " (revised)"
			return s.Revise(ctx, cur.Fence(), d, by)
		}
	}
	reviseExcluding := func(by Actor) change {
		return func(cur Revision) (Revision, error) {
			d := cur.draft()
			d.Exclusions = append(d.Exclusions, Exclusion{Kind: TargetConversation, Ref: "side-chat"})
			return s.Revise(ctx, cur.Fence(), d, by)
		}
	}
	accept := func(r PersonReceipt) change {
		return func(cur Revision) (Revision, error) { return s.Accept(ctx, cur.Fence(), r) }
	}
	reject := func(r PersonReceipt) change {
		return func(cur Revision) (Revision, error) { return s.Reject(ctx, cur.Fence(), r) }
	}
	withdraw := func(r PersonReceipt) change {
		return func(cur Revision) (Revision, error) { return s.Withdraw(ctx, cur.Fence(), "stopped by you", r) }
	}
	link := func(r PersonReceipt) change {
		return func(cur Revision) (Revision, error) {
			to := at(t, s, Rule, Accepted)
			kind := ConflictsWith
			if cur.Kind == Finding {
				kind = DerivedFrom
			}
			return s.Link(ctx, cur.Fence(), Link{Kind: kind, To: to.ID}, r)
		}
	}
	supersede := func(r PersonReceipt) change {
		return func(cur Revision) (Revision, error) {
			d := cur.draft()
			d.Text += " (replacement)"
			if _, err := s.Supersede(ctx, d, []Fence{cur.Fence()}, r); err != nil {
				return Revision{}, err
			}
			return s.Current(ctx, cur.ID)
		}
	}
	for _, row := range []struct {
		kind   Kind
		from   State
		name   string
		do     change
		want   error
		state  State // the state an allowed change leaves the record in
		author Actor // who proposed or noted the record; the model when zero
	}{
		// proposed: the person accepts or rejects; the model revises its own.
		{kind: Rule, from: Proposed, name: "the person revises", do: revise(AsPerson(person)), state: Proposed},
		{kind: Rule, from: Proposed, name: "the person revises in an exclusion", do: reviseExcluding(AsPerson(person)), state: Proposed},
		{kind: Rule, from: Proposed, name: "its author, the model, revises", do: revise(model), state: Proposed},
		{kind: Rule, from: Proposed, name: "its author, the model, revises in an exclusion", do: reviseExcluding(model), want: ErrTransition},
		{kind: Rule, from: Proposed, name: "another model revises", do: revise(other), want: ErrTransition},
		{kind: Rule, from: Proposed, name: "the steward revises the model's proposal", do: revise(steward), want: ErrTransition},
		{kind: Rule, from: Proposed, name: "the steward revises its own proposal", do: revise(steward), want: ErrTransition, author: steward},
		{kind: Decision, from: Proposed, name: "the extractor revises its own proposal", do: revise(extractor), want: ErrTransition, author: extractor},
		{kind: Rule, from: Proposed, name: "the person accepts", do: accept(person), state: Accepted},
		{kind: Rule, from: Proposed, name: "the steward accepts", do: accept(none), want: ErrNoReceipt},
		{kind: Rule, from: Proposed, name: "the person rejects", do: reject(person), state: Rejected},
		{kind: Rule, from: Proposed, name: "a model rejects", do: reject(none), want: ErrNoReceipt},
		{kind: Rule, from: Proposed, name: "the person withdraws", do: withdraw(person), want: ErrTransition},
		{kind: Rule, from: Proposed, name: "the person links", do: link(person), state: Proposed},
		{kind: Rule, from: Proposed, name: "a model links", do: link(none), want: ErrNoReceipt},
		{kind: Rule, from: Proposed, name: "the person supersedes", do: supersede(person), want: ErrTransition},

		// accepted: the person withdraws or supersedes, and revises in place.
		{kind: Rule, from: Accepted, name: "the person revises", do: revise(AsPerson(person)), state: Accepted},
		{kind: Rule, from: Accepted, name: "the person revises in an exclusion", do: reviseExcluding(AsPerson(person)), state: Accepted},
		{kind: Rule, from: Accepted, name: "its proposer revises", do: revise(model), want: ErrNoReceipt},
		{kind: Rule, from: Accepted, name: "the person accepts again", do: accept(person), want: ErrTransition},
		{kind: Rule, from: Accepted, name: "the person rejects", do: reject(person), want: ErrTransition},
		{kind: Rule, from: Accepted, name: "the person withdraws", do: withdraw(person), state: Withdrawn},
		{kind: Rule, from: Accepted, name: "a model withdraws", do: withdraw(none), want: ErrNoReceipt},
		{kind: Rule, from: Accepted, name: "the person links", do: link(person), state: Accepted},
		{kind: Rule, from: Accepted, name: "the person supersedes", do: supersede(person), state: Superseded},
		{kind: Rule, from: Accepted, name: "the steward supersedes", do: supersede(none), want: ErrNoReceipt},

		// rejected, withdrawn and superseded are closed, except that the person
		// takes a withdrawn record back.
		{kind: Decision, from: Rejected, name: "the person revises", do: revise(AsPerson(person)), want: ErrTransition},
		{kind: Decision, from: Rejected, name: "the person accepts", do: accept(person), want: ErrTransition},
		{kind: Decision, from: Rejected, name: "the person links", do: link(person), want: ErrTransition},
		{kind: Decision, from: Rejected, name: "the person supersedes", do: supersede(person), want: ErrTransition},
		{kind: Rule, from: Withdrawn, name: "the person takes it back", do: accept(person), state: Accepted},
		{kind: Rule, from: Withdrawn, name: "the steward takes it back", do: accept(none), want: ErrNoReceipt},
		{kind: Rule, from: Withdrawn, name: "the person revises", do: revise(AsPerson(person)), want: ErrTransition},
		{kind: Rule, from: Withdrawn, name: "the person withdraws again", do: withdraw(person), want: ErrTransition},
		{kind: Rule, from: Withdrawn, name: "the person links", do: link(person), want: ErrTransition},
		{kind: Rule, from: Superseded, name: "the person accepts", do: accept(person), want: ErrTransition},
		{kind: Rule, from: Superseded, name: "the person revises", do: revise(AsPerson(person)), want: ErrTransition},
		{kind: Rule, from: Superseded, name: "the person supersedes again", do: supersede(person), want: ErrTransition},

		// informational: no writer but the person revises a finding, and §4.1
		// gives no one a transition out of it (an import may, §3.4).
		{kind: Finding, from: Informational, name: "its author, the model, revises", do: revise(model), want: ErrTransition},
		{kind: Finding, from: Informational, name: "its author, a run, revises", do: revise(run), want: ErrTransition, author: run},
		{kind: Finding, from: Informational, name: "another model revises", do: revise(other), want: ErrTransition},
		{kind: Finding, from: Informational, name: "the person revises", do: revise(AsPerson(person)), state: Informational},
		{kind: Finding, from: Informational, name: "the person accepts", do: accept(person), want: ErrTransition},
		{kind: Finding, from: Informational, name: "the person rejects", do: reject(person), want: ErrTransition},
		{kind: Finding, from: Informational, name: "the person withdraws", do: withdraw(person), want: ErrTransition},
		{kind: Finding, from: Informational, name: "the person links", do: link(person), state: Informational},
		{kind: Finding, from: Informational, name: "the person supersedes", do: supersede(person), want: ErrTransition},
		{kind: Finding, from: Withdrawn, name: "the person accepts", do: accept(person), want: ErrTransition},
		{kind: Finding, from: Withdrawn, name: "the model revises", do: revise(model), want: ErrTransition},
	} {
		t.Run(fmt.Sprintf("%s %s: %s", row.from, row.kind, row.name), func(t *testing.T) {
			must := musts(t)
			author := row.author
			if author == (Actor{}) {
				author = model
			}
			cur := atBy(t, s, row.kind, row.from, author)
			got, err := row.do(cur)
			if row.want != nil {
				if !errors.Is(err, row.want) {
					t.Fatalf("want %v, got %v", row.want, err)
				}
				still := must(s.Current(ctx, cur.ID))
				if still.Revision != cur.Revision || still.State != cur.State {
					t.Fatalf("a refused change moved the record from %d %s to %d %s", cur.Revision, cur.State, still.Revision, still.State)
				}
				return
			}
			if err != nil {
				t.Fatalf("an allowed change was refused: %v", err)
			}
			if got.State != row.state || got.Revision != cur.Revision+1 {
				t.Fatalf("the change left revision %d %s, want %d %s", got.Revision, got.State, cur.Revision+1, row.state)
			}
			hasReceipt := got.Receipt != (Receipt{})
			if hasReceipt != receiptRequired(got.State, got.Author.Class) {
				t.Fatalf("a %s revision by %s has receipt %+v", got.State, got.Author.Class, got.Receipt)
			}
		})
	}
	if err := s.Verify(ctx); err != nil {
		t.Fatalf("after every transition: %v", err)
	}
}

// §4.1, WHO MAY CREATE WHAT, AS THE DESIGN WRITES IT. Each row is one cell of
// the design's table (the section is quoted beside it), not a description of
// what the store happens to do: the store must allow exactly the rows marked
// allowed and refuse the rest as a change that writer may not make.
func TestEachWriterCreatesExactlyWhatSection41Allows(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	must := musts(t)
	launch := folder(t, s, "Launch")
	other := must(s.Accept(ctx, must(s.Propose(ctx, rule("an accepted rule to point at"), AsPerson(card(t, "p")))).Fence(), card(t, "c")))
	person := AsPerson(card(t, "card-1"))
	extractor, run, voice, steward := As(AuthorExtractor, "tidy"), As(AuthorRun, "occurrence-9"), As(AuthorVoice, "voice-2"), As(AuthorSteward, "steward")
	draft := func(kind Kind, edit func(*Draft)) Draft {
		d := rule("a sentence")
		d.Kind = kind
		if edit != nil {
			edit(&d)
		}
		return d
	}
	extracted := func(d *Draft) { d.QuoteOrigin = ModelExtracted }
	targeted := func(d *Draft) { d.Targets = []Target{folderTarget(launch, Direct)} }
	fromOccurrence := func(d *Draft) { d.Source = Source{Class: SourceOccurrence, ID: "occurrence-9"} }
	fromOutcome := func(d *Draft) { d.Source = Source{Class: SourceConversationOutcome, ID: "conversation-3"} }
	excluding := func(d *Draft) { d.Exclusions = []Exclusion{{Kind: TargetConversation, Ref: "side-chat"}} }
	linking := func(kind LinkKind) func(*Draft) {
		return func(d *Draft) { d.Links = []Link{{Kind: kind, To: other.ID, ToRevision: other.Revision}} }
	}
	both := func(edits ...func(*Draft)) func(*Draft) {
		return func(d *Draft) {
			for _, e := range edits {
				e(d)
			}
		}
	}
	for i, row := range []struct {
		cell  string
		by    Actor
		draft Draft
		want  error
	}{
		// Person: "any kind, any state it asserts"; exclusions and overrides /
		// conflicts_with are theirs.
		{"person proposes a rule", person, draft(Rule, nil), nil},
		{"person notes a finding", person, draft(Finding, nil), nil},
		{"person proposes with an exclusion", person, draft(Rule, excluding), nil},
		{"person proposes with an override", person, draft(Rule, linking(Overrides)), nil},
		// Model: "proposed rule/decision; informational finding". Never an
		// exclusion (made only by the person, §1.2) or a precedence link.
		{"model proposes a rule", model, draft(Rule, nil), nil},
		{"model proposes a decision", model, draft(Decision, nil), nil},
		{"model notes a finding", model, draft(Finding, nil), nil},
		{"model proposes a replacement (propose_change)", model, draft(Rule, linking(Supersedes)), nil},
		{"model proposes with an exclusion", model, draft(Rule, excluding), ErrTransition},
		{"model proposes with an override", model, draft(Rule, linking(Overrides)), ErrTransition},
		{"model proposes with a conflict", model, draft(Rule, linking(ConflictsWith)), ErrTransition},
		// Extractor: "proposed decision only. Zero targets, quote_origin=model_extracted".
		{"extractor proposes a zero-target extracted decision", extractor, draft(Decision, extracted), nil},
		{"extractor proposes a targeted decision", extractor, draft(Decision, both(extracted, targeted)), ErrTransition},
		{"extractor proposes a rule", extractor, draft(Rule, extracted), ErrTransition},
		{"extractor proposes adopted wording", extractor, draft(Decision, nil), ErrTransition},
		{"extractor notes a finding", extractor, draft(Finding, extracted), ErrTransition},
		// Unattended run: "informational finding; proposed record with source_class=occurrence".
		{"run notes a finding", run, draft(Finding, nil), nil},
		{"run proposes from its occurrence", run, draft(Rule, fromOccurrence), nil},
		{"run proposes from a conversation", run, draft(Rule, nil), ErrTransition},
		// Voice [Q7 yes]: "decision-proposal → proposed decision,
		// source_class=conversation_outcome; findings → informational findings".
		{"voice proposes an outcome decision", voice, draft(Decision, fromOutcome), nil},
		{"voice notes a finding", voice, draft(Finding, nil), nil},
		{"voice proposes a rule", voice, draft(Rule, fromOutcome), ErrTransition},
		{"voice proposes a decision from a conversation", voice, draft(Decision, nil), ErrTransition},
		// Steward: "proposed".
		{"steward proposes a rule", steward, draft(Rule, nil), nil},
		{"steward proposes a decision", steward, draft(Decision, nil), nil},
		{"steward notes a finding", steward, draft(Finding, nil), ErrTransition},
		{"steward proposes with an exclusion", steward, draft(Rule, excluding), ErrTransition},
	} {
		t.Run(row.cell, func(t *testing.T) {
			d := row.draft
			d.Text = fmt.Sprintf("%s (%d)", d.Text, i)
			var err error
			if d.Kind == Finding {
				_, err = s.Note(ctx, d, row.by)
			} else {
				_, err = s.Propose(ctx, d, row.by)
			}
			if row.want == nil && err != nil {
				t.Fatalf("§4.1 allows this and the store refused it: %v", err)
			}
			if row.want != nil && !errors.Is(err, row.want) {
				t.Fatalf("§4.1 does not allow this; want %v, got %v", row.want, err)
			}
		})
	}
}

// The person is spelled only by a receipt and migration only through Import;
// each door writes only its own kind. Which writer creates what is the §4.1
// table above.
func TestOnlyReceiptsSpellThePersonAndOnlyImportSpellsMigration(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	if _, err := s.Propose(ctx, rule("claims to be the person"), As(AuthorPerson, "me")); !errors.Is(err, ErrNoReceipt) {
		t.Errorf("a writer named itself the person: %v", err)
	}
	if _, err := s.Propose(ctx, rule("claims to be an import"), As(AuthorMigration, "run")); !errors.Is(err, ErrInvalid) {
		t.Errorf("a writer named itself an import: %v", err)
	}
	if _, err := s.Propose(ctx, finding("a finding through Propose"), model); !errors.Is(err, ErrInvalid) {
		t.Errorf("Propose wrote a finding: %v", err)
	}
	if _, err := s.Note(ctx, rule("a rule through Note"), model); !errors.Is(err, ErrInvalid) {
		t.Errorf("Note wrote a rule: %v", err)
	}
	// The person may propose too; the proposal carries no receipt, because a
	// proposal is not authority.
	must := musts(t)
	p := must(s.Propose(ctx, rule("the person's own proposal"), AsPerson(card(t, "c"))))
	if p.Author.Class != AuthorPerson || p.Receipt != (Receipt{}) || p.Lane() != LanePending {
		t.Fatalf("the person's proposal: %+v", p)
	}
}

// The governing predicate over its whole domain.
func TestTheGoverningPredicateIsAcceptedRulesAndDecisionsOnly(t *testing.T) {
	for _, kind := range []Kind{Rule, Decision, Finding} {
		for _, state := range []State{Proposed, Accepted, Rejected, Withdrawn, Superseded, Informational} {
			want := laneNone
			switch {
			case kind != Finding && state == Accepted:
				want = LaneGoverning
			case kind != Finding && state == Proposed:
				want = LanePending
			case kind == Finding && state == Informational:
				want = LaneInformational
			}
			if got := lane(kind, state); got != want {
				t.Errorf("lane(%s, %s) = %q, want %q", kind, state, got, want)
			}
		}
	}
}
