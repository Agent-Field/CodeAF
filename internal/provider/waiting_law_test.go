package provider

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/calllog"
	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
)

// ── THE WAITING LAWS, AT THE SEAM THAT SENDS ────────────────────────────────
//
// `docs/design/waiting/DESIGN.md` states the design; `internal/lane/law_test.go`
// holds the half of it that is arithmetic; this file holds the half that is
// wiring. Every law here is about a place two layers have to agree, which is
// exactly the set of places the reported defect was hiding in: the funnel built
// no watch when the ledger had no opinion, the beat never learned a model
// somebody picked, and a pinned lane that went quiet had nothing at all to say.
//
// SEVERAL OF THESE ARE RED ON PURPOSE. The design's "laws currently red" section
// says which and which lane of the build plan turns each of them green.

// waitingFile parses one source file of this checkout for a law to read.
func waitingFile(t *testing.T, rel string) (*token.FileSet, *ast.File) {
	t.Helper()
	root := funnelRepoRoot(t)
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, filepath.Join(root, rel), nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", rel, err)
	}
	return fset, parsed
}

// waitingFunc is one named function of a parsed file, or a failure: a law that
// silently passed because the function had been renamed would be a law about
// nothing.
func waitingFunc(t *testing.T, file *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == name {
			return fn
		}
	}
	t.Fatalf("%s was not found — this law has nothing to hold", name)
	return nil
}

// waitingNames reports whether a node's subtree names an identifier.
func waitingNames(node ast.Node, name string) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if ident, ok := n.(*ast.Ident); ok && ident.Name == name {
			found = true
		}
		return !found
	})
	return found
}

// TestEveryFunnelCallHasAController is the reported defect, stated as a law.
//
// One function sends a completion on the wire, and until this wave the ONE place
// a clock was put on it was inside a gate that asked whether the lane router had
// an opinion. A cold ledger has none — which is the state of every model
// somebody picks after launch — so the request went out with no deadline, no
// silence beat and nothing to hedge to, and the first thing that acted on three
// minutes of silence was a transport bound.
//
// ROUTING AND WAITING ARE TWO QUESTIONS. The controller is built for every
// token-generating call, whatever the chooser said; what the chooser's silence
// costs is an ACT of [control.Report] instead of one of [control.Hedge].
func TestEveryFunnelCallHasAController(t *testing.T) {
	_, client := waitingFile(t, "internal/provider/client.go")
	sender := waitingFunc(t, client, "completeWithMessagesStreaming")
	if !waitingNames(sender, "Controller") {
		t.Error("internal/provider/client.go: completeWithMessagesStreaming never asks for a controller, " +
			"so a call the chooser had no opinion about is watched by nothing (lane W3)")
	}
	fset, hedge := waitingFile(t, "internal/provider/hedge.go")
	race := waitingFunc(t, hedge, "raceFor")
	ast.Inspect(race, func(node ast.Node) bool {
		binary, ok := node.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		selector, ok := binary.X.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Alt" {
			return true
		}
		t.Errorf("%s: the race refuses a call whose choice named no alternative — that is the cold ledger, and it is the case that most needs a clock",
			fset.Position(binary.Pos()))
		return true
	})
}

// TestTheControllerIsInstalledExactlyOnce is the registry's argument, applied to
// the one thing that decides when to act.
//
// Zero installations is a build where nothing waits on a policy at all. Two is
// two policies, and the first time one of them is fixed the surface's countdown
// and the transport's alarm point at different moments.
func TestTheControllerIsInstalledExactlyOnce(t *testing.T) {
	root := funnelRepoRoot(t)
	var sites []string
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return nil
		}
		// THE CALLS AND NEVER THE DECLARATION, and both spellings of a call.
		// The package that owns the seam installs it with an unqualified
		// `SetController(...)` — a qualified-name grep could not see that, and a
		// law that cannot see the one real installation would report zero while
		// the build is correct and one while somebody has added a second. The
		// declaration is a [ast.FuncDecl] and is not a call, so it excludes
		// itself.
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || !namesSetController(call.Fun) {
				return true
			}
			rel, _ := filepath.Rel(root, path)
			sites = append(sites, rel+":"+strconv.Itoa(fset.Position(call.Pos()).Line))
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk the tree: %v", err)
	}
	switch len(sites) {
	case 1:
	case 0:
		t.Error("nothing installs a waiting controller, so lane.Controller() is nil in every shipped build")
	default:
		t.Errorf("the waiting controller is installed in %d places: %s", len(sites), strings.Join(sites, ", "))
	}
}

// namesSetController reports whether this callee is the seam's installer, said
// either way a caller can say it: bare inside the package that owns it, and
// qualified everywhere else.
func namesSetController(fun ast.Expr) bool {
	switch named := fun.(type) {
	case *ast.Ident:
		return named.Name == "SetController"
	case *ast.SelectorExpr:
		return named.Sel != nil && named.Sel.Name == "SetController"
	}
	return false
}

// TestAHeartbeatIsReportedAsAHeartbeat keeps the two claims apart at the seam
// that can still confuse them.
//
// The read loop sees three things and they are three different facts: a comment
// line, which is proof about the PATH; a delta of thought, which is the endpoint
// writing where nobody can read; and a word, which is progress. The old loop had
// one door for the second and third and a separate one for the first, and the
// controller was told a thought was a first token.
func TestAHeartbeatIsReportedAsAHeartbeat(t *testing.T) {
	root := funnelRepoRoot(t)
	source, err := os.ReadFile(filepath.Join(root, "internal/provider/client.go"))
	if err != nil {
		t.Fatalf("read the stream loop: %v", err)
	}
	text := string(source)
	for _, needed := range []string{"Beat:", "Hidden:", "Visible:"} {
		if !strings.Contains(text, needed) {
			t.Errorf("the stream loop never fills %s on a reading — the controller cannot tell a heartbeat, "+
				"a thought and a word apart (lane W3)", strings.TrimSuffix(needed, ":"))
		}
	}
}

// TestAPinnedLaneCanRaiseAnOffer is the seam a person answers on.
//
// "X is slow · switch to auto? (y)" is one sentence that has to cross three
// layers: the transport raises it, the session forwards it, the surface draws it
// and takes the keystroke back. It rides the phase channel that already exists,
// because a second channel for one sentence is a second thing to keep alive.
func TestAPinnedLaneCanRaiseAnOffer(t *testing.T) {
	if !waitingHasIdent(t, "internal/provider/phase.go", "PhaseAsking") {
		t.Error("there is no phase for a wait a person can end, so a pinned lane that stalls says nothing (lane W3)")
	}
	if !waitingHasIdent(t, "internal/provider/offer.go", "AnswerOffer") {
		t.Error("there is no door for the answer, so `y` has nothing to call (lane W3)")
	}
}

// waitingHasIdent reports whether a file exists and declares a name. A missing
// file is a missing name and not a failure of the law's own machinery: the file
// is one of the things the build plan creates.
func waitingHasIdent(t *testing.T, rel, name string) bool {
	t.Helper()
	source, err := os.ReadFile(filepath.Join(funnelRepoRoot(t), rel))
	if err != nil {
		return false
	}
	return strings.Contains(string(source), name)
}

// TestTheCallLogSaysWhyItWaitedAndWhatItDid is what makes the next autopsy
// possible.
//
// The row already carries the deadline that was computed and whether a hedge
// fired. It does not carry the two figures that would have answered the reported
// question in one line: how long the silence had run when something was done,
// and what was done. A log that records the outcome and not the reasoning is a
// log that can only ever confirm what somebody already suspected.
func TestTheCallLogSaysWhyItWaitedAndWhatItDid(t *testing.T) {
	row := reflect.TypeOf(calllog.Record{})
	for _, field := range []string{"SilenceMs", "Action", "Arms", "WasteUSD"} {
		if _, ok := row.FieldByName(field); !ok {
			t.Errorf("calllog.Record carries no %s — an autopsy cannot say when the wait was acted on or what it cost (lane W3)", field)
		}
	}
}

// TestSetModelReachesTheBeat is the second half of the reported defect.
//
// The beat's model list is fixed when a session is constructed, from the two
// config slots. A person who picks another model in the picker gets a session
// that never fetches a sheet for it for the rest of its life — so cold start is
// the STEADY STATE for exactly the models people choose deliberately, and every
// prior the design rests on is absent for them.
func TestSetModelReachesTheBeat(t *testing.T) {
	_, agent := waitingFile(t, "internal/session/agent.go")
	set := waitingFunc(t, agent, "SetModel")
	if waitingNames(set, "laneBeat") || waitingNames(set, "noteLaneModel") || waitingNames(set, "startLaneBeat") {
		return
	}
	t.Error("internal/session/agent.go: SetModel never tells the lane beat about the new model, " +
		"so a model picked after launch never gets a sheet (lane W4)")
}

// TestEveryStreamingRoleIsBoundedAndMediaIsNot is the funnel law, said about
// waiting, and it is the one law here that is meant to be green.
//
// Every role that produces a token stream is watched, and the role only decides
// HOW LONG. Media is the single exclusion and it is an exclusion from the TOKEN
// controller rather than from patience: it produces no first token, no gaps and
// no drift, so what bounds it is a single duration and the transport's own wall.
func TestEveryStreamingRoleIsBoundedAndMediaIsNot(t *testing.T) {
	for _, role := range lanes.Roles() {
		if !role.Facts().Streams {
			if role != lanes.RoleMedia {
				t.Errorf("role %q produces no stream and is not media — the token controller has an exclusion nobody wrote down", role)
			}
			continue
		}
		if role.Ceiling() <= 0 {
			t.Errorf("role %q streams and has no ceiling", role)
		}
	}
	if lanes.RoleMedia.Facts().Streams {
		t.Error("media streams tokens now, so it belongs inside the controller and its exclusion is stale")
	}
}
