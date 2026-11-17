package tui3

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/session"
)

type r10RetrySource struct {
	*continuationListing
	held  atomic.Bool
	fence atomic.Uint64
}

func (*r10RetrySource) RelayHost() string { return "slow.example" }
func (s *r10RetrySource) SendLease(string) (string, uint64, error) {
	if !s.held.Load() {
		return "", 0, os.ErrNotExist
	}
	fence := s.fence.Load()
	if fence == 0 {
		fence = 3
	}
	return heldCell, fence, nil
}

type r10RetryAgent struct {
	*fakeAgent
	source  *r10RetrySource
	budgets []time.Duration
	first   error
	unknown int
}

func (a *r10RetryAgent) RenewalStatus() (time.Time, time.Duration, bool) {
	return time.Time{}, time.Second, a.source.held.Load()
}
func (a *r10RetryAgent) CheckWorkContext(ctx context.Context) error {
	deadline, _ := ctx.Deadline()
	a.budgets = append(a.budgets, time.Until(deadline))
	if len(a.budgets) <= max(1, a.unknown) {
		a.source.change(func(row *chatlist.Row) {
			row.Status, row.Fence, row.Released, row.AutoOpen = chatlist.Here, 3, false, false
		})
		return a.first
	}
	if a.unknown > 1 {
		a.source.fence.Store(4)
	}
	a.source.held.Store(true)
	return nil
}

func TestIdleRelease_R10_RetryableTakeBackKeepsDraftAndAvoidsSelfContinue(t *testing.T) {
	r10RetryTakeBack(t, &executor.WorkRefusal{Line: executor.TakeBackRetryLine, Retryable: true}, 1)
}

func TestIdleRelease_R10_ClientDeadlineKeepsDraftAndAvoidsSelfContinue(t *testing.T) {
	r10RetryTakeBack(t, context.DeadlineExceeded, 1)
}

func TestIdleRelease_R10b_UncertainOwnFenceKeepsDraftUntilLapsedRetry(t *testing.T) {
	r10RetryTakeBack(t, &executor.WorkRefusal{Line: executor.TakeBackRetryLine, Retryable: true}, 2)
}

func r10RetryTakeBack(t *testing.T, first error, unknown int) {
	t.Setenv("CODEAF_IDLE_HANDOFF", "on")
	source := &r10RetrySource{continuationListing: &continuationListing{row: chatlist.Row{Cell: heldCell, Status: chatlist.Idle, Mine: true, Released: true, AutoOpen: true, Fence: 2, Head: "saved", DeviceID: "A"}}}
	taker := &fakeTaker{}
	a, old := heldLab(t, source, taker)
	keepSurfaceWorkersInTest(t, a)
	a.closeHome()
	root := session.DirOf(a.file)
	if err := os.MkdirAll(filepath.Join(root, ".cell"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".cell", "adopted.json"), []byte(`{"V":1,"head":"saved","turns_size":0}`), 0600); err != nil {
		t.Fatal(err)
	}
	agent := &r10RetryAgent{fakeAgent: old, source: source, first: first, unknown: unknown}
	a.agent = agent
	a.input.setText("keep these words")
	drive(t, a, key("enter"))
	if len(old.sent) != 0 || a.input.String() != "keep these words" {
		t.Fatal("uncertain take-back spent the draft")
	}
	if _, card := a.homeAsking(); card {
		t.Fatal("uncertain take-back offered Continue here")
	}
	var notes strings.Builder
	for _, entry := range a.entries {
		if entry.kind == entryNote {
			notes.WriteString(plain(entry.text))
		}
	}
	if !strings.Contains(notes.String(), "waiting for slow.example; still checking") {
		t.Fatalf("missing take-back wait: %s", notes.String())
	}
	if unknown > 1 {
		drive(t, a, key("enter"))
		if len(old.sent) != 0 || a.input.String() != "keep these words" {
			t.Fatal("live uncertain fence spent the draft")
		}
		if _, card := a.homeAsking(); card {
			t.Fatal("live uncertain own fence offered Continue here")
		}
		// The relay listing now reports expiry; the next admission must
		// return a newly acquired fence rather than the uncertain fence.
		source.change(func(row *chatlist.Row) { row.Status = chatlist.Idle })
	}
	drive(t, a, key("enter"))
	if len(agent.budgets) != unknown+1 || len(old.sent) != 1 || len(taker.cells) != 0 {
		t.Fatalf("retry failed: checks=%d sent=%v takes=%v", len(agent.budgets), old.sent, taker.cells)
	}
	for _, budget := range agent.budgets {
		if budget < 10*time.Second || budget > executor.TakeBackWithin {
			t.Fatalf("take-back budget=%s", budget)
		}
	}
	if _, card := a.homeAsking(); card {
		t.Fatal("own held fence offered Continue here")
	}
}
