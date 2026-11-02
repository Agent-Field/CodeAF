package remote

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
)

// sealNoticePoll is how often a host looks again while owed news, and the old
// polling cadence for an agent whose seal watch cannot signal.
const sealNoticePoll = 100 * time.Millisecond

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
	news, hasNews := agent.(interface {
		SealNotices() (<-chan struct{}, bool)
		SealNoticesOwed() bool
	})
	// A watch that signals its news is waited on (events); a conversation that
	// can never be handed any (no seals, a task, an errand) is not watched for
	// it at all; anything else keeps the old poll.
	var sealChanges <-chan struct{}
	events := false
	if sealing && hasNews {
		sealChanges, events = news.SealNotices()
		if !events {
			sealChanges = nil
		} else if sealChanges == nil {
			sealing = false
		}
	}
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
		// An idle healthy chat waits on its watch's news with no timer at all.
		// The timer runs only while the host is owed news it was not woken
		// for: a failing seal, whose recovery is found by asking, or a
		// sentence held for a running turn or for a surface not yet here.
		var tick <-chan time.Time
		var timer *time.Timer
		if sealing && !events {
			legacy := time.NewTicker(sealNoticePoll)
			defer legacy.Stop()
			tick = legacy.C
		}
		defer func() {
			if timer != nil {
				timer.Stop()
			}
		}()
		look := func() bool {
			sess.mu.Lock()
			if sess.closed || sess.generation != generation {
				sess.mu.Unlock()
				return false
			}
			watching := sess.watchingLocked()
			sess.mu.Unlock()
			if len(watching) != 0 {
				for line := seals.TakeSealNotice(); line != ""; line = seals.TakeSealNotice() {
					sess.announce()
					for _, surface := range watching {
						_ = surface.send(Frame{Kind: "notice", Payload: mustJSON(line)})
					}
				}
			}
			if !events {
				return true
			}
			// Timers are synchronous since Go 1.23: Reset and Stop never leave a
			// stale fire behind, so no drain is needed.
			switch owed := news.SealNoticesOwed(); {
			case owed && timer == nil:
				timer = time.NewTimer(sealNoticePoll)
				tick = timer.C
			case owed:
				timer.Reset(sealNoticePoll)
				tick = timer.C
			case timer != nil:
				timer.Stop()
				tick = nil
			}
			return true
		}
		if sealing && events && !look() {
			return
		}
		for {
			select {
			case _, open := <-changes:
				if !open {
					return
				}
				sess.announce()
			case <-sealChanges:
				if !look() {
					return
				}
			case <-tick:
				if !look() {
					return
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
