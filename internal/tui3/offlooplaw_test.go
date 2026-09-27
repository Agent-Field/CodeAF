package tui3

import (
	tea "charm.land/bubbletea/v2"

	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ── THE STRUCTURAL LAW BEHIND offloop.go ────────────────────────────────────

// engineDoors is which calls this law is about, and IT IS DERIVED RATHER THAN
// LISTED.
//
// A LIST OF NAMES IS A LAW THAT GOES STALE THE DAY A LANE IS ADDED — the first
// cut of this test carried three of them and missed every READ the same agent
// makes over the same socket. What a door IS, exactly, is a method on
// [remote.Agent] that reaches the wire: `a.c.call`, `a.c.callWithin` or
// `a.c.callAnswered`, directly or through another of its own methods. That is
// read out of `internal/remote` itself, so a method added there is on this law
// the day it is written — and a method that turns out to be a local read off
// the welcome or the facts desk (`Usage`, `ContextTokens`, `SteerRepeatKnown`)
// is off it for the only reason that matters: it crosses nothing.
func engineDoors(t *testing.T) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, "../remote", func(f os.FileInfo) bool {
		return !strings.HasSuffix(f.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("reading the wire client: %v", err)
	}
	// calls is every Agent method and which other Agent methods it calls;
	// crosses is the ones that reach the wire without help.
	calls := map[string]map[string]bool{}
	crosses := map[string]bool{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 || fn.Body == nil {
					continue
				}
				star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
				if !ok {
					continue
				}
				if id, ok := star.X.(*ast.Ident); !ok || id.Name != "Agent" {
					continue
				}
				mine := map[string]bool{}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					switch sel.Sel.Name {
					case "call", "callWithin", "callAnswered":
						if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "c" {
							crosses[fn.Name.Name] = true
						}
						return true
					}
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "a" {
						mine[sel.Sel.Name] = true
					}
					return true
				})
				calls[fn.Name.Name] = mine
			}
		}
	}
	// And a method that calls a door is a door. The graph is tiny; repeat until
	// nothing new is found and that is the whole of the closure.
	for grew := true; grew; {
		grew = false
		for name, mine := range calls {
			if crosses[name] {
				continue
			}
			for at := range mine {
				if crosses[at] {
					crosses[name] = true
					grew = true
					break
				}
			}
		}
	}
	// EXPORTED ONLY: an unexported helper is reached through the exported method
	// that is already on the law, and naming both reports one call twice.
	doors := map[string]bool{}
	for name := range crosses {
		if name != "" && name[0] >= 'A' && name[0] <= 'Z' {
			doors[name] = true
		}
	}
	if len(doors) < 10 {
		t.Fatalf("only %d doors were derived from the wire client, which cannot be right — the reading is broken", len(doors))
	}
	return doors
}

// doorsStillOnTheLoop is the DEBT this law NAMES rather than hides.
//
// THE ANSWER ROAD IS CLOSED AND THE REST OF THE SURFACE IS NOT. This lane fixed
// the road a QUESTION'S ANSWER travels — every `Resolve…`, the dial, the record
// a switch rebuilds from — because that is where the ten-second freeze was
// measured and where a stalled call costs a person a decision rather than a
// frame. The same wire agent has thirty other doors, and the surface still asks
// a lot of them from Update: submitting a message, interrupting, setting a
// model, opening a rewind. Every one of them can take [remote.callDeadline] on
// a bad link, and every one of them is a freeze waiting to happen.
//
// So they are written down, by name, with how many call sites each has. A door
// on this list is EXPECTED to be asked from the loop and is not reported; a door
// that is not on it may never be. Moving the answer road's doors onto this list
// would be undoing the lane, and the ratchet below is what stops the list
// growing quietly instead.
//
// (`Usage` and `PendingConsent` were on an earlier reading of this list and are
// off it now for the best possible reason: the derivation above proves they
// cross nothing. `Usage` reads the facts desk in memory, and `PendingConsent`
// is not on the wire agent at all.)
var doorsStillOnTheLoop = map[string]string{
	"Submit":                "the message box, home's errand pane",
	"SubmitImage":           "a pasted image, from home's errand pane",
	"InterruptFor":          "closing a tab, the welcome, home's errand pane",
	"Interrupt":             "ctrl+c mid-turn",
	"Close":                 "closing a conversation",
	"Cancel":                "the stop card",
	"Attach":                "switching to another conversation",
	"Steer":                 "a steer typed into a task's room",
	"Compact":               "/compact",
	"FollowUp":              "the follow-up a finished turn offers",
	"AnswerLaneOffer":       "the lane offer's key",
	"NoteConnected":         "the connect panel",
	"ReferPlace":            "adding a folder as a place",
	"RemovePlace":           "removing one",
	"SetModel":              "the model picker",
	"SetReasoningFor":       "the effort picker",
	"SetConversationEffort": "the effort chip",
	"SetTaskEffort":         "a task's own effort",
	"TaskEffort":            "reading it back",
	"StartTask":             "starting one",
	"RetargetTask":          "pointing one somewhere else",
	"PendingTasks":          "the tasks place",
	"JudgeDecomposable":     "the divide sheet",
	"RewindPoints":          "the rewind sheet",
	"RewindAt":              "taking a rewind",
	"Transcript":            "/export, a window switch, the rewind sheet",
	"EarlierHistory":        "the region above a compaction",
}

// doorsOnTheLoopBudget is how many such call sites there may be, and it is a
// ratchet in `internal/ci`'s own shape: it may only ever be lowered. Move one
// off the loop and lower this number in the same commit.
const doorsOnTheLoopBudget = 38

// NO DOOR IS ASKED FROM THE UPDATE LOOP.
//
// The freeze this closes is measured in doorbell.go, and the shape is in
// offloop.go: an answer sent from Update held the loop for the round trip, and
// while it held the loop it also held the wire's reader — which was trying to
// hand this same loop the news that the answer had woken the turn. Ten seconds
// per keystroke, and the engine had applied the answer in one millisecond.
//
// A call inside an [app.offLoop] literal cannot do that: it runs on the door
// line's goroutine, and what it found is folded in on the next pass.
//
// WHAT COUNTS AS INSIDE IS THE OUTER LITERAL AND NOT THE FOLD. `offLoop` takes
// a function that returns a function: the first runs off the loop, the second
// runs ON it, on the next pass. A door called in the second is a door called
// from Update wearing the right shape, and the first cut of this law allowed it.
//
// AND A DOOR TAKEN AS A VALUE IS STILL A DOOR. `f := agent.ResolveQuestion`
// followed by `f(x)` is the same call at the same cost, and reading only
// CallExpr missed it.
func TestNoEngineDoorIsAskedFromTheUpdateLoop(t *testing.T) {
	doors := engineDoors(t)
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(f os.FileInfo) bool {
		return !strings.HasSuffix(f.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("reading the surface's source: %v", err)
	}
	found, debt := 0, 0
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			_ = importedNames(file)
			agents := agentNames(file)
			allowed, folds := offLoopSpans(file)
			inside := func(n ast.Node) bool {
				for _, fold := range folds {
					if n.Pos() > fold.Pos() && n.End() < fold.End() {
						return false
					}
				}
				for _, body := range allowed {
					if n.Pos() > body.Pos() && n.End() < body.End() {
						return true
					}
				}
				return false
			}
			ast.Inspect(file, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok || !doors[sel.Sel.Name] || !onTheAgent(sel, agents) {
					return true
				}
				if _, named := doorsStillOnTheLoop[sel.Sel.Name]; named {
					// Named debt, carrying where it is asked from. Counted so
					// the ratchet can hold, and not reported.
					debt++
					return true
				}
				found++
				if inside(sel) {
					return true
				}
				t.Errorf("%s:%d asks %s from the update loop — wrap it in a.offLoop(func() func(bool) tea.Cmd {…}) so the window can draw while the engine answers (offloop.go)",
					filepath.Base(path), fset.Position(sel.Pos()).Line, sel.Sel.Name)
				return true
			})
		}
	}
	if found == 0 {
		t.Error("no engine door is called anywhere in the surface, so this law is guarding nothing — delete it or fix the reading")
	}
	if debt > doorsOnTheLoopBudget {
		t.Errorf("%d named doors are asked from the update loop and the budget is %d — move one off the loop rather than raising the number (doorsStillOnTheLoop)", debt, doorsOnTheLoopBudget)
	}
	if debt < doorsOnTheLoopBudget {
		t.Errorf("%d named doors are asked from the update loop and the budget says %d — lower doorsOnTheLoopBudget to %d in this commit", debt, doorsOnTheLoopBudget, debt)
	}
}

// onTheAgent reports whether a selector is a call ON THE AGENT, which is the
// other half of what a door is.
//
// THE NAME IS NOT ENOUGH AND CANNOT BE. `Close`, `Cancel` and `Steer` are all
// doors on the wire agent AND all ordinary methods on files, contexts and
// rooms; a law that read the name alone reported thirty calls to `file.Close()`
// as wire crossings, which is a law nobody would keep. So the receiver has to
// BE the agent: `a.agent` itself, or a name this file bound to it.
func onTheAgent(sel *ast.SelectorExpr, agents map[string]bool) bool {
	switch x := sel.X.(type) {
	case *ast.SelectorExpr:
		return x.Sel.Name == "agent"
	case *ast.Ident:
		return agents[x.Name]
	}
	return false
}

// agentNames is every local name in one file that holds the agent: bound from
// `a.agent`, from a type assertion on it, or from [app.questionDoors] and the
// other readings that hand one back.
//
// A DOOR REACHED SOME OTHER WAY ESCAPES THIS LAW, and that is stated rather
// than pretended away: what it catches is every shape the surface actually
// uses, and a new one has to be added here the day it is written.
func agentNames(file *ast.File) map[string]bool {
	names := map[string]bool{}
	fromAgent := func(expr ast.Expr) bool {
		switch rhs := expr.(type) {
		case *ast.SelectorExpr:
			return rhs.Sel.Name == "agent"
		case *ast.TypeAssertExpr:
			if inner, ok := rhs.X.(*ast.SelectorExpr); ok {
				return inner.Sel.Name == "agent"
			}
		case *ast.CallExpr:
			if fn, ok := rhs.Fun.(*ast.SelectorExpr); ok {
				switch fn.Sel.Name {
				case "questionDoors", "stander", "Agent", "planReader":
					return true
				}
			}
		}
		return false
	}
	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) != 1 || !fromAgent(assign.Rhs[0]) {
			return true
		}
		if id, ok := assign.Lhs[0].(*ast.Ident); ok && id.Name != "_" {
			names[id.Name] = true
		}
		return true
	})
	return names
}

// offLoopSpans is every function literal handed to [app.offLoop] or to
// [app.besideLine] in one file — the only places a door may be asked, both off
// the update loop — and beside it every literal nested
// inside one of those, which is where a door may NOT be asked: the fold runs on
// the update loop.
func offLoopSpans(file *ast.File) (allowed, folds []*ast.FuncLit) {
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "offLoop" && sel.Sel.Name != "besideLine") || len(call.Args) != 1 {
			return true
		}
		lit, ok := call.Args[0].(*ast.FuncLit)
		if !ok {
			return true
		}
		allowed = append(allowed, lit)
		ast.Inspect(lit.Body, func(in ast.Node) bool {
			if nested, ok := in.(*ast.FuncLit); ok && nested != lit {
				folds = append(folds, nested)
			}
			return true
		})
		return true
	})
	return allowed, folds
}

// importedNames is every package name this file can spell.
func importedNames(file *ast.File) map[string]bool {
	names := map[string]bool{}
	for _, spec := range file.Imports {
		name := strings.Trim(spec.Path.Value, `"`)
		if at := strings.LastIndex(name, "/"); at >= 0 {
			name = name[at+1:]
		}
		if spec.Name != nil {
			name = spec.Name.Name
		}
		names[name] = true
	}
	return names
}

// ── THE WIRE SEES WHAT A PERSON DID, IN THE ORDER THEY DID IT ───────────────
//
// Review of #919, item 4. Every door used to be asked on the command's own
// goroutine, and commands start in whatever order the runtime feels like — so
// `D`, which writes the project's rule AND answers the question in one
// keystroke, could have its answer reach the engine first, resume the turn and
// re-ask the question before the rule it was meant to be written under existed.
func TestTheDoorsAreAskedInTheOrderTheKeysWerePressed(t *testing.T) {
	line := newDoorLine()
	defer line.close()
	var mu sync.Mutex
	var order []int
	waits := make([]chan func(bool) tea.Cmd, 0, 32)
	first := make(chan struct{})
	for i := 0; i < 32; i++ {
		at := i
		waits = append(waits, line.add(func() func(bool) tea.Cmd {
			if at == 0 {
				// THE FIRST ASK IS THE SLOW ONE, which is the whole test: a line
				// that did not hold its order would let the thirty-one behind it
				// past while this one was still on the wire.
				<-first
			}
			mu.Lock()
			order = append(order, at)
			mu.Unlock()
			return nil
		}))
	}
	close(first)
	for _, said := range waits {
		<-said
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 32 {
		t.Fatalf("the line asked %d of 32 doors", len(order))
	}
	for i, at := range order {
		if at != i {
			t.Fatalf("door %d was asked in position %d — the line does not hold the order: %v", at, i, order)
		}
	}
}

// AND WHAT IS ALREADY IN THE LINE IS STILL ASKED WHEN THE WINDOW CLOSES. The
// last keystroke before a window goes is an answer somebody gave.
func TestClosingTheLineStillAsksWhatIsInIt(t *testing.T) {
	line := newDoorLine()
	asked := make(chan int, 4)
	hold := make(chan struct{})
	waits := make([]chan func(bool) tea.Cmd, 0, 4)
	for i := 0; i < 4; i++ {
		at := i
		waits = append(waits, line.add(func() func(bool) tea.Cmd {
			if at == 0 {
				<-hold
			}
			asked <- at
			return nil
		}))
	}
	line.close()
	close(hold)
	for _, said := range waits {
		<-said
	}
	if len(asked) != 4 {
		t.Fatalf("closing the line dropped %d asks that were already in it", 4-len(asked))
	}
}

// ── AND THE DOOR BESIDE THE LINE IS NOT A WAY AROUND IT ─────────────────────
//
// [app.besideLine] gives up the order, so what may be asked through it is
// decided by property and written down here with the reason: the ask is not a
// gesture, and nothing a person does next depends on the engine having seen it
// first. A gesture moved there because its door felt slow would be a gesture
// the engine can see out of order, which is the fault the line was built to
// end (#919).
var doorsBesideTheLine = map[string]string{
	"PlanRunSummary":    "reads the run's stored summary for a refresh nobody pressed for",
	"PlanTaskPage":      "reads an open task room's page again on the room's own beat; nobody pressed for it, and a note's own read is asked on the ordered line",
	"PlanTaskWork":      "reads the run's working copy for the work tab; it changes nothing, and no later gesture waits on it",
	"PlanTasks":         "reads the run's rows for the side list after a message; nobody pressed for it, and a verb's own read is asked only once the verb has landed",
	"RefreshRunSummary": "asks a model for the run's summary under a budget of seconds; nobody pressed for it and no gesture depends on it",
	"NameTeam":          "asks a model for a suggested team name under a budget of seconds; it changes nothing on the engine, typing overrides it, and a message sent while it thinks must not wait behind it",
	"ProposeTeams":      "asks a model for Organize's proposals under a budget of seconds; it changes nothing on the engine (Apply writes through the teams store), and nothing after it depends on the engine having seen it",
}

func TestOnlyReadsNobodyPressedForAreAskedBesideTheLine(t *testing.T) {
	doors := engineDoors(t)
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(f os.FileInfo) bool {
		return !strings.HasSuffix(f.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("reading the surface's source: %v", err)
	}
	seen := map[string]bool{}
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			agents := agentNames(file)
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "besideLine" || len(call.Args) != 1 {
					return true
				}
				ast.Inspect(call.Args[0], func(in ast.Node) bool {
					door, ok := in.(*ast.SelectorExpr)
					if !ok || !doors[door.Sel.Name] || !onTheAgent(door, agents) {
						return true
					}
					seen[door.Sel.Name] = true
					if doorsBesideTheLine[door.Sel.Name] == "" {
						t.Errorf("%s:%d asks %s beside the ordered line — a gesture keeps its place in a.offLoop; only a read nobody pressed for may be named in doorsBesideTheLine, with its reason",
							filepath.Base(path), fset.Position(door.Pos()).Line, door.Sel.Name)
					}
					return true
				})
				return true
			})
		}
	}
	for door := range doorsBesideTheLine {
		if !seen[door] {
			t.Errorf("%s is named in doorsBesideTheLine and nothing asks it there any more — delete its line", door)
		}
	}
}
