package syncsetup

import (
	"testing"
	"time"
)

// TestCompletedPublishesNotifySubscribedWindows verifies that WatchPublishes
// listeners run after a sync publish completes, that a listener that schedules
// slow background work does not delay the publish, and that unsubscribing stops
// notifications.
func TestCompletedPublishesNotifySubscribedWindows(t *testing.T) {
	r := newDriveRig(t)
	events := make(chan struct{}, 1)
	slowRelease := make(chan struct{})
	defer close(slowRelease)
	stopSlow := WatchPublishes(func() {
		go func() {
			<-slowRelease
		}()
	})
	defer stopSlow()
	stop := WatchPublishes(func() {
		select {
		case events <- struct{}{}:
		default:
		}
	})
	defer stop()
	var notices noticeLog
	chat := r.startChat(&notices)
	chat.mustSay()
	select {
	case <-events:
	case <-time.After(30 * time.Second):
		t.Fatal("completed publish did not notify its subscriber")
	}
	stop()
	chat.mustSay()
	waitFor(t, "second publish", func() bool { return r.directoryHead(r.cell.ID) == r.head() })
	select {
	case <-events:
		t.Fatal("unsubscribed window received another publish")
	default:
	}
}
