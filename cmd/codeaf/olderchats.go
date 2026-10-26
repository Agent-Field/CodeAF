package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

const olderChatRecent = 30 * 24 * time.Hour

// copyOlderChat seals the folder as it stands today and exposes the independent
// copy only after that first turn is durable. Failed attempts remain hidden.
func copyOlderChat(ctx context.Context, dir string) (cell.Cell, error) {
	return cell.CopyLegacy(ctx, dir, func(ctx context.Context, c cell.Cell) error {
		brain, err := store.Open(filepath.Join(home.Dir(), "graph.db"))
		if err != nil {
			return err
		}
		err = session.PrepareCopy(ctx, dir, c, brain)
		_ = brain.Close()
		if err != nil {
			return err
		}
		meta, err := session.LoadMeta(c.Root)
		if err != nil {
			return err
		}
		e := cellstore.EngineFor(c.Root)
		e.Workspace = meta.Workspace
		if meta.Owned {
			e.Workspace = filepath.Join(c.Root, "work")
		}
		if _, err := e.Seal(ctx, c, cellstore.TurnInfo{}); err != nil {
			return err
		}
		return cellstore.Await(ctx, c)
	})
}

// runOlderChats visits one chat at a time between background rounds. A live
// original is retried after its window closes; its journal is never rewritten.
func runOlderChats(ctx context.Context) {
	ticker := time.NewTicker(syncsetup.DefaultInterval)
	defer ticker.Stop()
	select {
	case <-ctx.Done():
		return
	case <-ticker.C:
	}
	chats := olderChats{copy: copyOlderChat}
	for {
		if syncsetup.MayTalk(home.Dir()) {
			dirs, _ := filepath.Glob(home.Join("v3", "projects", "*", "*"))
			for _, dir := range dirs {
				if ctx.Err() != nil {
					return
				}
				c, meta, ok := chats.visit(ctx, dir)
				if !ok {
					continue
				}
				// A finished copy is uploaded and released as an ordinary chat. The
				// same drive and durability rules apply as for a newly started chat.
				if s, ok, err := syncsetup.Open(home.Dir()); err == nil && ok {
					copiedMeta, _ := session.LoadMeta(c.Root)
					e := cellstore.EngineFor(copiedMeta.Workspace)
					if err := s.PublishCopy(ctx, e, c, deviceName(), meta.Title); err == nil {
						_ = os.WriteFile(filepath.Join(c.Root, cell.CopyOfferedPath), nil, 0600)
					}
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// olderChats is the background copier's view of the older chats: which one is
// due for a copy, the copy itself, and the copies that failed this launch.
//
// A FAILED COPY WAITS FOR ITS CHAT TO CHANGE. Every round visits every older
// chat, and a copy seals the chat's whole folder, so an attempt costs as much as
// the folder is big. A copy that failed was tried again on the next pass for as
// long as the terminal stayed open. The same chat over the same folder fails the
// same way; one whose journal has moved on, or a new launch, is tried again, and
// opening the chat still copies it on demand (movablePlace).
type olderChats struct {
	copy   func(context.Context, string) (cell.Cell, error)
	failed map[string]olderChatStamp
}

// olderChatStamp is what an original's journal looked like when its copy failed.
type olderChatStamp struct {
	size int64
	mod  time.Time
}

// visit copies the chat in dir when it is a recent older chat that nothing is
// using and that has no copy yet. It answers the copy and the original's meta.
func (o *olderChats) visit(ctx context.Context, dir string) (cell.Cell, session.Meta, bool) {
	copied := cell.Copied(dir)
	if copied != dir && session.InUse(filepath.Join(copied, cell.TranscriptPath)) {
		return cell.Cell{}, session.Meta{}, false
	}
	if copied != dir {
		if _, err := os.Stat(filepath.Join(copied, cell.CopyOfferedPath)); err == nil {
			return cell.Cell{}, session.Meta{}, false
		}
	}
	journal, err := os.Stat(filepath.Join(dir, "transcript.jsonl"))
	if err != nil {
		return cell.Cell{}, session.Meta{}, false
	}
	stamp := olderChatStamp{size: journal.Size(), mod: journal.ModTime()}
	if o.failed[dir] == stamp {
		return cell.Cell{}, session.Meta{}, false
	}
	meta, _ := session.LoadMeta(dir)
	if meta.LastUserAt.IsZero() || time.Since(meta.LastUserAt) > olderChatRecent || session.InUse(filepath.Join(dir, "transcript.jsonl")) {
		return cell.Cell{}, session.Meta{}, false
	}
	if !meta.Owned && holdsHome(meta.Workspace) {
		return cell.Cell{}, session.Meta{}, false
	}
	c, err := o.copy(ctx, dir)
	if err != nil {
		// A copy cut short by the terminal closing did not fail.
		if ctx.Err() == nil {
			if o.failed == nil {
				o.failed = map[string]olderChatStamp{}
			}
			o.failed[dir] = stamp
		}
		return cell.Cell{}, session.Meta{}, false
	}
	delete(o.failed, dir)
	return c, meta, true
}

// holdsHome reports whether workspace contains codeaf's own home, as a chat
// started in a home directory does. Codeaf writes its state there for as long as
// it runs, so the whole-folder seal a copy begins with never settles: in a live
// terminal every such copy planned the folder eight times, about a minute each
// on a small home and far longer on a real one, and then failed with "files kept
// changing". The background copier leaves these chats to be copied when opened.
func holdsHome(workspace string) bool {
	if workspace == "" {
		return false
	}
	rel, err := filepath.Rel(resolvedPath(workspace), resolvedPath(home.Dir()))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolvedPath is path with its links resolved when it exists, so a folder named
// through a link still compares as itself.
func resolvedPath(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return filepath.Clean(path)
}

// movablePlace resolves a completed copy or prepares one when the person opens
// an older chat. Selection runs on the existing asynchronous opening door.
func movablePlace(ctx context.Context, place session.Place) (session.Place, error) {
	if place.Dir == "" || !cell.Available() || !syncsetup.MayTalk(home.Dir()) {
		return place, nil
	}
	if _, err := cell.OpenAt(place.Dir, place.ID()); err == nil {
		return place, nil
	}
	original := place.Dir
	copied := cell.Copied(original)
	if copied == original {
		if _, err := os.Stat(filepath.Join(original, "transcript.jsonl")); os.IsNotExist(err) {
			return place, nil
		}
		c, err := copyOlderChat(ctx, original)
		if err != nil {
			return place, err
		}
		copied = c.Root
	}
	meta, err := session.LoadMeta(copied)
	if err != nil {
		return place, err
	}
	return v3PlaceFor(copied, meta.Workspace, meta.Owned), nil
}
