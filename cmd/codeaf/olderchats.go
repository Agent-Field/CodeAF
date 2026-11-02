package main

import (
	"context"
	"os"
	"path/filepath"
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
	offered := ""
	var source *syncsetup.Sync
	for {
		if syncsetup.MayTalk(home.Dir()) {
			if s, ok, err := syncsetup.Open(home.Dir()); err == nil && ok {
				binding := s.Ledger + "/" + s.Device.ID()
				if source == nil || source.Ledger+"/"+source.Device.ID() != binding {
					source, offered = s, ""
				}
				if offered != binding {
					if err := source.PublishLocalChats(ctx, time.Now, func(_ cell.Cell, line string) { surfaceNotices.SayHomeOnce(line) }); err == nil {
						offered = binding
					}
				}
			}
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
	if meta.LastUserAt.IsZero() || time.Since(meta.LastUserAt) > olderChatRecent || session.InUse(filepath.Join(dir, "transcript.jsonl")) || !olderChatCopyable(meta) {
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

// olderChatCopyable says a copy of the chat may seal its folder.
//
// A CHAT IN THE HOME FOLDER IS NOT COPIED. A copy's first seal has no record of
// what changed, so it walks the chat's whole folder, and the engine holds its
// one writer for the shared store while it does. Over the home folder that walk
// takes minutes, and every open chat's tools wait behind it until they time out.
// The home folder is not a project (session.HoldsHomeFolder); such a chat keeps
// opening where it is. A chat with its own folder is always copyable.
func olderChatCopyable(meta session.Meta) bool {
	return meta.Owned || !session.HoldsHomeFolder(meta.Workspace)
}

// movablePlace resolves a completed copy or prepares one when the person opens
// an older chat. Selection runs on the existing asynchronous opening door.
// Sync off means not talking to the service, never forgetting a completed copy
// already on this computer, so an existing `.moving` copy is opened even when
// sync is quiet or off.
func movablePlace(ctx context.Context, place session.Place) (session.Place, error) {
	if place.Dir == "" {
		return place, nil
	}
	if _, err := cell.OpenAt(place.Dir, place.ID()); err == nil {
		return place, nil
	}
	original := place.Dir
	copied := cell.Copied(original)
	if copied == original {
		if !cell.Available() || !syncsetup.MayTalk(home.Dir()) {
			return place, nil
		}
		if _, err := os.Stat(filepath.Join(original, "transcript.jsonl")); os.IsNotExist(err) {
			return place, nil
		}
		if meta, err := session.LoadMeta(original); err == nil && !olderChatCopyable(meta) {
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
