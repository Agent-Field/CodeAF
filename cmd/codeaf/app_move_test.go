package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func appOpened(b *appBackend, id string) (appwire.Agent, error) { return appwire.Agent{ID: id}, nil }

// appChangedRow finds the last chat.changed row for id from index from on
// that passes ok.
func appChangedRow(rec *appRecorder, from int, id string, ok func(appwire.ChatRow) bool) (appwire.ChatRow, bool) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for i := len(rec.data) - 1; i >= from; i-- {
		if changed, is := rec.data[i].(appwire.ChatChanged); is && changed.Chat.Cell == id && ok(changed.Chat) {
			return changed.Chat, true
		}
	}
	return appwire.ChatRow{}, false
}

// A running move's row says how far it has got, and the row says nothing
// about a move once the chat is here.
func TestAppMoveRowSaysHowFarItHasGot(t *testing.T) {
	_, rec, wire, _, row := appLandedTakeFixture(t, appOpened)
	landed := appTakeSeen
	release := make(chan struct{})
	appTakeSeen = func(ctx context.Context, s *syncsetup.Sync, id string, seen *directory.LeaseExpectation, onMove func(syncsetup.MoveProgress)) (syncsetup.Continued, error) {
		onMove(syncsetup.MoveProgress{Step: syncsetup.MoveCopying, Done: 412 << 20, Total: 1433 << 20})
		<-release
		return landed(ctx, s, id, seen, onMove)
	}
	from := appRecorded(rec)
	done := make(chan error, 1)
	go func() {
		_, refusal := wire.call("agents.take", appwire.TakeParams{Cell: row.Cell, Seen: row.Seen})
		if refusal != nil {
			done <- fmt.Errorf("%v", refusal)
			return
		}
		done <- nil
	}()
	want := appwire.ChatMove{State: "moving", Step: "copying", Done: 412 << 20, Total: 1433 << 20}
	appSyncWait(t, "a row saying how far the move has got", func() bool {
		_, ok := appChangedRow(rec, from, row.Cell, func(r appwire.ChatRow) bool { return r.Move != nil && *r.Move == want && r.Taking })
		return ok
	})
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for _, r := range machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{}).Chats {
		if r.Cell == row.Cell && (r.Move != nil || r.MoveEnded != nil) {
			t.Fatalf("a finished move still shows on its row: %+v", r)
		}
	}
}

// A move that stopped says so on its row, with the computer the chat is still
// on, after the take has ended: a client that stopped waiting still learns it.
func TestAppMoveThatStoppedSaysWhyOnTheRow(t *testing.T) {
	_, rec, wire, _, row := appLandedTakeFixture(t, appOpened)
	appTakeSeen = func(context.Context, *syncsetup.Sync, string, *directory.LeaseExpectation, func(syncsetup.MoveProgress)) (syncsetup.Continued, error) {
		return syncsetup.Continued{}, fmt.Errorf("%w: handoff: fetch abc: context canceled", syncsetup.ErrMoveStalled)
	}
	from := appRecorded(rec)
	_, refusal := wire.call("agents.take", appwire.TakeParams{Cell: row.Cell, Seen: row.Seen})
	if refusal == nil {
		t.Fatal("a stalled move succeeded")
	}
	line := "Stopped: nothing came through for 2 minutes. The chat is still on " + row.Holder.Name + "."
	if refusal.Message != line {
		t.Fatalf("agents.take answered %v, want %q", refusal, line)
	}
	ended, ok := appChangedRow(rec, from, row.Cell, func(r appwire.ChatRow) bool { return !r.Taking })
	if !ok || ended.MoveEnded == nil || ended.MoveEnded.Outcome != "stopped" || ended.MoveEnded.Line != line || ended.Move != nil {
		t.Fatalf("the row after a stalled move = %+v, want taking:false with move_ended stopped", ended)
	}
	if !strings.Contains(ended.MoveEnded.Detail, "context canceled") {
		t.Fatalf("detail = %q, want what went wrong", ended.MoveEnded.Detail)
	}
	for _, r := range machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{}).Chats {
		if r.Cell == row.Cell && (r.MoveEnded == nil || r.MoveEnded.Line != line) {
			t.Fatalf("a list read after the move lost how it ended: %+v", r)
		}
	}
}

// Cancel ends a running move and a queued one; the queued one never starts.
func TestAppMoveCancelEndsRunningAndQueuedMoves(t *testing.T) {
	backend, rec, wire, _, row := appLandedTakeFixture(t, appOpened)
	started := make(chan string, 2)
	appTakeSeen = func(ctx context.Context, _ *syncsetup.Sync, id string, _ *directory.LeaseExpectation, _ func(syncsetup.MoveProgress)) (syncsetup.Continued, error) {
		started <- id
		<-ctx.Done()
		return syncsetup.Continued{}, ctx.Err()
	}
	from := appRecorded(rec)
	take := func(cell string, seen appwire.Seen) chan *appwire.Error {
		out := make(chan *appwire.Error, 1)
		go func() {
			_, refusal := wire.call("agents.take", appwire.TakeParams{Cell: cell, Seen: seen})
			out <- refusal
		}()
		return out
	}
	running := take(row.Cell, row.Seen)
	if got := <-started; got != row.Cell {
		t.Fatalf("started %s, want %s", got, row.Cell)
	}
	queuedCell, err := cell.NewID(timeNowForTest())
	if err != nil {
		t.Fatal(err)
	}
	queued := take(queuedCell, appwire.Seen{Device: row.Seen.Device, Fence: 1})
	appSyncWait(t, "the second move to queue", func() bool {
		backend.chats.mu.Lock()
		defer backend.chats.mu.Unlock()
		m := backend.chats.moves[queuedCell]
		return m != nil && m.state.State == "queued"
	})
	if got := machineCall[appwire.TakeCancelResult](t, wire, "agents.take_cancel", appwire.TakeCancelParams{Cell: queuedCell}); !got.Cancelled {
		t.Fatal("a queued move was not cancelled")
	}
	if refusal := <-queued; refusal == nil || !strings.Contains(refusal.Message, "Cancelled.") {
		t.Fatalf("a cancelled queued move answered %v", refusal)
	}
	if got := machineCall[appwire.TakeCancelResult](t, wire, "agents.take_cancel", appwire.TakeCancelParams{Cell: row.Cell}); !got.Cancelled {
		t.Fatal("a running move was not cancelled")
	}
	if refusal := <-running; refusal == nil || !strings.Contains(refusal.Message, "Cancelled.") {
		t.Fatalf("a cancelled running move answered %v", refusal)
	}
	select {
	case id := <-started:
		t.Fatalf("the cancelled queued move %s started", id)
	default:
	}
	ended, ok := appChangedRow(rec, from, row.Cell, func(r appwire.ChatRow) bool { return !r.Taking })
	if !ok || ended.MoveEnded == nil || ended.MoveEnded.Outcome != "cancelled" {
		t.Fatalf("the row after a cancelled move = %+v", ended)
	}
	if got := machineCall[appwire.TakeCancelResult](t, wire, "agents.take_cancel", appwire.TakeCancelParams{Cell: row.Cell}); got.Cancelled {
		t.Fatal("cancelling a move that already ended said it cancelled one")
	}
}

// The ended record names the reason in person words and keeps the error text
// for "Copy details".
func TestAppMoveEndingWords(t *testing.T) {
	cases := []struct {
		err     error
		outcome string
		line    string
	}{
		{fmt.Errorf("x: %w", syncsetup.ErrMoveStalled), "stopped", "Stopped: nothing came through for 2 minutes. The chat is still on Studio Mac."},
		{fmt.Errorf("cellsync: fetch object 12: %w", errBlobNotFound()), "failed", "Couldn't move it: part of this chat's folder is missing from sync. The chat is still on Studio Mac."},
		{fmt.Errorf("handoff: fetch: %w", errBlobUnreachable()), "failed", "Couldn't move it: sync could not be reached. The chat is still on Studio Mac."},
		{fmt.Errorf("something odd"), "failed", appTakeFailed},
	}
	for _, c := range cases {
		ended, wire := appMoveEnding(c.err, nil, "Studio Mac")
		if ended.Outcome != c.outcome || ended.Line != c.line || ended.Detail == "" {
			t.Errorf("%v: ended = %+v, want %s %q with detail", c.err, ended, c.outcome, c.line)
		}
		if !strings.Contains(wire.Error(), c.line) {
			t.Errorf("%v: wire error %v does not say %q", c.err, wire, c.line)
		}
	}
	ended, _ := appMoveEnding(context.Canceled, errMoveCancelled, "Studio Mac")
	if ended.Outcome != "cancelled" {
		t.Errorf("a cancelled move ended as %+v", ended)
	}
}

func errBlobNotFound() error    { return blobstore.ErrNotFound }
func errBlobUnreachable() error { return blobstore.ErrUnreachable }
func timeNowForTest() time.Time { return time.Now() }

// A chat whose holder has not finished sharing its newest turns says how far
// it has got and waits, unless the holder is not a known computer.
func TestAppChatSharingWaitsForTheHolder(t *testing.T) {
	online := true
	row := appwire.ChatRow{CanTake: true, Online: &online}
	appChatSharing(&row, chatlist.Row{Pending: 2, UploadSent: 412, UploadTotal: 1433})
	if row.Sharing == nil || *row.Sharing != (appwire.ChatSharing{Pending: 2, Sent: 412, Total: 1433}) || row.CanTake {
		t.Fatalf("a sharing holder's row = %+v %+v", row, row.Sharing)
	}
	offline := false
	row = appwire.ChatRow{CanTake: true, Online: &offline}
	appChatSharing(&row, chatlist.Row{Pending: 1})
	if row.Sharing == nil || row.Sharing.Total != 0 || row.CanTake {
		t.Fatalf("an offline holder's unshared chat = %+v", row)
	}
	row = appwire.ChatRow{CanTake: true}
	appChatSharing(&row, chatlist.Row{Pending: 1})
	if row.Sharing == nil || !row.CanTake {
		t.Fatalf("a holder of unknown presence locked the chat: %+v", row)
	}
	row = appwire.ChatRow{CanTake: true, Online: &online}
	appChatSharing(&row, chatlist.Row{Pending: 1, HolderRemoved: true})
	if !row.CanTake {
		t.Fatal("a removed holder locked the chat")
	}
	row = appwire.ChatRow{CanTake: true, Online: &online}
	appChatSharing(&row, chatlist.Row{Pending: 1, Mine: true})
	if row.Sharing != nil || !row.CanTake {
		t.Fatalf("this computer's own pending turns read as another's sharing: %+v", row)
	}
	// A relay's counts past int64 never reach the app as negative bytes.
	row = appwire.ChatRow{CanTake: true, Online: &online}
	appChatSharing(&row, chatlist.Row{Pending: 1, UploadSent: math.MaxUint64, UploadTotal: math.MaxUint64})
	if row.Sharing.Sent < 0 || row.Sharing.Total < 0 || row.Sharing.Sent > row.Sharing.Total {
		t.Fatalf("huge counts became %+v", row.Sharing)
	}
}

// A presence change re-judges a cached row: a holder whose presence becomes
// unknown stops blocking it, one seen again blocks it again, and a row that
// could not be taken anyway stays that way.
func TestAppChatSharingFollowsPresence(t *testing.T) {
	online, fact := true, chatlist.Row{Pending: 1}
	row := appwire.ChatRow{CanTake: true, Online: &online}
	appChatSharing(&row, fact)
	if row.CanTake {
		t.Fatal("a known holder still sharing did not block the row")
	}
	row.Online = nil
	appChatSharingAgain(&row, fact, true)
	if !row.CanTake {
		t.Fatal("a holder of unknown presence still blocks the row")
	}
	row.Online = &online
	appChatSharingAgain(&row, fact, true)
	if row.CanTake {
		t.Fatal("a holder seen again no longer blocks the row")
	}
	row.Online = nil
	appChatSharingAgain(&row, fact, false)
	if row.CanTake {
		t.Fatal("a row that could not be taken became takeable")
	}
}

// A refusal TakeAgent put in its own words reaches the client and the row as
// it is, not as the generic sentence with the words as detail.
func TestAppMoveKeepsTheEnginesOwnRefusal(t *testing.T) {
	for _, words := range []string{appSyncOff, "This older chat stays on this computer."} {
		ended, err := appMoveEnding(&appwire.Error{Code: appwire.CodeEngine, Message: words}, nil, "studio")
		var w *appwire.Error
		if !errors.As(err, &w) || w.Message != words || ended.Line != words || ended.Outcome != "failed" {
			t.Fatalf("refusal %q became %v, row %+v", words, err, ended)
		}
	}
}

// A second take of a chat already moving is refused, and the first keeps its
// Cancel and its outcome.
func TestAppMoveSecondTakeOfTheSameChatIsRefused(t *testing.T) {
	backend, _, wire, _, row := appLandedTakeFixture(t, appOpened)
	started := make(chan struct{}, 2)
	appTakeSeen = func(ctx context.Context, _ *syncsetup.Sync, _ string, _ *directory.LeaseExpectation, _ func(syncsetup.MoveProgress)) (syncsetup.Continued, error) {
		started <- struct{}{}
		<-ctx.Done()
		return syncsetup.Continued{}, ctx.Err()
	}
	first := make(chan *appwire.Error, 1)
	go func() {
		_, refusal := wire.call("agents.take", appwire.TakeParams{Cell: row.Cell, Seen: row.Seen})
		first <- refusal
	}()
	<-started
	backend.chats.mu.Lock()
	m := backend.chats.moves[row.Cell]
	backend.chats.mu.Unlock()
	_, refusal := wire.call("agents.take", appwire.TakeParams{Cell: row.Cell, Seen: row.Seen})
	if refusal == nil || refusal.Code != appwire.CodeBusy || refusal.Message != appMoveAlready {
		t.Fatalf("a second take of a moving chat answered %v, want busy %q", refusal, appMoveAlready)
	}
	backend.chats.mu.Lock()
	same := backend.chats.moves[row.Cell] == m
	backend.chats.mu.Unlock()
	if !same {
		t.Fatal("the second take replaced the first move")
	}
	if got := machineCall[appwire.TakeCancelResult](t, wire, "agents.take_cancel", appwire.TakeCancelParams{Cell: row.Cell}); !got.Cancelled {
		t.Fatal("the first move could not be cancelled after a second take")
	}
	if refusal := <-first; refusal == nil || !strings.Contains(refusal.Message, "Cancelled.") {
		t.Fatalf("the cancelled first move answered %v", refusal)
	}
}
