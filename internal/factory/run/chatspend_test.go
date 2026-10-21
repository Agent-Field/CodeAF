package run

import (
	"fmt"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE CHAT'S SPEND BEFORE START IS THE RUN'S FIRST MONEY. A start carries the
// job's ChatSpent into the new run and zeroes it; the worker that pulls the
// queued run and starts it again carries the same money once more rather than
// dropping it, because none of its steps began; a run whose steps began starts
// from nothing, as a new run always has.
func TestAStartCarriesTheChatsSpendIntoTheRun(t *testing.T) {
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	it := factory.Item{ID: 1, State: factory.StateNew, ChatSpent: 0.4,
		Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}}}
	if err := QueueItem(&it, now); err != nil {
		t.Fatal(err)
	}
	if it.Stream == nil || it.Stream.Spent != 0.4 || it.ChatSpent != 0 {
		t.Fatalf("after start: stream %+v, chat %v; want the run holding $0.40 and the chat's own figure zeroed", it.Stream, it.ChatSpent)
	}
	if got := factory.JobSpent(it); got != 0.4 {
		t.Fatalf("the job's spend is $%.2f, want $0.40", got)
	}

	// The worker that pulled the queued job starts it on its mirror.
	it.State = factory.StateNew
	if err := QueueItem(&it, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if it.Stream.Spent != 0.4 {
		t.Fatalf("a run no step of which began lost its money: $%.2f", it.Stream.Spent)
	}

	// A run whose step ran, then Run again: the new run's spend is its own.
	it.Stream.Phases[0].State, it.Stream.Phases[0].Round = factory.PhaseFailed, 1
	it.Stream.Spent = 2
	it.State = factory.StateNew
	if err := QueueItem(&it, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if it.Stream.Spent != 0 {
		t.Fatalf("a new run after one whose steps ran starts at $%.2f, want nothing", it.Stream.Spent)
	}
}

// A job's chat books where the job's money is: before any run on the item,
// with a run on the run.
func TestTheChatsSpendGoesWhereTheJobsMoneyIs(t *testing.T) {
	var it factory.Item
	factory.AddChatSpend(&it, 0.1)
	factory.AddChatSpend(&it, 0)
	if it.ChatSpent != 0.1 || it.Stream != nil {
		t.Fatalf("before a run: chat %v, stream %+v", it.ChatSpent, it.Stream)
	}
	it.Stream = &factory.Stream{Spent: 1}
	factory.AddChatSpend(&it, 0.25)
	if it.Stream.Spent != 1.25 || it.ChatSpent != 0.1 || factory.JobSpent(it) != 1.25 {
		t.Fatalf("with a run: stream $%v, chat $%v", it.Stream.Spent, it.ChatSpent)
	}
}

// The chat refuses a turn once the job's budget is spent, before a run as
// during one, and never while the job waits on its budget question, which the
// person answers by typing into that chat.
func TestTheChatRefusesAtTheJobsBudget(t *testing.T) {
	cases := []struct {
		name string
		it   factory.Item
		want string
	}{
		{"no budget", factory.Item{ChatSpent: 9}, ""},
		{"under it", factory.Item{Cap: 5, ChatSpent: 4.99}, ""},
		{"spent before a run", factory.Item{Cap: 5, ChatSpent: 5}, fmt.Sprintf(ChatBudgetReached, "$5", "$5")},
		{"spent on the run", factory.Item{Cap: 5, Stream: &factory.Stream{Spent: 5.02}}, fmt.Sprintf(ChatBudgetReached, "$5.02", "$5")},
		{"asking about it", factory.Item{Cap: 5, QKind: "cap", Stream: &factory.Stream{Spent: 5.02}}, fmt.Sprintf(ChatBudgetReached, "$5.02", "$5")},
	}
	for _, c := range cases {
		if got := ChatRefusal(c.it); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
