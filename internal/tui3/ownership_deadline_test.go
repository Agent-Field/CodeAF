package tui3

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
)

type deadlineChatListing struct{}

func (deadlineChatListing) RelayHost() string { return "sync.example" }

func (deadlineChatListing) Rows(context.Context) ([]chatlist.Row, error) {
	return nil, fmt.Errorf("%w: %w", directory.ErrUnreachable, context.DeadlineExceeded)
}

// Contract J22 describes an expired ownership read as a deadline on Home.
func TestHomeOwnershipDeadlineDoesNotClaimTheNetworkIsUnreachable(t *testing.T) {
	a := machinesHome(t, deadlineChatListing{}, 200)
	frame := homeText(a)
	if !strings.Contains(frame, "sync.example did not answer in 25s; still trying") || strings.Contains(frame, "the sync service is not answering") {
		t.Fatalf("ownership read deadline was misreported:\n%s", frame)
	}
}

// Contract J22 keeps a timed-out send unsent, names the deadline, and asks
// again on the next Enter instead of replaying the cached failure.
func TestSendOwnershipDeadlineKeepsWordsAndNamesTheDeadline(t *testing.T) {
	src := &continuationListing{row: leftOffRow(heldCell, "other computer", chatlist.Running, time.Minute)}
	a, agent := heldLab(t, src, &fakeTaker{})
	a.closeHome()
	a.machines = deadlineChatListing{}
	a.input.setText("keep these words")
	drive(t, a, key("enter"))
	if got := plain(lastNote(t, a)); !strings.Contains(got, "sync.example has not answered; your message is still in the box, press enter to send it") || strings.Contains(got, chatlist.Unreachable) {
		t.Fatalf("deadline = %q", got)
	}
	if len(agent.sent) != 0 || a.input.String() != "keep these words" {
		t.Fatal("deadline changed or sent the draft")
	}
	a.machines = src
	drive(t, a, key("enter"))
	if _, card := a.homeAsking(); !card {
		t.Fatal("a cached deadline prevented the next ownership reading")
	}
}
