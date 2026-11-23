package session

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// jobChatStub is a factory floor of one job whose own conversation is talk:
// the door answers it for that transcript alone, and keeps what it was given.
type jobChatStub struct {
	talk    string
	refusal string

	mu    sync.Mutex
	asked int
	spent []float64
}

func (s *jobChatStub) Chat(transcript string) JobChat {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked++
	if transcript != s.talk {
		return nil
	}
	return s
}

func (s *jobChatStub) Spend(usd float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spent = append(s.spent, usd)
}

func (s *jobChatStub) Refusal() string { return s.refusal }

func (s *jobChatStub) total() (float64, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var sum float64
	for _, usd := range s.spent {
		sum += usd
	}
	return sum, s.asked
}

// EVERYTHING DONE FOR THE JOB SPENDS FROM ITS ONE BUDGET: every priced call a
// job's own conversation makes goes on the job, the turns' and the errands'
// beside them (naming the conversation is one), and the ledger still has each
// call once, as the person's own record. The door is asked once per agent.
func TestAJobsChatSpendsEveryCallOnTheJobAndTheLedgerOnce(t *testing.T) {
	dir := t.TempDir()
	ledger := filepath.Join(dir, UsageLedgerName)
	talk := filepath.Join(dir, "talk.jsonl")
	door := &jobChatStub{talk: talk}
	answer := func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("noted"), nil }
	completer := pricedAt(&scriptedCompleter{steps: []step{answer, answer}}, 0.25)
	agent, _ := newTestAgent(t, completer, func(c *Config) {
		c.usageLedger, c.SessionFile, c.JobChat = ledger, talk, door
	})
	collect(t, mustSubmit(t, agent, "the brief is: fix the ledger"))
	collect(t, mustSubmit(t, agent, "and keep the meter honest"))
	agent.Close()
	FlushUsage()

	spent, asked := door.total()
	if asked != 1 {
		t.Fatalf("the door was asked %d times, want once per agent", asked)
	}
	rows, err := ReadUsage(ledger, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var onLedger float64
	for _, row := range rows {
		onLedger += row.USD
	}
	door.mu.Lock()
	charges := len(door.spent)
	door.mu.Unlock()
	if spent < 0.5 || charges != len(rows) || spent != onLedger {
		t.Fatalf("the job was charged $%.2f in %d calls and the ledger has $%.2f in %d rows; want both the same, the two turns' $0.50 at least: every call once on each, and the job's figure never added to the ledger", spent, charges, onLedger, len(rows))
	}
}

// A conversation that is no job's own spends nothing on any job.
func TestAConversationThatIsNoJobsChargesNoJob(t *testing.T) {
	dir := t.TempDir()
	door := &jobChatStub{talk: filepath.Join(dir, "the-jobs.jsonl")}
	answer := func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("hi"), nil }
	agent, _ := newTestAgent(t, pricedAt(&scriptedCompleter{steps: []step{answer}}, 0.25), func(c *Config) {
		c.usageLedger, c.SessionFile, c.JobChat = filepath.Join(dir, UsageLedgerName), filepath.Join(dir, "other.jsonl"), door
	})
	collect(t, mustSubmit(t, agent, "hello"))
	if spent, _ := door.total(); spent != 0 {
		t.Fatalf("another conversation charged the job $%.2f", spent)
	}
}

// A JOB'S CHAT STOPS AT THE JOB'S BUDGET the way a step does: the turn is
// refused before it does anything, in the job's own sentence, under the same
// sentinel a conversation limit carries.
func TestAJobsChatAtItsBudgetRefusesTheTurnInTheJobsWords(t *testing.T) {
	dir := t.TempDir()
	talk := filepath.Join(dir, "talk.jsonl")
	const said = "job budget reached · $5.02 spent of $5 · raise the job's budget to go on"
	door := &jobChatStub{talk: talk, refusal: said}
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			t.Error("a refused turn reached the provider")
			return textResponse("should not happen"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(c *Config) { c.SessionFile, c.JobChat = talk, door })

	events := collect(t, mustSubmit(t, agent, "one more thing"))

	if len(events) != 1 || events[0].Kind != EventError {
		t.Fatalf("events = %v, want one EventError", kinds(events))
	}
	if !errors.Is(events[0].Err, ErrSpendRail) || events[0].Err.Error() != said {
		t.Fatalf("error = %v, want the job's own sentence under ErrSpendRail", events[0].Err)
	}
	if completer.requests() != 0 {
		t.Fatalf("requests = %d, want 0", completer.requests())
	}
}
