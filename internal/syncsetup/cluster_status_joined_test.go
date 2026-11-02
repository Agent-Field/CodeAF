package syncsetup

import (
	"context"
	"testing"
	"time"
)

// Contract E6 keeps a joined computer's status off its local member lock and store.
func TestJoinedClusterStatusDoesNotReopenTheMemberStore(t *testing.T) {
	r := newMemberRig(t)
	s := r.home(nil)
	readyMember(t, s)
	s.stopMember()
	s.memberMu.Lock()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		status, err := s.ClusterStatus(ctx)
		if err == nil && status.Self == 0 {
			err = errMemberWaiting
		}
		done <- err
	}()
	select {
	case err := <-done:
		s.memberMu.Unlock()
		mustNil(t, err)
	case <-time.After(2 * time.Second):
		s.memberMu.Unlock()
		<-done
		t.Fatal("status waited for the local member lock instead of reading the relay")
	}
}
