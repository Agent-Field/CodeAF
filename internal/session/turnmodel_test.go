package session

// THE LAW: THE WORK IS ON ONE MODEL AT A TIME, IT SAYS WHICH, AND WHAT IS DRAWN
// BESIDE IT IS THAT MODEL.
//
// steer.go decides WHEN a person's pick reaches the work — at the next request,
// either by cutting one nothing had come back from or by letting an arriving
// answer finish. What was missing is everything that SAYS so: the phase news a
// surface draws its model cell from carried the dial, so a pick made three
// seconds into a turn re-labelled the whole of it while every request went to
// the old model.

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A pick made while an answer is ARRIVING leaves the work where it is: the model
// the chrome names, the stages it posts, and the pictures it is still sending
// all stay on the model that is writing the reply. This is the half of steer.go's
// law that says a reply somebody is reading is theirs.
func TestAPickWhileAnAnswerIsArrivingLeavesTheWorkWhereItIs(t *testing.T) {
	spoke := make(chan struct{}, 1)
	release := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "here is the first half")
			select {
			case spoke <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return toolResponseWithText("c1", "ls", `{"path":"."}`, "here is the first half and the rest"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("and the step after is on the new model"), nil
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
	case <-spoke:
	case <-time.After(10 * time.Second):
		t.Fatal("the first request never streamed a word")
	}
	if got := agent.TurnModel(); got != "test/model" {
		t.Fatalf("the work in flight says it is on %q, want test/model", got)
	}

	agent.SetModel("vendor/blind")

	// The dial moved and the work did not. These are two different questions and
	// this is the whole of the reported defect: the surface asked the first one
	// and drew the answer as though it were the second.
	if got := agent.Model(); got != "vendor/blind" {
		t.Fatalf("the dial reads %q, want the model just picked", got)
	}
	if got := agent.TurnModel(); got != "test/model" {
		t.Fatalf("the work moved to %q while an answer was arriving on test/model", got)
	}
	// AND THE PICTURES ARE STILL THERE. The request that is answering can see
	// them; replacing them with placeholders here takes a person's screenshot
	// away mid-read, and buys nothing.
	if got := countImageParts(agent); got != 1 {
		t.Fatalf("the arriving answer is left with %d pictures, want the 1 it started with", got)
	}

	close(release)
	collect(t, events)

	// Every stage posted while that answer was arriving named the model it was
	// arriving from. One of them is the tool call it ended with.
	said := false
	for _, news := range phases.all() {
		if news.Phase == "" || news.Model == "vendor/blind" {
			continue
		}
		said = true
		if news.Model != "test/model" {
			t.Fatalf("a %q stage was posted under %q, want the model the work was on",
				news.Phase, news.Model)
		}
	}
	if !said {
		t.Fatal("the turn posted no stage at all, so the law was never exercised")
	}
	if got := agent.TurnModel(); got != "" {
		t.Fatalf("an idle session claims work on %q", got)
	}
}

// AND A PICK THAT CUTS THE REQUEST TAKES THE PICTURES WITH IT. This is the other
// half of the law and the hole the first cut of this change left: when nothing
// has come back the request is let go of and asked again ON THE NEW MODEL inside
// the same turn, so a scrub that waited for the next TURN would have handed a
// blind model a transcript full of base64 — the leak arriving by the one road
// nobody would think to look down.
func TestAPickThatCutsTheRequestTakesThePicturesWithIt(t *testing.T) {
	out := make(chan struct{}, 1)
	sent := make(chan []ai.Message, 1)
	completer := &scriptedCompleter{steps: []step{
		waitingRequest(out),
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			select {
			case sent <- append([]ai.Message(nil), messages...):
			default:
			}
			return textResponse("answered without the picture"), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.SeesImages = func(model string) ModelSight { return SightOf(model == "test/model") }
		config.SessionFile = writeableJournal(t)
	})
	path := writeImage(t, workspace, "chart.png", "PHOTOBYTES")

	ctx, cancel := deadline(20 * time.Second)
	defer cancel()
	events := mustSubmitImage(t, agent, ctx, "what is wrong with this?", []Image{{Path: path}})
	select {
	case <-out:
	case <-time.After(10 * time.Second):
		t.Fatal("the first request never went out")
	}

	agent.SetModel("vendor/blind")
	collect(t, events)

	if got := completer.model(1); got != "vendor/blind" {
		t.Fatalf("the re-ask rode %q, want the model the person named", got)
	}
	// THE ASSERTION IS ON THE WIRE. What the blind model was actually handed is
	// the only thing that answers this, because the transcript was right before
	// this change too and the request was not.
	select {
	case messages := <-sent:
		pictures := 0
		for _, message := range messages {
			pictures += len(imagePartURLs(message))
		}
		if pictures != 0 {
			t.Fatalf("the request to the blind model carried %d pictures", pictures)
		}
	default:
		t.Fatal("the re-ask never reached the completer")
	}
	if got := countImageParts(agent); got != 0 {
		t.Fatalf("%d pictures are still in the transcript the blind model is being sent", got)
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
