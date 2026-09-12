package session

// THE LAW: A TURN IN FLIGHT IS ON ONE MODEL, IT SAYS WHICH, AND A SWAP MADE
// UNDER IT CHANGES NOTHING ABOUT IT.
//
// [Agent.SetModel] has promised since it was written that a turn finishes on the
// model it started on, and [TestSetModelAppliesFromTheNextTurn] has held it to
// that on the wire. What was missing is everything that SAYS so: the phase news
// a surface draws its model cell from carried the dial, so a pick made three
// seconds into a turn re-labelled the whole of it, and the transcript's pictures
// were taken away from a model that was in the middle of reading them.

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A swap made mid-turn leaves the turn's own model, the news about it, and the
// pictures it is still sending exactly where they were — and lands whole at the
// next turn, scrub included.
func TestAMidTurnSwapLeavesTheRunningTurnAlone(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(entered)
			<-release
			return toolResponse("c1", "ls", `{"path":"."}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("on the new one"), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.SeesImages = func(model string) ModelSight { return SightOf(model == "test/model") }
		config.SessionFile = writeableJournal(t)
	})
	phases := watchPhases(t)
	path := writeImage(t, workspace, "chart.png", "PHOTOBYTES")

	ctx, cancel := deadline(20 * time.Second)
	defer cancel()
	events := mustSubmitImage(t, agent, ctx, "what is wrong with this?", []Image{{Path: path}})
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the first step never started")
	}
	if got := agent.TurnModel(); got != "test/model" {
		t.Fatalf("the turn in flight says it is on %q, want test/model", got)
	}

	agent.SetModel("vendor/blind")

	// The dial moved and the turn did not. These are two different questions and
	// this is the whole of the reported defect: the surface asked the first one
	// and drew the answer as though it were the second.
	if got := agent.Model(); got != "vendor/blind" {
		t.Fatalf("the dial reads %q, want the model just picked", got)
	}
	if got := agent.TurnModel(); got != "test/model" {
		t.Fatalf("the turn in flight moved to %q — SetModel touched work already running", got)
	}
	// AND THE PICTURES ARE STILL THERE. The steps left in this turn are still
	// going to a model that can see them; replacing them with placeholders here
	// takes a person's screenshot away mid-read.
	if got := countImageParts(agent); got != 1 {
		t.Fatalf("the running turn is left with %d pictures, want the 1 it started with", got)
	}

	close(release)
	collect(t, events)

	// Every stage this turn posted after the swap named the model the turn was
	// on. One of them is the tool run released above.
	said := false
	for _, news := range phases.all() {
		if news.Phase == "" {
			continue
		}
		said = true
		if news.Model != "test/model" {
			t.Fatalf("a %q stage of the turn was posted under %q, want the model the turn is on",
				news.Phase, news.Model)
		}
	}
	if !said {
		t.Fatal("the turn posted no stage at all, so the law was never exercised")
	}

	// AND THE SWAP LANDS WHOLE AT THE NEXT TURN — the model on the wire and the
	// scrub together, because the one place a next turn begins is where both
	// happen ([Agent.startTurnLocked]).
	collect(t, mustSubmit(t, agent, "again"))
	if got := completer.model(2); got != "vendor/blind" {
		t.Fatalf("the next turn rode %q, want vendor/blind", got)
	}
	if got := countImageParts(agent); got != 0 {
		t.Fatalf("the turn on the blind model still carries %d pictures", got)
	}
	if got := agent.TurnModel(); got != "" {
		t.Fatalf("an idle session claims a turn on %q", got)
	}
}

// An idle session's news names the dial, which is the honest answer to "what
// will the next request use" — and the emptiness law is about figures nobody
// has, not about this one.
func TestAnIdleSessionsNewsNamesTheDial(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	phases := watchPhases(t)

	agent.SetModel("vendor/next")
	agent.tellPhase(provider.PhaseTidying, "", time.Time{})
	t.Cleanup(agent.endPhase)

	said := phases.all()
	if len(said) == 0 {
		t.Fatal("nothing was posted")
	}
	if got := said[len(said)-1].Model; got != "vendor/next" {
		t.Fatalf("an idle session's news names %q, want the dial", got)
	}
}

// A TURN THAT SWAPPED NOTHING TAKES NOTHING AWAY. The scrub is one-way and the
// bytes do not come back, so it fires on a swap and on nothing else: an oracle
// that answers `sees` off a warm cache and `blind` a minute later off a catalog
// fetch that failed used to be enough to eat a person's screenshot with nobody
// having touched the picker.
func TestAFlappingOracleCannotScrubAConversationThatNeverSwapped(t *testing.T) {
	sight := SightSees
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("first"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("second"), nil },
	}}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.SeesImages = func(string) ModelSight { return sight }
		config.SessionFile = writeableJournal(t)
	})
	path := writeImage(t, workspace, "chart.png", "PHOTOBYTES")

	ctx, cancel := deadline(20 * time.Second)
	defer cancel()
	collect(t, mustSubmitImage(t, agent, ctx, "what is wrong with this?", []Image{{Path: path}}))
	if got := countImageParts(agent); got != 1 {
		t.Fatalf("the picture never reached the transcript")
	}

	// The catalog goes cold under the same model id. Nothing was swapped.
	sight = SightBlind
	collect(t, mustSubmit(t, agent, "and now?"))
	if got := countImageParts(agent); got != 1 {
		t.Fatalf("a turn on the SAME model took the pictures away: %d left", got)
	}
}

// AND AN UNKNOWN MODEL IS NOT A BLIND ONE. A swap onto a model this build has
// never read a row for is ignorance, and ignorance may refuse to send but may
// never destroy.
func TestASwapOntoAnUnknownModelKeepsThePictures(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("first"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("second"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("third"), nil },
	}}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.SeesImages = func(model string) ModelSight {
			switch model {
			case "test/model":
				return SightSees
			case "vendor/blind":
				return SightBlind
			}
			return SightUnknown
		}
		config.SessionFile = writeableJournal(t)
	})
	path := writeImage(t, workspace, "chart.png", "PHOTOBYTES")

	ctx, cancel := deadline(20 * time.Second)
	defer cancel()
	collect(t, mustSubmitImage(t, agent, ctx, "what is wrong with this?", []Image{{Path: path}}))

	agent.SetModel("vendor/never-heard-of")
	collect(t, mustSubmit(t, agent, "still there?"))
	if got := countImageParts(agent); got != 1 {
		t.Fatalf("a swap onto a model nobody has vouched for destroyed %d pictures", 1-got)
	}

	// AND A SWAP ONTO A MODEL THIS BUILD KNOWS IS BLIND STILL SCRUBS, which is
	// the capability this guard must not have taken away.
	agent.SetModel("vendor/blind")
	collect(t, mustSubmit(t, agent, "and now?"))
	if got := countImageParts(agent); got != 0 {
		t.Fatalf("a swap onto a known-blind model left %d pictures", got)
	}
}
