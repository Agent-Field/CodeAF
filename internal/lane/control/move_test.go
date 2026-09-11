package control

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// planOf builds a plan with a serving set and a ladder, which is all [Next]
// reads. Nothing here holds a clock — the deadline is [Plan.Spent]'s question
// and is asked by the dispatcher, never by the move generator.
func planOf(model string, lanes []string, shapes []string, comeback time.Duration) Plan {
	plan := Plan{Model: model, Shapes: shapes, Comeback: comeback}
	if len(lanes) > 0 {
		plan.Lane = lanes[0]
		for _, lane := range lanes[1:] {
			plan.Alts = append(plan.Alts, Alternative{Lane: lane})
		}
	}
	return plan
}

// walk plays a plan out to exhaustion, the way the dispatcher does: ask, record,
// ask again. It returns every move in the order they were generated.
func walk(plan Plan) []Move {
	log := NewMoveLog()
	var made []Move
	for range 64 {
		move := Next(plan, log.List())
		if move.Kind == MoveNone {
			break
		}
		if !log.Add(move) {
			panic("Next returned a move that had already been made: " + move.Lane)
		}
		made = append(made, move)
	}
	return made
}

func TestNextWalksTheMachinesBeforeItChangesTheRequest(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name  string
		plan  Plan
		order []string
	}{
		{
			name:  "three machines, no ladder, no comeback",
			plan:  planOf("m", []string{"A", "B", "C"}, nil, 0),
			order: []string{"machine A@0", "machine B@0", "machine C@0"},
		},
		{
			name: "three machines then one rung then the machines again",
			plan: planOf("m", []string{"A", "B"}, []string{"removed reasoning"}, 0),
			order: []string{
				"machine A@0", "machine B@0",
				"shape A@1", "machine B@1",
			},
		},
		{
			name: "a set one machine wide waits out the comeback it was given, once",
			plan: planOf("m", []string{"A"}, []string{"removed tools"}, 3*time.Second),
			order: []string{
				"machine A@0", "wait A@0",
				"shape A@1",
			},
		},
		{
			name:  "a set one machine wide with no comeback never repeats it",
			plan:  planOf("m", []string{"A"}, nil, 0),
			order: []string{"machine A@0"},
		},
		{
			name: "two machines, one of them named twice, are one set",
			plan: planOf("m", []string{"A", "a", "B"}, nil, 0),
			order: []string{
				"machine A@0", "machine B@0",
			},
		},
		{
			name: "two rungs are climbed one at a time and never twice",
			plan: planOf("m", []string{"A"}, []string{"dropped the price ceiling", "removed tools"}, 0),
			order: []string{
				"machine A@0", "shape A@1", "shape A@2",
			},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, move := range walk(row.plan) {
				got = append(got, fmt.Sprintf("%s %s@%d", move.Kind, move.Lane, move.Shape))
			}
			if len(got) != len(row.order) {
				t.Fatalf("moves\n got %v\nwant %v", got, row.order)
			}
			for index := range got {
				if got[index] != row.order[index] {
					t.Fatalf("move %d\n got %q\nwant %q\n(whole walk %v)", index, got[index], row.order[index], got)
				}
			}
		})
	}
}

// TestNextNeverRepeatsAMove is the second clause of the rule
// (docs/design/recovery/DESIGN.md §3): a move is a (machine, shape, model)
// triple and the plan refuses one it has already made. It is asked of every
// shape of plan there is rather than of one, because the census's measured
// failure — 53% of chains never leaving the lane they started on — was a
// property of the LOOP and not of any one request.
func TestNextNeverRepeatsAMove(t *testing.T) {
	t.Parallel()
	machines := []string{"DeepInfra", "Parasail", "Fireworks", "GMICloud", "Io Net", "Together"}
	rungs := []string{"relaxed the endpoint filter", "dropped the price ceiling", "removed reasoning", "removed tools"}
	seed := rand.New(rand.NewSource(20260910))
	for trial := range 300 {
		lanes := append([]string(nil), machines[:1+seed.Intn(len(machines))]...)
		shapes := append([]string(nil), rungs[:seed.Intn(len(rungs)+1)]...)
		var comeback time.Duration
		if seed.Intn(2) == 0 {
			comeback = time.Duration(1+seed.Intn(30)) * time.Second
		}
		plan := planOf("sim/model", lanes, shapes, comeback)
		made := walk(plan)
		for i := range made {
			for j := i + 1; j < len(made); j++ {
				if made[i].sameAs(made[j]) {
					t.Fatalf("trial %d repeated %v at %d and %d", trial, made[i], i, j)
				}
			}
		}
		// AND THE WHOLE WALK IS BOUNDED BY THE SHAPE OF THE REQUEST, which is
		// what makes "one deadline" arithmetically safe rather than only
		// hopeful: machines × shapes, plus the one legal repeat.
		bound := len(plan.Serving())*(len(shapes)+1) + 1
		if len(made) > bound {
			t.Fatalf("trial %d made %d moves, bound is %d (lanes %v, rungs %v)", trial, len(made), bound, lanes, shapes)
		}
		// AND A MACHINE IS ASKED THE SAME BYTES TWICE ONLY ONCE IN A CALL'S LIFE.
		waits := 0
		for _, move := range made {
			if move.Kind == MoveWait {
				waits++
			}
		}
		if waits > 1 {
			t.Fatalf("trial %d took %d same-machine repeats; the rule allows one", trial, waits)
		}
		if waits == 1 && len(plan.Serving()) != 1 {
			t.Fatalf("trial %d repeated a machine while %d were admissible", trial, len(plan.Serving()))
		}
	}
}

// TestAnOpenSetAlwaysHasAnotherMachineAndOnlyTheDeadlineStopsIt is the state
// most calls in this build are in: nobody named the pool, so the router picks
// and each body carries a longer exclusion list than the last. Reading that as
// a set one machine wide would invent a pool this process cannot see, and would
// stop the walk after one send.
func TestAnOpenSetAlwaysHasAnotherMachineAndOnlyTheDeadlineStopsIt(t *testing.T) {
	t.Parallel()
	plan := planOf("m", nil, nil, 5*time.Second)
	log := NewMoveLog()
	for send := range 20 {
		move := Next(plan, log.List())
		if move.Kind != MoveMachine {
			t.Fatalf("send %d: an open set answered %s", send, move.Kind)
		}
		if move.Lane != "" {
			t.Fatalf("send %d: an open set named %q", send, move.Lane)
		}
		if !log.Add(move) {
			t.Fatalf("send %d: an anonymous move was refused as a repeat", send)
		}
	}
	if log.Count() != 20 {
		t.Fatalf("the log holds %d of 20 open moves", log.Count())
	}
	// And the ONE thing that stops it is the deadline.
	now := time.Date(2026, 9, 10, 23, 0, 0, 0, time.UTC)
	plan.Deadline = now
	if !plan.Spent(now) {
		t.Fatal("an open walk with a spent deadline kept going")
	}
}

func TestAPlanWithNoDeadlineIsNeverSpent(t *testing.T) {
	t.Parallel()
	var plan Plan
	if plan.Spent(time.Now()) {
		t.Fatal("a plan nobody gave a deadline decided to give up")
	}
	if left := plan.Left(time.Now()); left != 0 {
		t.Fatalf("countdown on no deadline: %s", left)
	}
	now := time.Date(2026, 9, 10, 23, 0, 0, 0, time.UTC)
	plan.Deadline = now.Add(90 * time.Second)
	if plan.Spent(now) {
		t.Fatal("spent at the start of its own deadline")
	}
	if left := plan.Left(now); left != 90*time.Second {
		t.Fatalf("countdown %s, want 90s", left)
	}
	if !plan.Spent(now.Add(90 * time.Second)) {
		t.Fatal("not spent at the deadline itself")
	}
}

// TestTheMovesOfOneQuestionAreSharedByItsArms is the reason the log is a
// pointer: a hedge is [Next] launched a second time while the first move is in
// flight, and two arms deciding at the same instant must not both take the last
// machine.
func TestTheMovesOfOneQuestionAreSharedByItsArms(t *testing.T) {
	t.Parallel()
	plan := planOf("m", []string{"A", "B"}, nil, 0)
	plan.Moves = NewMoveLog()
	primary := Next(plan, plan.Moves.List())
	if !plan.Moves.Add(primary) {
		t.Fatal("the primary's own move was refused")
	}
	// The second arm carries a COPY of the plan, as it does in the transport.
	arm := plan
	rescue := Next(arm, arm.Moves.List())
	if rescue.Lane == primary.Lane {
		t.Fatalf("two arms demanded one machine: both %q", rescue.Lane)
	}
	if !arm.Moves.Add(rescue) {
		t.Fatal("the rescue's move was refused")
	}
	if plan.Moves.Count() != 2 {
		t.Fatalf("the primary's log holds %d moves; the arms share one log", plan.Moves.Count())
	}
	if again := plan.Moves.Add(primary); again {
		t.Fatal("a repeat was recorded as new")
	}
}

func TestANilMoveLogIsEmptyAndDecidesNothing(t *testing.T) {
	t.Parallel()
	var log *MoveLog
	if log.Count() != 0 || log.List() != nil {
		t.Fatal("a nil log claimed to hold something")
	}
	if log.Add(Move{Kind: MoveMachine, Lane: "A"}) {
		t.Fatal("a nil log accepted a move")
	}
}

// ── AN ACCOUNT'S OWN CEILING IS NOT AN OPEN SET ─────────────────────────────
//
// THE LAW: a walk of an open set is only a walk while the exclusion list is
// growing. A ceiling over the whole key names no machine, so it adds nothing to
// the next body, so the next body is the one that was just refused — which is
// the same bytes to the same machine, the one thing the design forbids
// (docs/design/recovery/DESIGN.md §3).
//
// THE MEASURED FAILURE this pins is exactly that: seven sends of identical
// bytes behind a doubling wait, because [Next] answered every one of them with
// a machine move it could not name.
func TestAnAccountCeilingGetsOneComebackAndThenTheModel(t *testing.T) {
	t.Parallel()
	plan := planOf("m", nil, nil, 3*time.Second)
	plan.AccountRefused = true

	made := walk(plan)
	if len(made) != 1 {
		t.Fatalf("a refusal nothing could be excluded from earned %d moves: %+v", len(made), made)
	}
	if made[0].Kind != MoveWait {
		t.Fatalf("the one move was %s, want the comeback the refusal named", made[0].Kind)
	}
	if made[0].Wait != 3*time.Second {
		t.Fatalf("the comeback was %s, want the three seconds the refusal asked for", made[0].Wait)
	}
	if made[0].Lane != "" {
		t.Fatalf("the move named %q; nobody was named, which is the whole of why it is a wait", made[0].Lane)
	}
}

// AND A CEILING THAT ASKED FOR NOTHING HAS NO MOVE AT ALL. It cannot be routed
// around and it did not say when to come back, so the honest answer is the
// model — which is [MoveNone], and the session's.
func TestAnAccountCeilingWithNoComebackHasNowhereToGo(t *testing.T) {
	t.Parallel()
	plan := planOf("m", nil, nil, 0)
	plan.AccountRefused = true
	if move := Next(plan, nil); move.Kind != MoveNone {
		t.Fatalf("a ceiling with nothing to exclude and no comeback answered %s", move.Kind)
	}
}

// AND IT OUTRANKS THE LADDER TOO. No field of a request gets under a ceiling
// over the whole key, so offering a rung would be a relaxation spent to be told
// the identical thing.
func TestAnAccountCeilingIsNotAnswerableByARelaxedShape(t *testing.T) {
	t.Parallel()
	plan := planOf("m", []string{"A", "B"}, []string{"removed reasoning", "removed tools"}, 0)
	plan.AccountRefused = true
	if move := Next(plan, nil); move.Kind != MoveNone {
		t.Fatalf("a ceiling over the key was answered with %s", move.Kind)
	}
}
