package head

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// seedSubFloorGraph builds the shape the bug needs: several live jobs sharing
// one stray word with the sentence, so common between them that BM25 scores
// every match under the anchor floor, plus one job that shares nothing and is
// therefore absent from the ranked list entirely. That last job is the one the
// old code could never offer and the user was most likely to have meant.
func seedSubFloorGraph(t *testing.T, graph *store.Store) {
	t.Helper()
	for index := 0; index < 7; index++ {
		spliceSurgeryJob(t, graph, fmt.Sprintf("art-%d", index),
			fmt.Sprintf("Artwork %d", index), fmt.Sprintf("draw artwork %d differently", index))
	}
	spliceSurgeryJob(t, graph, "ledger", "Ledger reconciliation", "reconcile the quarterly ledger")
}

// TestSubFloorMatchesNeverBecomeTheOffer is L21. In the branch where nothing
// cleared the anchor floor, the disambiguation offered the ranked list — which
// is not a shortlist of what the user might have meant but the set of jobs whose
// briefs happen to share a word with the sentence, the exact coincidence the
// floor exists to reject. "Actually do the job differently" offered only the
// jobs that matched on a coincidence while the user's other live work, the thing
// they were most likely talking about, was never shown at all.
func TestSubFloorMatchesNeverBecomeTheOffer(t *testing.T) {
	graph := openHeadStore(t)
	seedSubFloorGraph(t, graph)

	head := New(nil, graph)
	user := store.Message{SessionID: "floor", Body: "actually do the job differently"}
	active, err := head.activeUserJobs()
	if err != nil {
		t.Fatal(err)
	}
	ranked, err := head.rankRedirectTargets(user.Body, active)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range ranked {
		if target.Score >= RedirectAnchorScore {
			t.Fatalf("the fixture cleared the floor, so the branch is untested: %s at %.4f",
				target.Node.ID, target.Score)
		}
		if target.Node.ID == "ledger" {
			t.Fatal("the fixture's unmatched job is in the ranked list, so nothing is hidden")
		}
	}

	intent, redirecting, err := head.recognizeRedirect(user)
	if err != nil || !redirecting {
		t.Fatalf("the sentence was not read as a redirection: redirecting=%t err=%v", redirecting, err)
	}
	if intent.Certain {
		t.Fatalf("a sub-floor match was treated as certain: %+v", intent.Candidates)
	}
	if !intent.Floorless {
		t.Fatal("the question was assembled from sub-floor evidence without saying so")
	}
	offered := map[string]bool{}
	for _, candidate := range intent.Candidates {
		offered[candidate.Node.ID] = true
	}
	if !offered["ledger"] {
		t.Fatalf("the user's own live work stayed hidden behind coincidences: offered %v", offered)
	}
}

// And the silent shortcut is closed. askRedirectTarget may assume the default
// when the default is evidence; with nothing above the floor there is no
// evidence, and steering a plan on the first row of an arbitrary list is
// precisely the wrong edit, made without ever showing it.
func TestNothingAboveTheFloorIsEverSilentlySteered(t *testing.T) {
	graph := openHeadStore(t)
	seedSubFloorGraph(t, graph)
	// Drive the category past its asking threshold, so the shortcut is live.
	for index := 0; index < 12; index++ {
		if err := graph.RecordAssumedWithDefault(store.QuestionCategoryRedirectTarget, "1",
			"floor", "earlier question"); err != nil {
			t.Fatal(err)
		}
	}

	head := New(nil, graph)
	user := postUser(t, graph, "floor", "actually do the job differently")
	handled, err := head.manageRedirect(user)
	if err != nil || !handled {
		t.Fatalf("redirect not handled: handled=%t err=%v", handled, err)
	}
	commands, err := graph.PendingCommands(50)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range commands {
		if command.Kind == store.CommandRedirect {
			t.Fatalf("a plan was steered on a sub-floor coincidence: %+v", command)
		}
	}
	questions, err := graph.UnresolvedQuestions(10)
	if err != nil || len(questions) != 1 {
		t.Fatalf("the user was never asked: %+v err=%v", questions, err)
	}
}

// TestReminderActionKeepsTheWholeMessage is the logic report's L7 sibling. The
// action split on the LAST " to ", so "remind me to submit the report to
// finance" promised to say "finance" — the reminder destroyed by the parse
// meant to extract it. The message begins after the cue and runs to the end,
// including every "to" inside it.
func TestReminderActionKeepsTheWholeMessage(t *testing.T) {
	for _, test := range []struct{ instruction, want string }{
		{"remind me to submit the report to finance", "Say: submit the report to finance"},
		{"remind me tomorrow to call Mom", "Say: call Mom"},
		{"notify me at 9 to send the invoice to accounting and to legal",
			"Say: send the invoice to accounting and to legal"},
		{"alert me to move the deploy to Friday", "Say: move the deploy to Friday"},
		// No " to " after the cue at all: the whole sentence is the reminder.
		{"remind me about standup", "Say this reminder: remind me about standup"},
	} {
		if got := reminderAction(test.instruction); got != test.want {
			t.Errorf("reminderAction(%q) = %q, want %q", test.instruction, got, test.want)
		}
	}
}

// TestRailConsentCoversTheStepTheQuestionQuoted is L19's head half. The posted
// question names an item the journal has not seen — "and the next step costs
// $15.00" — and promises "I'll continue"; consent then recomputed the raise from
// journaled spend alone, so the new ceiling landed under the very step the
// sentence had quoted and the work stopped again at the same place with the user
// having already said yes.
func TestRailConsentCoversTheStepTheQuestionQuoted(t *testing.T) {
	graph := openHeadStore(t)
	const base = 20.0
	// Journaled spend puts the rail at its ceiling, and the pending item is far
	// larger than one budget unit — which is the whole failure.
	if err := graph.RecordUsage(store.NodeUsage{NodeID: store.RootID, Cost: 19.50}); err != nil {
		t.Fatal(err)
	}
	rail, paused, err := graph.PauseDailyRailWithAdditionalSpend(base, "rail", 45)
	if err != nil || !paused {
		t.Fatalf("the rail never paused: paused=%t rail=%+v err=%v", paused, rail, err)
	}
	posted, err := graph.Messages("rail", 0, 10)
	if err != nil || len(posted) != 1 ||
		!strings.Contains(posted[0].Body, "the next step costs $45.00") {
		t.Fatalf("the question did not quote the pending step: %+v err=%v", posted, err)
	}

	head := New(&fakeClient{}, graph).WithDailyBudgetUSD(base)
	user := postUser(t, graph, "rail", "yes")
	raised, err := head.raiseRailFromReply(user)
	if err != nil || !raised {
		t.Fatalf("consent was not intercepted: raised=%t err=%v", raised, err)
	}

	updated, err := graph.DailyRailToday(base)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Ceiling <= 19.50+45 {
		t.Fatalf("consent raised to $%.2f, under the $%.2f the question promised to continue past",
			updated.Ceiling, 19.50+45)
	}
	if updated.Reached {
		t.Fatal(`the rail is still reached after the user said "yes" to continuing`)
	}
}

// A question with nothing pending raises exactly one budget unit, as it always
// has. The parse fails closed, so a reworded question costs today's behaviour
// rather than a wrong number.
func TestRailConsentWithNothingPendingIsUnchanged(t *testing.T) {
	graph := openHeadStore(t)
	const base = 20.0
	if err := graph.RecordUsage(store.NodeUsage{NodeID: store.RootID, Cost: 20.50}); err != nil {
		t.Fatal(err)
	}
	if _, paused, err := graph.PauseDailyRail(base, "plain"); err != nil || !paused {
		t.Fatalf("the rail never paused: paused=%t err=%v", paused, err)
	}
	head := New(&fakeClient{}, graph).WithDailyBudgetUSD(base)
	user := postUser(t, graph, "plain", "yes")
	if raised, err := head.raiseRailFromReply(user); err != nil || !raised {
		t.Fatalf("consent was not intercepted: raised=%t err=%v", raised, err)
	}
	updated, err := graph.DailyRailToday(base)
	if err != nil {
		t.Fatal(err)
	}
	// base + the overshoot the rail already covers, and not a cent more.
	if want := 20.0 + 20.50; updated.Ceiling != want {
		t.Fatalf("plain consent raised to $%.2f, want $%.2f", updated.Ceiling, want)
	}
}

// TestCompilerRefusesToDuplicateWorkAlreadyUnderway is #21's compiler half. The
// prompt's builds_on rule is about continuation; nothing told the compiler that
// a live job already covering the ask should be waited for rather than copied,
// so "do the webgpu scan" while a webgpu scan ran compiled a second one and the
// user paid twice.
func TestCompilerRefusesToDuplicateWorkAlreadyUnderway(t *testing.T) {
	for _, required := range []string{
		"already covers what is being asked for",
		"A duplicate is paid for twice and answers once",
	} {
		if !strings.Contains(compilerSystemPrompt, required) {
			t.Errorf("the compiler prompt lost its duplicate-work rule: %q", required)
		}
	}
	// The rule is values-based rather than a vocabulary, so it survives a
	// phrasing nobody wrote down.
	client := &fakeClient{responses: []string{
		`{"goal":"Wait for job-1, which is already scanning webgpu, and report what it finds.","assumptions":["job-1 already covers this"],"builds_on":["job-1"]}`,
	}}
	brief, err := NewCompiler(client).Compile(context.Background(), "do the webgpu scan",
		"- job-1 | running | scan the codebase for webgpu usage")
	if err != nil {
		t.Fatal(err)
	}
	if len(brief.BuildsOn) != 1 || brief.BuildsOn[0] != "job-1" {
		t.Fatalf("the brief did not follow the running job: %+v", brief.BuildsOn)
	}
}
