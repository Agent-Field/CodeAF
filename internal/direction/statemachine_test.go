package direction

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// at brings a fresh record of this kind to this state, the way the product
// would: a model proposes or notes, and the person acts on it.
func at(t *testing.T, s *Store, kind Kind, state State) Revision {
	t.Helper()
	must := musts(t)
	ctx := context.Background()
	d := rule(fmt.Sprintf("a %s that is %s, %p", kind, state, t))
	d.Kind = kind
	if kind == Finding {
		r := must(s.Note(ctx, d, model))
		switch state {
		case Informational:
			return r
		case Withdrawn:
			return must(s.Withdraw(ctx, r.Fence(), "no longer true", card(t, "c")))
		case Superseded:
			d.Text += " replaced"
			must(s.Supersede(ctx, d, []Fence{r.Fence()}, card(t, "c")))
			return must(s.Current(ctx, r.ID))
		}
		t.Fatalf("no finding is %s", state)
	}
	r := must(s.Propose(ctx, d, model))
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

// THE STATE MACHINE, WRITER BY TRANSITION. Each row starts a fresh record in a
// state, asks for one change as one writer, and says whether the store must
// allow it or which refusal it must give. A person-only change asked for by
// anyone else arrives with no receipt, because nobody else can construct one;
// that is the row's whole point, and ErrNoReceipt is the refusal.
func TestEveryWriterAndTransitionIsAllowedOrRefusedAsDesigned(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	person := card(t, "card-1")
	other := As(AuthorModel, "session-2")
	steward := As(AuthorSteward, "steward")
	var none PersonReceipt

	type change func(Revision) (Revision, error)
	revise := func(by Actor) change {
		return func(cur Revision) (Revision, error) {
			d := cur.draft()
			d.Text += " (revised)"
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
		kind  Kind
		from  State
		name  string
		do    change
		want  error
		state State // the state an allowed change leaves the record in
	}{
		{Rule, Proposed, "the person revises", revise(AsPerson(person)), nil, Proposed},
		{Rule, Proposed, "its author revises", revise(model), nil, Proposed},
		{Rule, Proposed, "another model revises", revise(other), ErrTransition, ""},
		{Rule, Proposed, "the steward revises", revise(steward), ErrTransition, ""},
		{Rule, Proposed, "the person accepts", accept(person), nil, Accepted},
		{Rule, Proposed, "the steward accepts", accept(none), ErrNoReceipt, ""},
		{Rule, Proposed, "the person rejects", reject(person), nil, Rejected},
		{Rule, Proposed, "a model rejects", reject(none), ErrNoReceipt, ""},
		{Rule, Proposed, "the person withdraws", withdraw(person), ErrTransition, ""},
		{Rule, Proposed, "the person links", link(person), nil, Proposed},
		{Rule, Proposed, "a model links", link(none), ErrNoReceipt, ""},
		{Rule, Proposed, "the person supersedes", supersede(person), nil, Superseded},

		{Rule, Accepted, "the person revises", revise(AsPerson(person)), nil, Accepted},
		{Rule, Accepted, "its author revises", revise(model), ErrNoReceipt, ""},
		{Rule, Accepted, "the person accepts again", accept(person), ErrTransition, ""},
		{Rule, Accepted, "the person rejects", reject(person), ErrTransition, ""},
		{Rule, Accepted, "the person withdraws", withdraw(person), nil, Withdrawn},
		{Rule, Accepted, "a model withdraws", withdraw(none), ErrNoReceipt, ""},
		{Rule, Accepted, "the person links", link(person), nil, Accepted},
		{Rule, Accepted, "the person supersedes", supersede(person), nil, Superseded},
		{Rule, Accepted, "the steward supersedes", supersede(none), ErrNoReceipt, ""},

		{Decision, Rejected, "the person revises", revise(AsPerson(person)), ErrTransition, ""},
		{Decision, Rejected, "the person accepts", accept(person), ErrTransition, ""},
		{Decision, Rejected, "the person links", link(person), ErrTransition, ""},
		{Decision, Rejected, "the person supersedes", supersede(person), ErrTransition, ""},

		{Rule, Withdrawn, "the person takes it back", accept(person), nil, Accepted},
		{Rule, Withdrawn, "the steward takes it back", accept(none), ErrNoReceipt, ""},
		{Rule, Withdrawn, "the person revises", revise(AsPerson(person)), ErrTransition, ""},
		{Rule, Withdrawn, "the person withdraws again", withdraw(person), ErrTransition, ""},
		{Rule, Withdrawn, "the person links", link(person), ErrTransition, ""},

		{Rule, Superseded, "the person accepts", accept(person), ErrTransition, ""},
		{Rule, Superseded, "the person revises", revise(AsPerson(person)), ErrTransition, ""},
		{Rule, Superseded, "the person supersedes again", supersede(person), ErrTransition, ""},

		{Finding, Informational, "its author revises", revise(model), nil, Informational},
		{Finding, Informational, "another model revises", revise(other), ErrTransition, ""},
		{Finding, Informational, "the person revises", revise(AsPerson(person)), nil, Informational},
		{Finding, Informational, "the person accepts", accept(person), ErrTransition, ""},
		{Finding, Informational, "the person rejects", reject(person), ErrTransition, ""},
		{Finding, Informational, "the person withdraws", withdraw(person), nil, Withdrawn},
		{Finding, Informational, "the person links", link(person), nil, Informational},
		{Finding, Informational, "the person supersedes", supersede(person), nil, Superseded},
		{Finding, Withdrawn, "the person accepts", accept(person), ErrTransition, ""},
		{Finding, Withdrawn, "its author revises", revise(model), ErrTransition, ""},
	} {
		t.Run(fmt.Sprintf("%s %s: %s", row.from, row.kind, row.name), func(t *testing.T) {
			must := musts(t)
			cur := at(t, s, row.kind, row.from)
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

// Who may create what. Every writer class may propose and note; the person
// class is spelled only by a receipt, and migration only through Import.
func TestOnlyReceiptsSpellThePersonAndOnlyImportSpellsMigration(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	for _, class := range []AuthorClass{AuthorModel, AuthorExtractor, AuthorRun, AuthorVoice, AuthorSteward} {
		if _, err := s.Propose(ctx, rule(fmt.Sprint("proposed by ", class)), As(class, "ref")); err != nil {
			t.Errorf("%s could not propose: %v", class, err)
		}
		if _, err := s.Note(ctx, finding(fmt.Sprint("noted by ", class)), As(class, "ref")); err != nil {
			t.Errorf("%s could not note: %v", class, err)
		}
	}
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
