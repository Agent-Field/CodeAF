package main

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// ── chats.read: A CHAT ON ANOTHER COMPUTER, OPENED AS A READ-ONLY CHAT ──────
//
// The desktop opens a chat another paired computer holds and shows what was
// said in it, newest first, older pages as the person scrolls — WITHOUT taking
// the chat over and WITHOUT downloading its project folder. A take was the only
// way to see inside such a chat, and a take copies the whole folder (gigabytes,
// for a real project) before it shows a line; most people only wanted to see
// where the conversation had got to.
//
// So this door asks syncsetup.ReadChatPage for a page — the transcript's own objects
// and the few content-defined chunks under that page, fetched into the store a
// later take of the chat uses, so "Continue here" after reading pays only for
// the rest — and shapes it with the very code agents.entries uses for a chat on
// this computer, so the page looks like the chat it is.
//
// It never leases, materializes, registers an agent or touches a workspace
// folder. docs/design/remote-read/DESIGN.md has the design and its numbers.

// appReadsPaths is whether the engine can read one path of a snapshot; a test
// swaps it, because the real answer depends on the furrow build in use.
var appReadsPaths = cellstore.ReadsPaths

// appReadWithin bounds one page. A page is a few chunks, but a first open on a
// slow link also brings the transcript's first chunk, and a hosted relay asks
// for at most a handful of objects per request.
const appReadWithin = 2 * time.Minute

// CanReadChats implements appwire.ChatsReader: a relay is configured and the
// engine can read a path. Asked, not tried — a capability that cannot work is
// absent from hello rather than present and refusing every call.
func (b *appBackend) CanReadChats() bool {
	r := syncsetup.Resolve(b.relayHome())
	return r.URL != "" && !r.Off && appReadsPaths()
}

// ReadChat implements appwire.ChatsReader.
func (b *appBackend) ReadChat(p appwire.ChatReadParams) (appwire.ChatReadResult, error) {
	if !cell.ValidID(p.Cell) {
		return appwire.ChatReadResult{}, appwire.Errorf(appwire.CodeNotFound, "that chat is not available")
	}
	s, ok, err := syncsetup.Open(b.relayHome())
	if err != nil {
		return appwire.ChatReadResult{}, appEngineError(err)
	}
	if !ok {
		return appwire.ChatReadResult{}, appwire.Errorf(appwire.CodeEngine, appSyncOff)
	}
	ctx, cancel := context.WithTimeout(context.Background(), appReadWithin)
	defer cancel()
	// The cell is named exactly as a take names it, so the engine resolves the
	// same store and timeline; its folder is never created by a read.
	c := cell.Cell{ID: p.Cell, Root: takeRootForAt(s.Home, p.Cell)}
	count := func(segment []byte) int {
		entries, _ := appRemoteEntries(p.Cell, segment, time.Time{})
		return len(entries)
	}
	page, err := s.ReadChatPage(ctx, cellstore.EngineFor(""), c, p.Before, p.Limit, count)
	switch {
	case errors.Is(err, syncsetup.ErrNoChat):
		return appwire.ChatReadResult{}, appwire.Errorf(appwire.CodeNotFound, "that chat is not available")
	case errors.Is(err, cellsync.ErrBadCursor):
		return appwire.ChatReadResult{}, appwire.Errorf(appwire.CodeBadParams, "before is not a cursor this engine wrote")
	case errors.Is(err, directory.ErrUnreachable) || errors.Is(err, wireauth.ErrUnavailable) || errors.Is(err, context.DeadlineExceeded):
		return appwire.ChatReadResult{}, appwire.Errorf(appwire.CodeUnreachable, "The sync service can't be reached, so this chat can't be read right now.")
	case err != nil:
		return appwire.ChatReadResult{}, appEngineError(err)
	}
	var at time.Time
	if page.SealedAtMs > 0 {
		at = time.UnixMilli(page.SealedAtMs).UTC()
	}
	entries, err := appRemoteEntries(p.Cell, page.Segment, at)
	if err != nil {
		return appwire.ChatReadResult{}, appEntriesBusy(err)
	}
	// Ids count the person's messages from the top of the bytes shaped, which
	// for a page is its own start; the start makes them unique across pages.
	prefix := "r" + strconv.FormatInt(page.Start, 10) + "."
	for i := range entries {
		entries[i].ID = prefix + entries[i].ID
	}
	return appwire.ChatReadResult{
		Entries:      entries,
		More:         page.More(),
		Before:       page.Before,
		Head:         page.Head,
		SealedAtMs:   page.SealedAtMs,
		FetchedBytes: page.Fetched,
	}, nil
}

// appRemoteEntries shapes a segment of a transcript into the entries
// agents.entries would show for it: the session's tail reader and the app's own
// page builder, with no local records beside them (turn footers, kept-process
// notices and tool output files live on the computer that holds the chat).
func appRemoteEntries(chat string, segment []byte, at time.Time) ([]appwire.Entry, error) {
	if len(segment) == 0 {
		return []appwire.Entry{}, nil
	}
	record := session.ReadTranscriptBytes(segment)
	// Every entry of the segment is on the page: the page was cut by bytes.
	bound := 2*(len(record.Earlier)+len(record.Entries)+len(record.ModelSwitches)) + 4
	entries, _, err := appEntryPageOf(chat, record, at, map[int]appTurnMark{}, nil, "", bound,
		func(session.DisplayEntry) appwire.ToolFacts { return appwire.ToolFacts{} }, nil)
	return entries, err
}
