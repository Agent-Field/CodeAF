package chat

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The store is the backend this door was written for, and the assertion is a
// compile-time one on purpose: [RoomReuser] is asked for by type assertion at
// runtime, so a signature that drifted apart from the store's would not fail to
// build — it would fail to MATCH, and the window would go back to minting an
// untitled room per press with nothing on screen saying so.
var _ RoomReuser = (*store.Store)(nil)

// reusingRooms is a backend that can tell an empty room from a conversation.
type reusingRooms struct {
	*boardBackend
	standing store.Session
	asked    int
}

func (r *reusingRooms) OpenOrReuseSession(id, surface string) (store.Session, bool, error) {
	r.asked++
	if r.standing.ID != "" {
		return r.standing, true, nil
	}
	opened, err := r.boardBackend.OpenSession(id, "", surface)
	return opened, false, err
}

// `+ new room` pressed against an empty room already standing walks into it.
func TestNewRoomWalksIntoTheEmptyRoomAlreadyStanding(t *testing.T) {
	backend := &reusingRooms{boardBackend: board(), standing: store.Session{ID: "already-empty"}}
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)

	cmd := app.openRoomCmd()
	if cmd == nil {
		t.Fatal("the new-room door produced no command")
	}
	opened, ok := cmd().(roomOpenedMsg)
	if !ok {
		t.Fatalf("the door answered with %T", cmd())
	}
	if opened.err != nil {
		t.Fatal(opened.err)
	}
	if opened.session.ID != "already-empty" {
		t.Fatalf("the door opened %q, want the empty room already standing", opened.session.ID)
	}
	if backend.asked != 1 {
		t.Fatalf("the reuse door was asked %d times, want once", backend.asked)
	}
	if len(backend.opened) != 0 {
		t.Fatalf("a room was minted anyway: %v", backend.opened)
	}
}

// A backend that cannot answer the question mints, exactly as it did before the
// door existed. The capability is optional and its absence is not a failure.
func TestNewRoomStillMintsWhenTheBackendCannotReuse(t *testing.T) {
	backend := board()
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)

	cmd := app.openRoomCmd()
	if cmd == nil {
		t.Fatal("the new-room door produced no command")
	}
	opened, ok := cmd().(roomOpenedMsg)
	if !ok {
		t.Fatalf("the door answered with %T", cmd())
	}
	if opened.err != nil {
		t.Fatal(opened.err)
	}
	if len(backend.opened) != 1 || backend.opened[0] != opened.session.ID {
		t.Fatalf("the door minted %v and opened %q", backend.opened, opened.session.ID)
	}
}
