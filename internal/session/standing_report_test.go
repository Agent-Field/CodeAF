package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// openFence is a fence with nothing behind it: a live pass and no store.
func openFence() effectFence { return effectFence{ctx: context.Background()} }

func TestAStandingReportIsPublishedInsideItsProjectOnly(t *testing.T) {
	workspace := t.TempDir()
	published, held, err := publishStandingReport(openFence(), workspace, "reports/inbox.md", "  # Report\nall quiet  ", "")
	if err != nil || held != notWithheld {
		t.Fatal(held, err)
	}
	raw, err := os.ReadFile(filepath.Join(workspace, "reports", "inbox.md"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if string(raw) != "# Report\nall quiet\n" || published.Path != "reports/inbox.md" || published.Bytes != len(raw) || published.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("published %+v wrote %q", published, raw)
	}

	// A folder planted as a symlink out of the project must not carry the write.
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workspace, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := publishStandingReport(openFence(), workspace, "escape/report.md", "x", ""); err == nil {
		t.Fatal("a report was written through a symlink outside the project")
	}
	// Nor does it make folders on the far side before refusing.
	if _, _, err := publishStandingReport(openFence(), workspace, "escape/deeper/still/report.md", "x", ""); err == nil {
		t.Fatal("a report was written through a symlinked parent")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("something landed outside the project: %v", entries)
	}
	// And a report path that is itself a symlink is refused rather than followed.
	if err := os.Symlink(filepath.Join(outside, "target.md"), filepath.Join(workspace, "reports", "linked.md")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := publishStandingReport(openFence(), workspace, "reports/linked.md", "x", ""); err == nil {
		t.Fatal("a symlinked report path was followed")
	}
}

func TestAFiringIsToldWhyItRunsAndWhereItsReportGoes(t *testing.T) {
	workspace := t.TempDir()
	item := standing.Item{Workspace: workspace, Does: standing.Action{Kind: standing.ActionTask, Report: "reports/r.md"}}
	block := standingReportBlock(item, "reports/r.md")
	if !strings.Contains(block, "FINAL REPLY is the complete report") || !strings.Contains(block, "between a line <report> and a line </report>") || strings.Contains(block, "previous version is at") {
		t.Fatalf("first report block: %s", block)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "reports", "r.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if block := standingReportBlock(item, "reports/r.md"); !strings.Contains(block, "previous version is at reports/r.md") {
		t.Fatalf("an existing report was not offered: %s", block)
	}
	prior := time.Date(2026, 9, 9, 9, 0, 0, 0, time.UTC)
	text := standingOccurrenceBlock(standing.Occurrence{Spec: 3, Because: "the files you are watching changed", PreviousFired: prior, Attempt: 2})
	for _, want := range []string{"instructions: version 3", "woken because: the files you are watching changed", "previous occurrence: " + prior.Local().Format(time.RFC3339), "attempt 2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("occurrence block lacks %q:\n%s", want, text)
		}
	}
	if first := standingOccurrenceBlock(standing.Occurrence{Spec: 1}); !strings.Contains(first, "none; this is the first") {
		t.Fatalf("a first occurrence claims a previous one:\n%s", first)
	}
}

// reporting is a watch whose final reply is published to reports/r.md.
func reporting(workspace string) standing.Item {
	item := nightly(workspace)
	item.When = standing.When{Kind: standing.WhenFile, Glob: "inbox/*", Words: "when inbox/* changes"}
	item.Does.Report = "reports/r.md"
	return item
}

// THE REPORT IS THE ANSWER, NOT THE NARRATION BEFORE THE READING. The words a
// model says before it calls a tool are about what it is about to do; only the
// last turn's words after its last tool call are published.
func TestAReportIsTheFinalAnswerNotTheNarration(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "inbox", "a.md"), []byte("Decision: ship Friday\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"path": "inbox/a.md"})
	model := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "Let me read the inbox first.")
			return toolResponseWithText("call_1", "read", string(args), "Let me read the inbox first."), nil
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "# Report\n- Decision: ship Friday")
			return textResponse("# Report\n- Decision: ship Friday"), nil
		},
	}}
	outcome, err := standingChildRunner(t, root, model).Run(context.Background(), reporting(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(workspace, "reports", "r.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "# Report\n- Decision: ship Friday\n" || outcome.Kind != "landed" || outcome.Published == nil || !strings.HasPrefix(outcome.Text, "report updated: reports/r.md") {
		t.Fatalf("published %q outcome %+v", raw, outcome)
	}
	// No rules reached this work, so nothing was checked and nothing was paid.
	if len(model.asides) != 0 {
		t.Fatalf("a report with no rules on its work was checked: %d", len(model.asides))
	}
}

// A RUN CUT OFF MID-ANSWER DID NOT LAND, AND THE LAST GOOD REPORT STAYS. The
// live journey found this: a pass deadline arrived while the final answer was
// streaming, and half a report was published as landed.
func TestARunCutOffMidAnswerPublishesNothing(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	previous := "# Report\nthe last good one\n"
	if err := os.WriteFile(filepath.Join(workspace, "reports", "r.md"), []byte(previous), 0o644); err != nil {
		t.Fatal(err)
	}
	model := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "# Report\n- Decision: ship Fri")
			return nil, context.DeadlineExceeded
		},
	}}
	outcome, err := standingChildRunner(t, root, model).Run(context.Background(), reporting(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(workspace, "reports", "r.md"))
	if outcome.Kind != standing.OutcomeFailed || outcome.Published != nil || string(raw) != previous || !strings.Contains(outcome.Text, "cut off") {
		t.Fatalf("a cut-off run: outcome %+v report %q", outcome, raw)
	}
}

// ── the rules check before a report is published ────────────────────────────

// rulesReader is a governing reader that answers the same rules for any work.
type rulesReader []standing.Item

func (r rulesReader) ApplicableScope(string, string, map[string]int) ([]standing.Item, error) {
	return r, nil
}

// privacyRule is the rule the live journey's first report broke.
var privacyRule = standing.Item{
	ID:     "5777cd193915f9d2",
	Words:  "Inbox reports never quote email addresses; write [redacted] instead.",
	When:   standing.When{Kind: standing.WhenHold},
	Status: standing.StatusActive,
}

// ruledRunner is the child runner with rules reaching its work, and the check
// answered off the queue by shape, from verdicts in order.
func ruledRunner(t *testing.T, root string, model *scriptedCompleter, verdicts ...string) (*standingRunner, *[]string) {
	t.Helper()
	var questions []string
	model.aside = func(messages []ai.Message) (*ai.Response, bool) {
		if len(messages) == 0 || messageText(messages[0]) != standingRulesPrompt {
			return nil, false
		}
		questions = append(questions, messageText(messages[len(messages)-1]))
		if len(verdicts) == 0 {
			t.Error("the report was checked more often than scripted")
			return textResponse(keptVerdict), true
		}
		verdict := verdicts[0]
		verdicts = verdicts[1:]
		return textResponse(verdict), true
	}
	runner := standingChildRunner(t, root, model)
	runner.parent.Governing = &Governing{Reader: rulesReader{privacyRule}}
	return runner, &questions
}

// admittedRun makes a run folder with the record the pass writes before a run.
func admittedRun(t *testing.T, root string) string {
	t.Helper()
	runDir := filepath.Join(root, "runs", "0001")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := standing.WriteOccurrence(runDir, standing.Occurrence{ID: "x/0001", Spec: 1, Attempt: 1, Phase: standing.PhaseAdmitted}); err != nil {
		t.Fatal(err)
	}
	return runDir
}

func saying(text string) step {
	return func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		provider.Emit(ctx, provider.StreamDelta, text)
		return textResponse(text), nil
	}
}

// The check's two answers about [privacyRule], one rule of one kind.
const (
	keptVerdict   = `{"rules": [{"rule": 1, "kind": "prohibition", "verdict": "kept"}]}`
	brokenVerdict = `{"rules": [{"rule": 1, "kind": "prohibition", "verdict": "broken", "quote": "alice@example.com", "why": "it quotes an email address"}]}`
)

// A REPORT THAT BREAKS A RULE IS SENT BACK ONCE WITH THE EVIDENCE, and the
// corrected report is what is published. The correction names the rule and
// quotes the words; it does not say how to fix them.
func TestAReportThatBreaksARuleIsCorrectedOnceBeforeItIsPublished(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	var corrections []string
	model := &scriptedCompleter{steps: []step{
		saying("# Report\n- Ask alice@example.com to confirm the venue."),
		func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			corrections = append(corrections, messageText(messages[len(messages)-1]))
			provider.Emit(ctx, provider.StreamDelta, "# Report\n- Ask [redacted] to confirm the venue.")
			return textResponse("# Report\n- Ask [redacted] to confirm the venue."), nil
		},
	}}
	runner, questions := ruledRunner(t, root, model, brokenVerdict, keptVerdict)
	runDir := admittedRun(t, root)
	outcome, err := runner.Run(context.Background(), reporting(workspace), runDir, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(workspace, "reports", "r.md"))
	if string(raw) != "# Report\n- Ask [redacted] to confirm the venue.\n" || outcome.Kind != "landed" || outcome.Published == nil {
		t.Fatalf("published %q outcome %+v", raw, outcome)
	}
	if len(*questions) != 2 || !strings.Contains((*questions)[0], privacyRule.Words) || !strings.Contains((*questions)[0], "alice@example.com") {
		t.Fatalf("the check was not shown the rule and the report: %q", *questions)
	}
	if len(corrections) != 1 || !strings.Contains(corrections[0], "alice@example.com") || !strings.Contains(corrections[0], "Inbox reports never quote email addresses") {
		t.Fatalf("the correction did not carry the finding: %q", corrections)
	}
	record, _ := standing.ReadOccurrence(runDir)
	if record.RuleCheck == nil || record.RuleCheck.Verdict != "kept" || !record.RuleCheck.Rewrote || !strings.Contains(record.RuleCheck.First, "alice@example.com") ||
		len(record.RuleCheck.Rules) != 1 || record.RuleCheck.Rules[0] != privacyRule.ID || record.Published == nil {
		t.Fatalf("the check was not recorded: %+v", record.RuleCheck)
	}
}

// A REPORT THAT STILL BREAKS A RULE IS HELD BACK. One correction, then the
// person: the previous report stays, the draft is kept in the run folder, and
// the run ends waiting on them with the finding as its line.
func TestAReportThatStillBreaksARuleIsHeldBackAndThePreviousOneStays(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	previous := "# Report\nthe last good one\n"
	if err := os.MkdirAll(filepath.Join(workspace, "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "reports", "r.md"), []byte(previous), 0o644); err != nil {
		t.Fatal(err)
	}
	model := &scriptedCompleter{steps: []step{
		saying("# Report\n- Ask alice@example.com to confirm."),
		saying("# Report\n- Ask alice@example.com [redacted] to confirm."),
	}}
	runner, _ := ruledRunner(t, root, model, brokenVerdict, brokenVerdict)
	runDir := admittedRun(t, root)
	outcome, err := runner.Run(context.Background(), reporting(workspace), runDir, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(workspace, "reports", "r.md"))
	if string(raw) != previous || outcome.Published != nil || outcome.Kind != standing.OutcomeNeedsYou || !strings.Contains(outcome.NeedsPerson, "held back") || outcome.Withheld != "held-by-rules" {
		t.Fatalf("a still-broken report: outcome %+v report %q", outcome, raw)
	}
	held, err := os.ReadFile(filepath.Join(runDir, heldReportFile))
	if err != nil || !strings.Contains(string(held), "alice@example.com [redacted]") {
		t.Fatalf("the draft was not kept: %q %v", held, err)
	}
	record, _ := standing.ReadOccurrence(runDir)
	if record.RuleCheck == nil || record.RuleCheck.Verdict != "broken" || record.RuleCheck.Quote != "alice@example.com" || record.RuleCheck.Held == "" || record.Published != nil {
		t.Fatalf("the hold was not recorded: %+v", record.RuleCheck)
	}
}

// A FINDING MUST QUOTE THE REPORT. A breach quoted from words the report does
// not contain is no finding; the check is asked once more, and its answer
// stands. The run itself is not sent back on a finding nobody could point to.
func TestAFindingThatDoesNotQuoteTheReportIsNotAFinding(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	model := &scriptedCompleter{steps: []step{saying("# Report\nall quiet")}}
	invented := `{"rules": [{"rule": 1, "kind": "prohibition", "verdict": "broken", "quote": "bob@example.com", "why": "an address"}]}`
	runner, questions := ruledRunner(t, root, model, invented, keptVerdict)
	outcome, err := runner.Run(context.Background(), reporting(workspace), admittedRun(t, root), "")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != "landed" || outcome.Published == nil || len(*questions) != 2 || len(model.seen) != 1 {
		t.Fatalf("outcome %+v checks %d turns %d", outcome, len(*questions), len(model.seen))
	}
}

// A CHECK THAT NEVER ANSWERS HOLDS THE REPORT BACK. Failing open would publish
// exactly the report the check exists to stop whenever the check is down.
func TestAReportWhoseCheckGaveNoAnswerIsHeldBack(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	model := &scriptedCompleter{steps: []step{saying("# Report\nall quiet")}}
	runner, _ := ruledRunner(t, root, model, "I think it is fine.", "Looks good to me!")
	outcome, err := runner.Run(context.Background(), reporting(workspace), admittedRun(t, root), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, "reports", "r.md")); outcome.Published != nil || statErr == nil || !strings.Contains(outcome.NeedsPerson, "gave no answer") {
		t.Fatalf("an unanswered check published: %+v", outcome)
	}
}

// STOPPED WHILE IT RAN: the run's own work is not undone, but aforge's last two
// acts — the report and the note — are not made for an item the person has
// stopped. A PAUSE IS NOT A STOP: work admitted before the pause finishes as it
// was admitted, report and all.
func TestAStopWhileARunIsWorkingWithholdsItsReportButAPauseDoesNot(t *testing.T) {
	for _, c := range []struct {
		status    standing.Status
		published bool
	}{{standing.StatusRetired, false}, {standing.StatusPaused, true}} {
		t.Run(string(c.status), func(t *testing.T) {
			root, workspace := t.TempDir(), t.TempDir()
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
			model := &scriptedCompleter{steps: []step{
				func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
					if _, err := store.SetStatus(made.ID, c.status, standing.StoppedWhy); err != nil {
						t.Error(err)
					}
					provider.Emit(ctx, provider.StreamDelta, "# Report\nall quiet")
					return textResponse("# Report\nall quiet"), nil
				},
			}}
			outcome, err := standingChildRunner(t, root, model).Run(context.Background(), made, filepath.Join(root, "runs", "0001"), "")
			if err != nil {
				t.Fatal(err)
			}
			_, statErr := os.Stat(filepath.Join(workspace, "reports", "r.md"))
			if (statErr == nil) != c.published || (outcome.Published != nil) != c.published || (c.published && outcome.Withheld != "") {
				t.Fatalf("%s mid-run: published=%v outcome %+v", c.status, statErr == nil, outcome)
			}
			if !c.published && (outcome.Kind != standing.OutcomeNothing || !strings.Contains(outcome.Text, "stopped while it ran") || outcome.Withheld != "stopped") {
				t.Fatalf("a stopped run's outcome: %+v", outcome)
			}
		})
	}
}

// THE REPORT IS WHAT IS BETWEEN ITS LINES. The live journey's reports opened
// with the model's own sentence on the way to writing them, in the same turn;
// a delimited report is published without it. A report opened and never closed
// is no report at all: its end is wherever the run stopped writing. Nor is one
// closed and never opened, whose start nobody can tell; and an empty one is
// read as the empty body it is, for the decision to refuse. A delimiter is a
// whole line, outside any fenced code block.
func TestAReportIsWhatIsBetweenItsLinesAndNotTheSentenceBeforeIt(t *testing.T) {
	for text, want := range map[string]struct {
		body  string
		lines reportLines
	}{
		"Now let me check the standing order.\n<report>\n# Report\n- ship Friday\n</report>\nDone.": {"# Report\n- ship Friday", closedReport},
		"<report>\n# Report\n- cut before the close":                                                {"", unclosedReport},
		"# Report\nno delimiter at all":                                                             {"", noReportLines},
		"a draft\n<report>\nfirst\n</report>\nthen again\n<report>\nsecond\n</report>":              {"second", closedReport},
		"<report>\nfirst\n</report>\na second try\n<report>\nhalf of it":                            {"", unclosedReport},
		// A closing tag glued to other words is no closing line, so this report
		// reads as unclosed (no model in the live runs wrote one that way).
		"<report>\n# Report\n- x</report>I cannot write the file myself.": {"", unclosedReport},
		// A leading byte-order mark is not part of the first line.
		"\uFEFF<report>\nvalid body\n</report>": {"valid body", closedReport},
		// A sentence that mentions the closing tag closes nothing and opens
		// nothing: the answer has no report lines at all.
		"Close a report with `</report>` on its own line.": {"", noReportLines},
		// Tags inside a fenced code block are the report's content.
		"<report>\n# R\n```\n</report>\n```\nafter\n</report>":                    {"# R\n```\n</report>\n```\nafter", closedReport},
		"<report>\nA\n</report>\n~~~\n<report>\n~~~":                              {"A", closedReport},
		"I will put it between <report> and </report>.\n<report>\n# R\n</report>": {"# R", closedReport},
		"<report>\n</report>":                   {"", closedReport},
		"<report>\n  \n</report>":               {"", closedReport},
		"Here it is.\n# Report\n- x\n</report>": {"", unopenedReport},
	} {
		if body, lines := delimitedReport(text); body != want.body || lines != want.lines {
			t.Errorf("delimitedReport(%q) = %q, %d; want %q, %d", text, body, lines, want.body, want.lines)
		}
	}
	root, workspace := t.TempDir(), t.TempDir()
	model := &scriptedCompleter{steps: []step{saying("Now let me check the standing order — it matches.\n<report>\n# Review\n- claim unsupported\n</report>")}}
	outcome, err := standingChildRunner(t, root, model).Run(context.Background(), reporting(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(workspace, "reports", "r.md"))
	if string(raw) != "# Review\n- claim unsupported\n" || outcome.Published == nil {
		t.Fatalf("published %q outcome %+v", raw, outcome)
	}
}

// A RUN THAT WROTE ITS REPORT AND THEN TRIED TO SAVE IT ITSELF is not waiting
// on the person. The live review of 2026-09-10 did exactly this: the report
// between its lines, then a `write` of the report path, refused as every
// unattended write is, then an apology. The write stays refused; the report it
// wrote is the one published, not the apology after it.
func TestARefusedWriteOfTheRunsOwnReportIsNotAQuestionForThePerson(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	body := "# Review\n- \"Works offline\" is unsupported: `Offline support: removed in this release.`"
	args, _ := json.Marshal(map[string]string{"path": filepath.Join(workspace, "reports", "r.md"), "content": "the model's own copy"})
	model := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			text := "Now the report.\n<report>\n" + body + "\n</report>\nNow I'll write it to the file."
			provider.Emit(ctx, provider.StreamDelta, text)
			return toolResponseWithText("call_w", "write", string(args), text), nil
		},
		saying("I cannot write the file; someone with permission needs to place it."),
	}}
	asking := &approval.Policy{Default: approval.ActionPrompt}
	runner := standingChildRunner(t, root, model)
	runner.parent.ApprovalPolicy = asking
	outcome, err := runner.Run(context.Background(), reporting(workspace), filepath.Join(root, "runs", "0001"), "")
	if err != nil {
		t.Fatal(err)
	}
	// Every write asks under that policy and nobody is there, so `write` is not
	// on the run's belt at all (standing_grant_test.go): the call is answered as
	// a tool it does not have, and the file is never the model's own copy.
	if calls := model.seen; len(calls) < 2 || !strings.Contains(messageText(calls[1][len(calls[1])-1]), "Unknown tool: write") {
		t.Fatalf("the write was not answered as a tool the run does not have: %d requests", len(calls))
	}
	raw, _ := os.ReadFile(filepath.Join(workspace, "reports", "r.md"))
	if string(raw) != body+"\n" || outcome.Kind != "landed" || outcome.NeedsPerson != "" || outcome.Published == nil {
		t.Fatalf("published %q outcome %+v", raw, outcome)
	}
	// A write of ANY OTHER file is the same absent tool, not a question for the
	// person: nothing anybody could allow is waiting. (A granted tool meeting a
	// real boundary is still one — TestAGrantedToolMeetingItsBoundaryStillWaitsOnThePerson.)
	workspace = t.TempDir()
	other, _ := json.Marshal(map[string]string{"path": filepath.Join(workspace, "notes.md"), "content": "x"})
	model = &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			return toolResponseWithText("call_o", "write", string(other), ""), nil
		},
		saying("<report>\n# Report\n</report>"),
	}}
	runner = standingChildRunner(t, root, model)
	runner.parent.ApprovalPolicy = asking
	outcome, err = runner.Run(context.Background(), reporting(workspace), filepath.Join(root, "runs", "0002"), "")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != "landed" || outcome.NeedsPerson != "" || outcome.Published == nil {
		t.Fatalf("an absent write of another file held the report: %+v", outcome)
	}
	if _, err := os.Stat(filepath.Join(workspace, "notes.md")); !os.IsNotExist(err) {
		t.Fatalf("the absent write ran: %v", err)
	}
}
