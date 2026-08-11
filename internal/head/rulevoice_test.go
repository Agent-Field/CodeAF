package head

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// activateReminder stands up one say-only weekly rule, the one the everyday
// simulation's user set on Monday morning.
func activateReminder(t *testing.T, graph *store.Store, id, invariant, cadence, says string) store.Charter {
	t.Helper()
	watch := store.CadenceWatchSpec(store.WatchCron, cadence, "", invariant, time.Now())
	charter, err := graph.DraftCharter(id, "rules", 0, store.CharterSpec{
		Invariant: invariant,
		Watch:     store.CharterWatch{Kind: store.WatchCron, Cadence: cadence, Spec: watch},
		Sentinel:  "Is it time for the reminder?", Action: "Say: " + says, SayOnly: true,
		Rails: store.CharterSpecRails{
			EstimatedCostUSD: 0.02, MaxPerDay: 1,
			MaxPerDayJustification: "one a day is all a reminder needs", Expiry: "never",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.SetCharterStatus(id, store.CharterActive, store.Ratification{
		Origin: store.OriginUser, SessionID: "rules", Evidence: "yes, stand this up",
	}); err != nil {
		t.Fatal(err)
	}
	return charter
}

// TestChangeItToTuesdayReachesTheRule is the everyday simulation's Wednesday
// 08:50. "change it to tuesday" was declined by charter management (no cadence
// pattern knew a weekday), claimed by node surgery, resolved to an empty
// reference, and answered with eight rows of work — from which standing rules
// are explicitly excluded. There was no sentence that moved a reminder to
// another day.
//
// The reading that knows better is now evidence rather than a verdict: it says
// the sentence edits a standing rule and what it reads the new rhythm as, above
// the message, and the rule tool is what journals it. A live job on the board
// must still not be the thing that gets changed.
func TestChangeItToTuesdayReachesTheRule(t *testing.T) {
	graph := openHeadStore(t)
	charter := activateReminder(t, graph, "plants",
		"remind me every sunday to water the plants", "every sunday", "water the plants")
	// A live job is on the board: surgery must not be the arm that claims this.
	spliceSurgeryJob(t, graph, "lisbon", "Lisbon trip research", "look into flights and hotels")

	user := postUser(t, graph, "rules", "change it to tuesday")
	reading := deterministicReading(t, New(nil, graph), user)
	if !strings.Contains(reading, "reads as an edit to a STANDING RULE (cadence)") {
		t.Fatalf("the rule reading never reached the loop:\n%s", reading)
	}

	head, _ := beltHead(graph,
		beltTurn{calls: []ai.ToolCall{beltCall("c1", beltToolRule, map[string]any{
			"verb": "cadence", "words": "tuesday"})}},
		beltTurn{text: "The plant reminder moves to Tuesdays."})
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandCharterCadence ||
		commands[0].Target != charter.ID || commands[0].Instruction != "tuesday" {
		t.Fatalf("commands = %+v err=%v", commands, err)
	}
	if err := resident.New(graph, nil, nil).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	updated, found, err := graph.Charter(charter.ID)
	if err != nil || !found {
		t.Fatalf("charter = %+v found=%t err=%v", updated, found, err)
	}
	if updated.Watch.Cron == nil || updated.Watch.Cron.Kind != store.CronWeekly ||
		updated.Watch.Cron.Weekday != time.Tuesday {
		t.Fatalf("retimed watch = %+v, want a weekly Tuesday rule", updated.Watch)
	}
	if updated.NextDue.Weekday() != time.Tuesday || !updated.NextDue.After(time.Now()) {
		t.Fatalf("next due = %s, want the coming Tuesday", updated.NextDue)
	}
	// One visible reply, and the change is journaled rather than written into
	// a row: it survives a rebuild from the events alone.
	reply := waitForAgentReply(t, graph, "rules", user.Seq)
	if strings.TrimSpace(reply.Body) == "" {
		t.Fatal("re-timing a rule said nothing")
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	replayed, _, err := graph.Charter(charter.ID)
	if err != nil || replayed.Watch.Cron == nil || replayed.Watch.Cron.Weekday != time.Tuesday {
		t.Fatalf("replayed charter = %+v err=%v", replayed.Watch, err)
	}
}

// TestPushTheReminderToEightPmReachesTheRule is the second sentence of the same
// friction: a verb the cue list never had, and a clock instead of a day.
func TestPushTheReminderToEightPmReachesTheRule(t *testing.T) {
	graph := openHeadStore(t)
	charter := activateReminder(t, graph, "plants",
		"remind me every sunday to water the plants", "every sunday", "water the plants")

	user := postUser(t, graph, "rules", "push the reminder to 8pm")
	head, _ := beltHead(graph,
		beltTurn{calls: []ai.ToolCall{beltCall("c1", beltToolRule, map[string]any{
			"verb": "cadence", "describes": "the reminder", "words": "8pm"})}},
		beltTurn{text: "It moves to 8pm."})
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandCharterCadence ||
		commands[0].Target != charter.ID {
		t.Fatalf("commands = %+v err=%v", commands, err)
	}
	if err := resident.New(graph, nil, nil).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	updated, _, err := graph.Charter(charter.ID)
	if err != nil || updated.Watch.Cron == nil || updated.Watch.Cron.Hour != 20 {
		t.Fatalf("retimed watch = %+v err=%v, want 20:00", updated.Watch, err)
	}
}

// TestRewordingARuleIsSayable covers the other verb a rule needs: changing what
// it says without touching when it runs.
func TestRewordingARuleIsSayable(t *testing.T) {
	graph := openHeadStore(t)
	charter := activateReminder(t, graph, "plants",
		"remind me every sunday to water the plants", "every sunday", "water the plants")

	user := postUser(t, graph, "rules",
		"change the plant reminder to say water the plants and take the bins out")
	// The wording cue is read before the rhythm: the sentence names a day and is
	// not about the day, and the reading says which of the two it is.
	reading := deterministicReading(t, New(nil, graph), user)
	if !strings.Contains(reading, "reads as an edit to a STANDING RULE (wording)") {
		t.Fatalf("a rewording was read as a retiming:\n%s", reading)
	}

	head, _ := beltHead(graph,
		beltTurn{calls: []ai.ToolCall{beltCall("c1", beltToolRule, map[string]any{
			"verb": "wording", "describes": "the plant reminder",
			"words": "water the plants and take the bins out"})}},
		beltTurn{text: "It will say that from now on."})
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandCharterWording ||
		commands[0].Target != charter.ID ||
		commands[0].Instruction != "water the plants and take the bins out" {
		t.Fatalf("commands = %+v err=%v", commands, err)
	}
	if err := resident.New(graph, nil, nil).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	updated, _, err := graph.Charter(charter.ID)
	if err != nil || updated.Action.Template != "water the plants and take the bins out" {
		t.Fatalf("reworded action = %+v err=%v", updated.Action, err)
	}
	// The rhythm is untouched, and so is the sentence she consented to.
	if updated.Watch.Cron == nil || updated.Watch.Cron.Weekday != time.Sunday {
		t.Fatalf("rewording moved the schedule: %+v", updated.Watch)
	}
	if updated.Invariant != charter.Invariant {
		t.Fatalf("rewording rewrote the ratified sentence: %q", updated.Invariant)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	replayed, _, err := graph.Charter(charter.ID)
	if err != nil || replayed.Action.Template != "water the plants and take the bins out" {
		t.Fatalf("replayed action = %+v err=%v", replayed.Action, err)
	}
}

// TestAnUndescribedRuleReferenceAsksOnePlainQuestion is the ambiguity rule from
// the design filter: never pick for the user, never a picker, one short
// question in plain words. The tool refuses to choose and hands back both rules
// in the words they were ratified in; the question is the ask tool's, and the
// day survives the round trip because the answer comes back to the loop with
// the question and the choice both in front of it.
func TestAnUndescribedRuleReferenceAsksOnePlainQuestion(t *testing.T) {
	graph := openHeadStore(t)
	activateReminder(t, graph, "plants",
		"remind me every sunday to water the plants", "every sunday", "water the plants")
	activateReminder(t, graph, "bins",
		"remind me every friday to put the bins out", "every friday", "put the bins out")

	user := postUser(t, graph, "rules", "change it to tuesday")
	head, client := beltHead(graph,
		beltTurn{calls: []ai.ToolCall{beltCall("c1", beltToolRule, map[string]any{
			"verb": "cadence", "words": "tuesday"})}},
		beltTurn{calls: []ai.ToolCall{beltCall("c2", beltToolAsk, map[string]any{
			"question": "Which rule do you mean?",
			"options": []string{"remind me every sunday to water the plants",
				"remind me every friday to put the bins out"}})}})
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	reply := waitForAgentReply(t, graph, "rules", user.Seq)
	if !strings.HasPrefix(reply.Body, "Which rule do you mean?") || len(reply.Options) != 2 {
		t.Fatalf("askback = %+v", reply)
	}
	for _, option := range reply.Options {
		if !strings.Contains(option.Label, "remind me every") {
			t.Fatalf("option is not named by its plain description: %+v", option)
		}
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 0 {
		t.Fatalf("an ambiguous reference changed something: %+v err=%v", commands, err)
	}
	// The words the model asked with are the rules' own, because that is what the
	// tool handed it rather than a pair of ids.
	offered := &beltRun{head: New(nil, graph), user: user}
	candidates, failed := offered.execute(beltToolRule, beltArguments(t, map[string]any{
		"verb": "cadence", "words": "tuesday"}))
	if failed || !strings.Contains(candidates, "water the plants") ||
		!strings.Contains(candidates, "put the bins out") {
		t.Fatalf("the candidate list is not both rules by name:\n%s", candidates)
	}
	if offered.acted {
		t.Fatal("offering candidates journaled something")
	}

	// Answering the question by number carries the day through to the rule: the
	// choice is not applied by the question machinery, it comes back as an
	// ordinary turn with both halves of the exchange in the prompt.
	client.turns = append(client.turns, beltTurn{calls: []ai.ToolCall{
		beltCall("c3", beltToolRule, map[string]any{
			"verb": "cadence", "id": "plants", "words": "tuesday"})}},
		beltTurn{text: "Moved to Tuesdays."})
	answer := postUser(t, graph, "rules", "1")
	if err := head.answer(context.Background(), answer); err != nil {
		t.Fatal(err)
	}
	settling := latestPrompt(client)
	if !strings.Contains(settling, "change it to tuesday") ||
		!strings.Contains(settling, "Which rule do you mean?") {
		t.Fatalf("the answering turn was not given the exchange it settles:\n%s", settling)
	}
	commands, err = graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandCharterCadence ||
		commands[0].Target != "plants" || commands[0].Instruction != "tuesday" {
		t.Fatalf("answered askback = %+v err=%v", commands, err)
	}
}

// TestRuleVerbsStillFallThroughWhenNoRuleExists keeps the shared vocabulary
// shared: with no standing rule to mean, "change it to tuesday" is about work,
// and the tool says so rather than inventing a rule to edit.
func TestRuleVerbsStillFallThroughWhenNoRuleExists(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "lisbon", "Lisbon trip research", "look into flights and hotels")
	user := postUser(t, graph, "no-rules", "change it to tuesday")
	head, _ := beltHead(graph,
		beltTurn{calls: []ai.ToolCall{beltCall("c1", beltToolRule, map[string]any{
			"verb": "cadence", "words": "tuesday"})}},
		beltTurn{text: "Which one do you mean?"})
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	charters, err := graph.ActiveCharters()
	if err != nil || len(charters) != 0 {
		t.Fatalf("charters = %+v err=%v", charters, err)
	}
	if commands, err := graph.PendingCommands(10); err != nil || len(commands) != 0 {
		t.Fatalf("a rule edit with no rule journaled something: %+v err=%v", commands, err)
	}
	if reply := waitForAgentReply(t, graph, "no-rules", user.Seq); strings.TrimSpace(reply.Body) == "" {
		t.Fatal("the sentence dead-ended in charter management")
	}
}
