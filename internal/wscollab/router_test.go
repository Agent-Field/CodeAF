package wscollab

import (
	"context"
	"errors"
	"testing"
)

func TestNewRefusesANilStore(t *testing.T) {
	_, err := New(nil)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestAcceptRecordProcessAreDistinct(t *testing.T) {
	ctx := context.Background()
	router, seam := liveRouter(t)
	got, err := router.Deliver(ctx, OriginPerson, Message{From: "mgr", Body: "status?", CauseID: "c1"}, []string{"chat-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d receipts", len(got))
	}
	rec := got[0]
	if rec.Queue != QueueAccepted {
		t.Fatalf("queue=%s; a live reader accepted", rec.Queue)
	}
	if !rec.Recorded {
		t.Fatal("the journal must hold the line once the seam appended")
	}
	if rec.Processed {
		t.Fatal("Deliver must not mark processed")
	}
	if err := router.MarkProcessed(ctx, rec.ID); err != nil {
		t.Fatal(err)
	}
	held, err := router.store.Get(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !held.Processed || !held.Recorded || held.Queue != QueueAccepted {
		t.Fatalf("acks drifted: %+v", held)
	}
	if seam.lines() != 1 || seam.accepts != 1 {
		t.Fatalf("append=%d accept=%d", seam.lines(), seam.accepts)
	}
}

func TestMarkProcessedRefusesAnUnrecordedLine(t *testing.T) {
	ctx := context.Background()
	router := pendingRouter(t)
	got, err := router.Deliver(ctx, OriginAgent, Message{From: "mgr", Body: "hello", CauseID: "late"}, []string{"offline"})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Recorded || got[0].Queue != QueuePending {
		t.Fatalf("offline should stay pending: %+v", got[0])
	}
	err = router.MarkProcessed(ctx, got[0].ID)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestDirectFanoutAndJointShareOnePath(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	router, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	a, b, room := newSeam(), newSeam(), newSeam()
	mustBind(t, router, "a", a)
	mustBind(t, router, "b", b)
	mustBind(t, router, "room", room)

	direct, err := router.Deliver(ctx, OriginPerson, Message{From: "mgr", Body: "progress?", CauseID: "d1"}, []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if direct[0].Pattern != PatternDirect || !direct[0].Recorded {
		t.Fatalf("direct: %+v", direct[0])
	}

	fan, err := router.Deliver(ctx, OriginPerson, Message{From: "mgr", Body: "use JSON", CauseID: "f1"}, []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fan) != 2 {
		t.Fatalf("fan-out receipts=%d", len(fan))
	}
	if fan[0].CauseID != fan[1].CauseID || fan[0].ID == fan[1].ID {
		t.Fatalf("fan-out must share a cause and split ids: %+v", fan)
	}
	if fan[0].Pattern != PatternFanout || fan[1].Pattern != PatternFanout {
		t.Fatalf("pattern=%s/%s", fan[0].Pattern, fan[1].Pattern)
	}

	joint, err := router.Deliver(ctx, OriginAgent, Message{From: "planner", Body: "tradeoff", CauseID: "j1", Discussion: "room", Role: "planner"}, []string{"room"})
	if err != nil {
		t.Fatal(err)
	}
	if joint[0].Pattern != PatternDiscussion || !joint[0].Recorded {
		t.Fatalf("joint: %+v", joint[0])
	}
	if a.lines() != 2 || b.lines() != 1 || room.lines() != 1 {
		t.Fatalf("append counts a=%d b=%d room=%d", a.lines(), b.lines(), room.lines())
	}
}

func TestOfflineResumeDeliversOnce(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	router, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	got, err := router.Deliver(ctx, OriginPerson, Message{From: "mgr", Body: "once", CauseID: "off"}, []string{"member"})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Queue != QueuePending || got[0].Recorded {
		t.Fatalf("retired host must leave pending: %+v", got[0])
	}

	seam := newSeam()
	first, err := router.Bind(ctx, "member", seam)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || !first[0].Recorded || first[0].Queue != QueueAccepted {
		t.Fatalf("resume: %+v", first)
	}
	if seam.lines() != 1 {
		t.Fatalf("first resume appended %d", seam.lines())
	}

	again, err := router.Deliver(ctx, OriginPerson, Message{From: "mgr", Body: "once", CauseID: "off"}, []string{"member"})
	if err != nil {
		t.Fatal(err)
	}
	if !again[0].Recorded || seam.lines() != 1 || seam.accepts != 1 {
		t.Fatalf("replay duplicated: append=%d accept=%d rec=%+v", seam.lines(), seam.accepts, again[0])
	}

	second, err := router.Resume(ctx, "member")
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 0 {
		t.Fatalf("recorded line is not pending: %+v", second)
	}
}

func TestArchiveSuppressesBindResumeAndHostWake(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	router, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	got, err := router.Deliver(ctx, OriginPerson, Message{From: "other", Body: "queued", CauseID: "arch"}, []string{"mgmt"})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Queue != QueuePending || got[0].Recorded {
		t.Fatalf("pre-archive must stay pending: %+v", got[0])
	}
	store.markArchived("mgmt")

	host := &memHost{alive: true}
	previous := registeredHosts()
	RegisterHostFinder(&memFinder{host: host})
	t.Cleanup(func() { RegisterHostFinder(previous) })

	seam := newSeam()
	flushed, err := router.Bind(ctx, "mgmt", seam)
	if err != nil {
		t.Fatal(err)
	}
	if len(flushed) != 0 || seam.lines() != 0 {
		t.Fatalf("archive Bind flushed: receipts=%+v append=%d", flushed, seam.lines())
	}
	again, err := router.Resume(ctx, "mgmt")
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 || seam.lines() != 0 {
		t.Fatalf("archive Resume flushed: receipts=%+v append=%d", again, seam.lines())
	}

	router.Unbind("mgmt")
	late, err := router.Deliver(ctx, OriginPerson, Message{From: "other", Body: "after", CauseID: "arch2"}, []string{"mgmt"})
	if err != nil {
		t.Fatal(err)
	}
	if late[0].Recorded || late[0].Queue != QueuePending || host.wakes != 0 || seam.lines() != 0 {
		t.Fatalf("archive must not spawn or append: wakes=%d append=%d rec=%+v", host.wakes, seam.lines(), late[0])
	}
	held, err := store.Pending(ctx, "mgmt")
	if err != nil || len(held) < 2 {
		t.Fatalf("history must remain pending: %+v, %v", held, err)
	}
}

func TestUnarchivedBindStillFlushesPending(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	router, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := router.Deliver(ctx, OriginPerson, Message{From: "other", Body: "queued", CauseID: "pause"}, []string{"mgmt"}); err != nil {
		t.Fatal(err)
	}
	seam := newSeam()
	flushed, err := router.Bind(ctx, "mgmt", seam)
	if err != nil {
		t.Fatal(err)
	}
	if len(flushed) != 1 || !flushed[0].Recorded || seam.lines() != 1 {
		t.Fatalf("pause is not archive: Bind must still flush pending: receipts=%+v append=%d", flushed, seam.lines())
	}
}

func TestCiteDoesNotWake(t *testing.T) {
	host := &memHost{alive: true}
	finder := &memFinder{host: host}
	previous := registeredHosts()
	RegisterHostFinder(finder)
	t.Cleanup(func() { RegisterHostFinder(previous) })

	router, err := New(newMemStore())
	if err != nil {
		t.Fatal(err)
	}
	cite := router.Cite("old-chat")
	if cite.Woke || cite.SessionID != "old-chat" {
		t.Fatalf("%+v", cite)
	}
	if finder.hits != 0 || host.wakes != 0 {
		t.Fatalf("cite woke a host: finds=%d wakes=%d", finder.hits, host.wakes)
	}
	pending, err := router.store.Pending(context.Background(), "old-chat")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("cite enqueued a delivery: %+v", pending)
	}
}

func TestRetiredHostLeavesPendingUntilBind(t *testing.T) {
	ctx := context.Background()
	host := &memHost{alive: false}
	previous := registeredHosts()
	RegisterHostFinder(&memFinder{host: host})
	t.Cleanup(func() { RegisterHostFinder(previous) })

	router, err := New(newMemStore())
	if err != nil {
		t.Fatal(err)
	}
	got, err := router.Deliver(ctx, OriginAgent, Message{From: "mgr", Body: "ping", CauseID: "ret"}, []string{"gone"})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Queue != QueuePending || host.wakes != 0 {
		t.Fatalf("retired host woke=%d rec=%+v", host.wakes, got[0])
	}
}

func TestWakeBindsThenFlushes(t *testing.T) {
	ctx := context.Background()
	router, err := New(newMemStore())
	if err != nil {
		t.Fatal(err)
	}
	seam := newSeam()
	host := &memHost{alive: true, onWake: func() {
		if _, err := router.Bind(ctx, "asleep", seam); err != nil {
			t.Errorf("bind on wake: %v", err)
		}
	}}
	previous := registeredHosts()
	RegisterHostFinder(&memFinder{host: host})
	t.Cleanup(func() { RegisterHostFinder(previous) })

	got, err := router.Deliver(ctx, OriginPerson, Message{From: "mgr", Body: "wake", CauseID: "w1"}, []string{"asleep"})
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].Recorded || got[0].Queue != QueueAccepted || host.wakes != 1 {
		t.Fatalf("wake flush: wakes=%d rec=%+v", host.wakes, got[0])
	}
}

func TestNoWakeRecordsWithoutAccepting(t *testing.T) {
	ctx := context.Background()
	router, seam := liveRouter(t)
	got, err := router.Deliver(ctx, OriginRuntime, Message{From: "sys", Body: "note", CauseID: "n1", NoWake: true}, []string{"chat-a"})
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].Recorded || got[0].Queue != QueueNobody || seam.accepts != 0 {
		t.Fatalf("silent line must record and not wake: rec=%+v accepts=%d", got[0], seam.accepts)
	}
}

func TestJournalCrashBeforeOutboxAckStillDedupes(t *testing.T) {
	ctx := context.Background()
	router, seam := liveRouter(t)
	id := mintDeliveryID("chat-a", "crash", PatternDirect)
	if err := seam.Append(ctx, Envelope{ID: id, To: "chat-a", Body: "held"}); err != nil {
		t.Fatal(err)
	}
	got, err := router.Deliver(ctx, OriginPerson, Message{From: "mgr", Body: "held", CauseID: "crash"}, []string{"chat-a"})
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].Recorded || seam.lines() != 1 {
		t.Fatalf("resume must trust the journal: append=%d rec=%+v", seam.lines(), got[0])
	}
}

func liveRouter(t *testing.T) (*Router, *memSeam) {
	t.Helper()
	router, err := New(newMemStore())
	if err != nil {
		t.Fatal(err)
	}
	seam := newSeam()
	mustBind(t, router, "chat-a", seam)
	return router, seam
}

func pendingRouter(t *testing.T) *Router {
	t.Helper()
	router, err := New(newMemStore())
	if err != nil {
		t.Fatal(err)
	}
	return router
}

func mustBind(t *testing.T, router *Router, id string, seam Seam) {
	t.Helper()
	if _, err := router.Bind(context.Background(), id, seam); err != nil {
		t.Fatal(err)
	}
}
