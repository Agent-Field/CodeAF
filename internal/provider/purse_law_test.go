package provider

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane/control"
)

// TestThePurseIsAskedThroughOneDoor is the law the deleted allowance broke.
//
// THERE IS ONE RAIL IN FRONT OF A SECOND REQUEST AND ONE PLACE THAT ASKS IT.
// The thing that made the process-wide budget able to overrule the controller
// was that it was asked from five places under three different names — the
// controller's hedge, the stall rescue, the walk, the countdown's reading and
// the ladder's — each with its own idea of what a refusal meant. This walks
// this package's own source and fails if [control.Purse.Allows] is called
// anywhere but [hedgeRace.affordsLocked].
func TestThePurseIsAskedThroughOneDoor(t *testing.T) {
	const door = "affordsLocked"
	set := token.NewFileSet()
	pkg, err := parser.ParseDir(set, ".", func(info os.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, files := range pkg {
		for name, file := range files.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Name.Name == door {
					continue
				}
				ast.Inspect(fn, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if !ok {
						return true
					}
					if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "Allows" {
						t.Errorf("%s: %s asks the purse; only %s may (see THE ONE DOOR in hedge.go)",
							set.Position(call.Pos()), fn.Name.Name, door)
					}
					return true
				})
			}
			_ = name
		}
	}
}

// TestEveryAttemptGetsItsOwnController is the law the 363-second attempt broke.
//
// A watch belongs to an ARM and an arm sends many times. Every once-only rule on
// this type — the act the row carries, the controller's own report, a purse
// refusal — is right for ONE SEND and wrong for a call, so a second attempt used
// to run inside the first one's answers and could not report at all. Re-arming
// is what makes each send a send; the deadline and the move log belong to the
// question and are untouched, which is the other half of the law.
func TestEveryAttemptGetsItsOwnController(t *testing.T) {
	began := time.Now()
	plan := control.Plan{
		Lane: "A", Ceiling: 10 * time.Millisecond, Began: began,
		Moves: control.NewMoveLog(), Deadline: began.Add(time.Hour),
	}
	race := &hedgeRace{model: "openrouter/attempts", winner: -1, plan: plan, build: control.New}
	watch := &streamWatch{race: race, arm: 0, plan: plan, control: control.New(plan), began: began}
	race.arms = []*hedgeArm{{index: 0, watch: watch}}

	// The first attempt runs out its ceiling and is acted on, which is what
	// writes the row.
	watch.mu.Lock()
	watch.after(watch.control.Quiet(began.Add(time.Second)), began.Add(time.Second))
	watch.mu.Unlock()
	if watch.acted.Kind == control.None {
		t.Fatal("the first attempt was never acted on, so this test proves nothing")
	}
	if watch.plan.Deadline != plan.Deadline {
		t.Fatal("the watch did not keep the question's own deadline")
	}

	// And the second attempt is a send like any other.
	second := began.Add(time.Minute)
	watch.attempt(second)
	if watch.acted.Kind != control.None {
		t.Fatalf("the second attempt inherited the first one's act (%v), so its row can never say what it did", watch.acted.Kind)
	}
	if !watch.began.Equal(second) {
		t.Fatalf("the second attempt is dated %s rather than the moment its own bytes left (%s)", watch.began, second)
	}
	if watch.tokens != 0 || watch.visible != 0 || watch.beats != 0 || watch.served != "" {
		t.Fatalf("the second attempt started holding the first one's stream: %+v", *watch)
	}
	if watch.deadline.Before(second) {
		t.Fatalf("the second attempt's deadline %s is already past at %s", watch.deadline, second)
	}
	// WHAT BELONGS TO THE QUESTION IS UNTOUCHED. A rescue does not buy the
	// question more time and a re-ask does not forget where it has been.
	if watch.plan.Deadline != plan.Deadline {
		t.Fatal("re-arming moved the question's deadline")
	}
	if race.plan.Moves != plan.Moves {
		t.Fatal("re-arming replaced the question's move log")
	}
}
