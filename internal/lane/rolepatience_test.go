package lane

import (
	"go/ast"
	"go/token"
	"math"
	"strings"
	"testing"
	"time"
)

// ── WHAT A ROLE'S OWN COLUMNS BUY ───────────────────────────────────────────
//
// The subject of this file is frontier.go's WHAT A ROLE'S OWN COLUMNS SAY ABOUT
// CHOOSING A MACHINE: one reading of a lane's wait, parameterised by the role
// table's own columns and by what has been measured, with no rule anywhere about
// a named role and no machine named outside a fixture.
//
// The two measured cases behind it are replayed here as fixtures rather than
// described: the reflex tier's 2026-09-11 09:33 drift onto a machine nothing had
// measured, and the same morning's task step served at two tokens a second
// beside two machines writing at ninety.

// beliefAt is one scripted machine: a first-token median in milliseconds, a
// generation rate in tokens a second and an output tariff in dollars a million
// tokens, all believed tightly.
//
// THE TARIFF IS WHY THE SLOWER MACHINES ARE STILL CANDIDATES. The frontier drops
// a lane another lane beats on EVERY axis, so three machines at one price would
// leave one survivor and the fixture would be asking nothing. A pool where the
// slower machine is also the cheaper one is both the real shape and the only one
// that reaches the ranking at all.
func beliefAt(model, lane string, ttftMs, rate, perMillion float64) Belief {
	return Belief{
		ID: ID{Model: model, Lane: lane},
		Facts: Facts{
			Tools: true, Quant: "fp8", MaxOut: 128_000, Context: 256_000, Uptime5m: 100,
			PriceIn: perMillion / 4 / 1_000_000, PriceOut: perMillion / 1_000_000,
		},
		TTFT:    Posterior{X: math.Log(ttftMs), P: 0.004},
		Rate:    Posterior{X: math.Log(rate), P: 0.004},
		Quality: Beta{A: 39, B: 1},
		At:      time.Now(),
	}
}

// askFor is one request in a role, with a short unread answer — the shape of
// every call this file is about.
func askFor(model string, role Role, visible, hidden int) Request {
	return Request{
		Model:       model,
		Role:        role,
		Visible:     visible,
		Hidden:      hidden,
		ValueOfTime: AttentionValue,
		QualityNeed: 0.8,
		Horizon:     role.Facts().Horizon,
		Now:         time.Now(),
	}
}

// choiceOver is the chooser's answer over a scripted pool. It goes through
// [chooserOn] for the reason that helper states: a chooser handed no sheet of
// its own is answered out of whatever the box running the test happens to have
// routed (#475).
func choiceOver(t *testing.T, req Request, beliefs ...Belief) Choice {
	t.Helper()
	ForgetPrefixes()
	t.Cleanup(ForgetPrefixes)
	return chooserOn(&fakeLedger{beliefs: beliefs}).Choose(req)
}

func namesLane(list []string, want string) bool {
	for _, lane := range list {
		if lane == want {
			return true
		}
	}
	return false
}

// TestAShortPatienceRoleRefusesTheMachineItWillNotWaitFor is the brief's own
// acceptance, and it asserts the REFUSAL rather than the ranking — because a
// ranking is what `provider.order` carries and the router may ignore it once
// `allow_fallbacks` is true (#850). Only `ignore` takes a machine off the table.
func TestAShortPatienceRoleRefusesTheMachineItWillNotWaitFor(t *testing.T) {
	const model = "vendor/short-patience"
	// A probe's declared patience is five seconds (roles.go). Three machines,
	// believed to start in 1.7, 2.9 and 5.5 seconds, writing at the same rate so
	// that only the first token separates them.
	req := askFor(model, RoleProbe, 0, 20)
	choice := choiceOver(t, req,
		beliefAt(model, "quicksilver", 1700, 400, 1.6),
		beliefAt(model, "brass", 2900, 400, 1.2),
		beliefAt(model, "molasses", 5500, 400, 0.8),
	)
	if len(choice.Order) != 2 || choice.Order[0] != "quicksilver" || choice.Order[1] != "brass" {
		t.Fatalf("order = %v, want the two machines inside the role's own patience, quickest first", choice.Order)
	}
	if !namesLane(choice.Ignore, "molasses") {
		t.Fatalf("ignore = %v, want the machine believed to still be silent at the ceiling named ON THE WIRE", choice.Ignore)
	}
	for _, candidate := range choice.Frontier {
		if candidate.ID.Lane == "molasses" {
			t.Fatal("a refused machine stayed in the frontier, where a rescue could still be pointed at it")
		}
	}
}

// TestTheDeadlineIsTheWholeOfTheVeto is the elegance the owner asked for, stated
// as a test: there is no second rule and no ratio anywhere. One pool, two roles,
// and the only thing that changes is the number the role table declares.
//
// A machine believed to start promptly and then write at sixteen tokens a second
// is inside every OTHER bound in this package — its first token is within
// [ignoreTTFTMultiple] of the best so [ignoredOf] says nothing about it, and its
// rate is above [FloorRate] so the service floor says nothing either — and it is
// nonetheless a thirteen-second answer for a request of two hundred tokens.
// Whether that is refused is the role's own deadline and nothing else.
func TestTheDeadlineIsTheWholeOfTheVeto(t *testing.T) {
	const model = "vendor/one-rule"
	pool := []Belief{
		beliefAt(model, "quicksilver", 800, 400, 1.6),
		beliefAt(model, "brass", 900, 400, 1.2),
		beliefAt(model, "molasses", 1000, 16, 0.8),
	}
	// Five seconds of patience (a probe's) against a thirteen-second answer.
	short := choiceOver(t, askFor(model, RoleProbe, 0, 200), pool...)
	if !namesLane(short.Ignore, "molasses") {
		t.Fatalf("ignore = %v; a five-second role was not going to wait thirteen", short.Ignore)
	}
	// A minute of patience, the same pool, the same arithmetic: admitted, and
	// ranked last, because a role that patient really would rather have it than
	// nothing.
	long := choiceOver(t, askFor(model, RoleStanding, 0, 200), pool...)
	if namesLane(long.Ignore, "molasses") {
		t.Fatalf("ignore = %v; a minute of patience covers a thirteen-second answer", long.Ignore)
	}
	if last := long.Frontier[len(long.Frontier)-1]; last.ID.Lane != "molasses" {
		t.Fatalf("the frontier ends with %q, want the slow writer still a candidate and ranked last", last.ID.Lane)
	}
}

// TestAPoolsSlowWriterIsNeverSentAReadRoleRequest is the 2026-09-11 10:02 case,
// scripted: a task step whose generation had collapsed took 219 seconds while two
// machines on the same model were writing at ninety, and the person read a `cd`
// row as stuck for three and a half minutes.
//
// ITS FIRST TOKEN WAS HEALTHY, which is the whole point of the fixture — nothing
// that compared first tokens could have seen it. AND ITS RATE IS ABOVE THE
// SERVICE FLOOR, which is the other half: [FloorRate] is an absolute figure
// about machines nobody would keep, and this is a machine that is perfectly
// ordinary for an errand and far too slow for somebody watching a step.
func TestAPoolsSlowWriterIsNeverSentAReadRoleRequest(t *testing.T) {
	const model = "vendor/slow-writer"
	req := askFor(model, RoleLeafAttached, 900, 200)
	choice := choiceOver(t, req,
		beliefAt(model, "quicksilver", 800, 90, 1.6),
		beliefAt(model, "brass", 900, 90, 1.2),
		beliefAt(model, "molasses", 800, 16, 0.8),
	)
	if namesLane(choice.Order, "molasses") {
		t.Fatalf("order = %v, want the slow writer nowhere in it", choice.Order)
	}
	if !namesLane(choice.Ignore, "molasses") {
		t.Fatalf("ignore = %v, want the slow writer refused on the wire, where leaving it out of the order does not reach", choice.Ignore)
	}
}

// TestNothingIsRefusedWhenThereIsNothingBetter keeps the one bound that may
// never be crossed: an empty frontier is "no opinion", which sends the request
// out blind to the very machines that were refused.
func TestNothingIsRefusedWhenThereIsNothingBetter(t *testing.T) {
	const model = "vendor/all-slow"
	req := askFor(model, RoleProbe, 0, 20)
	choice := choiceOver(t, req,
		beliefAt(model, "molasses", 30_000, 3, 1.2),
		beliefAt(model, "treacle", 40_000, 2, 0.8),
	)
	if len(choice.Order) == 0 {
		t.Fatal("every machine was refused, so the request goes out with no opinion to whichever of them the router picks")
	}
	if namesLane(choice.Ignore, choice.Order[0]) {
		t.Fatalf("%q is both asked for and refused in one object, which is an empty serving set written by us", choice.Order[0])
	}
}

// TestABimodalMachineRanksBehindASteadyOneForARoleSomebodyReads is the tail
// half of [rolePatience.expected].
//
// A MACHINE THAT ANSWERS IN A SECOND HALF THE TIME AND IN A MINUTE THE OTHER
// HALF IS NOT A FAST MACHINE to the person who drew the minute, and its MEDIAN
// cannot say so. The statistic is the one a thinking duration already keeps —
// how far one draw sits from the median — measured on the wait chain and read
// back as [Belief.Spread], and the test asserts the statistic as well as the
// outcome so that an accidental pass cannot look like a fix.
func TestABimodalMachineRanksBehindASteadyOneForARoleSomebodyReads(t *testing.T) {
	const model = "vendor/bimodal"
	ledger := newLedger()
	at := time.Now().Add(-time.Hour)
	for round := 0; round < 10; round++ {
		first := 1 * time.Second
		if round%2 == 1 {
			first = 60 * time.Second
		}
		at = at.Add(time.Minute)
		ledger.Note(Sighting{ID: ID{Model: model, Lane: "fickle"}, TTFT: first, Gen: time.Second, Tokens: 40, At: at})
		ledger.Note(Sighting{ID: ID{Model: model, Lane: "steady"}, TTFT: 3 * time.Second, Gen: time.Second, Tokens: 40, At: at})
	}
	fickle, _ := ledger.Belief(ID{Model: model, Lane: "fickle"})
	steady, _ := ledger.Belief(ID{Model: model, Lane: "steady"})
	if fickle.Spread <= steady.Spread {
		t.Fatalf("one draw from the bimodal machine is believed to vary by %.3f nats and from the steady one by %.3f; the statistic that is supposed to tell them apart cannot",
			fickle.Spread, steady.Spread)
	}
	read := rolePatience{ceiling: RoleTalk.Ceiling(), waited: true, read: true}
	unread := rolePatience{ceiling: RoleMemory.Ceiling()}
	req := Request{Visible: 400, Hidden: 40}
	fickleRead := read.expected(fickle, req, fickle.TTFT.Mean(), fickle.Rate.Mean())
	steadyRead := read.expected(steady, req, steady.TTFT.Mean(), steady.Rate.Mean())
	if fickleRead <= steadyRead {
		t.Fatalf("a role somebody reads felt %.1fs on the bimodal machine and %.1fs on the steady one, so the tail it pays is not priced",
			fickleRead, steadyRead)
	}
	// And a role nobody watches is not charged for a tail it does not pay: the
	// widening is the read column's, not a constant applied to everybody.
	if got, want := unread.expected(fickle, req, 1000, 40), PerceivedSeconds(1, 40, req.Visible, req.Hidden); got != want {
		t.Fatalf("an unread role felt %.3fs where the plain reading is %.3fs", got, want)
	}
}

// TestACollapsedRateResetsTheBeliefThatChoosesTheMachine is the 09:33 series
// from the call log, scripted: a machine writing at about sixty tokens a second
// all night fell to six, and the belief went on admitting it for five more steps
// over thirty-three minutes — one of them a nine-minute answer — because each
// collapse arrived as one observation against a posterior far too certain to
// move. A change point says the old evidence is about a different machine.
func TestACollapsedRateResetsTheBeliefThatChoosesTheMachine(t *testing.T) {
	const model = "vendor/collapse"
	id := ID{Model: model, Lane: "morph"}
	ledger := newLedger()
	at := time.Now().Add(-2 * time.Hour)
	for round := 0; round < 45; round++ {
		at = at.Add(time.Minute)
		// Sixty tokens a second: forty tokens in two thirds of a second.
		ledger.Note(Sighting{ID: id, TTFT: 300 * time.Millisecond, Gen: 667 * time.Millisecond, Tokens: 40, At: at})
	}
	settled, _ := ledger.Belief(id)
	if settled.Rate.Mean() < 50 {
		t.Fatalf("the scripted machine settled at %.0f tokens a second, so the fixture is not the one described", settled.Rate.Mean())
	}
	// Then the collapse: about six tokens a second, arriving one stream at a time.
	for round := 0; round < 3; round++ {
		at = at.Add(4 * time.Minute)
		ledger.Note(Sighting{ID: id, TTFT: 300 * time.Millisecond, Gen: 33 * time.Second, Tokens: 200, At: at})
	}
	after, _ := ledger.Belief(id)
	if after.Rate.Mean() > 15 {
		t.Fatalf("after three collapsed streams the belief still says %.0f tokens a second; it was %.0f, and the machine is writing at six",
			after.Rate.Mean(), settled.Rate.Mean())
	}
}

// ── THE LAWS ────────────────────────────────────────────────────────────────

// TestNoProbeRidesARoleSomebodyIsWaitingOn is a law over the role table rather
// than a test of one role, so a role ADDED to that table tomorrow is covered by
// it without anybody remembering this file exists.
//
// A probe is one request spent settling a doubt that every request after it
// profits from — a good bet on an errand and a bad one in front of a keypress,
// where the whole of the call is dead time.
func TestNoProbeRidesARoleSomebodyIsWaitingOn(t *testing.T) {
	const model = "vendor/probe-law"
	order := []Scored{
		{ID: ID{Model: model, Lane: "doubted"}, TTFT: 400},
		{ID: ID{Model: model, Lane: "trusted"}, TTFT: 900},
	}
	known := map[ID]Belief{
		{Model: model, Lane: "doubted"}: {Facts: Facts{Status: 3}},
		{Model: model, Lane: "trusted"}: {Facts: Facts{Tools: true, Uptime5m: 100}},
	}
	for _, role := range Roles() {
		req := Request{Model: model, Role: role, QualityNeed: 0.8}
		// Zero is the draw that always promotes, so this asks the question at
		// its worst rather than on average.
		got := sheetDoubtsLast(order, req, known, 0)
		promoted := got[0].ID.Lane == "doubted"
		if waited := role.Facts().Interactive; waited && promoted {
			t.Errorf("role %q has somebody waiting on it and a probe was promoted in front of them", role)
		} else if !waited && !promoted {
			t.Errorf("role %q has nobody waiting on it and its doubt can never be settled", role)
		}
	}
}

// TestTheChoiceNamesNoRoleAndNoMachine is the owner's architecture bar written
// as a law the build holds, and it is the one this file exists to keep.
//
// BEHAVIOUR IS DERIVED FROM DECLARED PROPERTIES AND FROM MEASURED BELIEFS. Not
// from a role's NAME — which is a second role table, disagreeing with the first
// one within a month — and not from a machine's, which is a list somebody has to
// maintain against a fleet that moves. The two files that decide where a request
// goes are held to it here with go/ast, so the day somebody writes `if role ==
// RoleRecall` in the chooser, or `if id.Lane == "…"`, the build says where.
//
// WHAT IT LOOKS FOR IS A COMPARISON, because a comparison is where a name
// becomes a branch. A string literal is otherwise perfectly ordinary in these
// files — an import path, a sentence for the picker, the published vocabulary of
// quantization names — and a law that refused all of them would be refusing
// prose. What it may not be is the right-hand side of a test against a lane.
//
// The test files are exempt and must be: a fixture has to name the role it is
// staging and the machines in its pool, which is what makes it a fixture.
func TestTheChoiceNamesNoRoleAndNoMachine(t *testing.T) {
	fset, files := sources(t)
	deciding := map[string]bool{"choose.go": true, "frontier.go": true}
	for name, file := range files {
		if !deciding[name] {
			continue
		}
		named := func(where ast.Node, role string) {
			t.Errorf("%s: a choice is made from the role %s by name; derive it from the role's columns instead",
				fset.Position(where.Pos()), role)
		}
		machine := func(where ast.Node, literal string) {
			t.Errorf("%s: a machine is named in code as %s; every lane name here came off the wire a moment ago",
				fset.Position(where.Pos()), literal)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch found := node.(type) {
			// A COMPARISON is the obvious way a name becomes a branch.
			case *ast.BinaryExpr:
				if found.Op != token.EQL && found.Op != token.NEQ {
					return true
				}
				sides := [2]ast.Expr{found.X, found.Y}
				for index, side := range sides {
					if role := roleConstant(side); role != "" {
						named(found, role)
					}
					// A machine can only arrive in one of these files off the
					// wire, so a comparison between something spelled `…Lane…`
					// and a literal is somebody hard-coding a fleet.
					if text := stringLiteral(side); text != "" && mentionsLane(sides[1-index]) {
						machine(found, text)
					}
				}
			// A SWITCH IS THE SAME BRANCH WITH NO OPERATOR IN IT, which is how
			// the first version of this law was got past: a case list holds bare
			// identifiers and the comparison the compiler makes is invisible to
			// go/ast. `switch role { case RoleRecall: }` is exactly the thing
			// the bar forbids and it contains no [ast.BinaryExpr] at all.
			case *ast.SwitchStmt:
				for _, statement := range found.Body.List {
					clause, ok := statement.(*ast.CaseClause)
					if !ok {
						continue
					}
					for _, expr := range clause.List {
						if role := roleConstant(expr); role != "" {
							named(clause, role)
						}
						if text := stringLiteral(expr); text != "" && mentionsLane(found.Tag) {
							machine(clause, text)
						}
					}
				}
			// AND A TABLE KEYED ON A ROLE IS A SECOND ROLE TABLE, which is the
			// same fault spelled as data rather than as control flow — and, like
			// the switch, it is a [ast.KeyValueExpr] and not a comparison. The
			// one table lives in roles.go and this law holds the two files that
			// decide where a request goes, so there is no legitimate key of this
			// shape in either of them.
			case *ast.KeyValueExpr:
				if role := roleConstant(found.Key); role != "" {
					named(found, role)
				}
			}
			return true
		})
	}
}

// stringLiteral is the text of this expression when it is a non-empty string
// literal, and empty otherwise — so that the three arms above ask the question
// once rather than three times.
func stringLiteral(expr ast.Expr) string {
	literal, ok := expr.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING || literal.Value == `""` {
		return ""
	}
	return literal.Value
}

// mentionsLane reports whether this expression is about a machine's name — an
// `ID.Lane`, a `candidate.ID.Lane`, a variable somebody called `lane`. It is a
// spelling test and that is the honest bound on it: the law it serves is about
// a habit a reader can see, not about types.
func mentionsLane(expr ast.Expr) bool {
	named := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok && strings.Contains(strings.ToLower(ident.Name), "lane") {
			named = true
		}
		return !named
	})
	return named
}

// roleConstant answers the name of the [Role] constant this expression is, and
// empty when it is not one. A role constant is spelled `RoleX` in this package
// and `lane.RoleX` outside it, and both shapes are checked so the law does not
// depend on where somebody writes the comparison.
func roleConstant(expr ast.Expr) string {
	switch found := expr.(type) {
	case *ast.Ident:
		if strings.HasPrefix(found.Name, "Role") && found.Name != "Role" && found.Name != "RoleFacts" {
			return found.Name
		}
	case *ast.SelectorExpr:
		return roleConstant(found.Sel)
	}
	return ""
}

// TestTwoRolesWithTheSameColumnsGetTheSameChoice is the behavioural half of the
// law above: not only is no name compared, two roles the table describes
// identically really do come out of [Chooser.Choose] with the same answer over
// the same pool.
//
// It is asserted over the whole role table rather than over a chosen pair, so a
// role added tomorrow is covered by it.
func TestTwoRolesWithTheSameColumnsGetTheSameChoice(t *testing.T) {
	const model = "vendor/same-columns"
	pool := []Belief{
		beliefAt(model, "quicksilver", 800, 400, 1.6),
		beliefAt(model, "brass", 900, 400, 1.2),
		beliefAt(model, "molasses", 1000, 16, 0.8),
	}
	// EVERY DECLARED COLUMN THE CHOOSER READS IS IN THE KEY, and Horizon is one
	// of them: it scales how wide the exploration draws are, so two roles with
	// the same patience and a different horizon SHOULD be answered differently.
	// Leaving it out was what made this law's first run fail on `memory` against
	// `leaf.unattended` — a real difference, correctly derived from the table.
	type columns struct {
		ceiling time.Duration
		waited  bool
		read    bool
		horizon int
	}
	seen := map[columns]struct {
		role   Role
		choice Choice
	}{}
	for _, role := range Roles() {
		// The moment is pinned so that two roles are asked the same question:
		// the sampler is seeded on it (seedFor).
		req := askFor(model, role, 0, 200)
		req.Now = noon
		p := patienceFor(req)
		key := columns{p.ceiling, p.waited, p.read, role.Facts().Horizon}
		choice := chooserOn(&fakeLedger{beliefs: pool}).Choose(req)
		first, already := seen[key]
		if !already {
			seen[key] = struct {
				role   Role
				choice Choice
			}{role, choice}
			continue
		}
		if strings.Join(choice.Order, ",") != strings.Join(first.choice.Order, ",") ||
			strings.Join(choice.Ignore, ",") != strings.Join(first.choice.Ignore, ",") {
			t.Errorf("roles %q and %q declare the same columns and were answered differently: %v/%v against %v/%v",
				first.role, role, choice.Order, choice.Ignore, first.choice.Order, first.choice.Ignore)
		}
	}
	if len(seen) < 2 {
		t.Fatal("every role in the table declares the same columns, so this law proved nothing")
	}
}

// TestAMachineWithNoRateBeliefIsRankedLastAndNeverRefused is B1 of the review,
// and it is the fault this whole change exists to end, seen from the other side.
//
// A lane that has only ever answered SHORT has a first-token belief and no rate
// belief at all — [ledger.see] teaches the rate only past [ratedFloor], which is
// thirty-two tokens — and that is the reflex tier's own shape. Its expectation
// is +∞ by [PerceivedSeconds]'s own design, which is the honest reading of "this
// lane is not believed to finish" and NOT evidence that it is slow. Refusing it
// would be striking a machine off the wire's table for want of evidence rather
// than because of it.
func TestAMachineWithNoRateBeliefIsRankedLastAndNeverRefused(t *testing.T) {
	const model = "vendor/short-answers-only"
	unrated := beliefAt(model, "unrated", 500, 400, 1.0)
	unrated.Rate = Posterior{}
	choice := choiceOver(t, askFor(model, RoleProbe, 0, 200),
		beliefAt(model, "quicksilver", 800, 400, 1.6),
		beliefAt(model, "brass", 900, 400, 1.2),
		unrated,
	)
	if namesLane(choice.Ignore, "unrated") {
		t.Fatalf("ignore = %v; a machine nothing has been measured of was refused for want of evidence", choice.Ignore)
	}
	found := false
	for _, candidate := range choice.Frontier {
		if candidate.ID.Lane == "unrated" {
			found = true
		}
	}
	if !found {
		t.Fatal("the unrated machine left the frontier, where a hedge, a walk or a rescue could still reach it")
	}
	if last := choice.Frontier[len(choice.Frontier)-1]; last.ID.Lane != "unrated" {
		t.Fatalf("the frontier ends with %q; a machine believed not to finish belongs last", last.ID.Lane)
	}
}

// TestAChangePointForgetsHowVariableTheMachineWas is B4 of the review. The
// dispersion account has no forgetting of its own and is persisted, so without
// this a machine that had one bad hour is widened for ever on a tail it no
// longer has — the absorbing gate this package ended everywhere else.
func TestAChangePointForgetsHowVariableTheMachineWas(t *testing.T) {
	const model = "vendor/one-bad-hour"
	id := ID{Model: model, Lane: "fickle"}
	ledger := newLedger()
	at := time.Now().Add(-2 * time.Hour)
	// An hour of wild first tokens, alternating a second and a minute.
	for round := 0; round < 20; round++ {
		first := 1 * time.Second
		if round%2 == 1 {
			first = 60 * time.Second
		}
		at = at.Add(time.Minute)
		ledger.Note(Sighting{ID: id, TTFT: first, Gen: time.Second, Tokens: 40, At: at})
	}
	erratic, _ := ledger.Belief(id)
	if erratic.Spread <= SpreadTightest {
		t.Fatalf("the erratic hour left a dispersion of %.3f nats, so the fixture is not the one described", erratic.Spread)
	}
	// Then the machine settles somewhere else entirely and stays there, which is
	// what trips the change point.
	for round := 0; round < 20; round++ {
		at = at.Add(time.Minute)
		ledger.Note(Sighting{ID: id, TTFT: 400 * time.Millisecond, Gen: time.Second, Tokens: 40, At: at})
	}
	settled, _ := ledger.Belief(id)
	if settled.Spread >= erratic.Spread {
		t.Fatalf("after the machine settled, one draw is still believed to vary by %.3f nats against the old %.3f; the change point kept the regime it said was over",
			settled.Spread, erratic.Spread)
	}
}
