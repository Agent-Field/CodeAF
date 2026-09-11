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
	"time"

	"github.com/Agent-Field/aforge-v2/internal/calllog"
	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
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
	client, _, model := stubbedRouter(t)
	primed(t, model,
		laneBelief(model, "quicksilver", 400, 70, 0.25),
		laneBelief(model, "brass", 900, 60, 0.30),
	)
	pinned(t, LanePin{Lane: "brass"})

	choice, made := client.drawLaneChoice(callKnobs{}, model, &ai.Request{Model: model, Messages: userMessages("hello")})
	if !made {
		t.Fatal("a pin made no choice at all, so nothing downstream is watched")
	}
	// THE PLAN IS WHERE THE OFFER LIVES, and both halves of it have to be true.
	// Pinned is what turns the act from a rescue into a question; the
	// alternatives are what the question points at. A plan with the first and
	// not the second raises `switch to ...?` with nothing after the "to", which
	// is a question nobody can answer — and the request would then report the
	// wait and leave a person watching a machine they chose go quiet.
	plan := lanes.PlanFor(choice, lanes.Pace{}, lanes.RoleTalk, time.Now())
	// AND THE PLAN READS THE CHOICE'S OWN WORD FOR IT, exactly as `planFor`
	// does. Counting `Only` here would be a second idea of what a pin is, and
	// since the chooser began demanding its admitted set it would be the wrong
	// one: every ordinary call would read as pinned.
	plan.Pinned = choice.Pinned
	if !plan.Pinned {
		t.Fatal("a strict pin did not read as pinned, so a stall would rescue away from a machine a person named")
	}
	if len(plan.Alts) == 0 {
		t.Fatal("a pinned plan names nowhere a `y` would go, so the offer can never be raised")
	}
	// AND THE DOORS THE SURFACE ANSWERS THROUGH EXIST AND MEAN "NO OFFER" WHEN
	// THERE IS NONE. False is a real answer: the lane came good while somebody
	// was reaching for the key, or the request ended, or the window lapsed.
	if AnswerOffer("a token nothing was ever raised under", true) {
		t.Error("a token naming nothing was answered, so a stale keystroke could fire a rescue")
	}
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
	// THE DOOR OR THE ONE FUNCTION IT HANDS TO. `SetModel` is the exported door
	// and its body is one line — the work, and the answer to "when does this
	// land", live in `setModel` beside it (internal/session's steer.go states why
	// the door cannot carry that answer). A law that only ever looked at the
	// exported name would have failed on a rename that changed nothing, and it
	// still cannot be satisfied by a door that does nothing: the beat has to be
	// named somewhere on the road the pick actually takes.
	road := []*ast.FuncDecl{waitingFunc(t, agent, "SetModel")}
	for _, handedTo := range []string{"setModel"} {
		if waitingNames(road[0], handedTo) {
			road = append(road, waitingFunc(t, agent, handedTo))
		}
	}
	for _, fn := range road {
		if waitingNames(fn, "laneBeat") || waitingNames(fn, "noteLaneModel") || waitingNames(fn, "startLaneBeat") {
			return
		}
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

// ── EVERY WAIT IN THE REQUEST PATH IS SPOKEN ────────────────────────────────

// spokenWaitExceptions are the functions in this package that park on a timer
// and are RIGHT to say nothing, each with the reason it is right.
//
// AN ALLOWLIST IS A CONFESSION AND IS MEANT TO READ LIKE ONE. Every name here
// is a place a person could in principle be waiting with nothing on the screen,
// and the only defence is that nobody is: the wait is a background beat nobody
// is blocked on, or it is bounded by a figure too small for anyone to perceive,
// or the phase for it is posted by the caller that owns the story. A name added
// here without one of those three sentences beside it is the law being switched
// off rather than satisfied.
var spokenWaitExceptions = map[string]string{
	// The clock seam itself: it is what every narrated wait in this package
	// sleeps on, so demanding that it narrate would be circular.
	"waitContext": "the timer seam every other wait is built from",
	// A beat is not a wait. Nobody is blocked on these: they wake a goroutine
	// that then decides whether there is anything to say, and what it says goes
	// out through the phase clock at that point.
	"(*hedgeRace).rearm":  "a beat that wakes the controller; the act it takes is what speaks",
	"(*stallWatch).watch": "the silence beat; a cut it decides on is composed upstream",
	// Bounded far under what a person can perceive. [lane.SpokenWithin] is one
	// second and these are hundredths of it.
	"(*Client).drainReceipts": "receipts are fetched after the answer has landed; nobody is waiting",
	// THE ONE THE RECOVERY DESIGN NAMES AND THE ONE IT IS WRONG ABOUT.
	// `abandonGrace` is listed in §2 problem 8 beside the limiter's slot, and it
	// is a different case: [hedgeRace.drainArms] is reached only from
	// [hedgeRace.accountForTheAbandoned], which runs on its own goroutine after
	// the cancelled caller has already been answered. It is NOT in the request
	// path at all — which it was until 2026-09-11, when it was measured spending
	// the whole of [lane.SpokenWithin] in front of a person who had just typed a
	// correction. What the arms owe is their log rows, which they write
	// themselves, and that is what the drain is for.
	"(*hedgeRace).drainArms": "runs behind the answered caller; nobody is on the other side of it",
}

// TestEveryWaitInTheRequestPathIsSpoken is the recovery design's clause 4 as a
// law: `no select on a timer in the request path without a notePhase`
// (`docs/design/recovery/DESIGN.md` §3).
//
// FOUR WAITS WERE SILENT and the census could not see any of them, because a
// wait with no phase leaves no row and no line — it is indistinguishable, from
// every surface above it, from a process that is doing nothing. The one this
// wave closed is the limiter's slot queue: a 429 storm halves the process-wide
// ceiling to one slot and every other call in the process parks there BEFORE
// its request is on the wire, so not one of the stream's own phases has started
// and the person reads a blank line for the length of somebody else's burst.
//
// WHAT IT READS IS A SELECT WITH A TIMER ARM IN IT. That is the shape of a wait
// this package can hold a person in — `time.After`, a `time.Timer`'s C, a
// `time.Ticker`'s C — and it is deliberately syntactic: a law that had to
// understand which waits are reachable from a request would be a law that gets
// the answer wrong quietly. The exceptions are named above, each with its
// reason.
func TestEveryWaitInTheRequestPathIsSpoken(t *testing.T) {
	root := funnelRepoRoot(t)
	dir := filepath.Join(root, "internal", "provider")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read internal/provider: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse internal/provider/%s: %v", name, err)
		}
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if !selectsOnATimer(fn.Body) {
				continue
			}
			spelled := spokenWaitName(fn)
			if _, excused := spokenWaitExceptions[spelled]; excused {
				continue
			}
			if saysAPhase(fn.Body) {
				continue
			}
			t.Errorf("internal/provider/%s: %s parks on a timer and says nothing — "+
				"a wait that is real is reported (docs/design/waiting/DESIGN.md). "+
				"Post a notePhase with an honest deadline, or name it in spokenWaitExceptions "+
				"with the sentence that says why nobody is waiting on it.",
				name, spelled)
		}
	}
}

// spokenWaitName spells a declaration the way the allowlist does: bare for a
// function, `(*Type).Method` for a method, so a name there is unambiguous.
func spokenWaitName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	var receiver string
	switch typed := fn.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if ident, ok := typed.X.(*ast.Ident); ok {
			receiver = "(*" + ident.Name + ")"
		}
	case *ast.Ident:
		receiver = typed.Name
	}
	return receiver + "." + fn.Name.Name
}

// selectsOnATimer reports whether this body parks on a clock inside a select:
// a `time.After` call, or a receive from something spelled `.C`. Both are what
// a wait a person can be held in looks like in this package.
func selectsOnATimer(body *ast.BlockStmt) bool {
	parks := false
	ast.Inspect(body, func(n ast.Node) bool {
		stmt, ok := n.(*ast.SelectStmt)
		if !ok {
			return !parks
		}
		ast.Inspect(stmt, func(inner ast.Node) bool {
			switch typed := inner.(type) {
			case *ast.SelectorExpr:
				if typed.Sel.Name == "After" || typed.Sel.Name == "Tick" || typed.Sel.Name == "C" {
					parks = true
				}
			}
			return !parks
		})
		return !parks
	})
	return parks
}

// saysAPhase reports whether this body reaches the one phase pipe, by any of
// the three doors: the plain post, the clock a request carries, or the beat
// that keeps one alive.
func saysAPhase(body *ast.BlockStmt) bool {
	for _, door := range []string{"notePhase", "postPhase", "announce", "say", "enter", "firstWord", "switching", "asking", "allSlow", "tellTheWait"} {
		if waitingNames(body, door) {
			return true
		}
	}
	return false
}
