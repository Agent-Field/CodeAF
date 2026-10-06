package session

// A NEW PROBE'S DISPLAYED CADENCE IS DERIVED FROM THE TYPED LOOK, NEVER FROM THE
// MODEL'S PROSE.
//
// The floor gate refuses a NEW look faster than the pass, but a model that
// retried with a valid probe_every could still leave a false "every minute" in
// when_words and put a lying cadence on an otherwise truthful card. The when
// band is now spelled from [standing.When.ProbeEvery] - or [standing.Interval]
// when the call named none - so the card and the one clock cannot disagree, and
// no model sentence can promise a rhythm the machinery cannot reach. The
// person's own words and the probe's compiled hint are untouched, so the
// condition is not lost.

import (
	"strings"
	"testing"
	"time"
)

func TestAProbeDisplaysTheTypedCadenceNotTheModelsIntervalClaim(t *testing.T) {
	agent, _ := ordersAgent(t)
	parsed := probeCall("5m")
	parsed.WhenWords = "every minute"
	item, problem := agent.standingItem(parsed, time.Now())
	if problem != "" {
		t.Fatalf("a valid five-minute look was refused: %s", problem)
	}
	if item.When.Words != "every 5 minutes" {
		t.Fatalf("the card's cadence reads %q, want the typed five minutes", item.When.Words)
	}
	if strings.Contains(item.When.Words, "every minute") {
		t.Fatalf("the model's false interval claim survived onto the card: %q", item.When.Words)
	}
	if item.Words != "tell me when the endpoint is ready" {
		t.Fatalf("the person's own words were rewritten: %q", item.Words)
	}
	if notice := (StandingNotice{Item: item, WhenWords: item.When.Words}); notice.WhenWords != "every 5 minutes" {
		t.Fatalf("the approval notice carried %q", notice.WhenWords)
	}
}

func TestAProbeDisplaysWiderTypedCadencesExactly(t *testing.T) {
	agent, _ := ordersAgent(t)
	for _, probe := range []struct {
		every string
		want  string
	}{
		{"10m", "every 10 minutes"},
		{"1h", "every 1 hour"},
		{"2h30m", "every 2 hours 30 minutes"},
	} {
		t.Run(probe.every, func(t *testing.T) {
			parsed := probeCall(probe.every)
			parsed.WhenWords = "every minute"
			item, problem := agent.standingItem(parsed, time.Now())
			if problem != "" {
				t.Fatalf("a %s look was refused: %s", probe.every, problem)
			}
			if item.When.Words != probe.want {
				t.Fatalf("the card's cadence reads %q, want %q", item.When.Words, probe.want)
			}
		})
	}
}

func TestAProbeWithNoTypedCadenceDisplaysThePass(t *testing.T) {
	agent, _ := ordersAgent(t)
	parsed := probeCall("")
	parsed.WhenWords = "every minute"
	item, problem := agent.standingItem(parsed, time.Now())
	if problem != "" {
		t.Fatalf("a look with no typed cadence was refused: %s", problem)
	}
	if item.When.Words != "every 5 minutes" {
		t.Fatalf("the card's cadence reads %q, want the native pass", item.When.Words)
	}
}

func TestTheProbesConditionRidesThePersonWordsAndHintNotTheCadence(t *testing.T) {
	agent, _ := ordersAgent(t)
	parsed := probeCall("5m")
	parsed.WhenWords = "when the endpoint is ready"
	parsed.When.Hint = "yes when the curl prints ready"
	item, problem := agent.standingItem(parsed, time.Now())
	if problem != "" {
		t.Fatalf("the call was refused: %s", problem)
	}
	if item.When.Words != "every 5 minutes" {
		t.Fatalf("the when band reads %q, want the derived cadence", item.When.Words)
	}
	if item.When.Hint != "yes when the curl prints ready" {
		t.Fatalf("the compiled condition was changed: %q", item.When.Hint)
	}
	if item.Words != "tell me when the endpoint is ready" {
		t.Fatalf("the person's own words were not preserved whole: %q", item.Words)
	}
}

func TestCadenceWordsCarriesTheRemainderRatherThanRoundingIt(t *testing.T) {
	if got := cadenceWords(7*time.Minute + 30*time.Second); got != "7 minutes 30 seconds" {
		t.Fatalf("cadenceWords = %q, want the exact remainder", got)
	}
	if got := cadenceWords(90 * time.Second); got != "1 minute 30 seconds" {
		t.Fatalf("cadenceWords = %q", got)
	}
	if got := cadenceWords(0); got != "0 seconds" {
		t.Fatalf("cadenceWords(0) = %q", got)
	}
}
