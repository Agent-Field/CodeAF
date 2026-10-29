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
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/handoff"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

type appChats struct {
	start           sync.Once
	byAgent         map[string]appwire.ChatRow
	take            sync.Mutex
	op              sync.Mutex
	mu              sync.Mutex
	rows            []appwire.ChatRow
	facts           map[string]chatlist.Row
	sent            []appwire.ChatRow
	version         uint64
	source          *syncsetup.Sync
	feed            dirwatch.Follower
	cancel          context.CancelFunc
	done            chan struct{}
	closed          bool
	publicationDone chan struct{}
	publicationWake chan struct{}
}
type appLocalChat struct {
	agent    string
	copyable bool
}

// Startup offers local cells without starting a chat-inventory subscriber.
// Device publication owns the paired engine's shared directory follower.
func (b *appBackend) startLocalChatPublication() {
	c := &b.chats
	c.publicationDone = make(chan struct{})
	c.publicationWake = make(chan struct{}, 1)
	guard.Go("app/existing-chats", func() {
		defer close(c.publicationDone)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		guard.Go("app/existing-chats-stop", func() {
			select {
			case <-b.left:
				cancel()
			case <-ctx.Done():
			}
		})
		ticker := time.NewTicker(syncsetup.DefaultInterval)
		defer ticker.Stop()
		b.runLocalChatPublication(ctx, time.Now, ticker.C, nil)
	})
}

func (c *appChats) wakePublication() {
	select {
	case c.publicationWake <- struct{}{}:
	default:
	}
}

// runLocalChatPublication samples host ticks; settled acknowledges completed
// rounds when a caller needs to observe deterministic request counts.
func (b *appBackend) runLocalChatPublication(ctx context.Context, now func() time.Time, ticks <-chan time.Time, settled chan<- struct{}) {
	c := &b.chats
	var source *syncsetup.Sync
	offered := false
	refresh := false
	var stateRetry time.Time
	var stateBackoff time.Duration
	for {
		if b.pairingWorkRefusal() == nil {
			ready, shared := false, false
			func() {
				if c.isClosed() || ctx.Err() != nil {
					return
				}
				next, ok, err := syncsetup.Open(b.relayHome())
				if err != nil {
					b.sharingStatus("Sharing could not finish yet. Your chats are still saved here; codeaf will try again.")
					return
				}
				if !ok || !syncsetup.MayTalk(b.relayHome()) {
					return
				}
				if source == nil || source.Identity.ID() != next.Identity.ID() || source.Device.ID() != next.Device.ID() || source.Relay != next.Relay {
					source, offered = next, false
					stateRetry, stateBackoff = time.Time{}, 0
				}
				if refresh {
					if err := source.RefreshLocalChats(); err != nil {
						b.sharingStatus("Sharing could not finish yet. Your chats are still saved here; codeaf will try again.")
						return
					}
					refresh, offered = false, false
				}
				if !offered {
					offered = b.publishLocalChats(source, now) == nil
				}
				ready, shared = true, offered
			}()
			// Home state has its own lock. Network work here must not hold up
			// inventory reads or Continue here after the startup chat pass.
			if ready && ctx.Err() == nil && pair.SharingPending(source.Home, source.Identity.ID()) && !now().Before(stateRetry) {
				err := source.SyncState(ctx)
				if err != nil {
					if stateBackoff == 0 {
						stateBackoff = syncsetup.DefaultInterval
					} else {
						stateBackoff = min(2*stateBackoff, syncsetup.SharingRetryMax)
					}
					stateRetry = now().Add(stateBackoff)
					b.sharingStatus("Saved keys, memory and preferences could not be shared yet; codeaf will try again.")
				} else if shared {
					b.sharingStatus("")
				}
			}
		}
		if settled != nil {
			select {
			case settled <- struct{}{}:
			case <-ctx.Done():
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		case <-c.publicationWake:
			// Pairing may follow chats made after the startup scan without
			// changing the binding. Keep its publication progress when scanning.
			refresh = true
		}
	}
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
	if c.inventoryActive() {
		return
	}
	c.start.Do(func() {
		guard.Go("app/chats-bootstrap", func() {
			if !c.inventoryActive() {
				_, _ = b.ListChats()
			}
		})
	})
}

func (c *appChats) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *appChats) inventoryActive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed || c.source != nil
}

func (b *appBackend) ListChats() (appwire.ChatsResult, error) {
	c := &b.chats
	c.op.Lock()
	defer c.op.Unlock()
	if c.isClosed() {
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
		readVersion := func() uint64 {
			c.mu.Lock()
			defer c.mu.Unlock()
			return c.version
		}
		version := readVersion()
		b.publishChats([]appwire.ChatRow{}, version, false)
		return appwire.ChatsResult{Chats: []appwire.ChatRow{}, Version: version}, nil
	}
	feed := b.startChatFeed(s)
	return b.readChats(context.Background(), s, feed, false)
}

func (b *appBackend) startChatFeed(s *syncsetup.Sync) dirwatch.Follower {
	c := &b.chats
	c.mu.Lock()
	defer c.mu.Unlock()
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
	return c.feed
}

// PublishCopy is also the terminal's background publication door. One finite,
// sequential pass offers existing cell chats without opening or resealing them.
// Older chats are left to the terminal's separate conversion policy.
func (b *appBackend) publishLocalChats(s *syncsetup.Sync, now func() time.Time) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	guard.Go("app/existing-chats-cancel", func() {
		select {
		case <-b.left:
			cancel()
		case <-ctx.Done():
		}
	})
	b.sharingStatus("Sharing this computer's chats.")
	err := s.PublishLocalChats(ctx, now, func(c cell.Cell, line string) {
		// Unopened chats use the global sync reason. Queueing one identical
		// per-chat fact on every outage retry would grow without a reader.
		known := false
		b.locked(func() {
			for _, a := range b.agents {
				if session.DirOf(a.transcript) == c.Root {
					known = true
					break
				}
			}
		})
		if known {
			b.syncFact(c, appSyncFact{line: line})
		}
	})
	if err != nil {
		b.sharingStatus("Some chats could not be shared yet. They are still saved here; codeaf will try again.")
	} else {
		b.sharingStatus("")
	}
	return err
}
func (b *appBackend) readChats(parent context.Context, s *syncsetup.Sync, feed dirwatch.Follower, emit bool) (appwire.ChatsResult, error) {
	ctx, cancel := context.WithTimeout(parent, syncsetup.DeviceRequestWithin)
	defer cancel()
	rows, version, err := s.RowsAt(ctx)
	if err != nil {
		return appwire.ChatsResult{}, appEngineError(err)
	}
	if result, newer := b.chats.newerChats(version); newer {
		return result, nil
	}
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
	cacheFacts := func() {
		b.chats.mu.Lock()
		defer b.chats.mu.Unlock()
		b.chats.facts = facts
	}
	cacheFacts()
	b.publishChats(out, version, emit)
	return appwire.ChatsResult{Chats: out, Version: version}, nil
}

func (c *appChats) newerChats(version uint64) (appwire.ChatsResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if version < c.version {
		return appwire.ChatsResult{Chats: slices.Clone(c.rows), Version: c.version}, true
	}
	return appwire.ChatsResult{}, false
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
	old, current := b.chats.cacheRows(rows, version, emit)
	if !current || !emit {
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

func (c *appChats) cacheRows(rows []appwire.ChatRow, version uint64, emit bool) ([]appwire.ChatRow, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if version < c.version {
		return nil, false
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
	return old, true
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
	// Device publication may have connected the shared watch before this
	// subscriber joined. Changes announces only future frames, so reconcile
	// its current state once; a watch that has not spoken still stays silent.
	if feed.State().Up && !b.refreshChatInventory(ctx, feed) {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-feed.Changes():
			if !ok {
				return
			}
		}
		if !b.refreshChatInventory(ctx, feed) {
			return
		}
	}
}

func (b *appBackend) refreshChatInventory(ctx context.Context, feed dirwatch.Follower) bool {
	c := &b.chats
	c.op.Lock()
	defer c.op.Unlock()
	if ctx.Err() != nil {
		return false
	}
	readSource := func() (*syncsetup.Sync, uint64) {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.source, c.version
	}
	s, version := readSource()
	state := feed.State()
	if state.Version > version {
		_, _ = b.readChats(ctx, s, feed, true)
	} else {
		// Presence alone changes no directory version and needs no directory GET.
		cloneRows := func() []appwire.ChatRow {
			c.mu.Lock()
			defer c.mu.Unlock()
			return slices.Clone(c.rows)
		}
		rows := cloneRows()
		known := b.devices.watch.Snapshot(s.Identity, s.Device).Online
		for i := range rows {
			r := &rows[i]
			appChatPresence(r, c.facts[r.Cell], state, known)
		}
		b.publishChats(rows, version, true)
	}
	return true
}

func (b *appBackend) closeChats() {
	c := &b.chats
	stopFollower := func() (context.CancelFunc, dirwatch.Follower, chan struct{}) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.closed = true
		return c.cancel, c.feed, c.done
	}
	cancel, feed, done := stopFollower()
	if cancel != nil {
		cancel()
	}
	if feed != nil {
		feed.Close()
	}
	if done != nil {
		<-done
	}
	if c.publicationDone != nil {
		<-c.publicationDone
	}
	// Join foreground continuations and inventory reads before the remaining teardown.
	c.take.Lock()
	defer c.take.Unlock()
	c.op.Lock()
	defer c.op.Unlock()
}

// Cached enrichment is also used on agent.changed; emitting a turn's event
// never adds network I/O. Existing refusal facts take precedence until the next
// directory reading explicitly replaces them.
func (b *appBackend) cachedChatLocation(id string) *appwire.Location {
	readRow := func() (appwire.ChatRow, bool) {
		b.chats.mu.Lock()
		defer b.chats.mu.Unlock()
		row, ok := b.chats.byAgent[id]
		return row, ok
	}
	row, ok := readRow()
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
