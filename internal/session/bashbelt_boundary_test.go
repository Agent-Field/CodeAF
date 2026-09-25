package session

import (
	"context"
	"testing"
	"testing/synctest"
)

// The event reader may take arbitrarily longer than the next model call.
// Prove the boundary waits on its acknowledgement, without relying on either
// goroutine winning a race or on a sleep to make the reader seem slow.
func TestBeltStepWaitsForItsReaderAndCancellationReleasesIt(t *testing.T) {
	for _, cancelInstead := range []bool{false, true} {
		t.Run(map[bool]string{false: "acknowledge", true: "cancel"}[cancelInstead], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				hub := newEventHub()
				defer hub.close()
				events := hub.subscribe()
				agent := &Agent{config: Config{WaitForBeltSteps: true}}
				returned := make(chan struct{})
				go func() {
					agent.sendBeltStep(ctx, hub, Event{Kind: EventToolEnd})
					close(returned)
				}()
				event := <-events
				if event.BeltStepHandled == nil {
					t.Fatal("the step has no acknowledgement")
				}
				synctest.Wait()
				select {
				case <-returned:
					t.Fatal("the next action could start before the reader handled this step")
				default:
				}
				if cancelInstead {
					cancel()
				} else {
					close(event.BeltStepHandled)
				}
				<-returned
			})
		})
	}
}

func TestOrdinaryToolEventsDoNotWaitForTheirReader(t *testing.T) {
	hub := newEventHub()
	defer hub.close()
	events := hub.subscribe()
	agent := &Agent{}
	agent.sendBeltStep(context.Background(), hub, Event{Kind: EventToolEnd})
	if event := <-events; event.BeltStepHandled != nil {
		t.Fatal("an ordinary agent unexpectedly requires a step acknowledgement")
	}
}
