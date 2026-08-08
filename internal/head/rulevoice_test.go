package head

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
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
func TestChangeItToTuesdayReachesTheRule(t *testing.T) {
	graph := openHeadStore(t)
	charter := activateReminder(t, graph, "plants",
		"remind me every sunday to water the plants", "every sunday", "water the plants")
	// A live job is on the board: surgery must not be the arm that claims this.
	spliceSurgeryJob(t, graph, "lisbon", "Lisbon trip research", "look into flights and hotels")

	user := postUser(t, graph, "rules", "change it to tuesday")
	if err := New(&fakeClient{}, graph).answer(context.Background(), user); err != nil {
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
	if err := New(&fakeClient{}, graph).answer(context.Background(), user); err != nil {
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
	if err := New(&fakeClient{}, graph).answer(context.Background(), user); err != nil {
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
// question in plain words.
func TestAnUndescribedRuleReferenceAsksOnePlainQuestion(t *testing.T) {
	graph := openHeadStore(t)
	activateReminder(t, graph, "plants",
		"remind me every sunday to water the plants", "every sunday", "water the plants")
	activateReminder(t, graph, "bins",
		"remind me every friday to put the bins out", "every friday", "put the bins out")

	user := postUser(t, graph, "rules", "change it to tuesday")
	if err := New(&fakeClient{}, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	reply := waitForAgentReply(t, graph, "rules", user.Seq)
	if !strings.HasPrefix(reply.Body, "Which rule do you mean?") || len(reply.Options) != 2 {
		t.Fatalf("askback = %+v", reply)
	}
	for _, option := range reply.Options {
		if !strings.HasSuffix(option.Value, ":tuesday") {
			t.Fatalf("option loses the new day: %+v", option)
		}
		if !strings.Contains(option.Label, "remind me every") {
			t.Fatalf("option is not named by its plain description: %+v", option)
		}
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 0 {
		t.Fatalf("an ambiguous reference changed something: %+v err=%v", commands, err)
	}

	// Answering the question by number carries the day through to the rule.
	answer := postUser(t, graph, "rules", "1")
	if err := New(&fakeClient{}, graph).answer(context.Background(), answer); err != nil {
		t.Fatal(err)
	}
	commands, err = graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandCharterCadence ||
		commands[0].Instruction != "tuesday" {
		t.Fatalf("answered askback = %+v err=%v", commands, err)
	}
}

// TestRuleVerbsStillFallThroughWhenNoRuleExists keeps the shared vocabulary
// shared: with no standing rule to mean, "change it to tuesday" is about work.
func TestRuleVerbsStillFallThroughWhenNoRuleExists(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "lisbon", "Lisbon trip research", "look into flights and hotels")
	client := &fakeClient{responses: []string{`{"reply":"Which one do you mean?","command":null}`}}
	user := postUser(t, graph, "no-rules", "change it to tuesday")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	charters, err := graph.ActiveCharters()
	if err != nil || len(charters) != 0 {
		t.Fatalf("charters = %+v err=%v", charters, err)
	}
	if reply := waitForAgentReply(t, graph, "no-rules", user.Seq); strings.TrimSpace(reply.Body) == "" {
		t.Fatal("the sentence dead-ended in charter management")
	}
}
