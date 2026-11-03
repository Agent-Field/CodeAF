package tui3

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

type r10CachedWriter struct {
	*fakeAgent
	source  *r10RetrySource
	held    atomic.Bool
	read    chan struct{}
	changed chan struct{}
}

func (a *r10CachedWriter) CheckWorkContext(context.Context) error {
	a.source.held.Store(true)
	return nil
}
func (a *r10CachedWriter) WriterChanges() <-chan struct{} { return a.changed }
func (a *r10CachedWriter) RenewalStatus() (time.Time, time.Duration, bool) {
	held := a.held.Load()
	if !held {
		select {
		case a.read <- struct{}{}:
		default:
		}
	}
	return time.Time{}, time.Second, held
}

func TestIdleRelease_R10_ConfirmedRetryWaitsForCachedWriter(t *testing.T) {
	row := chatlist.Row{Cell: heldCell, Status: chatlist.Here, Mine: true, Fence: 3, Head: "saved"}
	source := &r10RetrySource{continuationListing: &continuationListing{row: row}}
	agent := &r10CachedWriter{fakeAgent: &fakeAgent{}, source: source, read: make(chan struct{}, 1), changed: make(chan struct{})}
	done := make(chan ownershipReadMsg, 1)
	release := sync.OnceFunc(func() { agent.held.Store(true); close(agent.changed) })
	joined := false
	defer func() {
		release()
		if !joined {
			<-done
		}
	}()
	go func() {
		done <- resumeIdleSend(t.Context(), agent, source, ownershipReadMsg{retryIdle: true, head: "saved", leaseCell: heldCell, reading: homeMachinesMsg{rows: []chatlist.Row{row}}}, false)
	}()
	select {
	case <-agent.read:
	case <-time.After(time.Second):
		t.Fatal("no cached writer read after admission")
	}
	release()
	msg := <-done
	joined = true
	if msg.admissionErr != nil || !msg.local || msg.fence != 3 {
		t.Fatalf("confirmed own receipt became an availability refusal: %v; local=%v fence=%d", msg.admissionErr, msg.local, msg.fence)
	}
}
