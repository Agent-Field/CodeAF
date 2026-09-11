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
			name: "a request that names no machine at all is a set one wide",
			plan: planOf("m", nil, nil, 5*time.Second),
			order: []string{
				"machine @0", "wait @0",
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
