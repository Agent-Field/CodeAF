package syncsetup

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
)

type pendingPublication struct {
	root    string
	retryAt time.Time
	backoff time.Duration
	err     error
	// changed is when the chat last changed on disk, read when it was found.
	changed time.Time
	// size is what the last attempt had to upload, 0 before the first one.
	size int64
	// logged is the last outcome written to the log for this chat.
	logged string
}

// SharingRetryMax bounds retry pauses for unfinished pairing publication.
const SharingRetryMax = time.Minute

// PublishIdleWithin is how long a background publish may go without progress.
//
// A BACKGROUND PUBLISH IS BOUNDED BY PROGRESS, NOT BY SIZE. It once ran under
// cellstore.SealWithin for the whole chat, the budget that keeps a save from
// stalling a turn. Thirty seconds moves perhaps a hundred megabytes over a home
// uplink, so a chat whose saved folder was larger failed on every attempt and
// was never listed on the other computer. Now each answer from the relay starts
// the clock again: a large chat uploads for as long as it keeps moving, and a
// stuck connection still ends the attempt. Frames already sent are kept
// (cellsync.Publisher.Upload), so the next attempt resumes. Tests shorten it.
var PublishIdleWithin = 2 * time.Minute

// An unsaved chat stays pending without reporting a failure or delaying retry.
var errNoChatHead = errors.New("chat has nothing to share yet")

// errPublishStalled ends an attempt that made no progress for PublishIdleWithin.
var errPublishStalled = errors.New("the upload made no progress")

// RefreshLocalChats discovers newly saved chats without forgetting completed
// publications or the retry deadlines of chats still waiting to be shared.
func (s *Sync) RefreshLocalChats() error {
	s.publicationMu.Lock()
	defer s.publicationMu.Unlock()
	return s.scanLocalChats()
}

// LocalChatRoots are the folders of the chats this home could share: every
// chat folder with a cell, and every finished copy of an older chat, which
// lives in its project's `.moving` folder (cell.CopyRoot), when the original
// may be shared at all (OlderChatShareable).
func LocalChatRoots(home string) ([]string, error) {
	dirs, err := filepath.Glob(filepath.Join(home, "v3", "projects", "*", "*"))
	if err != nil {
		return nil, err
	}
	roots := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if _, err := os.Stat(filepath.Join(dir, cell.MetaPath)); err == nil {
			roots = append(roots, dir)
			continue
		}
		copied := cell.CopyRoot(dir)
		if _, err := os.Stat(filepath.Join(copied, filepath.FromSlash(cell.CopyReadyPath))); err != nil {
			continue
		}
		if meta, err := session.LoadMeta(dir); err == nil && !OlderChatShareable(meta) {
			continue
		}
		roots = append(roots, copied)
	}
	return roots, nil
}

// OlderChatShareable says an older chat may be copied and shared: it has its
// own folder, or it does not work in the home folder. A copy's first seal walks
// the whole working folder, and over the home folder that takes minutes while
// every open chat's tools wait; such a chat keeps opening where it is and stays
// on this computer (PROTOCOL: older chats working in the home folder stay local).
func OlderChatShareable(meta session.Meta) bool {
	return meta.Owned || !session.HoldsHomeFolder(meta.Workspace)
}

func (s *Sync) scanLocalChats() error {
	roots, err := LocalChatRoots(s.Home)
	if err != nil {
		return err
	}
	if s.publication == nil {
		s.publication = map[string]*pendingPublication{}
	}
	if s.publicationSeen == nil {
		s.publicationSeen = map[string]struct{}{}
	}
	for _, root := range roots {
		id := filepath.Base(root)
		if _, known := s.publicationSeen[id]; known || !cell.ValidID(id) {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, cell.MetaPath)); os.IsNotExist(err) {
			continue
		}
		pending := &pendingPublication{root: root}
		if info, err := os.Stat(filepath.Join(root, cell.TranscriptPath)); err == nil {
			pending.changed = info.ModTime()
		}
		s.publication[id] = pending
		s.publicationSeen[id] = struct{}{}
	}
	return nil
}

// publicationOrder is the order one pass offers the pending chats in.
//
// SMALL AND RECENT FIRST. The pass is sequential, and it once went in map
// order, so one large chat could hold every other chat back on every pass.
// A chat not tried yet comes first, newest change first: its size is unknown,
// and the chat a person used last is the one they look for on the other
// computer. A chat already tried comes after them, smallest upload first.
func (s *Sync) publicationOrder() []string {
	ids := make([]string, 0, len(s.publication))
	for id := range s.publication {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b string) int {
		pa, pb := s.publication[a], s.publication[b]
		if tried := cmp.Compare(pa.tried(), pb.tried()); tried != 0 {
			return tried
		}
		if pa.size != pb.size {
			return cmp.Compare(pa.size, pb.size)
		}
		if !pa.changed.Equal(pb.changed) {
			return pb.changed.Compare(pa.changed)
		}
		return strings.Compare(a, b)
	})
	return ids
}

// tried is 1 for a chat an earlier attempt failed to share, so it sorts last.
func (p *pendingPublication) tried() int {
	if p.err != nil || p.size > 0 {
		return 1
	}
	return 0
}

// PublishLocalChats looks for new chats on every pass and offers each chat
// not yet shared, smallest and most recent first (publicationOrder). Chats this
// pass has shared are not looked at again until the next launch. A rescan is
// a glob and a stat for folders not seen yet, so a chat restored to disk or
// written by another process is picked up on the next pass.
func (s *Sync) PublishLocalChats(ctx context.Context, now func() time.Time, say func(cell.Cell, string)) error {
	s.publicationMu.Lock()
	defer s.publicationMu.Unlock()
	if Quiet(s.Home) {
		return nil
	}
	if err := s.scanLocalChats(); err != nil {
		return err
	}
	var failures error
	for _, id := range s.publicationOrder() {
		pending := s.publication[id]
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if now().Before(pending.retryAt) {
			failures = errors.Join(failures, pending.err)
			continue
		}
		began := time.Now()
		attempt, err := s.publishLocalChat(ctx, pending.root, id)
		if errors.Is(err, errNoChatHead) {
			continue
		}
		if attempt.size > 0 {
			pending.size = attempt.size
		}
		// One line per chat and outcome: how long it took and what it sent, so a
		// stalled connection reads differently from a slow upload.
		if outcome := publicationOutcome(err); outcome != pending.logged {
			pending.logged = outcome
			log.Printf("sharing chat %s: %s after %s, %d of %d bytes sent", id, outcome, time.Since(began).Round(time.Millisecond), attempt.sent, attempt.size)
		}
		if err == nil {
			delete(s.publication, id)
			continue
		}
		pending.err = err
		if pending.backoff == 0 {
			pending.backoff = DefaultInterval
		} else {
			pending.backoff = min(2*pending.backoff, SharingRetryMax)
		}
		pending.retryAt = now().Add(pending.backoff)
		failures = errors.Join(failures, err)
		if say != nil {
			say(cell.Cell{ID: id, Root: pending.root}, "This chat could not be shared yet. It is still saved here; codeaf will try again.")
		}
	}
	return failures
}

// publicationOutcome is the log's word for how one attempt ended.
func publicationOutcome(err error) string {
	switch {
	case err == nil:
		return "shared"
	case errors.Is(err, errPublishStalled):
		return fmt.Sprintf("stopped, no progress for %s", PublishIdleWithin)
	}
	return "failed: " + err.Error()
}

// copyAttempt is what one background publish sent and had to send.
type copyAttempt struct {
	sent, size int64
}

func (s *Sync) publishLocalChat(ctx context.Context, root, id string) (copyAttempt, error) {
	c, err := cell.OpenAt(root, id)
	if err != nil {
		return copyAttempt{}, err
	}
	head, err := cellstore.Head(c)
	if err != nil {
		return copyAttempt{}, err
	}
	if head == nil {
		return copyAttempt{}, errNoChatHead
	}
	meta, err := session.LoadMeta(root)
	if err != nil {
		return copyAttempt{}, err
	}
	e := cellstore.EngineFor(meta.Workspace)
	e.DataRoot = filepath.Join(s.Home, "v3", "stores")
	if meta.Owned {
		e.Workspace = filepath.Join(root, "work")
	}
	idle, watch, stop := progressContext(ctx, PublishIdleWithin)
	defer stop()
	attempt, err := s.publishCopy(idle, e, c, devname.Name(s.Home), meta.Title, watch)
	if err != nil && context.Cause(idle) == errPublishStalled && ctx.Err() == nil {
		err = fmt.Errorf("%w: %w", errPublishStalled, err)
	}
	if err == nil && filepath.Base(filepath.Dir(root)) == ".moving" {
		// The older chat's copy is shared; the terminal's copier skips it now.
		_ = os.WriteFile(filepath.Join(root, filepath.FromSlash(cell.CopyOfferedPath)), nil, 0600)
	}
	return attempt, err
}

// progressContext is ctx ended once a request to the relay goes unanswered
// for within. Only time spent waiting for the relay counts: packing a large chat
// on this computer, or waiting for its seal, is not a stalled connection.
func progressContext(ctx context.Context, within time.Duration) (context.Context, *watchdog, func()) {
	idle, cancel := context.WithCancelCause(ctx)
	timer := time.AfterFunc(within, func() { cancel(errPublishStalled) })
	timer.Stop()
	return idle, &watchdog{timer: timer, within: within}, func() { timer.Stop(); cancel(nil) }
}

// watchdog times one request to the relay at a time; a background publish
// asks them one after another.
type watchdog struct {
	timer  *time.Timer
	within time.Duration
}

// asking starts the clock for one request; the returned func stops it on the
// answer, whatever it is. A nil watchdog times nothing.
func (w *watchdog) asking() func() {
	if w == nil {
		return func() {}
	}
	w.timer.Reset(w.within)
	return func() { w.timer.Stop() }
}

// progressStore times every request to the relay and counts the bytes that
// reached it: a put that failed or was cut short sent nothing that stays.
type progressStore struct {
	blobstore.Store
	watch  *watchdog
	landed *int64
}

func (p progressStore) PutFrame(ctx context.Context, frame []byte) (blobstore.FrameID, error) {
	answered := p.watch.asking()
	defer answered()
	id, err := p.Store.PutFrame(ctx, frame)
	if err == nil {
		*p.landed += int64(len(frame))
	}
	return id, err
}

func (p progressStore) Has(ctx context.Context, rids []string) ([]bool, error) {
	answered := p.watch.asking()
	defer answered()
	return p.Store.Has(ctx, rids)
}

// PublishCopy offers a background copy once, without ever acquiring an existing
// chat. The foreground may have opened it while it was copied; only Create's
// atomic absence check can decide who first publishes it (C29.2/C31.3).
func (s *Sync) PublishCopy(ctx context.Context, e cellstore.Engine, c cell.Cell, name, title string) error {
	_, err := s.publishCopy(ctx, e, c, name, title, nil)
	return err
}

// publishCopy is PublishCopy that times each request to the relay with watch
// and reports what it sent.
func (s *Sync) publishCopy(ctx context.Context, e cellstore.Engine, c cell.Cell, name, title string, watch *watchdog) (copyAttempt, error) {
	var attempt copyAttempt
	if Quiet(s.Home) {
		return attempt, ErrQuiet
	}
	answered := watch.asking()
	_, err := s.Dir.Cell(ctx, c.ID)
	if errors.Is(err, directory.ErrNotFound) {
		err = s.putDevice(ctx, name)
	} else if err == nil {
		answered()
		return attempt, nil
	}
	answered()
	if err != nil {
		return attempt, err
	}
	if err := cellstore.Await(ctx, c); err != nil {
		return attempt, err
	}
	head, err := cellstore.Head(c)
	if err != nil || head == nil {
		return attempt, err
	}
	scope := s.scope(c.ID)
	defer scope.Settle()
	var landed int64
	store := progressStore{Store: scope.Store, watch: watch, landed: &landed}
	engine := e.Sync(cellstore.SyncKeys{CellKey: s.Identity.CellKey(), Dedup: s.Identity.DedupSecret()}, s.Ledger)
	publisher := cellsync.Publisher{Engine: engine, Store: store, Dir: s.Dir}
	ex, err := publisher.Upload(ctx, c, head.Turn.ID)
	attempt = copyAttempt{sent: landed, size: ex.Bytes}
	if err != nil {
		return attempt, err
	}
	info := s.publishInfo(c, title)
	// Serialize Create and its release with foreground opening on this home.
	// A foreground resume deliberately keeps its existing fence.
	lock, err := s.lockChat(c.ID, ".publication")
	if err != nil {
		return attempt, err
	}
	defer lock.Close()
	answered = watch.asking()
	view, err := s.Dir.Create(ctx, c.ID, directory.CellInit{Head: head.Turn.ID, SavedAt: head.Turn.SealedAtMs, Class: info.Class, Title: info.Title, Size: info.Size, Keys: info.Keys})
	answered()
	if errors.Is(err, directory.ErrExists) {
		return attempt, nil
	}
	if err != nil {
		return attempt, err
	}
	if err := cellstore.NotePublished(c, head.Turn.ID); err != nil {
		return attempt, err
	}
	answered = watch.asking()
	err = directory.ReleaseQuiet(ctx, s.Dir, c.ID, view.Cell.Lease.Fence)
	answered()
	if errors.Is(err, directory.ErrFenceStale) {
		return attempt, nil
	}
	return attempt, err
}

// Unshared counts the chats on this computer that the other computers cannot
// list yet, because no directory record names them. The terminal and the app
// say it in a sentence, so a short list on another computer reads as "not
// shared yet" and not as lost.
type Unshared struct {
	// Waiting have not been uploaded yet: queued, uploading, or older chats
	// still to be copied.
	Waiting int `json:"waiting"`
	// Failing are chats whose last upload failed; codeaf tries again.
	Failing int `json:"failing"`
	// KeptHere are older chats a rule keeps on this computer: they were last
	// used over 30 days ago, which opening one here shares, or they work in
	// the home folder, which stays here.
	KeptHere int `json:"kept_here"`
}

// Total is every chat the other computers cannot list yet.
func (u Unshared) Total() int { return u.Waiting + u.Failing + u.KeptHere }

// CountUnshared counts this home's cell chats that have never been shared.
// It reads only the disk: a chat counts as shared once it crossed a
// publication or adoption boundary (cellstore.Shared), or once this process
// found a directory record for it. A chat with nothing saved yet is not
// counted; there is nothing to list. Older chats are counted by their owner.
func (s *Sync) CountUnshared() (Unshared, error) {
	roots, err := LocalChatRoots(s.Home)
	if err != nil {
		return Unshared{}, err
	}
	s.publicationMu.Lock()
	failing := map[string]bool{}
	confirmed := map[string]bool{}
	for id := range s.publicationSeen {
		confirmed[id] = true
	}
	for id, p := range s.publication {
		delete(confirmed, id)
		if p.err != nil {
			failing[id] = true
		}
	}
	s.publicationMu.Unlock()
	var out Unshared
	for _, root := range roots {
		id := filepath.Base(root)
		if !cell.ValidID(id) || confirmed[id] {
			continue
		}
		c, err := cell.OpenAt(root, id)
		if err != nil {
			continue
		}
		if shared, err := cellstore.Shared(c); err != nil || shared {
			continue
		}
		if head, err := cellstore.Head(c); err != nil || head == nil {
			continue
		}
		if failing[id] {
			out.Failing++
		} else {
			out.Waiting++
		}
	}
	return out, nil
}
