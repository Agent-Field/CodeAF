package provider

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/control"
)

// TestThePurseIsAskedThroughOneDoor is the law the deleted allowance broke.
//
// THERE IS ONE RAIL IN FRONT OF A SECOND REQUEST, ONE PRICE, AND TWO PLACES
// THAT ASK IT — one per side of the decision, and no more.
//
// What made the process-wide budget able to overrule the controller was that it
// was asked from five places under three different names — the controller's
// hedge, the stall rescue, the walk, the countdown's reading and the ladder's —
// each with its own idea of what a refusal meant. Two doors survive that, and
// they are not two mechanisms:
//
//   - [hazard.reachable] asks BEFORE DECIDING. A move the purse will refuse is
//     not a move, so a controller that did not ask would hand back a hedge that
//     cannot happen and the request would sit through a rescue nobody could pay
//     for — see THE PURSE IS ASKED LAST in hazard.go's act.
//   - [hedgeRace.affordsLocked] asks BEFORE SENDING, because the controller is
//     not the only thing that starts an arm: the stall rescue, the walk past a
//     refusal and the surface's own countdown all reach the wire through here.
//
// AND THEY ASK WITH THE SAME NUMBER, which is the half that makes them one
// mechanism rather than two. [Alternative.Extra] is priced once where the plan
// is built and both sides read it ([hedgeRace.altPrice]); two estimates for one
// bound is how the rail and the decision come to disagree about what this
// question can afford.
//
// This walks the source of both packages and fails on any third asker.
func TestThePurseIsAskedThroughOneDoor(t *testing.T) {
	doors := map[string]string{
		"affordsLocked": "asks before sending",
		"reachable":     "asks before deciding",
	}
	for _, dir := range []string{".", "../lane/control"} {
		set := token.NewFileSet()
		pkg, err := parser.ParseDir(set, dir, func(info os.FileInfo) bool {
			return !strings.HasSuffix(info.Name(), "_test.go")
		}, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, files := range pkg {
			for _, file := range files.Files {
				for _, decl := range file.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if !ok {
						continue
					}
					if _, allowed := doors[fn.Name.Name]; allowed {
						continue
					}
					ast.Inspect(fn, func(node ast.Node) bool {
						call, ok := node.(*ast.CallExpr)
						if !ok {
							return true
						}
						if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "Allows" {
							t.Errorf("%s: %s asks the purse; only affordsLocked (before sending) and reachable (before deciding) may — see THE ONE DOOR in hedge.go",
								set.Position(call.Pos()), fn.Name.Name)
						}
						return true
					})
				}
			}
		}
	}
}

// TestTheTwoDoorsAskThePurseWithOnePrice is the other half of the law above:
// one rail asked with two estimates is two rails.
//
// The number is [Alternative.Extra], priced once from the frontier entry this
// race would send to, where the plan is built. The controller asks with it
// directly; the race asks with it through [hedgeRace.altPrice]. This drives the
// shipped door with a plan whose alternative is priced differently from the
// frontier beside it, and fails if the race quietly answers from the frontier.
func TestTheTwoDoorsAskThePurseWithOnePrice(t *testing.T) {
	const dear = 4.0
	asked := []float64{}
	plan := control.Plan{
		Lane:  "A",
		Alts:  []control.Alternative{{Lane: "B", Extra: dear}},
		Purse: recordingPurse{seen: &asked},
	}
	race := &hedgeRace{model: "openrouter/oneprice", winner: -1, plan: plan, expected: 100}
	// The frontier prices the same machine at a tenth of what the plan does, so
	// an answer taken from here instead of from the plan is visible.
	race.choice.Frontier = []lanes.Scored{{ID: lanes.ID{Model: "openrouter/oneprice", Lane: "B"}, Price: dear / 10}}
	race.affords("B")
	if len(asked) != 1 {
		t.Fatalf("the purse was asked %d times for one arm, want once", len(asked))
	}
	if asked[0] != dear {
		t.Fatalf("the race asked the purse for $%.2f and the plan prices this arm at $%.2f; two estimates for one bound",
			asked[0], dear)
	}
}

// recordingPurse allows everything and remembers what it was asked for.
type recordingPurse struct{ seen *[]float64 }

func (p recordingPurse) Allows(usd float64, _ time.Time) bool {
	*p.seen = append(*p.seen, usd)
	return true
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
		t.Fatalf("the second attempt started holding the first one's stream: %d tokens, %d visible, %d beats, served by %q",
			watch.tokens, watch.visible, watch.beats, watch.served)
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

// TestTheDeletedAllowanceIsNotStillDescribed is bar §6 with teeth: delete what
// you replace, INCLUDING the sentences that describe it.
//
// WHY A GREP IS THE RIGHT SHAPE HERE. `go build` deletes a dead identifier from
// the code the moment nothing calls it, and then leaves it standing in every
// comment and every design page that named it — where it is worse than dead,
// because a comment is what the next person reads INSTEAD of the code. The
// review of #924 found six: this file's own top-of-file law still said money was
// bounded by `lanes.Budget`, and `docs/ARCHITECTURE.md`'s clock table still
// published a tenth of the hour's spend over twenty requests as the rail in
// front of a rescue. Two of them asserted the OPPOSITE of the law that replaced
// them.
//
// A DEAD NAME MAY STILL BE SAID IN THE PAST TENSE, and that is the whole of the
// exception: "it was a rolling allowance until 2026-09-11" is the sentence that
// stops somebody re-inventing it. So a line carrying one of these names has to
// carry a word that puts it in the past as well.
func TestTheDeletedAllowanceIsNotStillDescribed(t *testing.T) {
	dead := []string{
		"lanes.Budget", "lane.Budget", "NewBudget", "DefaultBudget",
		"SetHedgeBudget", "currentHedgeBudget", "requestWindow", "spendWindow",
	}
	// A name in the past tense is a record and not a claim. These are the words
	// this codebase uses to say so.
	past := []string{
		"deleted", "no longer", "used to", "until 2026-09-11", "was written against",
		"stopped", "gone", "replaced", "does not exist", "gets deleted",
	}
	// The live prose: the two packages that owned the rail and the two documents
	// that publish what bounds a wait. `docs/changes` and `bench/lanelab/REPORT.md`
	// are records of what was true on a day and are deliberately not read.
	for _, where := range []string{"../provider", "../lane", "../../docs/ARCHITECTURE.md", "../../docs/design/waiting/DESIGN.md"} {
		for _, file := range prose(t, where) {
			// THE LAW IS ALLOWED TO SAY THE NAMES IT BANS. Every dead identifier
			// is written out in this file, twice — once in the list and once in
			// the account of where they were found — and a law that failed on
			// its own statement of itself would be unwritable.
			if strings.HasSuffix(file, "purse_law_test.go") {
				continue
			}
			body, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			for number, line := range strings.Split(string(body), "\n") {
				lower := strings.ToLower(line)
				for _, name := range dead {
					if !strings.Contains(line, name) {
						continue
					}
					buried := false
					for _, word := range past {
						if strings.Contains(lower, word) {
							buried = true
							break
						}
					}
					if !buried {
						t.Errorf("%s:%d still describes %s as though it existed: %s",
							file, number+1, name, strings.TrimSpace(line))
					}
				}
			}
		}
	}
}

// prose is every Go source file and Markdown page under one path, the path
// itself when it names a file. Test sources are read too: a law stated in a
// test's own comment is read by exactly the person this is for.
func prose(t *testing.T, where string) []string {
	t.Helper()
	info, err := os.Stat(where)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		return []string{where}
	}
	entries, err := os.ReadDir(where)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if name := entry.Name(); strings.HasSuffix(name, ".go") || strings.HasSuffix(name, ".md") {
			out = append(out, filepath.Join(where, name))
		}
	}
	return out
}
