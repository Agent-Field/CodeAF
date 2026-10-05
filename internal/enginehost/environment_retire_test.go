package enginehost

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// C1 and F1: Only environment replacement disregards a disconnected window's
// grace. Ordinary stand-down and status retain the stalled-pipe protection.
func TestEnvironmentRetirementDisregardsOnlyTheDisconnectedWatchGrace(t *testing.T) {
	was := remote.WatchFor
	remote.WatchFor = time.Hour
	t.Cleanup(func() { remote.WatchFor = was })
	workspace := hostHolding(t, &workingAgent{})
	client := dialHost(t, workspace)
	// A pipe close retains the grace; an explicit Detach already clears it.
	_ = client.Close()
	waitUntil(t, "the window disconnected", func() bool {
		self, err := Ask(workspace, remote.WhoIs{})
		return err == nil && self.Surfaces == 0 && self.Busy
	})
	if err := Retire(workspace, false); !errors.Is(err, ErrHostBusy) {
		t.Fatalf("ordinary retirement lost the grace: %v", err)
	}
	if self, err := Ask(workspace, remote.WhoIs{IgnoreWatchGrace: true}); err != nil || !self.Busy {
		t.Fatalf("a status probe changed the idle policy: %+v, %v", self, err)
	}
	if err := RetireForEnvironment(workspace); err != nil {
		t.Fatalf("recent act preserved the old environment: %v", err)
	}
}

// C2 and F1: Even without a stream or an attached window, a real host and
// session keep outstanding questions and handed-off work alive on this road.
func TestEnvironmentRetirementKeepsQuestionsAndHandedOffWork(t *testing.T) {
	for _, mode := range []string{"question", "task", "job", "run"} {
		t.Run(mode, func(t *testing.T) {
			far := &workingAgent{}
			workspace := hostHolding(t, far)
			first := dialHost(t, workspace)
			before, err := Ask(workspace, remote.WhoIs{})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "question" {
				events, err := first.Agent().Submit(t.Context(), "ask before continuing")
				if err != nil {
					t.Fatal(err)
				}
				far.ask(session.Event{Kind: session.EventConsentRequest, ID: 33})
				far.finish()
				for range events {
				}
			} else {
				far.setWork([]session.WorkNode{{ID: mode + ":4", Title: "still going", State: session.WorkRunning}})
			}
			if err := first.Agent().Detach(); err != nil {
				t.Fatal(err)
			}
			_ = first.Close()
			waitUntil(t, "the window disconnected", func() bool {
				self, err := Ask(workspace, remote.WhoIs{})
				return err == nil && self.Surfaces == 0
			})
			if err := RetireForEnvironment(workspace); !errors.Is(err, ErrHostBusy) {
				t.Fatalf("%s did not preserve its host: %v", mode, err)
			}
			if after, err := Ask(workspace, remote.WhoIs{}); err != nil || after.PID != before.PID || after.Retiring {
				t.Fatalf("host changed under %s: %+v, %v", mode, after, err)
			}
		})
	}
}

// F1: Before the sweep removes it, an ended real conversation still has its
// last act and can read busy itself; neither host idle reading counts it.
func TestEndedConversationCannotKeepItsHostBusyBeforeTheSweep(t *testing.T) {
	shortHome(t)
	far := &workingAgent{}
	host := stubHostAround(t, "/test/ended", far)
	host.live = 1 // The real listener counts the accepted pipe before attach.
	surface, engine := net.Pipe()
	go host.attach(engine)
	client, err := remote.Dial(surface, "", remote.Hello{Version: remote.Version})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Agent().Close(); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	waitUntil(t, "the ended window disconnected", func() bool {
		host.mu.Lock()
		defer host.mu.Unlock()
		return host.live == 0
	})
	held := host.held()
	if len(held) != 1 || !held[0].Ended() {
		t.Fatal("fixture was swept before checking the ended conversation")
	}
	if !held[0].IdleSince().IsZero() {
		t.Fatal("fixture did not retain the ended conversation's watch grace")
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	if !host.idleLocked() || !host.idleWithWatchGraceLocked(true) {
		t.Fatal("an ended conversation made its host busy")
	}
}
