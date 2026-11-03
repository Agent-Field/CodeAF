package session

import "time"

const conversationTimeLimitReason = "a time limit you set stopped it"

// The launch wall owns the conversation's work, not the attached window. A
// window can detach without ending a run; expiry is an explicit terminal limit.
func (a *Agent) armConversationWall() {
	a.mu.Lock()
	if a.closed || a.wallStop != nil {
		a.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	a.wallStop = stop
	left := time.Until(a.startedAt.Add(a.config.Budget.Wall))
	a.mu.Unlock()
	go func() {
		timer := time.NewTimer(left)
		defer timer.Stop()
		select {
		case <-stop:
			return
		case <-timer.C:
		}
		a.endConversationOnWall()
	}()
}

func (a *Agent) endConversationOnWall() {
	a.mu.Lock()
	if a.closed || a.wallEndingTaken {
		a.mu.Unlock()
		return
	}
	a.wallEndingTaken = true
	a.mu.Unlock()
	if err := a.stopWorkFor(conversationTimeLimitReason, StopByTimeLimit); err != nil {
		return
	}
	a.mu.Lock()
	done := a.workStopDone
	a.mu.Unlock()
	if done != nil {
		<-done
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	// The terminal error is published only after owned producers have joined.
	for _, lane := range a.wakeLanes {
		stream := newEventStream()
		select {
		case lane <- stream.out:
			stream.send(Event{Kind: EventError, Err: launchBudgetReached{said: conversationTimeLimitWord}})
			stream.close()
		default:
			stream.close()
		}
	}
}
