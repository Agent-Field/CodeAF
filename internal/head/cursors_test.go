package head

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The defect a shared watermark makes inevitable. Two rooms are open; the head
// answers in one; the number that says "everything below here is handled" is
// now above the other room's unanswered message. On the next start that message
// is never read again — the person is left waiting on a reply nothing will ever
// produce.
func TestARoomIsNotAnsweredAwayByAnotherRoomsReply(t *testing.T) {
	graphStore := openHeadStore(t)
	waiting := postUserLine(t, graphStore, "that-window", "what is running?")
	postUserLine(t, graphStore, "this-window", "how are the totals?")
	answered, err := graphStore.PostMessage(store.Message{
		SessionID: "this-window", Role: store.RoleAgent, Body: "12,004 this quarter.",
	})
	if err != nil {
		t.Fatalf("post the reply that already landed: %v", err)
	}
	if waiting.Seq >= answered.Seq {
		t.Fatalf("the waiting row must sit below the other room's reply (%d, %d)", waiting.Seq, answered.Seq)
	}

	client := &foldClient{reply: `{"reply":"Two jobs are running.","command":null}`}
	conversationalHead := New(client, graphStore)
	cursors, err := conversationalHead.initialCursors()
	if err != nil {
		t.Fatalf("initial cursors: %v", err)
	}
	if err := conversationalHead.poll(context.Background(), cursors); err != nil {
		t.Fatalf("poll: %v", err)
	}

	if calls := client.callCount(); calls != 1 {
		t.Fatalf("provider calls = %d, want the stranded room answered exactly once", calls)
	}
	if prompt := client.prompt(0); !strings.Contains(prompt, waiting.Body) {
		t.Fatalf("the turn answered something other than the waiting message:\n%s", prompt)
	}
	if replies := agentReplies(t, graphStore, "that-window"); len(replies) != 1 {
		t.Fatalf("the waiting room has %d replies, want one", len(replies))
	}
	if replies := agentReplies(t, graphStore, "this-window"); len(replies) != 1 {
		t.Fatalf("the answered room has %d replies, want its own one untouched", len(replies))
	}
}

// The same rule read from the other end: a room whose history already ends in
// an answer is resumed past it, and a second poll from the same cursors says
// nothing twice.
func TestEachRoomResumesAtItsOwnLastAnswer(t *testing.T) {
	graphStore := openHeadStore(t)
	postUserLine(t, graphStore, "quiet-room", "did that land?")
	if _, err := graphStore.PostMessage(store.Message{
		SessionID: "quiet-room", Role: store.RoleAgent, Body: "it did.",
	}); err != nil {
		t.Fatalf("post reply: %v", err)
	}
	live := postUserLine(t, graphStore, "live-room", "and the other one?")

	client := &foldClient{reply: `{"reply":"Still running.","command":null}`}
	conversationalHead := New(client, graphStore)
	cursors, err := conversationalHead.initialCursors()
	if err != nil {
		t.Fatalf("initial cursors: %v", err)
	}
	if err := conversationalHead.poll(context.Background(), cursors); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := conversationalHead.poll(context.Background(), cursors); err != nil {
		t.Fatalf("second poll: %v", err)
	}
	if calls := client.callCount(); calls != 1 {
		t.Fatalf("provider calls = %d, want the one unanswered room answered once", calls)
	}
	if cursor := cursors.answeredThrough("live-room"); cursor < live.Seq {
		t.Fatalf("the live room resumes at %d, want past the row it answered (%d)", cursor, live.Seq)
	}
	if replies := agentReplies(t, graphStore, "quiet-room"); len(replies) != 1 {
		t.Fatalf("the quiet room has %d replies, want the one it already had", len(replies))
	}
}

// A turn can answer rows the poll has not reached — the mid-turn watch folds
// arrivals in — and that answer must not carry the read position with it. The
// rows it would skip belong to other rooms.
func TestAnAnswerAboveThePageDoesNotMoveTheReadPosition(t *testing.T) {
	cursors := newSessionCursors(10)
	cursors.read(20)
	cursors.mark("this-window", 45)
	if cursors.scanned != 20 {
		t.Fatalf("read position = %d, want the newest row actually read (20)", cursors.scanned)
	}
	if got := cursors.answeredThrough("this-window"); got != 45 {
		t.Fatalf("answered through %d, want the row the turn covered (45)", got)
	}
	// Never backwards, and never another room's business.
	cursors.mark("this-window", 30)
	if got := cursors.answeredThrough("this-window"); got != 45 {
		t.Fatalf("answered through %d after an older mark, want 45", got)
	}
	if got := cursors.answeredThrough("that-window"); got != 0 {
		t.Fatalf("an untouched room is answered through %d, want nothing", got)
	}
}

// A room resumed from far back reads pages that are entirely somebody else's
// settled history. The poll has to come out the other side of them: it is one
// journal position walking forward past every row it reads, not a mark that
// only moves when something is owed.
func TestAPageOfAlreadyAnsweredRowsStillMovesThePoll(t *testing.T) {
	graphStore := openHeadStore(t)
	stranded := postUserLine(t, graphStore, "quiet-room", "what is running?")
	// More settled rows than one page holds, so a poll that only advanced on
	// what it answered would read the same page for ever.
	for index := 0; index < messagePageSize+16; index++ {
		if _, err := graphStore.PostMessage(store.Message{
			SessionID: "busy-room", Role: store.RoleSystem, Body: "· a job moved",
		}); err != nil {
			t.Fatalf("post settled row %d: %v", index, err)
		}
	}

	client := &foldClient{reply: `{"reply":"Two jobs are running.","command":null}`}
	conversationalHead := New(client, graphStore)
	cursors, err := conversationalHead.initialCursors()
	if err != nil {
		t.Fatalf("initial cursors: %v", err)
	}
	if cursors.scanned >= stranded.Seq {
		t.Fatalf("the poll starts at %d, want at or below the stranded row %d", cursors.scanned, stranded.Seq)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := conversationalHead.poll(ctx, cursors); err != nil {
		t.Fatalf("poll: %v — a poll that cannot get past a settled page never returns", err)
	}
	if calls := client.callCount(); calls != 1 {
		t.Fatalf("provider calls = %d, want the stranded row answered once", calls)
	}
}

// Zero values and unknown rooms are ordinary states here, not edge cases: the
// head reads this on a first-ever start and on every row of an unknown session.
func TestCursorsAreSafeWhenEmpty(t *testing.T) {
	if got := resumeCursors(nil).scanned; got != 0 {
		t.Fatalf("a store with no messages resumes at %d, want the beginning", got)
	}
	if got := resumeCursors(map[string]int64{"a": 7, "b": 3}).scanned; got != 3 {
		t.Fatalf("read position = %d, want the oldest room's resume point (3)", got)
	}
	var absent *sessionCursors
	if got := absent.answeredThrough("anything"); got != 0 {
		t.Fatalf("a nil cursor set answered through %d, want nothing", got)
	}
	absent.mark("anything", 4)
	absent.read(4)
}
