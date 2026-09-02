package session

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// ── WHAT THE SURFACE IS TOLD ────────────────────────────────────────────────

// hears registers a reader for the length of one test and hands back what it
// collected. The seam is process-wide, so a test that left one registered would
// be a test whose reader saw every later test's answers.
func hears(t *testing.T) *[]LaneNews {
	t.Helper()
	var heard []LaneNews
	previous := OnLaneNews(func(news LaneNews) { heard = append(heard, news) })
	t.Cleanup(func() { OnLaneNews(previous) })
	return &heard
}

// AN ORDINARY ANSWER IS ONE POST: who served, how long the first token took,
// and how fast it wrote.
func TestAnOrdinaryAnswerTellsTheSurfaceWhoServedIt(t *testing.T) {
	heard := hears(t)
	agent := &Agent{}
	agent.tellLaneNews("openrouter/model", laneFacts{
		Lane: "cloudflare", TTFT: 600 * time.Millisecond, Gen: 2 * time.Second, Output: 122,
	}, &provider.HedgeReport{})

	if len(*heard) != 1 {
		t.Fatalf("%d posts for one answer, want one", len(*heard))
	}
	news := (*heard)[0]
	if news.Lane != "cloudflare" || news.Hedged || news.Trying {
		t.Fatalf("news = %+v, want a plain answer from cloudflare", news)
	}
	if news.TTFT != 600*time.Millisecond {
		t.Fatalf("the first token read %v", news.TTFT)
	}
	if news.Rate < 60 || news.Rate > 62 {
		t.Fatalf("the rate read %v, want 122 tokens over two seconds", news.Rate)
	}
	if news.At.IsZero() {
		t.Fatal("the news carries no moment, so nothing can tell it from an old one")
	}
}

// AN ANSWER THAT COULD NOT SAY WHICH MACHINE WROTE IT SAYS NOTHING. It is the
// attribution law: a measurement credited to nobody draws a lane that was never
// involved.
func TestAnAnonymousAnswerTellsTheSurfaceNothing(t *testing.T) {
	heard := hears(t)
	agent := &Agent{}
	agent.tellLaneNews("openrouter/model", laneFacts{TTFT: time.Second}, &provider.HedgeReport{})
	if len(*heard) != 0 {
		t.Fatalf("an answer naming no lane posted %+v", *heard)
	}
}

// A RESCUE IS TWO POSTS, and the first of them is the whole point: it goes out
// WHILE the second request is in flight, which is the only moment the sentence
// "slow · trying coreweave…" is worth saying.
func TestARescueIsToldWhileItIsStillOutAndAgainWhenItLands(t *testing.T) {
	heard := hears(t)
	agent := &Agent{}
	report := &provider.HedgeReport{}
	agent.watchLaneRescue("openrouter/model", report)

	agent.laneRescueStarted("openrouter/model")(provider.RescueNews{Alt: "coreweave", Reason: provider.RescueSlow})
	if len(*heard) != 1 {
		t.Fatalf("%d posts while the rescue was out, want one", len(*heard))
	}
	if trying := (*heard)[0]; !trying.Trying || trying.Alt != "coreweave" {
		t.Fatalf("the in-flight post reads %+v, want a rescue out to coreweave", trying)
	}
}

// AND THE SEAM CARRIES WHY, NOT ONLY WHO. A lane that was LATE and a lane that
// REFUSED the model outright used to arrive here identical, because the
// callback carried a lane name and nothing else — so a 404 saying the machine
// could not serve the model at all was drawn as `· slow · trying nextbit…`
// (issue #266). The word is the transport's and this seam only carries it.
func TestARescueCarriesTheWordTheWireSaid(t *testing.T) {
	heard := hears(t)
	agent := &Agent{}
	tell := agent.laneRescueStarted("openrouter/model")

	tell(provider.RescueNews{Alt: "nextbit", Reason: provider.RescueRefused})
	if len(*heard) != 1 {
		t.Fatalf("%d posts, want one", len(*heard))
	}
	if news := (*heard)[0]; news.Reason != provider.RescueRefused || !news.Trying || news.Failed {
		t.Fatalf("a refusal in flight reads %+v", news)
	}
}

// AND A RESCUE THAT DIES TAKES ITS OWN SENTENCE BACK. A failed rescue is not a
// rescue in flight: the post exists to withdraw a promise, and one that arrived
// with Trying still set would put the promise straight back.
func TestARescueThatFailsWithdrawsTheClaimItMade(t *testing.T) {
	heard := hears(t)
	agent := &Agent{}
	tell := agent.laneRescueStarted("openrouter/model")

	tell(provider.RescueNews{Alt: "nextbit", Reason: provider.RescueRefused})
	tell(provider.RescueNews{Alt: "nextbit", Reason: provider.RescueRefused, Failed: true})
	if len(*heard) != 2 {
		t.Fatalf("%d posts, want the claim and its retraction", len(*heard))
	}
	withdrawn := (*heard)[1]
	if !withdrawn.Failed || withdrawn.Trying {
		t.Fatalf("the retraction reads %+v, want a claim taken back rather than made again", withdrawn)
	}
	if withdrawn.Alt != "nextbit" {
		t.Fatalf("the retraction is about %q, want the machine the claim named", withdrawn.Alt)
	}
}

// ── WHAT A SECOND IS WORTH ──────────────────────────────────────────────────

// A TASK TURN IS WORTH A PERSON'S ATTENTION WHILE A PERSON IS HERE, and nothing
// at all when they are not. Every task turn this build ever ran declared the
// second — a fifth off the money for nearly four times the wait, which is not a
// trade anybody chose (bench/lanelab/REPORT.md).
func TestATaskTurnIsWorthSomethingOnlyWhileSomebodyIsWatching(t *testing.T) {
	watched := &Agent{config: Config{InTask: true}}
	alone := &Agent{config: Config{InTask: true}}
	talk := &Agent{}

	withNoWindowOpen(t, func() {
		if got := alone.turnLambda(); got != 0 {
			t.Fatalf("a task nobody is watching valued a second at %v", got)
		}
		if got := talk.turnLambda(); got != lane.AttentionValue {
			t.Fatalf("a conversation's turn valued a second at %v, want %v", got, lane.AttentionValue)
		}
	})

	withAWindowOpen(t, func() {
		if got := watched.turnLambda(); got != lane.AttentionValue {
			t.Fatalf("a task somebody is watching valued a second at %v, want %v", got, lane.AttentionValue)
		}
	})
}

// withNoWindowOpen and withAWindowOpen drive the one fact [someoneIsWatching]
// reads, and put the register back exactly as they found it — the map is
// process-wide, and a test that emptied it would tell every later test's
// standing item that nobody is home.
func withNoWindowOpen(t *testing.T, run func()) {
	t.Helper()
	liveSessionsMu.Lock()
	held := liveSessions
	liveSessions = map[string]liveWindow{}
	liveSessionsMu.Unlock()
	defer func() {
		liveSessionsMu.Lock()
		liveSessions = held
		liveSessionsMu.Unlock()
	}()
	run()
}

func withAWindowOpen(t *testing.T, run func()) {
	t.Helper()
	liveSessionsMu.Lock()
	held := liveSessions
	liveSessions = map[string]liveWindow{"open": {opened: time.Now()}}
	liveSessionsMu.Unlock()
	defer func() {
		liveSessionsMu.Lock()
		liveSessions = held
		liveSessionsMu.Unlock()
	}()
	run()
}

// ── THE KEYSTROKE ──────────────────────────────────────────────────────────

// probingCompleter is a completer that also answers the optional probe half.
type probingCompleter struct {
	scriptedCompleter
	probed []string
}

func (p *probingCompleter) ProbeLanes(_ context.Context, model string) {
	p.probed = append(p.probed, model)
}

// TYPING REACHES THE TRANSPORT, and a completer that cannot probe is simply
// never asked — the capability is absent rather than present and failing.
func TestTypingReachesTheTransportAndOnlyWhereThereIsOne(t *testing.T) {
	client := &probingCompleter{}
	agent := &Agent{client: client, model: "openrouter/model"}
	agent.Typing()
	if len(client.probed) != 1 || client.probed[0] != "openrouter/model" {
		t.Fatalf("typing bought %v, want one measurement of the model in hand", client.probed)
	}

	// A node's turn is not somebody typing, so nothing is bought for one.
	node := &Agent{client: client, model: "openrouter/model", config: Config{InTask: true}}
	node.Typing()
	if len(client.probed) != 1 {
		t.Fatalf("a task node's agent bought a measurement: %v", client.probed)
	}

	// And a completer with no probe under it is never asked at all.
	plain := &Agent{client: &scriptedCompleter{}, model: "openrouter/model"}
	plain.Typing()
}
