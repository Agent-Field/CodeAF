package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// errMoveCancelled is the cause of a move the person cancelled.
var errMoveCancelled = errors.New("the move was cancelled")

// errAppLeft is the cause of a move the app left behind.
var errAppLeft = errors.New("the app closed")

// appMoveEmitEvery spaces the rows a running move emits: bytes arrive many
// times a second and a row a quarter second is plenty to read.
var appMoveEmitEvery = 250 * time.Millisecond

// appMove is one agents.take while it waits for its turn or runs.
type appMove struct {
	cancel context.CancelCauseFunc
	state  appwire.ChatMove
}

// takeSlot is the one move that runs at a time. A channel rather than a
// mutex, so a move waiting behind another can be cancelled.
func (c *appChats) takeSlot() chan struct{} {
	c.slotOnce.Do(func() { c.slot = make(chan struct{}, 1) })
	return c.slot
}

func (c *appChats) waitSlot(ctx context.Context) error {
	select {
	case c.takeSlot() <- struct{}{}:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func (c *appChats) freeSlot() { <-c.takeSlot() }

// beginMove files a move of chat id as queued. A new move clears how the last
// one ended.
func (c *appChats) beginMove(id string, cancel context.CancelCauseFunc) *appMove {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.moves == nil {
		c.moves = map[string]*appMove{}
	}
	m := &appMove{cancel: cancel, state: appwire.ChatMove{State: "queued"}}
	c.moves[id] = m
	delete(c.ended, id)
	return m
}

// progress records how far move m has got; it answers false once the move is
// no longer the chat's latest.
func (c *appChats) progress(id string, m *appMove, state appwire.ChatMove) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.moves[id] != m {
		return false
	}
	m.state = state
	return true
}

// endMove takes move m off chat id's rows, and files how it ended when it did
// not move the chat.
func (c *appChats) endMove(id string, m *appMove, ended *appwire.ChatMoveEnded) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.moves[id] != m {
		return
	}
	delete(c.moves, id)
	if ended != nil {
		if c.ended == nil {
			c.ended = map[string]appwire.ChatMoveEnded{}
		}
		c.ended[id] = *ended
	}
}

// markMoves puts each row's move and how its last move ended on it. Called
// with c.mu held. A row that says the chat is here has nothing left to say
// about a move that did not happen.
func (c *appChats) markMoves(r *appwire.ChatRow) {
	r.Move, r.MoveEnded = nil, nil
	if m, ok := c.moves[r.Cell]; ok {
		state := m.state
		r.Move = &state
	}
	if ended, ok := c.ended[r.Cell]; ok {
		if r.Here {
			delete(c.ended, r.Cell)
		} else {
			r.MoveEnded = &ended
		}
	}
}

// holderName is the computer the cached row says holds chat id.
func (c *appChats) holderName(id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.rows {
		if r.Cell == id && r.Holder != nil && r.Holder.Name != "" {
			return r.Holder.Name
		}
	}
	return "the other computer"
}

// CancelTake ends a move of the chat that runs or waits. The take cleans up
// as it does after any failure: nothing is acquired before every byte is
// here, and an acquired lease is handed back.
func (b *appBackend) CancelTake(p appwire.TakeCancelParams) (appwire.TakeCancelResult, error) {
	c := &b.chats
	c.mu.Lock()
	m := c.moves[p.Cell]
	c.mu.Unlock()
	if m == nil {
		return appwire.TakeCancelResult{}, nil
	}
	m.cancel(errMoveCancelled)
	return appwire.TakeCancelResult{Cancelled: true}, nil
}

// moveReporter turns a move's progress into rows: at most one emission per
// appMoveEmitEvery, never from the take's own goroutine, so a slow inventory
// lock never slows the copy. stop waits for the reporter to finish.
func (b *appBackend) moveReporter(id string, m *appMove) (report func(syncsetup.MoveProgress), stop func()) {
	wake := make(chan struct{}, 1)
	done, finished := make(chan struct{}), make(chan struct{})
	guard.Go("app/take-progress", func() {
		defer close(finished)
		for {
			select {
			case <-done:
				return
			case <-wake:
			}
			b.publishTaking()
			select {
			case <-done:
				return
			case <-time.After(appMoveEmitEvery):
			}
		}
	})
	report = func(p syncsetup.MoveProgress) {
		if !b.chats.progress(id, m, appwire.ChatMove{State: "moving", Step: p.Step, Done: p.Done, Total: p.Total}) {
			return
		}
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	return report, func() { close(done); <-finished }
}

// appMoveEnding is how a move that did not move the chat ended: the row's
// record and the error agents.take answers. cause is why the move's context
// ended, if it did.
func appMoveEnding(err, cause error, holder string) (appwire.ChatMoveEnded, error) {
	wire := appTakeError(err)
	var w *appwire.Error
	errors.As(wire, &w)
	detail := ""
	if w != nil {
		if words, ok := w.Detail.(string); ok {
			detail = words
		}
	}
	if detail == "" {
		detail = appTakeWords(err.Error())
	}
	still := fmt.Sprintf("The chat is still on %s.", holder)
	ended := appwire.ChatMoveEnded{Outcome: "failed", Detail: detail}
	switch {
	case errors.Is(cause, errMoveCancelled) || errors.Is(err, errMoveCancelled):
		ended = appwire.ChatMoveEnded{Outcome: "cancelled", Line: "Cancelled. " + still}
		return ended, &appwire.Error{Code: appwire.CodeEngine, Message: ended.Line}
	case errors.Is(err, syncsetup.ErrMoveStalled) || errors.Is(cause, syncsetup.ErrMoveStalled):
		ended.Outcome = "stopped"
		ended.Line = "Stopped: nothing came through for 2 minutes. " + still
	case errors.Is(err, directory.ErrLeaseHeld) || errors.Is(err, directory.ErrFenceStale):
		ended.Line = w.Message
		ended.Detail = ""
		return ended, wire
	case errors.Is(err, blobstore.ErrNotFound):
		ended.Line = "Couldn't move it: part of this chat's folder is missing from sync. " + still
	case errors.Is(err, blobstore.ErrUnreachable) || errors.Is(err, directory.ErrUnreachable):
		ended.Line = "Couldn't move it: sync could not be reached. " + still
	case w != nil && w.Code != appwire.CodeEngine:
		// Busy, not available, signed out: the engine's own sentence.
		ended.Line = w.Message
		return ended, wire
	case w != nil && w.Message != appTakeFailed:
		ended.Line = w.Message
	default:
		// Older desktops know this sentence; the detail says what went wrong.
		ended.Line = appTakeFailed
	}
	code := appwire.CodeEngine
	if w != nil {
		code = w.Code
	}
	return ended, &appwire.Error{Code: code, Message: ended.Line, Detail: ended.Detail}
}
