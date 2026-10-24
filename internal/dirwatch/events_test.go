package dirwatch

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func ev(e Event) Frame { return Frame{Event: &e} }

func TestEventsFillOnlineAndJoinedAndLeaveTheVersionAlone(t *testing.T) {
	list := make(chan *fakeStream, 4)
	r := newRig(t, sockets(list))
	s := <-list
	s.send(Frame{Version: 7}, nil)
	s.send(ev(PresenceEvent("dev_b", true, 1)), nil)
	s.send(ev(PresenceEvent("dev_a", true, 2)), nil)
	s.send(ev(JoinedEvent("dev_c", "bg==", "linux", 3)), nil)
	s.send(ev(PresenceEvent("dev_b", false, 4)), nil)
	s.send(Frame{Pong: true}, nil)
	s.send(Frame{Pong: true}, nil)
	got := r.sub.State()
	if got.Version != 7 {
		t.Fatalf("an event moved the version to %d", got.Version)
	}
	if !reflect.DeepEqual(got.Online, []string{"dev_a"}) {
		t.Fatalf("online was %v, want [dev_a]", got.Online)
	}
	want := []Joined{{Seq: 1, Device: "dev_c", Name: "bg==", Platform: "linux", At: 3}}
	if !reflect.DeepEqual(got.Joined, want) {
		t.Fatalf("joined was %+v", got.Joined)
	}
}

func TestARevokedDeviceIsNoLongerOnlineAndAnUnknownEventIsIgnored(t *testing.T) {
	v := view{}.withEvent(PresenceEvent("dev_b", true, 1))
	v = v.withEvent(Event{T: "from-the-future", Device: "dev_b"})
	if len(v.online) != 1 {
		t.Fatalf("an unknown event changed the view: %+v", v)
	}
	if v = v.withEvent(RevokedEvent("dev_b", 2)); len(v.online) != 0 {
		t.Fatalf("a revoked device stayed online: %+v", v)
	}
}

func TestJoinedKeepsTheNewestEightWithRisingSeq(t *testing.T) {
	var v view
	for i := 0; i < MaxJoined+3; i++ {
		v = v.withEvent(JoinedEvent("d", "", "", int64(i)))
	}
	if len(v.joined) != MaxJoined || v.joined[0].Seq != 4 || v.joined[MaxJoined-1].Seq != MaxJoined+3 {
		t.Fatalf("joined ring is %+v", v.joined)
	}
}

func TestAStateHandedOutIsNotEditedByLaterEvents(t *testing.T) {
	v := view{}.withEvent(PresenceEvent("dev_a", true, 1))
	before := v.online
	v.withEvent(PresenceEvent("dev_b", true, 2))
	v.withEvent(PresenceEvent("dev_a", false, 3))
	if !reflect.DeepEqual(before, []string{"dev_a"}) {
		t.Fatalf("a published slice changed: %v", before)
	}
}

func TestWhenTheSocketDropsNobodyIsKnownOnlineButJoinsStay(t *testing.T) {
	list := make(chan *fakeStream, 4)
	r := newRig(t, sockets(list))
	s := <-list
	s.send(ev(PresenceEvent("dev_b", true, 1)), nil)
	s.send(ev(JoinedEvent("dev_c", "", "linux", 2)), nil)
	s.send(Frame{}, errors.New("reset"))
	deadline := time.Now().Add(5 * time.Second)
	for r.sub.State().Up && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	got := r.sub.State()
	if got.Up || len(got.Online) != 0 || len(got.Joined) != 1 {
		t.Fatalf("state after the drop was %+v", got)
	}
}
