package tui3

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/remote"
)

// The arrival opens a view on the same host session as the other window.
// Closing that view must not close the shared engine or release its ownership.
func TestAnAbandonedArrivalLeavesAnotherAttachmentRunning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	for _, alreadyHere := range []bool{false, true} {
		t.Run(map[bool]string{false: "arrived", true: "already here"}[alreadyHere], func(t *testing.T) {
			far := &servedAgent{fakeAgent: &fakeAgent{model: "m"}}
			engineAgent := &arrivalHostAgent{servedAgent: far, closed: make(chan struct{})}
			sess := remote.NewSession(&remote.Engine{Agent: engineAgent, ReleaseOnLeave: func(remote.WrappedAgent) bool { return true }}, true)
			t.Cleanup(func() { _ = sess.Close() })
			attach := func() *remote.Client {
				surface, engine := remote.Pipe()
				go func() {
					_ = remote.ServeAttach(engine, engine, remote.AttachOptions{Open: func(remote.Hello) (*remote.Session, error) { return sess, nil }})
					_ = engine.Close()
				}()
				client, err := remote.Dial(surface, "loopback", remote.Hello{Version: remote.Version})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = client.Close() })
				return client
			}
			other, unused := attach(), attach()
			f := &intentTaker{}
			releaseUnusedTake(f, homeTakenMsg{taken: Taken{Fence: 42, AlreadyHere: alreadyHere}}, unused.Agent())
			if sess.Ended() || len(f.released) != 0 {
				t.Fatal("cleanup closed or released another window's conversation")
			}
			if err := other.Take(); err != nil {
				t.Fatal(err)
			}
			if _, err := other.Agent().Submit(context.Background(), "still working"); err != nil {
				t.Fatalf("other attachment stopped working: %v", err)
			}
			far.finish()
			if alreadyHere && sess.Attached() != 2 {
				t.Fatal("already-here cleanup was not skipped")
			}
			if !alreadyHere {
				if sess.Attached() != 1 {
					t.Fatal("unused view was not detached")
				}
				if err := other.Agent().DetachAndWait(); err != nil {
					t.Fatal(err)
				}
				// The final stream may still be draining when detach answers.
				// Its engine close is the last-window release acknowledgment.
				<-engineAgent.closed
				if !sess.Ended() {
					t.Fatal("last-window departure did not release the conversation")
				}
			}
		})
	}
}

type arrivalHostAgent struct {
	*servedAgent
	closed chan struct{}
}

func (a *arrivalHostAgent) Close() error {
	err := a.servedAgent.Close()
	close(a.closed)
	return err
}
