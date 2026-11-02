package directory

import (
	"testing"
	"time"
)

func TestPresenceP1QueriesRespectReconnectDebounce(t *testing.T) {
	m := NewMemory(func() time.Time { return time.UnixMilli(5000) })
	ts := &timers{}
	m.feed.SetAfter(ts.after)
	a, b := m.For("dev_a"), m.For("dev_b")
	if err := a.PutDevice(t.Context(), "dev_a", Device{V: 1}); err != nil {
		t.Fatal(err)
	}
	sub, err := m.feed.Subscribe("dev_a", nil)
	if err != nil {
		t.Fatal(err)
	}
	sub.Close()
	view, err := b.Presence(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !view.Devices["dev_a"].Online {
		t.Fatal("P1 presence query marked a freshly disconnected computer offline during reconnect debounce")
	}
	ts.fire()
	view, err = b.Presence(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if view.Devices["dev_a"].Online {
		t.Fatal("P1 computer stayed online after presence lapsed")
	}
}
