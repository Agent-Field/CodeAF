package tui3

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

type cachedHeldSource struct{ *continuationListing }

func (*cachedHeldSource) SendLease(string) (string, uint64, error) { return heldCell, 2, nil }

func (s *cachedHeldSource) RowsForChatSend(ctx context.Context, _ string, _, held bool) ([]chatlist.Row, uint64, bool, error) {
	if held {
		return []chatlist.Row{{Cell: heldCell, Mine: true, Status: chatlist.Here, Fence: 2}}, 0, true, nil
	}
	rows, err := s.Rows(ctx)
	return rows, 3, false, err
}

type cachedHeldAgent struct{ *fakeAgent }

func (cachedHeldAgent) RenewalStatus() (time.Time, time.Duration, bool) {
	return time.Time{}, time.Second, true
}

// Contract K1 gives listing and notice displacement priority over a cached held flag.
func TestKnownDisplacementOutranksTheCachedWriterHold(t *testing.T) {
	for _, news := range []string{"listing", "notice"} {
		t.Run(news, func(t *testing.T) {
			src := &cachedHeldSource{&continuationListing{row: chatlist.Row{Cell: heldCell, Mine: true, Status: chatlist.Here, Fence: 2}}}
			a, agent := heldLab(t, src, &fakeTaker{})
			keepSurfaceWorkersInTest(t, a)
			a.agent = cachedHeldAgent{agent}
			a.closeHome()
			src.change(func(row *chatlist.Row) {
				row.Status, row.Mine, row.Fence = chatlist.Running, false, 3
				row.Device, row.DeviceID = "other computer", "other"
			})
			if news == "listing" {
				drain(t, a, a.askMachines())
			} else {
				desk := NewNotices()
				a.useNotices(desk)
				desk.Say(chatlist.Superseded("other computer"))
				a.saidNotices()
			}
			if !strings.Contains(plain(lastNote(t, a)), "continued this chat") {
				t.Fatal("displacement was not shown")
			}
			a.input.setText("keep these words unsent")
			drive(t, a, key("enter"))
			if len(agent.sent) != 0 {
				t.Fatalf("known displaced window sent %v", agent.sent)
			}
			if _, card := a.homeAsking(); !card {
				t.Fatal("known displaced window did not raise its continue card")
			}
			if got := string(a.mainComposer().box.value); got != "keep these words unsent" {
				t.Errorf("unsent composer = %q", got)
			}
		})
	}
}
