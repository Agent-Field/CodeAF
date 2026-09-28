package tui3

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/standing"
)

func TestStandingHistoryActionKindFormatting(t *testing.T) {
	cardTask := &standingCard{
		name:   "nightly-sync",
		update: "fired",
		text:   "run sync and check logs",
		item: standing.Item{
			Does: standing.Action{
				Kind:  standing.ActionTask,
				Brief: "run sync and check logs",
			},
		},
	}
	if got := standActivityUpdateWord(cardTask); got != "task: run sync and check logs" {
		t.Fatalf("task action fired got %q, want %q", got, "task: run sync and check logs")
	}

	cardTaskEmpty := &standingCard{
		name:   "nightly-sync",
		update: "fired",
		text:   "",
		item: standing.Item{
			Does: standing.Action{
				Kind: standing.ActionTask,
			},
		},
	}
	if got := standActivityUpdateWord(cardTaskEmpty); got != "task" {
		t.Fatalf("task action fired empty text got %q, want %q", got, "task")
	}

	cardSay := &standingCard{
		name:   "morning-reminder",
		update: "fired",
		text:   "check email",
		item: standing.Item{
			Does: standing.Action{
				Kind: standing.ActionSay,
				Say:  "check email",
			},
		},
	}
	if got := standActivityUpdateWord(cardSay); got != "said: check email" {
		t.Fatalf("say action fired got %q, want %q", got, "said: check email")
	}
}
