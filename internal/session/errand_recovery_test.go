package session

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── ONE RECOVERY MACHINE FOR ERRANDS ────────────────────────────────────────
//
// THE MEASURED FAILURE. Task 1 of conversation 57d51779f63ac603, 2026-09-10.
// A division review was asked of `z-ai/glm-5.3`, which wrote its first token in
// one second and was still writing at ninety, when it was cut. The fall-through
// rung, `deepseek/deepseek-v4-flash-0731`, wrote its first token in 4.8 seconds
// and was cut at ninety too. The parts were then admitted with nobody having
// read them, on a journal line indistinguishable from a reviewed admission,
// after three minutes and twenty seconds in which the person watching saw the
// word "sizing the work" and nothing else.
//
// The ninety seconds was arithmetic: the review's three minutes divided by the
// two rungs of the ladder and armed as a hard deadline on each call. A wall
// clock cannot tell a rung that is silent from a rung that is answering, so it
// killed both — the shape #786 fixed for a person's turn and left errands out of.

// errandScript is what one model does when an errand asks it.
type errandScript struct {
	// answer is what comes back, after `after` has passed. An empty answer with
	// no error is a model that said nothing.
	answer string
	// err is what comes back instead, and it is how a test spells the guard
	// having cut a rung: [context.DeadlineExceeded] is exactly what the call
	// returns when internal/provider's stream guard ends a silent stream.
	err   error
	after time.Duration
}

// errandLadder is a completer that answers per model and records what each call
// was handed — including, and this is the assertion the whole wave turns on, HOW
// MUCH TIME WAS LEFT ON ITS CLOCK when it arrived.
type errandLadder struct {
	mu     sync.Mutex
	script map[string][]errandScript
	calls  []errandCall
}

// errandCall is one request as the model saw it.
type errandCall struct {
	model string
	// left is what [context.Context.Deadline] said was still to come. A ladder
	// that hands its first rung an even share of the errand's patience gives it
	// half; a ladder that bounds the ERRAND gives it nearly all of it, and no
	// clock on the test machine can turn one of those readings into the other.
	left time.Duration
}

func (l *errandLadder) CompleteWithMessages(ctx context.Context, _ []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	l.mu.Lock()
	var left time.Duration
	if deadline, ok := ctx.Deadline(); ok {
		left = time.Until(deadline)
	}
	l.calls = append(l.calls, errandCall{model: request.Model, left: left})
	steps := l.script[request.Model]
	step := errandScript{err: errors.New("no script for " + request.Model)}
	if len(steps) > 0 {
		step = steps[0]
		if len(steps) > 1 {
			l.script[request.Model] = steps[1:]
		}
	}
	l.mu.Unlock()

	if step.after > 0 {
		select {
		case <-time.After(step.after):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if step.err != nil {
		return nil, step.err
	}
	return textResponse(step.answer), nil
}

func (l *errandLadder) seen() []errandCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]errandCall{}, l.calls...)
}

// twoRungAgent is an agent whose errands walk a two-rung ladder: a model on the
// role's tier, then the conversation's own.
func twoRungAgent(t *testing.T, client Completer) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, client, func(config *Config) {
		config.RolesSource = tierSettings(map[string]string{
			string(roles.TierKey(roles.TierLow)): "tier/one",
		})
	})
	return agent
}

// A RUNG THAT IS STILL ANSWERING IS NOT CUT BY THE LADDER'S ARITHMETIC.
//
// The first rung is handed what is left of the ERRAND, not a share of it, so an
// answer that takes longer than the old even split allowed still lands and is
// still used. Both halves are asserted: the clock the call was handed, which is
// the same on any machine, and the answer arriving after the moment the split
// would have killed it.
func TestAnErrandRungStillAnsweringIsNotCutByTheLaddersOwnShare(t *testing.T) {
	const budget = 2 * time.Second
	ladder := &errandLadder{script: map[string][]errandScript{
		"tier/one": {{answer: "the answer", after: 1200 * time.Millisecond}},
	}}
	agent := twoRungAgent(t, ladder)

	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	response, model, err := agent.callRole(ctx, roles.RoleTitle, "session/model", nil)
	if err != nil {
		t.Fatalf("the errand failed: %v", err)
	}
	if model != "tier/one" || strings.TrimSpace(response.Text()) != "the answer" {
		t.Fatalf("the errand answered %q on %q, want the first rung's own answer", response.Text(), model)
	}

	calls := ladder.seen()
	if len(calls) != 1 {
		t.Fatalf("the ladder made %d calls, want the one rung that answered: %+v", len(calls), calls)
	}
	// THE FIRST RUNG WAS HANDED THE WHOLE ERRAND'S CLOCK. Under the even split it
	// was handed budget/2 — one second — and the answer above, which takes 1.2s,
	// was cut. Anything at or under half is that arithmetic still in place.
	if calls[0].left < budget-300*time.Millisecond {
		t.Fatalf("the first rung was handed %s of a %s errand, want nearly the whole of it",
			calls[0].left, budget)
	}
}

// A RUNG THE GUARD CUT LEAVES THE NEXT ONE WHAT IS LEFT, WHICH IS NEARLY ALL OF IT.
//
// This is the half the even split was written for on 2026-08-28 — a wedged
// endpoint must not eat the budget the fall-through rung needs — and it is
// better served by the errand's own clock than by arithmetic, because the guard
// ends a silent rung in a fraction of the patience rather than at its share of
// it. The cut is spelled the way the guard spells it: the call returns the
// context's own deadline error.
func TestASilentErrandRungLeavesTheNextOneTheRestOfThePatience(t *testing.T) {
	// ONE ATTEMPT PER RUNG, so this test is about the LADDER and not about the
	// retry the boundary is separately allowed to ask for.
	t.Setenv("AFORGE_RESPONSE_ATTEMPTS", "1")
	const budget = 2 * time.Second
	ladder := &errandLadder{script: map[string][]errandScript{
		"tier/one":      {{err: context.DeadlineExceeded, after: 200 * time.Millisecond}},
		"session/model": {{answer: "the floor answered"}},
	}}
	agent := twoRungAgent(t, ladder)

	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	response, model, err := agent.callRole(ctx, roles.RoleTitle, "session/model", nil)
	if err != nil {
		t.Fatalf("the errand failed: %v", err)
	}
	if model != "session/model" || strings.TrimSpace(response.Text()) != "the floor answered" {
		t.Fatalf("the errand answered %q on %q, want the fall-through rung", response.Text(), model)
	}

	calls := ladder.seen()
	if len(calls) != 2 {
		t.Fatalf("the ladder made %d calls, want one per rung: %+v", len(calls), calls)
	}
	// THE SECOND RUNG GOT THE REMAINDER AND NOT A SHARE. Under the even split it
	// was handed what was left divided by one — which looks the same here — but
	// the FIRST rung was handed half, and the fact this asserts is that neither
	// of them was.
	if calls[1].left < budget-600*time.Millisecond {
		t.Fatalf("the fall-through rung was handed %s of a %s errand, want what the first rung did not spend",
			calls[1].left, budget)
	}
}

// AND WHAT THE BOUNDARY SAYS ABOUT A FAILED RUNG IS WHAT THE LADDER DOES.
//
// The verdict used to be read and dropped: the rung below was the only move an
// errand had, whatever the failure was. A transport verdict that asks for
// another try now gets one, on the same rung, inside what is left of the
// errand's patience — and when the tries are spent the ladder moves, which is
// what every other action means to an errand.
func TestAnErrandRungUnderStrainIsAskedAgainBeforeTheLadderMoves(t *testing.T) {
	// TWO ATTEMPTS: one try and one retry, so the ladder must ask the first rung
	// twice before it is allowed to move.
	t.Setenv("AFORGE_RESPONSE_ATTEMPTS", "2")
	ladder := &errandLadder{script: map[string][]errandScript{
		"tier/one": {
			{err: errors.New("connection reset by peer")},
			{err: errors.New("connection reset by peer")},
		},
		"session/model": {{answer: "the floor answered"}},
	}}
	agent := twoRungAgent(t, ladder)

	// The backoff the transport policy asks for is spent out of this budget, so
	// it has to be able to pay for it.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	response, model, err := agent.callRole(ctx, roles.RoleTitle, "session/model", nil)
	if err != nil {
		t.Fatalf("the errand failed: %v", err)
	}
	if model != "session/model" || strings.TrimSpace(response.Text()) != "the floor answered" {
		t.Fatalf("the errand answered %q on %q, want the fall-through rung", response.Text(), model)
	}
	var asked []string
	for _, call := range ladder.seen() {
		asked = append(asked, call.model)
	}
	want := []string{"tier/one", "tier/one", "session/model"}
	if strings.Join(asked, ",") != strings.Join(want, ",") {
		t.Fatalf("the ladder asked %v, want %v", asked, want)
	}
}

// AND THE ERRAND'S PATIENCE IS NEVER EXCEEDED, whatever the ladder does inside
// it. Every rung fails, the errand ends, and it ends on the clock the caller set
// rather than on any arithmetic of its own.
func TestAnErrandWhoseEveryRungFailsEndsInsideTheCallersPatience(t *testing.T) {
	t.Setenv("AFORGE_RESPONSE_ATTEMPTS", "1")
	const budget = time.Second
	ladder := &errandLadder{script: map[string][]errandScript{
		"tier/one":      {{err: errors.New("the endpoint refused")}},
		"session/model": {{err: errors.New("the endpoint refused")}},
	}}
	agent := twoRungAgent(t, ladder)

	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	began := time.Now()
	if _, _, err := agent.callRole(ctx, roles.RoleTitle, "session/model", nil); err == nil {
		t.Fatal("an errand whose every rung failed answered without an error")
	}
	if took := time.Since(began); took > budget {
		t.Fatalf("the errand took %s of a %s patience", took, budget)
	}
	if len(ladder.seen()) != 2 {
		t.Fatalf("the ladder made %d calls, want one per rung: %+v", len(ladder.seen()), ladder.seen())
	}
}

// ── what the record says about a division nobody read ───────────────────────

// A DIVISION ADMITTED WITH NOBODY READING IT SAYS SO ON ITS OWN LINE.
//
// The fail-open road is the right posture — two gates have already passed these
// parts and a second opinion that cannot be had is not a refusal — but it used
// to be INVISIBLE. The line read `decision: admitted` and nothing else, byte for
// byte the same row a reviewed admission writes, so the autopsy of the measured
// run could not tell the two apart without matching call rows by hand.
func TestADivisionAdmittedWithNobodyReadingItSaysSoOnTheRecord(t *testing.T) {
	t.Setenv("AFORGE_RESPONSE_ATTEMPTS", "1")
	nest := newDivideNestOn(t, wideBrief, 0, &divideReviewer{fails: true}, nil)

	if answer := nest.divide(t, divideArgs(wideEvidence, 2)); !strings.HasPrefix(answer, "split into 2 parts:") {
		t.Fatalf("the worker was told %q, want the parts admitted with nobody reading them", answer)
	}
	lines := journaledDivisions(t, nest.journal)
	if len(lines) != 1 {
		t.Fatalf("the journal holds %d division lines, want one: %+v", len(lines), lines)
	}
	// THE WORD STAYS `admitted`, because that is what happened to the parts.
	if lines[0].Decision != divisionAdmitted {
		t.Fatalf("the line reads %q, want %q", lines[0].Decision, divisionAdmitted)
	}
	// AND THE REASON THERE WAS NO READING IS BESIDE IT, in the field a refusal
	// already writes it in.
	if !strings.HasPrefix(lines[0].Error, "unreached") {
		t.Fatalf("the line's error reads %q, want why the reading never happened", lines[0].Error)
	}
}

// divideRungs is a division reviewer that answers per MODEL, so a test can put
// one rung of the errand's ladder out of action and read what the node's row
// said while the ladder walked.
type divideRungs struct {
	mu sync.Mutex
	// broken is what each model fails with. A model with no entry answers.
	broken map[string]error
	answer string
	asked  []string
}

func (r *divideRungs) CompleteWithMessages(_ context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	if len(messages) == 0 || messageText(messages[0]) != divideReviewBrief {
		return textResponse("(unscripted)"), nil
	}
	r.mu.Lock()
	r.asked = append(r.asked, request.Model)
	err, broken := r.broken[request.Model]
	r.mu.Unlock()
	if broken {
		return nil, err
	}
	return textResponse(r.answer), nil
}

// THE ROW SAYS WHICH MODEL IS BEING ASKED, AND WHAT HAPPENED TO THE LAST ONE.
//
// This is the visible half of the measured failure: three minutes and twenty
// seconds of "sizing the work" over a row that showed nothing else, while two
// models were asked in turn and neither answered. The words are the surface's —
// nothing is a reviewer, nothing is unreached, nothing has a verdict.
func TestSizingSaysWhichModelIsBeingAskedAndWhatHappenedToTheLast(t *testing.T) {
	t.Setenv("AFORGE_RESPONSE_ATTEMPTS", "1")
	reviewer := &divideRungs{broken: map[string]error{
		"big/model":  context.DeadlineExceeded,
		"test/model": errors.New("the endpoint refused"),
	}}
	nest := newDivideNestOn(t, wideBrief, 0, reviewer, tierSettings(map[string]string{
		string(roles.TierKey(roles.TierMastermind)): "big/model",
	}))
	updates := nest.session.TaskUpdates()

	if answer := nest.divide(t, divideArgs(wideEvidence, 2)); !strings.HasPrefix(answer, "split into 2 parts:") {
		t.Fatalf("the worker was told %q, want the parts admitted with nobody reading them", answer)
	}

	moves := phaseMovesAbout(t, updates, nest.parent.id, 5)
	var said []string
	for _, move := range moves {
		said = append(said, move.Phase+"|"+move.Text)
	}
	want := []string{
		// The reading opens with nothing under it: no model has been asked yet.
		TaskPhaseSizing + "|",
		TaskPhaseSizing + "|asking big/model · 1 of 2",
		TaskPhaseSizing + "|big/model did not answer in time · asking test/model",
		TaskPhaseSizing + "|nobody answered · going with the parts as drawn",
		// And the node goes back to its own work with the row cleared.
		TaskPhaseWorking + "|",
	}
	if strings.Join(said, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the row said:\n%s\nwant:\n%s", strings.Join(said, "\n"), strings.Join(want, "\n"))
	}
}

// ── the legend names every part it named ────────────────────────────────────

// A LABEL OPENING A CLAUSE IS A BOUNDARY WHEREVER IT STANDS.
//
// This is the legend from task 1 of conversation 57d51779f63ac603, copied out of
// the node's own brief. It is one line with no separator between the clauses, so
// the old cut — newlines, semicolons and commas — never found B, C or D: A's
// clause swallowed the whole line and every other part fell to [sketchName]'s
// last resort. Two of the four parts went out called "read seam.start in" and
// "part 2".
func TestALegendThatNamesItsPartsOnOneLineNamesEveryOneOfThem(t *testing.T) {
	const legend = "**A:** read `seam.start` in `cmd/aforge/chatv3.go` (wired at line 519 as `Options.Start`) " +
		"to see whether the seam is reached at all " +
		"**B:** trace the folder-pick path — `folderConfirm` in `folderact.go` → the place the chosen folder is written " +
		"**C:** with both ends in view, pinpoint where the two disagree " +
		"**D:** write the fix and a test that fails without it"

	segments := legendSegments(legend)
	for index, piece := range []string{"A", "B", "C", "D"} {
		said := sketchSaid(piece, segments)
		if said == "" {
			t.Fatalf("the legend named %s and nothing was read back for it: %v", piece, segments)
		}
		name := sketchName(said, piece, index)
		// THE ONE ANSWER THAT IS ALWAYS WRONG HERE. "part 2" is what a part is
		// called when the legend never named it, and this legend named all four.
		if strings.HasPrefix(name, "part ") {
			t.Fatalf("%s came out called %q from a legend that named it %q", piece, name, said)
		}
	}
	// AND THE TWO THE MEASURED RUN GOT WRONG COME OUT AS THE LEGEND'S OWN WORDS,
	// cut by the one hand that cuts every name on this surface ([cleanTaskName]).
	if got := sketchName(sketchSaid("A", segments), "A", 0); got != "read seam.start in" {
		t.Fatalf("A is called %q", got)
	}
	if got := sketchName(sketchSaid("B", segments), "B", 1); got != "trace the folder-pick" {
		t.Fatalf("B is called %q, want the legend's own words rather than a number", got)
	}
}

// AND THE SHAPES THE LEGEND ALREADY READ STILL READ. The comma-separated
// sentence the ask actually asks for is the common case and the inline label is
// the addition, so both are pinned here rather than one replacing the other.
func TestTheLegendStillReadsTheSentenceAndTheLineApiece(t *testing.T) {
	for _, test := range []struct {
		name   string
		legend string
		want   map[string]string
	}{
		{
			"one sentence, commas between",
			"A is the validation workflow, B is the docs sweep, C is the release notes",
			map[string]string{"A": "the validation workflow", "B": "the docs sweep", "C": "the release notes"},
		},
		{
			"a line apiece",
			"A — the validation workflow\nB — the docs sweep",
			map[string]string{"A": "the validation workflow", "B": "the docs sweep"},
		},
		{
			"labels in brackets on one line",
			"(A) the validation workflow (B) the docs sweep",
			map[string]string{"A": "the validation workflow", "B": "the docs sweep"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			segments := legendSegments(test.legend)
			for piece, want := range test.want {
				if got := sketchSaid(piece, segments); got != want {
					t.Errorf("%s reads as %q, want %q (segments %v)", piece, got, want, segments)
				}
			}
		})
	}
}
