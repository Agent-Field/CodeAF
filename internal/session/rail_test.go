package session

import (
	"context"
	"errors"
	"strings"
	"testing"

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
// the implementation acceptance 6: one line, the limit, the figure
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
	want := "conversation limit reached · $2.05 spent of $2 · /budget conversation changes it"
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

func TestSpendRailRefusesTurnWhenDailyBudgetExceeded(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			t.Error("a refused turn reached the provider")
			return textResponse("should not happen"), nil
		},
	}}
	profileDir := t.TempDir()
	if err := config.WriteDailyBudgetUSD(profileDir, 1.00); err != nil {
		t.Fatalf("write daily budget: %v", err)
	}
	agent, _ := newTestAgent(t, completer, func(cfg *Config) {
		cfg.ProfileDir = profileDir
	})
	agent.crewDayHeld = NewSpendDay(1.50)

	events := collect(t, mustSubmit(t, agent, "keep going"))
	if len(events) != 1 || events[0].Kind != EventError {
		t.Fatalf("events = %v, want one EventError", kinds(events))
	}
	if !errors.Is(events[0].Err, ErrSpendRail) {
		t.Fatalf("error = %v, want it to wrap ErrSpendRail", events[0].Err)
	}
	if !strings.Contains(events[0].Err.Error(), "daily limit reached") || !strings.Contains(events[0].Err.Error(), "/budget day changes it") {
		t.Fatalf("error = %v, want it to say daily limit reached", events[0].Err)
	}
}

// TestTheDailyCeilingBindsTaskAndErrandTurnsToo guards #1546: the daily budget
// is a machine-wide ceiling, and before this fix [Agent.railBlockLocked] skipped
// it for an InTask runner (a task node, including one under `--one-model`, which
// has no crew guard behind it) and for the home Errand. Each of those is now
// held to the day's allowance exactly as a chat turn is: a conversation past the
// ceiling starts no new turn, however the work was asked for.
func TestTheDailyCeilingBindsTaskAndErrandTurnsToo(t *testing.T) {
	for _, posture := range []struct {
		what   string
		config func(*Config)
	}{
		{"a task node runner", func(cfg *Config) { cfg.InTask = true }},
		{"a task node under one-model", func(cfg *Config) { cfg.InTask = true; cfg.OneModel = true }},
		{"the home errand", func(cfg *Config) { cfg.Errand = true }},
	} {
		t.Run(posture.what, func(t *testing.T) {
			profileDir := t.TempDir()
			if err := config.WriteDailyBudgetUSD(profileDir, 1.00); err != nil {
				t.Fatalf("write daily budget: %v", err)
			}
			agent, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) {
				cfg.ProfileDir = profileDir
				posture.config(cfg)
			})
			// The day's spend is over the ceiling, read the way the rail reads it.
			agent.crewDayHeld = NewSpendDay(1.50)

			err := agent.railBlockLocked()
			if err == nil {
				t.Fatalf("%s past the daily ceiling was allowed to start a turn", posture.what)
			}
			if !errors.Is(err, ErrSpendRail) {
				t.Fatalf("the refusal must carry the sentinel: %v", err)
			}
			if !strings.Contains(err.Error(), "daily limit reached") {
				t.Fatalf("error = %v, want it to say the daily limit was reached", err)
			}
		})
	}

	// And under the ceiling, each posture still starts: the fix refuses only the
	// turn the day's allowance cannot pay for.
	profileDir := t.TempDir()
	if err := config.WriteDailyBudgetUSD(profileDir, 1.00); err != nil {
		t.Fatalf("write daily budget: %v", err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) {
		cfg.ProfileDir = profileDir
		cfg.InTask = true
	})
	agent.crewDayHeld = NewSpendDay(0.50)
	if err := agent.railBlockLocked(); err != nil {
		t.Fatalf("a task turn under the daily ceiling was refused: %v", err)
	}
}

// TestAWorkerIsHeldToTheSelectedBudgetItCarriesNotTheDefault covers the #1842
// review: a worker is handed no ProfileDir (it would open the settings door), so
// the daily check must read the figure the root resolved and carried
// ([Config.DailyBudgetUSD]), never a profile at an empty path - which returns
// the default profile's limit and would refuse a worker under a higher selected limit and
// let it spend past a lower one.
func TestAWorkerIsHeldToTheSelectedBudgetItCarriesNotTheDefault(t *testing.T) {
	// A worker: no ProfileDir, the selected limit carried down as $1.
	worker := &Agent{}
	worker.config.DailyBudgetUSD = 1.00
	worker.config.DailyBudgetResolved = true
	worker.crewDayHeld = NewSpendDay(1.50)
	if err := worker.railBlockLocked(); err == nil {
		t.Fatal("a worker past its carried $1 ceiling was allowed to start")
	} else if !strings.Contains(err.Error(), "daily limit reached") {
		t.Fatalf("error = %v, want the daily limit", err)
	}

	// The same spend ($600) under a HIGHER selected limit ($1000) carried down
	// must NOT refuse - the bug was reading the default $500 and refusing here.
	high := &Agent{}
	high.config.DailyBudgetUSD = 1000.00
	high.config.DailyBudgetResolved = true
	high.crewDayHeld = NewSpendDay(600.00)
	if err := high.railBlockLocked(); err != nil {
		t.Fatalf("a worker under its carried $1000 ceiling was refused: %v", err)
	}

	// An explicitly unlimited worker keeps its selected zero ceiling.
	none := &Agent{config: Config{DailyBudgetResolved: true}}
	none.crewDayHeld = NewSpendDay(1000.00)
	if err := none.railBlockLocked(); err != nil {
		t.Fatalf("an explicitly unlimited worker was refused: %v", err)
	}
}

// TestSpendPreauthorizationLiftsTheDailyCeiling covers the #1842 review's second
// case: `--yes-spend` / `CODEAF_PREAUTHORIZE_SPEND=1` is the person's standing
// permission to spend past the daily limit, so a preauthorized worker past the
// ceiling runs rather than refusing - the same authority the crew guard already
// honours. A separate conversation limit is NOT lifted by it.
func TestSpendPreauthorizationLiftsTheDailyCeiling(t *testing.T) {
	pre := &Agent{}
	pre.config.DailyBudgetUSD = 1.00
	pre.config.DailyBudgetResolved = true
	pre.config.SpendPreauthorized = true
	pre.crewDayHeld = NewSpendDay(1.50)
	if err := pre.railBlockLocked(); err != nil {
		t.Fatalf("a preauthorized worker past the daily ceiling was refused: %v", err)
	}

	// But a conversation limit stands even under preauthorization: --yes-spend
	// lifts the day, not a ceiling the person put on this one conversation.
	bounded := &Agent{}
	bounded.config.DailyBudgetUSD = 1.00
	bounded.config.DailyBudgetResolved = true
	bounded.config.SpendPreauthorized = true
	bounded.config.SpendRailUSD = 0.50
	bounded.usage.CostUSD = 0.60
	if err := bounded.railBlockLocked(); err == nil {
		t.Fatal("preauthorization lifted a conversation limit it should not")
	} else if !strings.Contains(err.Error(), "conversation limit reached") {
		t.Fatalf("error = %v, want the conversation limit", err)
	}
}

// Raising the day's allowance does not remove a conversation's separate cap.
// The named conversation setting must release the same live agent on retry.
func TestBudgetScopeAndLiveConversationRecovery(t *testing.T) {
	profile := t.TempDir()
	if err := config.WriteDailyBudgetUSD(profile, 20); err != nil {
		t.Fatal(err)
	}
	c := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("carried on"), nil },
	}}
	a, _ := newTestAgent(t, c, func(cfg *Config) { cfg.ProfileDir = profile; cfg.SpendRailUSD = 5 })
	spend(a, 5.5)
	for _, daily := range []float64{20, 1000} {
		if err := config.WriteDailyBudgetUSD(profile, daily); err != nil {
			t.Fatal(err)
		}
		events := collect(t, mustSubmit(t, a, "continue"))
		if len(events) != 1 || !errors.Is(events[0].Err, ErrSpendRail) || !strings.Contains(events[0].Err.Error(), "/budget conversation changes it") {
			t.Fatalf("daily $%.0f: want conversation-specific recovery hint, got %+v", daily, events)
		}
	}
	if c.requests() != 0 {
		t.Fatal("blocked messages reached the model")
	}
	if err := a.SetSpendRail(20); err != nil {
		t.Fatal(err)
	}
	events := collect(t, mustSubmit(t, a, "continue"))
	if events[len(events)-1].Kind != EventTurnDone || messageText(lastMessage(a)) != "carried on" {
		t.Fatalf("raising the conversation limit did not unblock the live agent: %v", kinds(events))
	}
}
