package main

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/handoff"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

type appChats struct {
	start   sync.Once
	byAgent map[string]appwire.ChatRow
	take    sync.Mutex
	op      sync.Mutex
	// Offered is guarded by op and records an attempted startup pass.
	offered bool
	mu      sync.Mutex
	rows    []appwire.ChatRow
	facts   map[string]chatlist.Row
	sent    []appwire.ChatRow
	version uint64
	source  *syncsetup.Sync
	feed    dirwatch.Follower
	cancel  context.CancelFunc
	done    chan struct{}
	closed  bool
}
type appLocalChat struct {
	agent    string
	copyable bool
}

// Startup offers local cells without subscribing to directory events. Device
// management stays passive until the desktop asks for the chat inventory.
func (b *appBackend) startLocalChatPublication() {
	guard.Go("app/existing-chats", func() {
		c := &b.chats
		c.op.Lock()
		defer c.op.Unlock()
		c.mu.Lock()
		closed := c.closed
		c.mu.Unlock()
		if closed || c.offered {
			return
		}
		if s, ok, err := syncsetup.Open(b.relayHome()); err == nil && ok && syncsetup.MayTalk(b.relayHome()) {
			b.publishLocalChats(s)
			c.offered = true
		}
	})
}

// localChats relates physical chat folders to directory ids. Older chats may
// have a separate moving copy, and a drive may publish under a mapped id.
func (b *appBackend) localChats(s *syncsetup.Sync) map[string]appLocalChat {
	type entry struct{ id, file string }
	var local []entry
	for _, r := range session.ReadWorld(filepath.Join(b.relayHome(), "v3", "projects")).Sessions() {
		if appChatStaging(r.Dir) {
			continue
		}
		local = append(local, entry{r.ID, r.Transcript})
	}
	b.locked(func() {
		for id, a := range b.agents {
			if a.transcript != "" {
				local = append(local, entry{id, a.transcript})
			}
		}
	})
	out := map[string]appLocalChat{}
	for _, item := range local {
		agent, file := item.id, item.file
		root := session.DirOf(file)
		copied := cell.Copied(root)
		if _, err := cell.OpenAt(copied, filepath.Base(copied)); err != nil {
			continue
		}
		remote := filepath.Base(copied)
		if held, _, err := s.SendLease(remote); err == nil && held != "" {
			remote = held
		}
		copyable := true
		if meta, err := session.LoadMeta(root); err == nil && copied != root {
			copyable = olderChatCopyable(meta)
		}
		out[remote] = appLocalChat{agent: agent, copyable: copyable}
	}
	return out
}

// appChatStaging excludes private takeover scratch from app agent admission.
// Opening its copied working presence can attach a journal and directory watch
// before the taker renames it, preventing installation on native Windows.
func appChatStaging(dir string) bool {
	return strings.HasSuffix(filepath.Clean(dir), handoff.StagingSuffix)
}

// Local list/open responses keep their disk-only latency. The desktop's
// explicit inventory bootstrap and sync callbacks share this same follower.
func (b *appBackend) startChatInventory() {
	c := &b.chats
	c.mu.Lock()
	active := c.closed || c.source != nil
	c.mu.Unlock()
	if active {
		return
	}
	c.start.Do(func() {
		guard.Go("app/chats-bootstrap", func() {
			c.mu.Lock()
			active := c.closed || c.source != nil
			c.mu.Unlock()
			if !active {
				_, _ = b.ListChats()
			}
		})
	})
}

func (b *appBackend) ListChats() (appwire.ChatsResult, error) {
	return b.listChats(false)
}

func (b *appBackend) listChats(publishExisting bool) (appwire.ChatsResult, error) {
	c := &b.chats
	c.op.Lock()
	defer c.op.Unlock()
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return appwire.ChatsResult{}, appwire.Errorf(appwire.CodeClosed, "codeaf is shutting down")
	}
	s, ok, err := syncsetup.Open(b.relayHome())
	if errors.Is(err, syncsetup.ErrNoIdentity) {
		err = nil
	}
	if err != nil {
		return appwire.ChatsResult{}, appEngineError(err)
	}
	if !ok || s == nil || !syncsetup.MayTalk(b.relayHome()) {
		c.mu.Lock()
		version := c.version
		c.mu.Unlock()
		b.publishChats([]appwire.ChatRow{}, version, false)
		return appwire.ChatsResult{Chats: []appwire.ChatRow{}, Version: version}, nil
	}
	c.mu.Lock()
	if c.source == nil {
		c.source = s
		c.feed = s.Follow()
		if c.feed != nil {
			ctx, cancel := context.WithCancel(context.Background())
			c.cancel = cancel
			c.done = make(chan struct{})
			feed, done := c.feed, c.done
			guard.Go("app/chats", func() { defer close(done); b.followChats(ctx, feed) })
		}
	}
	feed := c.feed
	c.mu.Unlock()
	if !c.offered || publishExisting {
		b.publishLocalChats(s)
		c.offered = true
	}
	return b.readChats(context.Background(), s, feed, false)
}

// PublishCopy is also the terminal's background publication door. One finite,
// sequential pass offers existing cell chats without opening or resealing them.
// Older chats are left to the terminal's separate conversion policy.
func (b *appBackend) publishLocalChats(s *syncsetup.Sync) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-b.left:
			cancel()
		case <-ctx.Done():
		}
	}()
	// ReadWorld resolves older originals to conversion copies. Enumerate direct
	// cells instead so this pass never offers a legacy home-folder conversion.
	roots, _ := filepath.Glob(filepath.Join(s.Home, "v3", "projects", "*", "*"))
	name := devname.Name(s.Home)
	for _, root := range roots {
		if ctx.Err() != nil {
			return
		}
		if !cell.ValidID(filepath.Base(root)) {
			continue
		}
		c, err := cell.OpenAt(root, filepath.Base(root))
		if err != nil {
			continue
		}
		// An unopened shell has nothing to share and needs no relay request.
		if head, err := cellstore.Head(c); err != nil || head == nil {
			continue
		}
		meta, err := session.LoadMeta(root)
		if err != nil {
			continue
		}
		e := cellstore.EngineFor(meta.Workspace)
		e.DataRoot = filepath.Join(s.Home, "v3", "stores")
		if meta.Owned {
			e.Workspace = filepath.Join(root, "work")
		}
		one, stop := context.WithTimeout(ctx, cellstore.SealWithin)
		_ = s.PublishCopy(one, e, c, name, meta.Title)
		stop()
	}
}
func (b *appBackend) readChats(parent context.Context, s *syncsetup.Sync, feed dirwatch.Follower, emit bool) (appwire.ChatsResult, error) {
	ctx, cancel := context.WithTimeout(parent, syncsetup.DeviceRequestWithin)
	defer cancel()
	rows, version, err := s.RowsAt(ctx)
	if err != nil {
		return appwire.ChatsResult{}, appEngineError(err)
	}
	b.chats.mu.Lock()
	if version < b.chats.version {
		result := appwire.ChatsResult{Chats: slices.Clone(b.chats.rows), Version: b.chats.version}
		b.chats.mu.Unlock()
		return result, nil
	}
	b.chats.mu.Unlock()
	state := dirwatch.State{}
	if feed != nil {
		state = feed.State()
	}
	known := b.devices.watch.Snapshot(s.Identity, s.Device).Online
	local := b.localChats(s)
	out := make([]appwire.ChatRow, 0, len(rows))
	facts := make(map[string]chatlist.Row, len(rows))
	for _, r := range rows {
		if r.Parent != "" || r.Status == chatlist.Branch {
			continue
		}
		facts[r.Cell] = r
		_, installed := local[r.Cell]
		here := r.Mine && (r.Status == chatlist.Here || installed)
		row := appwire.ChatRow{Cell: r.Cell, Title: r.Title, Here: here, Status: string(r.Status), Seen: appwire.Seen{Device: r.DeviceID, Fence: r.Fence, Head: r.Head}, CanTake: !here && r.Head != ""}
		if service, ok := s.Dir.(interface{ HeadTakes() bool }); ok && !service.HeadTakes() {
			row.Seen.Head = ""
		}
		if r.DeviceID != "" {
			row.Holder = &appwire.LocationHolder{ID: r.DeviceID, Name: r.Device}
		}
		appChatPresence(&row, r, state, known)
		if r.SavedAt > 0 {
			row.SavedAt = time.UnixMilli(r.SavedAt).UTC()
		}
		if l, ok := local[r.Cell]; ok {
			row.LocalAgent = l.agent
			row.CanTake = row.CanTake && l.copyable
		}
		if row.Here {
			row.Status = "here"
		}
		out = append(out, row)
	}
	b.chats.mu.Lock()
	b.chats.facts = facts
	b.chats.mu.Unlock()
	b.publishChats(out, version, emit)
	return appwire.ChatsResult{Chats: out, Version: version}, nil
}

func appChatPresence(row *appwire.ChatRow, fact chatlist.Row, state dirwatch.State, known map[string]bool) {
	row.Online = nil
	if fact.DeviceID != "" {
		if online, ok := known[fact.DeviceID]; ok {
			row.Online = &online
		}
	}
	row.Line = appChatStatusLine(fact, row.Online)
	row.TakeoverLine = appChatTakeoverLine(fact, row.Online)
}

// Inventory operations hold op through caching and emission. A list refreshes
// the cache silently; only the follower compares against rows actually emitted.
func (b *appBackend) publishChats(rows []appwire.ChatRow, version uint64, emit bool) {
	c := &b.chats
	c.mu.Lock()
	if version < c.version {
		c.mu.Unlock()
		return
	}
	c.rows = rows
	c.byAgent = make(map[string]appwire.ChatRow)
	for _, row := range rows {
		if row.LocalAgent != "" {
			c.byAgent[row.LocalAgent] = row
		}
	}
	c.version = version
	old := c.sent
	if emit {
		c.sent = rows
	}
	c.mu.Unlock()
	if !emit {
		return
	}
	before := map[string]appwire.ChatRow{}
	for _, r := range old {
		before[r.Cell] = r
	}
	for _, r := range rows {
		prev, exists := before[r.Cell]
		delete(before, r.Cell)
		if !exists || !reflect.DeepEqual(prev, r) {
			b.emit.Emit(appwire.EventChatChanged, appwire.ChatChanged{Chat: r, Version: version})
		}
		if r.LocalAgent != "" {
			var a *appAgent
			b.locked(func() { a = b.agents[r.LocalAgent] })
			if a != nil {
				b.located(a, chatLocation(r))
			}
		}
	}
	for id := range before {
		b.emit.Emit(appwire.EventChatRemoved, appwire.ChatRemoved{Cell: id, Version: version})
	}
}
func chatLocation(r appwire.ChatRow) *appwire.Location {
	seen := r.Seen
	line := r.Line
	if !r.Here && r.Status == string(chatlist.Idle) {
		line = chatlist.Available
	} else if !r.Here && r.Holder != nil {
		line = chatlist.Superseded(r.Holder.Name)
	}
	return &appwire.Location{Cell: r.Cell, Seen: &seen, Here: r.Here, Holder: r.Holder, CanTake: r.CanTake, Line: line, TakeoverLine: r.TakeoverLine}
}
func (b *appBackend) followChats(ctx context.Context, feed dirwatch.Follower) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-feed.Changes():
			if !ok {
				return
			}
		}
		c := &b.chats
		c.op.Lock()
		if ctx.Err() != nil {
			c.op.Unlock()
			return
		}
		c.mu.Lock()
		s, version := c.source, c.version
		c.mu.Unlock()
		state := feed.State()
		if state.Version > version {
			_, _ = b.readChats(ctx, s, feed, true)
		} else {
			// Presence alone changes no directory version and needs no directory GET.
			c.mu.Lock()
			rows := slices.Clone(c.rows)
			c.mu.Unlock()
			known := b.devices.watch.Snapshot(s.Identity, s.Device).Online
			for i := range rows {
				r := &rows[i]
				appChatPresence(r, c.facts[r.Cell], state, known)
			}
			b.publishChats(rows, version, true)
		}
		c.op.Unlock()
	}
}
func (b *appBackend) closeChats() {
	c := &b.chats
	c.mu.Lock()
	c.closed = true
	cancel, feed, done := c.cancel, c.feed, c.done
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if feed != nil {
		feed.Close()
	}
	if done != nil {
		<-done
	}
	// Join foreground continuations and inventory reads before the remaining teardown.
	c.take.Lock()
	defer c.take.Unlock()
	c.op.Lock()
	c.op.Unlock()
}

// Cached enrichment is also used on agent.changed; emitting a turn's event
// never adds network I/O. Existing refusal facts take precedence until the next
// directory reading explicitly replaces them.
func (b *appBackend) cachedChatLocation(id string) *appwire.Location {
	b.chats.mu.Lock()
	row, ok := b.chats.byAgent[id]
	b.chats.mu.Unlock()
	if !ok {
		return nil
	}
	return chatLocation(row)
}

func (b *appBackend) dressChatLocation(view *appwire.Agent) {
	fresh := b.cachedChatLocation(view.ID)
	if fresh == nil {
		return
	}
	if view.Location != nil {
		copy := *view.Location
		if copy.Cell == "" {
			copy.Cell = fresh.Cell
		}
		if copy.Seen == nil {
			copy.Seen = fresh.Seen
		}
		if !copy.Here {
			copy.CanTake = fresh.CanTake
			copy.TakeoverLine = fresh.TakeoverLine
		}
		fresh = &copy
	}
	view.Location = fresh
}
