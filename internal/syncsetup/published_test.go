package syncsetup

import "testing"

func TestPublishListenersStopWithTheirWindow(t *testing.T) {
	calls := 0
	stop := WatchPublishes(func() { calls++ })
	published()
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
	stop()
	published()
	if calls != 1 {
		t.Fatal("closed window still called")
	}
}
