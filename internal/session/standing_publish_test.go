package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/filelock"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// These are the final review's two blockers of 2026-09-10, each a run whose
// turns were not evidence of a finished report and which published anyway.
// Every case starts from a last good report and asserts its bytes survive.

const lastGoodReport = "# Report\nthe last good one\n"

// withLastGoodReport is a workspace whose report already holds lastGoodReport.
func withLastGoodReport(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "reports", "r.md"), []byte(lastGoodReport), 0o644); err != nil {
		t.Fatal(err)
	}
	return workspace
}

// expectUnpublished asserts the last good report is untouched and the run
// failed with the line that says why, recorded with its code.
func expectUnpublished(t *testing.T, workspace string, outcome standing.Outcome, code, line string) {
	t.Helper()
	raw, _ := os.ReadFile(filepath.Join(workspace, "reports", "r.md"))
	if string(raw) != lastGoodReport || outcome.Published != nil {
		t.Fatalf("the last good report was replaced with %q (outcome %+v)", raw, outcome)
	}
	if outcome.Kind != standing.OutcomeFailed || !strings.Contains(outcome.Text, line) {
		t.Fatalf("outcome %s %q, want failed saying %q", outcome.Kind, outcome.Text, line)
	}
	if outcome.Withheld != code {
		t.Fatalf("withheld code %q, want %q", outcome.Withheld, code)
	}
}

// readingStep is a turn that says text and then calls read — one step of work
// after the words, so a step limit of one stops the run there.
func readingStep(workspace, text string) step {
	args, _ := json.Marshal(map[string]string{"path": filepath.Join(workspace, "reports", "r.md")})
	return func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		if text != "" {
			provider.Emit(ctx, provider.StreamDelta, text)
		}
		return toolResponseWithText("call_r", "read", string(args), text), nil
	}
}

// BLOCKER 1. The run tried to write its own report — refused, as every
// unattended write is — and then apologised, with no report between the lines.
// The apology is not a report, and the refused write's content never is: the
// report's authority is aforge's alone.
func TestARefusedWriteOfItsOwnReportWithNoReportLinesPublishesNothing(t *testing.T) {
	root, workspace := t.TempDir(), withLastGoodReport(t)
	args, _ := json.Marshal(map[string]string{"path": filepath.Join(workspace, "reports", "r.md"), "content": "the model's own copy"})
	model := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "Now I'll write the report to the file.")
			return toolResponseWithText("call_w", "write", string(args), "Now I'll write the report to the file."), nil
		},
		saying("I cannot write the file; someone with permission needs to place it."),
	}}
	runner := standingChildRunner(t, root, model)
	runner.parent.ApprovalPolicy = &approval.Policy{Default: approval.ActionPrompt}
	outcome, err := runner.Run(context.Background(), reporting(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	expectUnpublished(t, workspace, outcome, "self-write", "tried to write its report instead of replying with it")
}

// BLOCKER 2, THE LIMIT. A step limit ends the run's turn normally — the
// interrupt closes it with no error — so a report written before the stopped
// work was published as though the run had finished.
func TestARunStoppedAtItsStepLimitPublishesNothing(t *testing.T) {
	root, workspace := t.TempDir(), withLastGoodReport(t)
	model := &scriptedCompleter{steps: []step{
		readingStep(workspace, "<report>\n# Report\n- written before the stopped work\n</report>"),
		saying("<report>\n# Report\n- after the limit\n</report>"),
	}}
	item := reporting(workspace)
	item.Does.MaxSteps = 1
	outcome, err := standingChildRunner(t, root, model).Run(context.Background(), item, filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	expectUnpublished(t, workspace, outcome, "at-a-limit", "reached its step or spending limit")
}

// BLOCKER 2, THE UNCLOSED REPORT. A report opened and never closed ends
// wherever the run stopped writing, which is half a page.
func TestAReportOpenedAndNeverClosedIsNotPublished(t *testing.T) {
	root, workspace := t.TempDir(), withLastGoodReport(t)
	model := &scriptedCompleter{steps: []step{saying("<report>\n# Report\n- half a page, and then")}}
	outcome, err := standingChildRunner(t, root, model).Run(context.Background(), reporting(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	expectUnpublished(t, workspace, outcome, "unclosed-report", "has no closing line")
}

// BLOCKER 2, THE LIMIT DURING THE CORRECTION. The rules check sent the run
// back once, and the correction turn was stopped at the step limit after it
// wrote a report. The check was handed "not capped" before the correction ran,
// so the stop went unseen and the corrected report was published.
func TestACorrectionStoppedAtTheStepLimitPublishesNothing(t *testing.T) {
	root, workspace := t.TempDir(), withLastGoodReport(t)
	model := &scriptedCompleter{steps: []step{
		saying("<report>\n# Report\n- Ask alice@example.com to confirm.\n</report>"),
		readingStep(workspace, "<report>\n# Report\n- Ask [redacted] to confirm.\n</report>"),
		saying("<report>\n# Report\n- after the limit\n</report>"),
	}}
	runner, _ := ruledRunner(t, root, model, brokenVerdict, keptVerdict)
	item := reporting(workspace)
	item.Does.MaxSteps = 1
	outcome, err := runner.Run(context.Background(), item, admittedRun(t, root), "")
	if err != nil {
		t.Fatal(err)
	}
	expectUnpublished(t, workspace, outcome, "at-a-limit", "reached its step or spending limit")
}

// BLOCKER A. A report between both lines with nothing in it is no report. It
// replaced the last good one with an empty line.
func TestAnEmptyReportBetweenItsLinesIsNotPublished(t *testing.T) {
	root, workspace := t.TempDir(), withLastGoodReport(t)
	model := &scriptedCompleter{steps: []step{saying("<report>\n</report>")}}
	outcome, err := standingChildRunner(t, root, model).Run(context.Background(), reporting(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	expectUnpublished(t, workspace, outcome, "empty-report", "report was empty")
}

// cutAtTheLimit is a tool-free answer that stopped at the provider's output
// limit — the shape the loop continues twice and then gives up on.
func cutAtTheLimit(text string) step {
	return func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		provider.Emit(ctx, provider.StreamDelta, text)
		response := textResponse(text)
		response.Choices[0].FinishReason = "length"
		return response, nil
	}
}

// BLOCKER B. Three answers in a row stopped at the output limit: the loop asks
// for the rest twice, gives up, and ends the turn like any other. The partial
// answer — no report lines at all — was published as the report.
func TestAnAnswerCutAtTheOutputLimitIsNotPublished(t *testing.T) {
	root, workspace := t.TempDir(), withLastGoodReport(t)
	model := &scriptedCompleter{steps: []step{
		cutAtTheLimit("# Report\n- the first third"),
		cutAtTheLimit(" and the second"),
		cutAtTheLimit(" and still not the end"),
	}}
	outcome, err := standingChildRunner(t, root, model).Run(context.Background(), reporting(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	expectUnpublished(t, workspace, outcome, "output-limit", "cut off at the model's output limit")
}

// BLOCKER B DURING THE CORRECTION. The rules check sent the run back once, and
// the correction ran out of continuations at the output limit.
func TestACorrectionCutAtTheOutputLimitIsNotPublished(t *testing.T) {
	root, workspace := t.TempDir(), withLastGoodReport(t)
	model := &scriptedCompleter{steps: []step{
		saying("<report>\n# Report\n- Ask alice@example.com to confirm.\n</report>"),
		cutAtTheLimit("# Report\n- Ask [redacted]"),
		cutAtTheLimit(" to confirm"),
		cutAtTheLimit(" the venue, and"),
	}}
	runner, _ := ruledRunner(t, root, model, brokenVerdict, keptVerdict)
	outcome, err := runner.Run(context.Background(), reporting(workspace), admittedRun(t, root), "")
	if err != nil {
		t.Fatal(err)
	}
	expectUnpublished(t, workspace, outcome, "output-limit", "cut off at the model's output limit")
}

// A report with a closing line and no opening line has no start anyone can
// tell. It used to fall through to "no lines" and publish the whole answer —
// the narration before it and the stray tag with it.
func TestAReportClosedButNeverOpenedIsNotPublished(t *testing.T) {
	root, workspace := t.TempDir(), withLastGoodReport(t)
	model := &scriptedCompleter{steps: []step{saying("Here is the update.\n# Report\n- ship Friday\n</report>")}}
	outcome, err := standingChildRunner(t, root, model).Run(context.Background(), reporting(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	expectUnpublished(t, workspace, outcome, "unopened-report", "has a closing line but no opening line")
}

// THE CODES ARE IDENTIFIERS. A front end reads occurrence.json's "withheld"
// instead of the line, so this set is pinned exactly: renaming, adding or
// dropping one fails here, and doing it on purpose means changing this test
// and saying so in a change entry.
func TestTheWithheldCodesArePinned(t *testing.T) {
	want := map[reportWithheld]string{
		withheldForAPerson:  "waiting-on-person",
		withheldCutOff:      "cut-off",
		withheldOutputLimit: "output-limit",
		withheldAtALimit:    "at-a-limit",
		withheldUnclosed:    "unclosed-report",
		withheldUnopened:    "unopened-report",
		withheldEmpty:       "empty-report",
		withheldSelfWrite:   "self-write",
		withheldNoReport:    "no-report",
		withheldByRules:     "held-by-rules",
		withheldStopped:     "stopped",
		withheldUnwritten:   "not-written",
		// Added by the third round (2026-09-10), deliberately.
		withheldReportChanged: "report-changed",
		// Added by round 3b (2026-09-11), deliberately: a fence that cannot
		// read the stop holds instead of falling through.
		withheldStopUnknown: "stop-unknown",
		// Added on the review of 417fa43a3 (2026-09-11), deliberately: another
		// live item keeps the report path, and the two would take turns.
		withheldReportOwned: "report-owned",
	}
	if len(withheldCodes) != len(want) {
		t.Fatalf("withheldCodes has %d codes, the pinned set has %d", len(withheldCodes), len(want))
	}
	for reason, code := range want {
		if withheldCodes[reason] != code {
			t.Errorf("reason %d is coded %q, pinned as %q", reason, withheldCodes[reason], code)
		}
	}
	// Every reason but "not withheld" has a code, and that one has none.
	for reason := notWithheld + 1; reason <= withheldStopUnknown; reason++ {
		if reason.code() == "" {
			t.Errorf("reason %d has no code", reason)
		}
	}
	if notWithheld.code() != "" {
		t.Errorf("a run nothing withheld is coded %q", notWithheld.code())
	}
}

// ── the third round: a report and its turn, truthful outcomes, the window ──

// cutThenFolding divides on its first turn, then answers three times cut at the
// output limit — together a whole report between its lines — and says after on
// every turn after, which only happens once the parts are home.
type cutThenFolding struct {
	mu    sync.Mutex
	turns int
	cut   []string
	after string
	// cutDone is closed once the last cut answer has been served.
	cutDone chan struct{}
}

func (m *cutThenFolding) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	if len(messages) == 0 || len(messages[0].Content) == 0 || messages[0].Content[0].Text != "SYSTEM" {
		return textResponse("the nightly tidy"), nil
	}
	m.mu.Lock()
	turn := m.turns
	m.turns++
	m.mu.Unlock()
	if turn == 0 {
		return toolResponse("d1", "divide_work", string(divideArgs(wideEvidence, 2))), nil
	}
	if turn <= len(m.cut) {
		if turn == len(m.cut) {
			defer close(m.cutDone)
		}
		return cutAtTheLimit(m.cut[turn-1])(ctx, messages)
	}
	return saying(m.after)(ctx, messages)
}

// dividedReportRun runs a wide report firing whose parts land AFTER the turn
// that was cut has ended — so their reports are owed to a turn of their own,
// which the run resumes. Parts landing sooner would be carried by the cut
// turn's own continuations, and no later turn would ever be asked for.
func dividedReportRun(t *testing.T, workspace string, model *cutThenFolding) standing.Outcome {
	t.Helper()
	root := t.TempDir()
	model.cutDone = make(chan struct{})
	runner := watchedFiring(t, root, model, func(cfg Config, agent *Agent) {
		graph := cfg.tasker
		graph.mu.Lock()
		graph.run = func(node *TaskNode) {
			<-model.cutDone
			for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
				agent.mu.Lock()
				running := agent.running
				agent.mu.Unlock()
				if !running {
					break
				}
			}
			node.finish("this part is done", nil, "", mergeInPlace)
			node.graph.complete(node, TaskDone)
		}
		graph.mu.Unlock()
	})
	item := reporting(workspace)
	item.Does.Brief = wideBrief
	outcome, err := runner.Run(context.Background(), item, filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	return outcome
}

// BLOCKER 1. A divided run wrote a closed report in a turn cut at the output
// limit; its parts came home and the resumed turn said only "Acknowledged."
// The report survived because the new words had no lines, and the cut was
// forgotten because the new turn ended clean: a disqualified report was
// published on the word of a turn that never touched it.
func TestAReportCutAtTheLimitIsNotRehabilitatedByALaterTurn(t *testing.T) {
	workspace := t.TempDir()
	outcome := dividedReportRun(t, workspace, &cutThenFolding{
		cut:   []string{"<report>\n# Report\n- the first part", "\n- the second part\n</report>", "\nand then more notes"},
		after: "Acknowledged.",
	})
	if _, err := os.Stat(filepath.Join(workspace, "reports", "r.md")); !os.IsNotExist(err) {
		raw, _ := os.ReadFile(filepath.Join(workspace, "reports", "r.md"))
		t.Fatalf("the cut report was published: %q (outcome %+v)", raw, outcome)
	}
	if outcome.Kind != standing.OutcomeFailed || outcome.Withheld != "output-limit" || !strings.Contains(outcome.Text, "cut off at the model's output limit") {
		t.Fatalf("outcome %+v, want failed · output-limit", outcome)
	}
}

// AND A LATER TURN THAT WRITES A NEW REPORT IS THE ONE THING THAT STANDS IN FOR
// IT: its own turn ended clean.
func TestALaterTurnsNewReportStandsInForOneCutAtTheLimit(t *testing.T) {
	workspace := t.TempDir()
	outcome := dividedReportRun(t, workspace, &cutThenFolding{
		cut:   []string{"<report>\n# Report\n- the first part", "\n- the second part\n</report>", "\nand then more notes"},
		after: "<report>\n# Report\n- both parts, folded\n</report>",
	})
	raw, _ := os.ReadFile(filepath.Join(workspace, "reports", "r.md"))
	if string(raw) != "# Report\n- both parts, folded\n" || outcome.Published == nil || outcome.Withheld != "" {
		t.Fatalf("published %q, outcome %+v", raw, outcome)
	}
}

// RECEIPT, THE WORDLESS CONTINUATION (round 3b; the third review named it
// missing). A report cut at the limit, then a resumed turn that says nothing at
// all and ends clean: silence is not a newer account, so it neither replaces
// the report nor clears the cut that came with it, and nothing is published.
func TestAReportCutAtTheLimitIsNotRehabilitatedByAWordlessTurn(t *testing.T) {
	workspace := t.TempDir()
	outcome := dividedReportRun(t, workspace, &cutThenFolding{
		cut:   []string{"<report>\n# Report\n- the first part", "\n- the second part\n</report>", "\nand then more notes"},
		after: "",
	})
	if _, err := os.Stat(filepath.Join(workspace, "reports", "r.md")); !os.IsNotExist(err) {
		raw, _ := os.ReadFile(filepath.Join(workspace, "reports", "r.md"))
		t.Fatalf("the cut report was published after a wordless turn: %q (outcome %+v)", raw, outcome)
	}
	if outcome.Kind != standing.OutcomeFailed || outcome.Withheld != "output-limit" {
		t.Fatalf("outcome %+v, want failed · output-limit", outcome)
	}
}

// The same two orders one turn at a time, and the third sequence the review
// asked about. A cut report, then a wordless clean turn, is withheld. A cut
// report, then a new closed report in a clean turn, then a wordless clean turn,
// PUBLISHES THE NEW REPORT, and that is the design rather than a hole: the
// report standing is one whose own turn ended clean, and silence after it is
// not a newer account of anything.
func TestSilenceAfterAReportKeepsWhateverThatReportsOwnTurnSaid(t *testing.T) {
	cut := "<report>\n# Report\n- half"
	var e firingEnd
	e.closeTurn(cut, cut, true)
	e.closeTurn("", "", false)
	if w := e.withheld(true, nil); w != withheldOutputLimit {
		t.Fatalf("cut then wordless: %q, want output-limit", w.code())
	}
	e = firingEnd{}
	e.closeTurn(cut, cut, true)
	e.closeTurn("<report>\n# Report\n- whole\n</report>", "<report>\n# Report\n- whole\n</report>", false)
	e.closeTurn("", "", false)
	if w := e.withheld(true, nil); w != notWithheld || e.body() != "# Report\n- whole" {
		t.Fatalf("cut, then a whole report, then wordless: %q with body %q; want the whole report", w.code(), e.body())
	}
}

// BLOCKER 2. An order with no report still owes the truth: a run stopped at
// its step limit, or whose answer ran out of output, did not finish, and it was
// announced as landed because the no-report exemption came first.
func TestANoReportRunStoppedAtItsStepLimitDidNotLand(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	model := &scriptedCompleter{steps: []step{readingStep(workspace, "Looked at the first failure."), saying("after the limit")}}
	item := nightly(workspace)
	item.Does.MaxSteps = 1
	outcome, err := standingChildRunner(t, root, model).Run(context.Background(), item, filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != standing.OutcomeFailed || outcome.Withheld != "at-a-limit" || outcome.Text != "the run reached its step or spending limit before it finished" {
		t.Fatalf("outcome %+v, want failed · at-a-limit with no report named", outcome)
	}
}

func TestANoReportRunCutAtTheOutputLimitDidNotLand(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	model := &scriptedCompleter{steps: []step{
		cutAtTheLimit("Three flaky tests: the first"),
		cutAtTheLimit(" the second"),
		cutAtTheLimit(" and the third is"),
	}}
	outcome, err := standingChildRunner(t, root, model).Run(context.Background(), nightly(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != standing.OutcomeFailed || outcome.Withheld != "output-limit" || outcome.Text != "the run's answer was cut off at the model's output limit" {
		t.Fatalf("outcome %+v, want failed · output-limit with no report named", outcome)
	}
}

// And the exemption still says what it always said: owing no report is not a
// failure, so a clean run lands, with no code.
func TestACleanNoReportRunStillLands(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	model := &scriptedCompleter{steps: []step{saying("Three flaky tests, all the same timeout.")}}
	outcome, err := standingChildRunner(t, root, model).Run(context.Background(), nightly(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != "landed" || outcome.Withheld != "" {
		t.Fatalf("outcome %+v, want landed with no code", outcome)
	}
}

// storedReporting is a report watch made in a real store at root, so the stop
// and the last receipt are read where the product reads them.
func storedReporting(t *testing.T, root, workspace string) (*standing.Store, standing.Item) {
	t.Helper()
	store, err := standing.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	item := reporting(workspace)
	item.ID = ""
	made, err := store.Create(item)
	if err != nil {
		t.Fatal(err)
	}
	return store, made
}

// heldAtTheAct runs a report firing while the test holds the item's own lock,
// waits until the run has its report ready to rename — its temporary file is
// in the report's folder — then does between, releases the lock and answers
// the outcome. A writer that takes no lock never waits, and is finished by the
// time the test looks.
func heldAtTheAct(t *testing.T, ctx context.Context, root, workspace string, store *standing.Store, item standing.Item, between func()) standing.Outcome {
	t.Helper()
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(store.ItemPath(item.ID)), item.ID+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, false); err != nil {
		t.Fatal(err)
	}
	model := &scriptedCompleter{steps: []step{saying("<report>\n# Report\n- written before the stop\n</report>")}}
	type result struct {
		outcome standing.Outcome
		err     error
	}
	done := make(chan result, 1)
	go func() {
		outcome, err := standingChildRunner(t, root, model).Run(ctx, item, filepath.Join(store.RunsDir(item.ID), standing.RunName(1)), "")
		done <- result{outcome, err}
	}()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(time.Millisecond) {
		select {
		case finished := <-done:
			_ = filelock.Unlock(lock)
			t.Fatalf("the report was written without waiting for the item's lock: outcome %+v (%v)", finished.outcome, finished.err)
		default:
		}
		if temps, _ := filepath.Glob(filepath.Join(workspace, "reports", ".report-*")); len(temps) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the run never reached its report")
		}
	}
	between()
	_ = filelock.Unlock(lock)
	finished := <-done
	if finished.err != nil {
		t.Fatal(finished.err)
	}
	return finished.outcome
}

// BLOCKER 3, THE STOP IN THE WINDOW. The item's status was read once, a few
// lines before the report was written by a writer that read nothing: a stop
// landing in between still published. The stop is written here while the run
// has its report ready and is waiting at the act.
func TestAStopBetweenTheDecisionAndTheActPublishesNothing(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	store, item := storedReporting(t, root, workspace)
	outcome := heldAtTheAct(t, context.Background(), root, workspace, store, item, func() {
		// The person's stop, as the document says it — written directly,
		// because the lock SetStatus takes is the one this test is holding.
		stopped, err := store.Get(item.ID)
		if err != nil {
			t.Fatal(err)
		}
		stopped.Status = standing.StatusRetired
		raw, _ := json.MarshalIndent(stopped, "", "  ")
		if err := os.WriteFile(store.ItemPath(item.ID), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := os.Stat(filepath.Join(workspace, "reports", "r.md")); !os.IsNotExist(err) {
		t.Fatalf("a stopped item's report was published (outcome %+v)", outcome)
	}
	if outcome.Withheld != "stopped" || !strings.Contains(outcome.Text, "stopped while it ran") {
		t.Fatalf("outcome %+v, want stopped", outcome)
	}
}

// BLOCKER 3, THE CANCELLATION IN THE WINDOW. The pass's context was last read
// where the decision was taken; the writer took none.
func TestACancellationBetweenTheDecisionAndTheActPublishesNothing(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	store, item := storedReporting(t, root, workspace)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	outcome := heldAtTheAct(t, ctx, root, workspace, store, item, cancel)
	if _, err := os.Stat(filepath.Join(workspace, "reports", "r.md")); !os.IsNotExist(err) {
		t.Fatalf("a cancelled pass published (outcome %+v)", outcome)
	}
	if outcome.Kind != standing.OutcomeFailed || outcome.Withheld != "cut-off" || !strings.Contains(outcome.Text, "cut off before its report was published") {
		t.Fatalf("outcome %+v, want failed · cut-off", outcome)
	}
}

// publishedBefore records, in the item's store, that it once published body to
// its report — the receipt a publication is compared against.
func publishedBefore(t *testing.T, store *standing.Store, item standing.Item, body string) {
	t.Helper()
	runDir := filepath.Join(store.RunsDir(item.ID), standing.RunName(1))
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	receipt := &standing.Publication{Path: item.Does.Report, SHA256: sha256Hex(body), Bytes: len(body)}
	if err := standing.WriteOccurrence(runDir, standing.Occurrence{ID: item.ID + "/1", ItemID: item.ID, Phase: standing.PhaseFinished, Outcome: "landed", Published: receipt}); err != nil {
		t.Fatal(err)
	}
}

// F1, THE PERSON'S EDIT. The report is a file in the person's project, and a
// run lasts minutes: somebody annotated it while the run worked, and the
// publication replaced it with no look at what it was replacing. It is not
// written over now; the draft waits beside the run.
func TestAReportThePersonEditedMidRunIsNotWrittenOver(t *testing.T) {
	root, workspace := t.TempDir(), withLastGoodReport(t)
	store, item := storedReporting(t, root, workspace)
	publishedBefore(t, store, item, lastGoodReport)
	const theirs = "# Report\nthe last good one\n\n> my note: call Priya back\n"
	model := &scriptedCompleter{steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		// The person types into the file while the run is working.
		if err := os.WriteFile(filepath.Join(workspace, "reports", "r.md"), []byte(theirs), 0o644); err != nil {
			t.Error(err)
		}
		return saying("<report>\n# Report\n- the run's new page\n</report>")(ctx, nil)
	}}}
	runDir := filepath.Join(store.RunsDir(item.ID), standing.RunName(2))
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outcome, err := standingChildRunner(t, root, model).Run(context.Background(), item, runDir, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(workspace, "reports", "r.md"))
	if string(raw) != theirs || outcome.Published != nil {
		t.Fatalf("the person's edit was written over: %q (outcome %+v)", raw, outcome)
	}
	held, _ := os.ReadFile(filepath.Join(runDir, heldReportFile))
	if outcome.Kind != standing.OutcomeNeedsYou || outcome.Withheld != "report-changed" || string(held) != "# Report\n- the run's new page\n" {
		t.Fatalf("outcome %+v, draft %q; want needs-you · report-changed with the draft held", outcome, held)
	}
}

// AND A FILE THAT WAS THERE BEFORE aforge EVER PUBLISHED is the person's the
// same way: with no receipt behind it, it is not aforge's to replace.
func TestAReportFileAforgeNeverWroteIsNotWrittenOver(t *testing.T) {
	root, workspace := t.TempDir(), withLastGoodReport(t)
	store, item := storedReporting(t, root, workspace)
	model := &scriptedCompleter{steps: []step{saying("<report>\n# Report\n- the first page\n</report>")}}
	runDir := filepath.Join(store.RunsDir(item.ID), standing.RunName(1))
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outcome, err := standingChildRunner(t, root, model).Run(context.Background(), item, runDir, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(workspace, "reports", "r.md"))
	if string(raw) != lastGoodReport || outcome.Withheld != "report-changed" || outcome.Kind != standing.OutcomeNeedsYou {
		t.Fatalf("a file aforge never wrote was replaced: %q (outcome %+v)", raw, outcome)
	}
	// And the one it did write, unchanged since, is replaced as always.
	publishedBefore(t, store, item, lastGoodReport)
	model = &scriptedCompleter{steps: []step{saying("<report>\n# Report\n- the next page\n</report>")}}
	runDir = filepath.Join(store.RunsDir(item.ID), standing.RunName(2))
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outcome, err = standingChildRunner(t, root, model).Run(context.Background(), item, runDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(workspace, "reports", "r.md")); string(raw) != "# Report\n- the next page\n" || outcome.Published == nil {
		t.Fatalf("a report unchanged since its receipt was not replaced: %q (outcome %+v)", raw, outcome)
	}
}

// F2, CONSUME AFTER COMMIT, through the conversation. The fold was taken off
// disk the moment a window opened, and lived only in memory until a turn
// carried it: a window opened and closed with no turn, or a process killed in
// between, lost the news for good. Now the files stay until the fold is in the
// conversation's record, the next open reads them again, and the turn that
// records the fold is what consumes them.
func TestInboxNewsOutlivesAWindowThatNeverRecordedIt(t *testing.T) {
	dir := t.TempDir()
	if err := standing.Deliver(dir, standing.Note{At: time.Now(), ItemID: "aaaaaaaaaaaaaaaa", Words: "keep the inbox report", Kind: "landed", Text: "report updated: reports/r.md"}); err != nil {
		t.Fatal(err)
	}
	inboxFiles := func() []string {
		files, _ := filepath.Glob(filepath.Join(dir, "inbox.jsonl*"))
		return files
	}
	// The first window folds it, and its process dies before any turn.
	newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.Place = Place{Dir: dir} })
	if len(inboxFiles()) == 0 {
		t.Fatal("the inbox was consumed before anything recorded the fold")
	}
	// The next window still has the news.
	model := &scriptedCompleter{steps: []step{saying("Noted.")}}
	next, _ := newTestAgent(t, model, func(config *Config) { config.Place = Place{Dir: dir} })
	next.mu.Lock()
	var queued []string
	for _, message := range next.steering {
		queued = append(queued, messageContentText(message.message))
	}
	next.mu.Unlock()
	if !strings.Contains(strings.Join(queued, "\n"), "report updated: reports/r.md") {
		t.Fatalf("the next window lost the news: queued %q", queued)
	}
	// Its first turn carries the fold into the record, and that consumes it.
	collect(t, mustSubmit(t, next, "what happened overnight?"))
	if files := inboxFiles(); len(files) != 0 {
		t.Fatalf("the inbox outlived the fold's record: %v", files)
	}
}
