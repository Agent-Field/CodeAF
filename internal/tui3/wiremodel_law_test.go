package tui3

// THE LAW: WHAT IS DRAWN BESIDE THE MODEL CELL READS THE MODEL THE ENGINE IS ON.
//
// `a.model` is THE DIAL — what the person last picked, what the next turn will
// use. [app.wireModel] is what the engine is on right now. The two part company
// the moment somebody picks a model while a turn is running, and on 2026-09-11 a
// person read the two answers side by side for seventeen minutes: the model cell
// drawn from the dial, the `via` machine drawn from the request in flight,
// neither cell wrong on its own terms.
//
// So this is a RATCHET over the whole package rather than a rule about one
// file. Every function that reads the dial is named below with the reason it is
// allowed to; a reader that appears without being named fails, which is the only
// moment anybody is going to ask the question this law exists for — is this
// about what the person chose, or about what the machine is doing? Both are
// legitimate answers and they are not interchangeable, and the ones that got it
// wrong had all been written by people who never knew there were two.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// dialReaders are the functions that may read `a.model`, each because it is
// genuinely about WHAT THE PERSON CHOSE rather than about what is answering.
//
// Four groups, and the group is the reason:
//
//   - THE DIAL AS A CONTROL. The settings sheet, the picker's rows and filters,
//     the lane list, the composer's own label, the welcome line: every one of
//     these draws or edits the choice itself, and a control that showed the
//     model mid-flight would refuse to let a person see what they had set.
//   - THE DIAL AS STATE BEING MOVED. Opening, resuming, switching, adopting a
//     model a service handed back: these WRITE the field or read it to write it.
//   - THE CONVERSATION'S OWN BOOKKEEPING. Cost, the context meter, the
//     compaction threshold, the price cache, the desk key: all of them are about
//     the conversation as a whole and are keyed by the model it belongs to.
//   - THE DOOR ITSELF. [app.wireModel] reads the dial as its idle fallback,
//     which is the one reading that makes every other cell honest.
var dialReaders = map[string]string{
	// the dial as a control
	"setupControlsForm":            "the settings sheet draws the choice itself",
	"setupModelChoices":            "the settings sheet's own rows",
	"setupModelDetail":             "the settings sheet's own detail line",
	"takeSetupModel":               "the settings sheet taking a pick",
	"filterSetupModels":            "the settings sheet's list, filtered against the choice",
	"raiseSettings":                "opening the sheet on the row in force",
	"pickerKey":                    "the picker's own keystroke, about the row it opened on",
	"welcomeModelLine":             "the opening line says what this conversation was started on",
	"composerModel":                "the composer's label is the choice, not the flight",
	"talkingTo":                    "the words `you talk to` name the choice",
	"targetModel":                  "home's next-conversation target, which has no turn at all",
	"targetModelPinned":            "the same target's pin",
	"openLaneList":                 "the lane list is about the model whose lanes are being chosen",
	"cycleLane":                    "walking that list",
	"laneRowChanged":               "the person's own act on the lane row",
	"setEffortRung":                "the rung is stored per model id, and this sets it",
	"nonChatWarning":               "the refusal names the model that would have been switched to",
	"disconnectModelService":       "a service being disconnected, about the chosen model",
	"connectedServiceCarriesModel": "whether the choice belongs to the service just connected",

	// the dial as state being moved
	"newApp":                           "the field's own opening value",
	"applyEvent":                       "the engine handing the dial back",
	"attachConversation":               "taking over a conversation brings its dial",
	"switchModel":                      "this is the road the dial moves along",
	"adoptModelConnectResult":          "a connect flow handing back a model to switch to",
	"moveConversationToConnectedModel": "the same move, from the connect card",

	// the conversation's own bookkeeping
	"measureContext":     "the context meter is the conversation's, keyed by its model",
	"ctxHeat":            "the meter's colour, from the same reading",
	"ctxSpark":           "the meter's sparkline, from the same reading",
	"compactionETA":      "the threshold is a fraction of the chosen model's window",
	"compactionRuleWord": "the sentence about that threshold",
	"cacheNote":          "the price cache is keyed by the chosen model",
	"repriceCache":       "the same cache",
	"registry":           "the settings registry's rows",
	"readWorldKnown":     "the world reading this window was opened with",
	"taskSheetSelfRow":   "the conversation's own row on the task sheet",
	"talkKeys":           "the news desk's LEGACY fallback key, for news that names no conversation",

	// the door itself
	"wireModel": "the idle fallback, which is what makes every other cell honest",
}

// forEachPackageFile parses every non-test file of this package.
func forEachPackageFile(t *testing.T, visit func(path string, file *ast.File, fset *token.FileSet)) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package: %v", err)
	}
	fset := token.NewFileSet()
	scanned := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		scanned++
		visit(name, parsed, fset)
	}
	if scanned < 100 {
		t.Fatalf("only %d files were scanned; this package holds far more, so the walk is broken rather than clean", scanned)
	}
}

func TestEveryDialReaderIsOneThatMeansTheDial(t *testing.T) {
	seen := map[string]bool{}
	forEachPackageFile(t, func(path string, file *ast.File, fset *token.FileSet) {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				selector, ok := node.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "model" {
					return true
				}
				if ident, ok := selector.X.(*ast.Ident); !ok || ident.Name != "a" {
					return true
				}
				seen[fn.Name.Name] = true
				if _, allowed := dialReaders[fn.Name.Name]; !allowed {
					t.Errorf("%s reads the dial `a.model` at %s.\n"+
						"    The dial is what the person PICKED; app.wireModel() is what the engine is ON,\n"+
						"    and anything drawn beside the model cell must read the second or the frame\n"+
						"    names two models at once. If this really is about the choice, add it to\n"+
						"    dialReaders with the reason.",
						fn.Name.Name, fset.Position(selector.Pos()))
				}
				return true
			})
		}
	})
	// AND THE LIST ONLY EVER SHRINKS. A name left on it after its reader moved to
	// [app.wireModel] is permission nobody is using, and the next reader to be
	// written under that name would inherit it silently.
	for name := range dialReaders {
		if !seen[name] {
			t.Errorf("dialReaders still allows %q, which no longer reads the dial — delete the line", name)
		}
	}
}

// AND THE WIRE READING ITSELF KEEPS ITS TWO HALVES. [app.turnModel] must ask the
// engine and must never infer a turn from the news desk: a phase entry is
// deleted at the end of every request and every tool batch, so a reading taken
// from its presence goes empty between two steps of one turn — the cell flipping
// back to the dial at every step boundary, which is the defect wearing a
// different hat.
func TestTheWireReadingAsksTheEngineAndNotTheNewsDesk(t *testing.T) {
	source, err := os.ReadFile("lanes.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "lanes.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Name.Name != "turnModel" {
			continue
		}
		found = true
		asksEngine, readsDesk := false, false
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fun := call.Fun.(type) {
			case *ast.SelectorExpr:
				switch fun.Sel.Name {
				case "TurnModel":
					asksEngine = true
				case "livePhase", "talkPhase", "roomPhase":
					readsDesk = true
				}
			case *ast.Ident:
				if fun.Name == "phaseNewsFor" {
					readsDesk = true
				}
			}
			return true
		})
		if !asksEngine {
			t.Error("turnModel no longer asks the engine for its latched model")
		}
		if readsDesk {
			t.Error("turnModel infers a turn from the news desk, whose entries are deleted at every request end and every tool batch end")
		}
	}
	if !found {
		t.Fatal("turnModel is no longer in lanes.go, so this law is guarding nothing")
	}
}
