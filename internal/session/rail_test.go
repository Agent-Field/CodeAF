package session

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
)

// spend puts a session's accumulated cost where the rail reads it, the way a
// turn's own accounting would.
func spend(a *Agent, usd float64) {
	a.mu.Lock()
	a.usage.CostUSD = usd
	a.mu.Unlock()
}

// Over the line, the turn is refused and NOTHING happens: no request, no
// journaled message. The refusal names the sentinel so a surface can say what
// it is instead of matching on words.
func TestSpendRailRefusesTheTurnAndDoesNoWork(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			t.Error("a refused turn reached the provider")
			return textResponse("should not happen"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SpendRailUSD = 2 })
	spend(agent, 2.50)

	events := collect(t, mustSubmit(t, agent, "keep going"))

	if len(events) != 1 || events[0].Kind != EventError {
		t.Fatalf("events = %v, want one EventError", kinds(events))
	}
	if !errors.Is(events[0].Err, ErrSpendRail) {
		t.Fatalf("error = %v, want it to wrap ErrSpendRail", events[0].Err)
	}
	if !strings.Contains(events[0].Err.Error(), "$2.50") {
		t.Fatalf("error = %v, want it to say what was spent", events[0].Err)
	}
	if completer.requests() != 0 {
		t.Fatalf("requests = %d, want 0", completer.requests())
	}
	if got := transcriptRoles(agent); len(got) != 1 || got[0] != "system" {
		t.Fatalf("transcript = %v, want a refused turn to record nothing", got)
	}
}

func TestDailySpendRailShowsCardAndStopsOrRaises(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		want string
	}{
		{name: "stop", key: "2", want: "today's spending limit of $1.00 is spent, so nothing was started"},
		{name: "raise", key: "1", want: "turn finished"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile := t.TempDir()
			ledger := filepath.Join(t.TempDir(), "usage.jsonl")
			if err := config.WriteDailyBudgetUSD(profile, 1); err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			recordUsage(t, ledger, UsageLine{At: now, Day: localDay(now), Calls: 1, USD: 1.01})
			completer := &scriptedCompleter{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return textResponse("continued"), nil
				},
			}}
			agent, _ := newTestAgent(t, completer, func(c *Config) {
				c.ProfileDir = profile
				c.usageLedger = ledger
			})

			stream := mustSubmit(t, agent, "start work")
			question := waitForOneQuestion(t, agent)
			if question.Kind != QuestionDailyBudget || len(question.Options) != 2 || question.Options[0].Key != "1" || question.Options[0].Label != "Raise to $2" || question.Options[1].Key != "2" || question.Options[1].Label != "Stop for today" {
				t.Fatalf("daily question = %+v, want raise and stop choices", question)
			}
			if got := completer.requests(); got != 0 {
				t.Fatalf("provider calls before the answer = %d, want 0", got)
			}
			if err := agent.ResolveQuestion(Answer{Kind: question.Kind, ID: question.ID, Key: tc.key}); err != nil {
				t.Fatalf("ResolveQuestion: %v", err)
			}
			events := collect(t, stream)
			if tc.key == "2" {
				if len(events) != 1 || events[0].Kind != EventError || events[0].Err == nil || events[0].Err.Error() != tc.want {
					t.Fatalf("stop events = %v, want the daily refusal", events)
				}
				if got := completer.requests(); got != 0 {
					t.Fatalf("provider calls after stop = %d, want 0", got)
				}
				return
			}
			if len(events) == 0 || events[len(events)-1].Kind != EventTurnDone || completer.requests() != 1 {
				t.Fatalf("raise events = %v, calls = %d, want a completed turn and one call", kinds(events), completer.requests())
			}
			if daily, err := DailySpendAt(profile, now, ledger); err != nil || daily.Limit != 2 {
				t.Fatalf("raised daily limit = %+v, err=%v, want $2.00 today", daily, err)
			}
		})
	}
}

// Under the line the rail is not there.
func TestSpendRailUnderTheLineRunsTheTurn(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("carried on"), nil },
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SpendRailUSD = 2 })
	spend(agent, 1.99)

	events := collect(t, mustSubmit(t, agent, "keep going"))

	if last := events[len(events)-1]; last.Kind != EventTurnDone {
		t.Fatalf("turn ended with %v, want EventTurnDone", last.Kind)
	}
	if got := messageText(lastMessage(agent)); got != "carried on" {
		t.Fatalf("answer = %q", got)
	}
}

// Zero is off, whatever the session has spent.
func TestSpendRailOffByDefault(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("no ceiling here"), nil },
	}}
	agent, _ := newTestAgent(t, completer, nil)
	spend(agent, 9000)

	events := collect(t, mustSubmit(t, agent, "keep going"))

	if last := events[len(events)-1]; last.Kind != EventTurnDone {
		t.Fatalf("turn ended with %v, want EventTurnDone", last.Kind)
	}
}

// The rail stops the NEXT turn, never the one in flight: a session killed
// between an assistant's tool_calls and their results is a transcript no
// provider will take back.
func TestSpendRailNeverCutsTheTurnInFlight(t *testing.T) {
	runs := make(chan string, 2)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "touch", "{}"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("finished anyway"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SpendRailUSD = 1 })
	agent.tools = append(agent.tools, countingTool("touch", runs, nil))

	events := mustSubmit(t, agent, "start the work")
	// The rail is crossed while the turn is mid-batch.
	spend(agent, 5)
	collected := collect(t, events)

	if last := collected[len(collected)-1]; last.Kind != EventTurnDone {
		t.Fatalf("turn ended with %v, want it to finish", last.Kind)
	}
	if len(runs) != 1 {
		t.Fatalf("the tool ran %d times, want 1 — the batch was cut short", len(runs))
	}
	// And the next turn is the one that is refused.
	next := collect(t, mustSubmit(t, agent, "and again"))
	if len(next) != 1 || !errors.Is(next[0].Err, ErrSpendRail) {
		t.Fatalf("the next turn = %v, want the rail's refusal", kinds(next))
	}
}

// TestTheRefusalNamesTheLimitTheFigureAndTheDoor is
// docs/design/spending/DESIGN.md acceptance 6: one line, the limit, the figure
// and the key — and nothing about it said twice.
//
// IT SAYS `limit` AND NOT `rail`. The machinery's word is this file's; the
// person's word is theirs. And it names `/budget` rather than a bare letter,
// because the person reading it is standing over the message box with their
// refused message still in it, and every printable key there belongs to that
// box.
func TestTheRefusalNamesTheLimitTheFigureAndTheDoor(t *testing.T) {
	agent := &Agent{}
	agent.config.SpendRailUSD = 2
	agent.usage.CostUSD = 2.05
	err := agent.railBlockLocked()
	if err == nil {
		t.Fatal("a session past its ceiling must be refused")
	}
	if !errors.Is(err, ErrSpendRail) {
		t.Fatalf("the refusal must carry the sentinel: %v", err)
	}
	want := "conversation limit reached · $2.05 spent of $2 · /budget changes it"
	if got := err.Error(); got != want {
		t.Fatalf("the refusal reads\n  %s\nwant\n  %s", got, want)
	}
	// AND THE SENTINEL'S OWN WORDS REACH NOBODY. A sentinel is matched, never
	// read, and `%w` in front of a sentence is the machinery talking over it.
	for _, banned := range []string{"rail", "ceiling", "raise it to keep going", "session:"} {
		if strings.Contains(err.Error(), banned) {
			t.Fatalf("the refusal still says %q: %s", banned, err)
		}
	}
	// AND A WHOLE FIGURE IS WRITTEN WHOLE. `$500.00` is a number somebody typed
	// with two cells of noise on the end.
	if got := railMoney(500); got != "$500" {
		t.Fatalf("a whole limit reads %q", got)
	}
	if got := railMoney(4.1); got != "$4.10" {
		t.Fatalf("a part-dollar limit reads %q", got)
	}
	// AND A SUB-CENT FIGURE IS A FIGURE. `$0.00 spent of $0.00` names nothing.
	if got := railMoney(0.0006); got != "$0.0006" {
		t.Fatalf("a sub-cent figure reads %q", got)
	}
}

func TestDailyBudgetHeldTurnEndsOnCancellation(t *testing.T) {
	for _, ending := range []string{"cancel", "answer", "raise", "close"} {
		t.Run(ending, func(t *testing.T) {
			profile, ledger := t.TempDir(), filepath.Join(t.TempDir(), "usage.jsonl")
			if err := config.WriteDailyBudgetUSD(profile, 1); err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			recordUsage(t, ledger, UsageLine{At: now, Day: localDay(now), Calls: 1, USD: 2})
			agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.ProfileDir, c.usageLedger = profile, ledger })
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			stream, err := agent.Submit(ctx, "work")
			if err != nil {
				t.Fatal(err)
			}
			q := waitForOneQuestion(t, agent)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				switch ending {
				case "answer":
					_ = agent.ResolveQuestion(Answer{Kind: q.Kind, ID: q.ID, Key: "2"})
				case "raise":
					_ = agent.ResolveQuestion(Answer{Kind: q.Kind, ID: q.ID, Key: "1"})
				case "close":
					_ = agent.Close()
				}
			}()
			cancel()
			deadline := time.After(5 * time.Second)
			for {
				select {
				case _, open := <-stream:
					if !open {
						<-finished
						if len(agent.OpenQuestions()) != 0 {
							t.Fatal("ended hold kept its question")
						}
						return
					}
				case <-deadline:
					t.Fatal("cancelled budget hold kept its stream open")
				}
			}
		})
	}
}

func TestDailySpendAuthorizationLeavesOtherRailsInForce(t *testing.T) {
	profile, ledger := t.TempDir(), filepath.Join(t.TempDir(), "usage.jsonl")
	if err := config.WriteDailyBudgetUSD(profile, 1); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	recordUsage(t, ledger, UsageLine{At: now, Day: localDay(now), Calls: 1, USD: 2})
	for _, tc := range []struct {
		name        string
		authorized  bool
		rail        float64
		wantBlocked bool
	}{
		{"ordinary", false, 0, true},
		{"authorized", true, 0, false},
		{"conversation limit", true, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			if tc.authorized {
				ctx = WithDailySpendPreauthorized(ctx)
			}
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
				c.ProfileDir, c.usageLedger = profile, ledger
				c.DailySpendPreauthorized = DailySpendPreauthorized(ctx)
				c.SpendRailUSD = tc.rail
			})
			spend(agent, 2)
			agent.mu.Lock()
			err := agent.railBlockLocked()
			agent.mu.Unlock()
			if (err != nil) != tc.wantBlocked {
				t.Fatalf("rail = %v, want blocked %v", err, tc.wantBlocked)
			}
			if tc.rail > 0 && !errors.Is(err, ErrSpendRail) {
				t.Fatalf("authorization lifted conversation rail: %v", err)
			}
		})
	}
	if DailySpendPreauthorized(t.Context()) {
		t.Fatal("authorization escaped its run context")
	}
}
