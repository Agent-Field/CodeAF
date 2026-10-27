package remote

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
)

// watchRenewals forwards idle health changes through the ordinary cached facts.
// A swap replaces the subscription so the previous writer cannot keep it alive.
func (sess *Session) watchRenewals() {
	sess.mu.Lock()
	if sess.renewalStop != nil {
		close(sess.renewalStop)
		sess.renewalStop = nil
	}
	agent, generation := sess.agent, sess.generation
	closed := sess.closed
	sess.mu.Unlock()
	if closed {
		return
	}
	var changes <-chan struct{}
	if source, ok := agent.(interface{ RenewalChanges() <-chan struct{} }); ok {
		changes = source.RenewalChanges()
	}
	seals, sealing := agent.(interface{ TakeSealNotice() string })
	if changes == nil && !sealing {
		return
	}
	stop := make(chan struct{})
	sess.mu.Lock()
	if sess.closed || sess.generation != generation {
		sess.mu.Unlock()
		return
	}
	sess.renewalStop = stop
	sess.pumps.Add(1)
	sess.mu.Unlock()
	guard.Go("chat renewal changes", func() {
		defer sess.pumps.Done()
		var tick <-chan time.Time
		if sealing {
			timer := time.NewTicker(100 * time.Millisecond)
			defer timer.Stop()
			tick = timer.C
		}
		for {
			select {
			case _, open := <-changes:
				if !open {
					return
				}
				sess.announce()
			case <-tick:
				sess.mu.Lock()
				if sess.closed || sess.generation != generation {
					sess.mu.Unlock()
					return
				}
				watching := sess.watchingLocked()
				sess.mu.Unlock()
				if len(watching) == 0 {
					continue
				}
				for line := seals.TakeSealNotice(); line != ""; line = seals.TakeSealNotice() {
					sess.announce()
					for _, surface := range watching {
						_ = surface.send(Frame{Kind: "notice", Payload: mustJSON(line)})
					}
				}
			case <-stop:
				return
			}
		}
	})
}

func (sess *Session) stopRenewals() {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.renewalStop != nil {
		close(sess.renewalStop)
		sess.renewalStop = nil
	}
}
