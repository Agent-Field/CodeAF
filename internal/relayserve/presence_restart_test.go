package relayserve_test

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
)

func TestNativePresenceP2RestartKeepsUnseenComputerUnknown(t *testing.T) {
	r := newRig(t)
	id, a := newIdentity(t)
	b := newDevice(t, id, t.TempDir())
	for _, d := range []device{a, b} {
		if err := r.dir(d).PutDevice(bg, d.dev.ID(), directory.Device{V: 1}); err != nil {
			t.Fatal(err)
		}
	}
	r.restart()
	view, err := r.dir(a).Presence(bg)
	if err != nil {
		t.Fatal(err)
	}
	if _, known := view.Devices[b.dev.ID()]; known {
		t.Fatalf("P2 native restart invented offline presence: %+v", view)
	}
	conn, _, err := directorytest.DialWatch(bg, r.srv.URL, r.signer(b))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if _, _, err = conn.Read(bg); err != nil {
		t.Fatal(err)
	}
	view, err = r.dir(a).Presence(bg)
	if err != nil || !view.Devices[b.dev.ID()].Online {
		t.Fatalf("P2 native relay did not observe returned computer: %+v, %v", view, err)
	}
	r.clock.Advance(directory.Window(directory.DefaultBeat) + time.Second)
	view, err = r.dir(a).Presence(bg)
	if peer, known := view.Devices[b.dev.ID()]; err != nil || !known || peer.Online {
		t.Fatalf("P2 native relay did not report actual lapse: %+v, %v", view, err)
	}
}
