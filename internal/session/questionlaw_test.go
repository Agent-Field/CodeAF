package session

// THE LAW: EVERY QUESTION THIS ENGINE ASKS IS RAISED THROUGH ONE DOOR.
//
// A question has three moments — raised, answered, withdrawn — and a surface
// that heard the first and can never hear the other two draws a question that
// stays on screen after it is settled, in every window but the one that
// answered it. That is exactly what five lanes did: connect, the harness offer,
// the harness design, a sub-harness intake card, a running sub-harness's own
// question and an adaptive run at its fuel gate each sent their own turn event
// and nothing else, so the question existed on the questions lane only as
// something [Agent.OpenQuestions] derived when somebody happened to subscribe.
//
// The fix is one door ([Agent.raiseQuestion]) that banks the words, lets the
// lane say its own row, and emits the raise — and this test is what keeps it
// one. It walks this package's own source, because a rule that is only written
// in a doc comment is a rule the next lane will not read.
//
// WHAT IT ASKS, and what each answer is worth:
//
//   - [EventQuestion] is emitted in exactly one function. A lane that emits it
//     itself has skipped the banking, which is what makes the answer and the
//     withdrawal speak at all.
//   - [Agent.rememberQuestion] is called in exactly one function, for the same
//     reason from the other side: words banked without a raise are a question
//     nobody was ever told about that still refuses the next one.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// questionDoor is the one function every question in this package is raised
// through, and the only place the two calls below may appear.
const questionDoor = "raiseQuestion"

// questionPutBack is the one road back onto the book that is not a raise: an
// answer this engine REFUSED, whose question was claimed a moment earlier and
// has to go back exactly as it was, with nothing said on the lane about it
// ([Agent.putBackQuestion]).
const questionPutBack = "putBackQuestion"

// questionRestate is the other road that is not a raise: a question ALREADY
// standing, said again because a fact ON it changed — its clock stopped under
// somebody's hand ([Agent.restateQuestion], asklane.go). It may not go through
// the door: raising it again would put `no longer needed` on the screen of the
// person reading it and hand it a fresh settle guard and a fresh reading clock
// mid-decision. It banks the same question under the same token and says it
// once more, which every surface upserts.
const questionRestate = "restateQuestion"

func TestEveryQuestionIsRaisedThroughTheOneDoor(t *testing.T) {
	for _, call := range []string{"emitQuestion", "rememberQuestion"} {
		for _, site := range callSitesIn(t, ".", call) {
			if site.fn == questionDoor || site.fn == questionPutBack || site.fn == questionRestate {
				continue
			}
			if call == "emitQuestion" && !site.raises {
				// The same door emits the answered and the withdrawn events, and
				// those belong to the lanes that settle a question rather than to
				// the one that raises it.
				continue
			}
			t.Errorf("%s calls %s: every question is raised through Agent.%s, which banks the words, lets the lane say its own row and emits the raise",
				site.where, call, questionDoor)
		}
	}
}

// callSite is one call to a watched function: the function it sits in, where to
// find it, and whether it is the raise (as against the answered or withdrawn
// event, which share [Agent.emitQuestion]).
type callSite struct {
	fn     string
	where  string
	raises bool
}

func callSitesIn(t *testing.T, dir, name string) []callSite {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	var found []callSite
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			decl, ok := node.(*ast.FuncDecl)
			if !ok {
				return true
			}
			ast.Inspect(decl.Body, func(inner ast.Node) bool {
				call, ok := inner.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != name {
					return true
				}
				raises := false
				if len(call.Args) > 0 {
					if ident, ok := call.Args[0].(*ast.Ident); ok && ident.Name == "EventQuestion" {
						raises = true
					}
				}
				found = append(found, callSite{
					fn:     decl.Name.Name,
					where:  fset.Position(call.Pos()).String(),
					raises: raises,
				})
				return true
			})
			return false
		})
	}
	return found
}

// AND EVERY TURN RETIRES THE QUESTIONS IT LEAVES BEHIND.
//
// [Agent.retireTurnQuestions] is the ask lane's withdrawal trigger, and the
// whole of its value is being called at the one moment a turn ends. A sweep
// written, tested and never wired would leave the defect exactly where it was —
// ratifies and questions somebody asked back on standing in
// [Agent.OpenQuestions] and on the presence desk for the rest of the session —
// and nothing else in this package would notice, because everything else about
// it would still pass. So the wiring is the law.
func TestTheEndOfATurnRetiresTheQuestionsItLeftBehind(t *testing.T) {
	const sweep = "retireTurnQuestions"
	const turn = "startTurnLocked"
	for _, site := range callSitesIn(t, ".", sweep) {
		if site.fn == turn {
			return
		}
	}
	t.Errorf("nothing in Agent.%s calls Agent.%s: a question the model raised and nobody answered has no other way off the book (tools_ask.go's askOpen)", turn, sweep)
}
