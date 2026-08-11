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

// theObservedFailure is the message that shipped the bug, typos and trailing
// ellipsis included. The routing model read it as a durable preference and
// wrote a notebook fact; no command, no charter, no ratification card.
const theObservedFailure = "whenever a new pr comes to agentfield org, make sure to check for security scan and vulnerability..."

// The reading survives; its authority does not. "whenever a new PR comes in…"
// is still recognized as durable intent, and that reading is now put in front of
// the loop as evidence — with the consequence spelled out, because commissioning
// durable intent is not commissioning an errand. What must not happen is what
// happened: the ask filed as a notebook line, with no command, no charter and no
// ratification card behind it.
func TestRecognizedStandingLanguageDraftsACharterFromTheVerbatimAsk(t *testing.T) {
	graph := openHeadStore(t)
	user, err := graph.PostMessage(store.Message{
		SessionID: "standing", Role: store.RoleUser, Body: theObservedFailure,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !RecognizesStandingIntent(theObservedFailure) {
		t.Fatal("the durable ask is no longer recognized as standing intent")
	}
	reading := deterministicReading(t, New(nil, graph), user)
	if !strings.Contains(reading, "reads as DURABLE intent") ||
		!strings.Contains(reading, "standing rule the person is asked to ratify") {
		t.Fatalf("the loop was not told this is durable intent:\n%s", reading)
	}

	// The words travel verbatim, because the compiler behind the splice reads
	// the sentence itself: an improved paraphrase is a different rule.
	head, _ := beltHead(graph, beltTurn{calls: []ai.ToolCall{
		beltCall("c1", beltToolSpawn, map[string]any{"instruction": theObservedFailure})}},
		beltTurn{text: "I'll set that up as a standing rule and check with you before it stands."})
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	facts, err := graph.RecentFacts(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 0 {
		t.Fatalf("durable ask was noted as a fact instead of drafted: %+v", facts)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandSplice ||
		commands[0].Instruction != theObservedFailure {
		t.Fatalf("commands = %+v err=%v", commands, err)
	}
	messages, err := graph.Messages("standing", user.Seq, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Role != store.RoleAgent ||
		messages[0].CommandSeq != commands[0].Seq {
		t.Fatalf("thread after recognition = %+v", messages)
	}

	// The rest of the path is the compiler's, exactly as it would have been had
	// the routing model chosen splice: one charter draft and one ratification
	// card carrying its options.
	compilerClient := &fakeClient{responses: []string{
		`{"watch":{},"sentinel":"Did a new PR open in the agentfield org?",` +
			`"action":"Check the new PR for security scan and vulnerability findings.","rails":{}}`,
	}}
	compiler := NewCompiler(compilerClient)
	reconciler := resident.New(graph, func(ctx context.Context, instruction, graphContext string) (resident.Compiled, error) {
		brief, err := compiler.Compile(ctx, instruction, graphContext)
		if err != nil {
			return resident.Compiled{}, err
		}
		return resident.Compiled{
			Goal: brief.Goal, Assumptions: brief.Assumptions, Scale: brief.Scale,
			BuildsOn: brief.BuildsOn, Question: brief.Question,
			QuestionOptions: brief.QuestionOptions, Charter: brief.Charter,
		}, nil
	}, nil)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	charters, err := graph.Charters()
	if err != nil || len(charters) != 1 {
		t.Fatalf("charters = %+v err=%v", charters, err)
	}
	if charters[0].Invariant != theObservedFailure || charters[0].Status != store.CharterProposed {
		t.Fatalf("charter = %+v", charters[0])
	}
	card := waitForAgentReply(t, graph, "standing", messages[0].Seq)
	if len(card.Options) != 3 || card.Options[0].Value != "charter:ratify:"+charters[0].ID ||
		card.Options[0].Label != "yes, stand this up" {
		t.Fatalf("ratification card = %+v", card)
	}
	if !strings.Contains(card.Body, theObservedFailure) || !strings.Contains(card.Body, "What should I do?") {
		t.Fatalf("ratification question body = %q", card.Body)
	}
}

func TestNonStandingPhrasingStaysOnTheOrdinaryPath(t *testing.T) {
	// The cue set is unchanged, so the negative case is phrasing the cues
	// already exclude: a one-shot trigger definition, and plain work. ("whenever
	// I say X I mean Y" does still fire the cues; that costs one ratification
	// card, which is the deliberate side of the trade.)
	for _, message := range []string{
		"when i say ship it i mean run the deploy script once",
		"check the last pr for a security scan",
	} {
		t.Run(message, func(t *testing.T) {
			if RecognizesStandingIntent(message) {
				t.Fatalf("cues fired on non-standing phrasing")
			}
			graph := openHeadStore(t)
			client := &fakeClient{responses: []string{"On it."}}
			user, err := graph.PostMessage(store.Message{
				SessionID: "ordinary", Role: store.RoleUser, Body: message,
			})
			if err != nil {
				t.Fatal(err)
			}
			// Nothing here says the ask is durable, so nothing in front of the
			// loop suggests a rule — and one ordinary turn is one call.
			if reading := New(nil, graph).renderHints(user, nil); strings.Contains(reading, "DURABLE intent") {
				t.Fatalf("a one-shot ask was reported as durable:\n%s", reading)
			}
			if err := New(client, graph).answer(context.Background(), user); err != nil {
				t.Fatal(err)
			}
			if calls := client.callCount(); calls != 1 {
				t.Fatalf("provider calls = %d, want 1", calls)
			}
			reply := waitForAgentReply(t, graph, "ordinary", user.Seq)
			if reply.Body != "On it." {
				t.Fatalf("reply = %q, want the model's own answer", reply.Body)
			}
			charters, err := graph.Charters()
			if err != nil || len(charters) != 0 {
				t.Fatalf("charters = %+v err=%v", charters, err)
			}
		})
	}
}

// TestEverySundayCompilesToAWeeklyRuleThatOutlivesItsFirstFiring is the
// everyday simulation's Monday 08:14, at the layer that read the sentence.
// "remind me every sunday …" produced no cadence at all, so the head
// substituted a literal two minutes, and the reminder fired once — two minutes
// after ratification — and expired before the Sunday it named.
func TestEverySundayCompilesToAWeeklyRuleThatOutlivesItsFirstFiring(t *testing.T) {
	const said = "remind me every sunday to water the plants and check my mom called"
	if !RecognizesStandingIntent(said) {
		t.Fatal("a weekly reminder is not recognized as standing intent")
	}
	if cadence := extractCadence(said); cadence != "every sunday" {
		t.Fatalf("extracted cadence = %q, want the words she said", cadence)
	}
	spec := normalizeCharterSpec(store.CharterSpec{}, said, "")
	if spec.Watch.Kind != store.WatchCron || spec.Watch.Spec.Cron == nil {
		t.Fatalf("weekly reminder watch = %+v, want a clock rule", spec.Watch)
	}
	schedule := *spec.Watch.Spec.Cron
	if schedule.Kind != store.CronWeekly || schedule.Weekday != time.Sunday || schedule.Hour != 9 {
		t.Fatalf("schedule = %+v, want Sunday at 9", schedule)
	}
	if spec.Watch.Spec.CadenceGuessed {
		t.Fatal("a cadence the user said was recorded as a guess")
	}
	if spec.Rails.Expiry != "never" {
		t.Fatalf("recurring reminder expiry = %q, want never", spec.Rails.Expiry)
	}
	// The stated clock survives, and so does the day.
	timed := normalizeCharterSpec(store.CharterSpec{}, "remind me every sunday at 8pm to take the bins out", "")
	if got := *timed.Watch.Spec.Cron; got.Kind != store.CronWeekly ||
		got.Weekday != time.Sunday || got.Hour != 20 || got.Minute != 0 {
		t.Fatalf("schedule = %+v, want Sunday at 20:00", got)
	}
	// A reminder that genuinely happens once still expires once.
	once := normalizeCharterSpec(store.CharterSpec{}, "remind me tomorrow at 9 to call the dentist", "")
	if once.Rails.Expiry != "once" {
		t.Fatalf("one-shot reminder expiry = %q, want once", once.Rails.Expiry)
	}
}

// TestTheCompilersOwnCadenceIsUsedRatherThanReplaced covers the discarded
// reading: the temporal compiler is asked for human cadence words, answers with
// them, and the head threw the answer away in favour of a default.
func TestTheCompilersOwnCadenceIsUsedRatherThanReplaced(t *testing.T) {
	// A sentence no deterministic pattern reads, with a model that read it.
	watch := standingWatch("keep an eye on the release queue",
		store.CharterWatch{Cadence: "every 30 minutes", Schedule: "queue depth"})
	if watch.Cadence != "every 30 minutes" {
		t.Fatalf("cadence = %q, want the compiler's own reading", watch.Cadence)
	}
	if watch.Spec.CadenceGuessed {
		t.Fatal("a cadence the compiler read was recorded as a guess")
	}
	if watch.Spec.Poll == nil || watch.Spec.Poll.Cadence != 30*time.Minute {
		t.Fatalf("poll watch = %+v, want a 30 minute cadence", watch.Spec.Poll)
	}

	// A compiler reading that says nothing about time cannot rescue anything,
	// and the default that follows announces itself as a guess.
	guessed := standingWatch("watch for when the dyson v15 drops under 500",
		store.CharterWatch{Cadence: "when the price drops"})
	if guessed.Cadence != "about every 2 minutes" || !guessed.Spec.CadenceGuessed {
		t.Fatalf("unreadable cadence = %q guessed=%t, want an announced default",
			guessed.Cadence, guessed.Spec.CadenceGuessed)
	}
}
