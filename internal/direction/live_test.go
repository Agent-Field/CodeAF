package direction

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// The live index holds one row per target of every current revision in a live
// lane, and nothing else: a rejected, withdrawn or superseded record leaves it
// in the transaction that moved its pointer, and a record with no targets was
// never in it.
func TestTheLiveIndexHoldsExactlyTheLiveLanes(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	must := musts(t)
	launch := folder(t, s, "Launch")
	p := must(s.Propose(ctx, rule("never include phone numbers", folderTarget(launch, Subtree), chatTarget("w")), model))
	f := must(s.Note(ctx, finding("the venue holds 40", chatTarget("w")), model))
	must(s.Propose(ctx, rule("a proposal that reaches nothing yet"), model))
	lanes := func() map[string]Lane {
		got := map[string]Lane{}
		for _, row := range liveOf(t, s) {
			got[fmt.Sprint(row.record == p.ID, row.kind, row.ref)] = row.lane
		}
		return got
	}
	want := map[string]Lane{
		fmt.Sprint(true, TargetCollection, launch): LanePending,
		fmt.Sprint(true, TargetConversation, "w"):  LanePending,
		fmt.Sprint(false, TargetConversation, "w"): LaneInformational,
	}
	if got := lanes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("after proposing: %v", got)
	}
	accepted := must(s.Accept(ctx, p.Fence(), card(t, "yes")))
	want[fmt.Sprint(true, TargetCollection, launch)] = LaneGoverning
	want[fmt.Sprint(true, TargetConversation, "w")] = LaneGoverning
	if got := lanes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("after accepting: %v", got)
	}
	must(s.Withdraw(ctx, accepted.Fence(), "paused", card(t, "c")))
	if rows := liveOf(t, s); len(rows) != 1 || rows[0].record != f.ID || rows[0].lane != LaneInformational {
		t.Fatalf("a withdrawn record left rows, or the finding lost its own: %+v", rows)
	}
	if err := s.Verify(ctx); err != nil {
		t.Fatal(err)
	}
}

// VERIFY CATCHES A POINTER TO NOTHING. A record whose current pointer names a
// revision that does not exist has no live rows to disagree with, so a check
// that only compares derived rows would pass it; the records themselves are
// broken, and Verify says so.
func TestVerifyCatchesADanglingCurrentPointer(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	must := musts(t)
	r := must(s.Accept(ctx, must(s.Propose(ctx, rule("formal tone", chatTarget("w")), AsPerson(card(t, "p")))).Fence(), card(t, "c")))
	if err := workspace.WriteImmediate(ctx, s.ws, func(tx *sqlTx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE direction_records SET revision=99 WHERE id=?", r.ID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM direction_live WHERE record_id=?", r.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Verify(ctx); !errors.Is(err, ErrDrift) {
		t.Fatalf("a pointer to a revision that does not exist passed Verify: %v", err)
	}
}

// REBUILD ≡ INCREMENTAL. Random sequences of every operation — including the
// ones the store refuses — are applied, and after every step the live index
// must equal what a rebuild from the revisions derives. At the end the index
// is rebuilt and must be row for row what the writes left.
func TestRebuildingTheLiveIndexEqualsWhatTheWritesMaintained(t *testing.T) {
	for seed := int64(1); seed <= 24; seed++ {
		t.Run(fmt.Sprint("seed ", seed), func(t *testing.T) {
			randomHistory(t, seed, 70)
		})
	}
}

func randomHistory(t *testing.T, seed int64, steps int) {
	ctx := context.Background()
	s := openTest(t)
	rng := rand.New(rand.NewSource(seed))
	folders := []string{folder(t, s, "A"), folder(t, s, "B"), folder(t, s, "C")}
	writers := []Actor{model, As(AuthorModel, "session-2"), As(AuthorExtractor, "tidy"), As(AuthorSteward, "s"), As(AuthorRun, "run-1")}
	texts := []string{"formal tone", "concise tone", "never include phone numbers", "budget at most $200", "ship Friday"}
	var ids []string
	places := func() []Target {
		var out []Target
		for i := rng.Intn(4); i > 0; i-- {
			switch rng.Intn(4) {
			case 0:
				out = append(out, folderTarget(folders[rng.Intn(len(folders))], []Reach{Direct, Subtree}[rng.Intn(2)]))
			case 1:
				out = append(out, chatTarget(fmt.Sprint("chat-", rng.Intn(3))))
			case 2:
				out = append(out, Target{Kind: TargetTask, Ref: fmt.Sprint(1 + rng.Intn(3)), Session: "chat-0"})
			default:
				out = append(out, Target{Kind: TargetEverywhere, Ref: Everywhere})
			}
		}
		return out
	}
	// draft is shaped for the writer most of the time — the person's may carry
	// exclusions and precedence links, the extractor's is an untargeted
	// extracted decision, a run's cites its occurrence — and now and then
	// ignores §4.1, so refusals stay part of the history.
	draft := func(kind Kind, by Actor) Draft {
		d := rule(fmt.Sprintf("%s %d", texts[rng.Intn(len(texts))], rng.Intn(3)), places()...)
		d.Kind = kind
		shaped := rng.Intn(6) != 0
		if !shaped || by.person() {
			if rng.Intn(3) == 0 {
				d.Exclusions = []Exclusion{{Kind: TargetConversation, Ref: fmt.Sprint("chat-", rng.Intn(3))}}
			}
			if len(ids) > 0 && kind != Finding && rng.Intn(3) == 0 {
				d.Links = []Link{{Kind: []LinkKind{Overrides, ConflictsWith}[rng.Intn(2)], To: ids[rng.Intn(len(ids))]}}
			}
		}
		if shaped {
			switch by.author.Class {
			case AuthorExtractor:
				d.Kind, d.QuoteOrigin, d.Targets = Decision, ModelExtracted, nil
			case AuthorRun:
				d.Source = Source{Class: SourceOccurrence, ID: "occurrence-1"}
			}
		}
		return d
	}
	person := func() PersonReceipt { return card(t, fmt.Sprint("answer-", rng.Intn(1000))) }
	writer := func() Actor {
		if rng.Intn(3) == 0 {
			return AsPerson(person())
		}
		return writers[rng.Intn(len(writers))]
	}
	pick := func() (Revision, bool) {
		if len(ids) == 0 {
			return Revision{}, false
		}
		cur, err := s.Current(ctx, ids[rng.Intn(len(ids))])
		if err != nil {
			t.Fatal(err)
		}
		// A stale fence now and then, so refusals are part of the history too.
		if rng.Intn(8) == 0 && cur.Revision > 1 {
			cur.Revision--
		}
		return cur, true
	}
	kinds := []Kind{Rule, Decision, Finding}
	applied := 0
	for step := 0; step < steps; step++ {
		var r Revision
		var err error
		cur, ok := pick()
		switch op := rng.Intn(10); {
		case op < 2 || !ok:
			kind, by := kinds[rng.Intn(len(kinds))], writer()
			if kind == Finding {
				r, err = s.Note(ctx, draft(kind, by), by)
			} else {
				r, err = s.Propose(ctx, draft(kind, by), by)
			}
		case op == 2:
			by := writer()
			if rng.Intn(2) == 0 {
				by = Actor{author: cur.Author} // its own writer, when that is not the person
				if cur.Author.Class == AuthorPerson || cur.Author.Class == AuthorMigration {
					by = AsPerson(person())
				}
			}
			r, err = s.Revise(ctx, cur.Fence(), draft(cur.Kind, by), by)
		case op == 3:
			r, err = s.Accept(ctx, cur.Fence(), person())
		case op == 4:
			r, err = s.Reject(ctx, cur.Fence(), person())
		case op == 5:
			r, err = s.Withdraw(ctx, cur.Fence(), "paused", person())
		case op == 6:
			by := AsPerson(person())
			r, err = s.Supersede(ctx, draft(cur.Kind, by), []Fence{cur.Fence()}, person())
		case op == 7 && len(ids) > 1:
			kind := DerivedFrom
			if cur.Kind != Finding {
				kind = []LinkKind{Overrides, ConflictsWith}[rng.Intn(2)]
			}
			r, err = s.Link(ctx, cur.Fence(), Link{Kind: kind, To: ids[rng.Intn(len(ids))]}, person())
		case op == 8 && len(cur.Links) > 0:
			l := cur.Links[rng.Intn(len(cur.Links))]
			r, err = s.Unlink(ctx, cur.Fence(), l.Kind, l.To, person())
		default:
			by := writer()
			r, err = s.Note(ctx, draft(Finding, by), by)
		}
		switch {
		case err == nil:
			applied++
			if r.Revision == 1 {
				ids = append(ids, r.ID)
			}
		case errors.Is(err, ErrTransition), errors.Is(err, ErrConflict), errors.Is(err, ErrNoReceipt),
			errors.Is(err, ErrRejectedText), errors.Is(err, ErrInvalid):
		default:
			t.Fatalf("step %d failed in a way no refusal explains: %v", step, err)
		}
		if err := s.Verify(ctx); err != nil {
			t.Fatalf("step %d: %v", step, err)
		}
	}
	incremental := liveOf(t, s)
	if err := s.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	rebuilt := liveOf(t, s)
	if !reflect.DeepEqual(incremental, rebuilt) {
		t.Fatalf("rebuild differs from the writes:\nincremental %d rows %+v\nrebuilt %d rows %+v", len(incremental), incremental, len(rebuilt), rebuilt)
	}
	// A history made only of refusals would prove nothing about maintenance.
	if applied < steps/3 {
		t.Fatalf("only %d of %d steps were applied", applied, steps)
	}
}
