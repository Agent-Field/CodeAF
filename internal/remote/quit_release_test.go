package remote

import (
	"testing"
	"testing/synctest"
	"time"
)

// Contract D2 keeps the saving result in every idle writer's departure reply.
func TestDetachAcknowledgesOnlyAfterClosingRelease(t *testing.T) {
	for _, ordinary := range []bool{false, true} {
		t.Run(map[bool]string{false: "coordinating", true: "ordinary"}[ordinary], func(t *testing.T) {
			far := newCloseGatedAgent()
			engine := &Engine{Agent: far, ReleaseOnLeave: func(WrappedAgent) bool { return true }}
			client := viewOn(t, NewSession(engine, true))
			done := make(chan error, 1)
			go func() {
				if ordinary {
					done <- client.Agent().Detach()
				} else {
					done <- client.Agent().DetachAndWait()
				}
			}()
			select {
			case <-far.entered:
			case <-done:
				close(far.release)
				_ = client.Close()
				t.Fatal("detach acknowledged before starting the closing release")
			case <-time.After(time.Second):
				close(far.release)
				t.Fatal("detach did not start the closing release")
			}
			acknowledged := false
			select {
			case <-done:
				acknowledged = true
				t.Error("detach acknowledged before the closing release finished")
			default:
			}
			close(far.release)
			if !acknowledged {
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// A clean quit requires an overlapping closing boundary to join the release already
// in progress, rather than treating the early closed flag as a completed save.
func TestConcurrentCloseJoinsTheClosingRelease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		far := newCloseGatedAgent()
		sess := NewSession(&Engine{Agent: far}, true)
		first, second := make(chan error, 1), make(chan error, 1)
		go func() { first <- sess.Close() }()
		<-far.entered
		go func() { second <- sess.Close() }()
		synctest.Wait()
		acknowledged := false
		select {
		case <-second:
			acknowledged = true
			t.Error("overlapping close returned before its release finished")
		default:
		}
		close(far.release)
		if err := <-first; err != nil {
			t.Fatal(err)
		}
		if !acknowledged {
			if err := <-second; err != nil {
				t.Fatal(err)
			}
		}
	})
}
