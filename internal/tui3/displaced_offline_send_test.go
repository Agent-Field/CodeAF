package tui3

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
)

type unheldOfflineSource struct{ *continuationListing }

func (unheldOfflineSource) RowsForChatSend(context.Context, string, bool, bool) ([]chatlist.Row, uint64, bool, error) {
	return nil, 0, false, directory.ErrUnreachable
}
func (unheldOfflineSource) SendLease(string) (string, uint64, error) { return "", 0, os.ErrNotExist }

type displacedSendAgent struct{ *fakeAgent }

func (displacedSendAgent) RenewalStatus() (time.Time, time.Duration, bool) {
	return time.Time{}, time.Second, false
}

// Contract F1 keeps a displaced copy unsent even after its receipt and listing are gone.
func TestDisplacedOfflineWindowCannotBorrowAnEmptyListing(t *testing.T) {
	src := unheldOfflineSource{&continuationListing{err: directory.ErrUnreachable}}
	a, agent := heldLab(t, src, &fakeTaker{})
	keepSurfaceWorkersInTest(t, a)
	a.closeHome()
	a.machineRead.rows = nil
	a.machineRead.sendHolds = nil
	a.agent = displacedSendAgent{agent}
	a.input.setText("keep these words here")
	drive(t, a, key("enter"))
	if len(agent.sent) != 0 || a.input.String() != "keep these words here" {
		t.Fatalf("offline copy sent: %v, draft=%q", agent.sent, a.input.String())
	}
	if !strings.Contains(strings.Join(noteTexts(a), "\n"), chatlist.Unreachable) {
		t.Fatal("the unsent draft has no explanation")
	}
}

type localEmptyAfterHoldSource struct{ *continuationListing }

func (localEmptyAfterHoldSource) RowsForChatSend(context.Context, string, bool, bool) ([]chatlist.Row, uint64, bool, error) {
	return nil, 0, true, nil
}
func (localEmptyAfterHoldSource) SendLease(string) (string, uint64, error) {
	return "", 0, os.ErrNotExist
}

// A window with sync wired that previously recorded a fence must never treat an
// empty local reading as permission to send after losing its hold.
func TestDisplacedWindowWithLocalEmptyReadingRefusesSend(t *testing.T) {
	src := localEmptyAfterHoldSource{&continuationListing{
		row: chatlist.Row{Cell: heldCell, Mine: true, Status: chatlist.Here, Fence: 5},
	}}
	a, agent := heldLab(t, src, &fakeTaker{})
	keepSurfaceWorkersInTest(t, a)
	a.closeHome()
	if got := a.machineRead.sendHolds[heldCell].fence; got != 5 {
		t.Fatalf("sendHolds[%s].fence = %d, want 5", heldCell, got)
	}
	a.agent = displacedSendAgent{agent}
	a.input.setText("do not fork this chat")
	drive(t, a, key("enter"))
	if len(agent.sent) != 0 || a.input.String() != "do not fork this chat" {
		t.Fatalf("displaced window sent: sent=%v, draft=%q", agent.sent, a.input.String())
	}
	if !strings.Contains(strings.Join(noteTexts(a), "\n"), chatlist.Superseded("another device")) {
		t.Fatalf("expected superseded notice %q, got %v", chatlist.Superseded("another device"), noteTexts(a))
	}
}
