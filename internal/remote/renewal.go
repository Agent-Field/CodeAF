package remote

import "github.com/Agent-Field/codeaf/internal/guard"

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
	source, ok := agent.(interface{ RenewalChanges() <-chan struct{} })
	if !ok {
		return
	}
	changes := source.RenewalChanges()
	if changes == nil {
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
		for {
			select {
			case _, open := <-changes:
				if !open {
					return
				}
				sess.announce()
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
