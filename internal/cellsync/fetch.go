package cellsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
	"github.com/Agent-Field/codeaf/internal/syncwords"
)

// FetchWindow is the most frame fetches a take has in flight at once
// (contract §22.6).
const FetchWindow = 8

// GatherWindow is the most GetMany batches a fetch has in flight at once (C5).
const GatherWindow = 8

// ImportEvery is how many frames land between two partial imports (contract
// §22.12): each import walks what is stored so far, so asking after every
// frame would pay the walk many times for little new work.
const ImportEvery = 4

// Fetcher brings a head and everything it needs from the store onto this device.
type Fetcher struct {
	// WarmFrameHints opts into bounded latest-frame priming; off by default.
	WarmFrameHints bool
	Engine         Engine
	Store          blobstore.Store
	// Dir reads the cell's record for the plan (contract §22.2). Nil means no
	// priming: the fetch is exactly the want loop (rotation uses it that way).
	Dir directory.Client
	// View is an already-read directory record for the cell, avoiding a second
	// directory round trip during priming (C6).
	View       *directory.CellView
	Inbox      func(c cell.Cell) string
	OnProgress func(have, want int) // optional; want grows as the graph is learned
	// ManyMax bounds the rids one GetMany asks for; 0 is blobstore.MaxGetMany.
	// ManyMaxFor picks it for a relay.
	ManyMax int
	// Concurrency bounds how many GetMany requests run in flight at once; 0 is GatherWindow.
	Concurrency int
	// Sleep waits between retries; nil uses blobstore.Sleep.
	Sleep      func(context.Context, time.Duration) error
	openRounds atomic.Int64
}

func (f *Fetcher) addOpenRound(n int) {
	f.openRounds.Add(int64(n))
}

// OpenRounds reports how many serial store rounds StageChat performed.
func (f *Fetcher) OpenRounds() int {
	return int(f.openRounds.Load())
}

// HostedGetMany is the most rids one GetMany asks the hosted relay for. Its
// batch get reads, for every frame a request touches, the bytes from the first
// wanted object to the last, all at once in a 128 MB isolate, while bulk frames
// are BulkFrame (8 MiB). Measured on a 1.4 GB chat's real frames (N21), 256
// rids made it read up to 65 MB for a few hundred KB wanted, and the move
// failed with a 500 on every try; 16 keeps a request at about 8 MB in walk
// order and under 15 MB even for scattered objects. A big move asks about
// three times as often.
const HostedGetMany = 16

// ManyMaxFor is the GetMany bound for relay: HostedGetMany for a hosted sync
// relay (the "(hosted)" one the device screens name), the full batch for a
// relay of the person's own, which reads each object by itself.
func ManyMaxFor(relay string) int {
	if syncwords.Hosted(relay) {
		return HostedGetMany
	}
	return blobstore.MaxGetMany
}

func (f *Fetcher) manyMax() int {
	if f.ManyMax <= 0 || f.ManyMax > blobstore.MaxGetMany {
		return blobstore.MaxGetMany
	}
	return f.ManyMax
}

func (f *Fetcher) gatherWindow() int {
	if f.Concurrency <= 0 {
		return GatherWindow
	}
	return f.Concurrency
}

func (f *Fetcher) sleep(ctx context.Context, d time.Duration) error {
	if f.Sleep != nil {
		return f.Sleep(ctx, d)
	}
	return blobstore.Sleep(ctx, d)
}

func (f *Fetcher) cellView(ctx context.Context, id string) (directory.CellView, error) {
	if f.View != nil {
		return *f.View, nil
	}
	if f.Dir == nil {
		return directory.CellView{}, errors.New("cellsync: no directory")
	}
	return f.Dir.Cell(ctx, id)
}

// Fetch completes head in the store and then materializes it. A rid the
// store does not hold is an error that names it.
func (f *Fetcher) Fetch(ctx context.Context, c cell.Cell, head string) error {
	if err := f.Complete(ctx, c, head); err != nil {
		return err
	}
	return f.Engine.Materialize(ctx, c, head)
}

// StageChat fetches the conversation frames needed for stageState to restore
// the chat's conversation records (.cell/) before the lease is acquired, and
// leaves any remaining workspace frames for Complete after the chat opens (C1).
// It reports whether all objects head wants are already in the store.
func (f *Fetcher) StageChat(ctx context.Context, c cell.Cell, head string, stageState func(context.Context, cell.Cell, string) error) (complete bool, err error) {
	f.openRounds.Store(0)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	mainInbox := f.Inbox(c)
	if err := os.MkdirAll(mainInbox, 0o700); err != nil {
		return false, err
	}
	if _, _, _, hasKeys := parseKeys(f.Engine, c); hasKeys {
		if view, vErr := f.cellView(ctx, c.ID); vErr == nil && len(view.Cell.Frames) > 0 {
			frames := view.Cell.Frames
			stageInbox := mainInbox + ".stage-" + head
			if err := os.MkdirAll(stageInbox, 0o700); err != nil {
				return false, err
			}
			lastFrame := frames[len(frames)-1]
			seenStage, _ := readSeenFrames(stageInbox)
			if !seenStage[lastFrame] {
				f.addOpenRound(1)
				if _, fErr := f.fetchFrames(ctx, stageInbox, []string{lastFrame}, func() {}); fErr != nil {
					return false, fErr
				}
			}
			if ok, sErr := f.stageOpenFromInbox(ctx, c, head, stageInbox, mainInbox); sErr == nil && ok {
				return false, nil
			}
		}
	}
	want, err := f.Engine.Want(ctx, c, head)
	if err != nil {
		return false, err
	}
	if len(want) == 0 {
		if stageState != nil {
			if err := stageState(ctx, c, head); err != nil {
				return false, err
			}
		}
		return true, nil
	}
	if view, vErr := f.cellView(ctx, c.ID); vErr == nil && len(view.Cell.Frames) > 0 {
		frames := view.Cell.Frames
		stageInbox := mainInbox + ".stage-" + head
		if err := os.MkdirAll(stageInbox, 0o700); err != nil {
			return false, err
		}
		convFrames := []string{frames[len(frames)-1]}
		f.addOpenRound(1)
		allLanded, err := f.fetchFrames(ctx, stageInbox, convFrames, func() {})
		if err != nil {
			return false, err
		}
		if !f.Engine.Holds(c) && len(convFrames) == len(frames) && allLanded {
			if _, err := f.Engine.ImportPrimed(ctx, c, head, stageInbox); err == nil {
				_ = os.WriteFile(filepath.Join(mainInbox, ".primed-"+head), []byte("1"), 0o600)
				markSeenFrames(mainInbox, frames)
				removeStageInbox(stageInbox)
			}
		} else {
			for {
				n, impErr := f.Engine.ImportPartial(ctx, c, head, stageInbox)
				if impErr != nil || n == 0 {
					break
				}
			}
		}
		if stageState != nil {
			sErr := stageState(ctx, c, head)
			for pass := 0; pass < 2 && sErr != nil && len(convFrames) < len(frames); pass++ {
				want, wErr := f.Engine.Want(ctx, c, head)
				if wErr != nil || len(want) == 0 {
					break
				}
				f.addOpenRound(1)
				if gErr := f.gather(ctx, stageInbox, want); gErr != nil {
					break
				}
				_, _ = f.Engine.ImportPartial(ctx, c, head, stageInbox)
				sErr = stageState(ctx, c, head)
			}
			if sErr == nil {
				rem, wErr := f.Engine.Want(ctx, c, head)
				if wErr == nil && len(rem) == 0 {
					_ = os.WriteFile(filepath.Join(mainInbox, ".primed-"+head), []byte("1"), 0o600)
					markSeenFrames(mainInbox, frames)
					removeStageInbox(stageInbox)
					return true, nil
				}
				return false, nil
			}
		}
	}
	if err := f.Complete(ctx, c, head); err != nil {
		return false, err
	}
	if stageState != nil {
		if err := stageState(ctx, c, head); err != nil {
			return false, err
		}
	}
	return true, nil
}

// Complete is Fetch without the restore: everything head needs is in the
// device's store afterwards and the folder the person works in is not touched.
// It is how a device holds a chat's objects without taking the chat over.
func (f *Fetcher) Complete(ctx context.Context, c cell.Cell, head string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	inbox := f.Inbox(c)
	if err := os.MkdirAll(inbox, 0o700); err != nil {
		return err
	}
	// Priming works in an inbox of its own beside the shared one: a device's
	// daemon fetches a head it watches through the same inbox the take uses,
	// and a primed import deletes what no pass wanted — in a shared inbox that
	// is another fetcher's file, read one moment and gone the next. Each
	// priming owns its own files and the strict loop never sees them.
	if err := f.prime(ctx, c, head, inbox+".prime"); err != nil {
		return err
	}
	primedMark := filepath.Join(inbox, ".primed-"+head)
	have, known := 0, 0
	for {
		want, err := f.Engine.Want(ctx, c, head)
		if err != nil {
			return err
		}
		if len(want) == 0 {
			if view, vErr := f.cellView(ctx, c.ID); vErr == nil && len(view.Cell.Frames) > 0 {
				markSeenFrames(inbox, view.Cell.Frames)
			}
			_ = os.Remove(primedMark)
			return nil
		}
		known += len(want)
		n, err := f.round(ctx, c, head, inbox, want)
		if err != nil {
			return err
		}
		_ = os.WriteFile(primedMark, []byte("1"), 0o600)
		have += n
		f.progress(have, known)
	}
}

// round gets every wanted rid and imports them. An import that takes nothing
// would repeat forever, so it is an error.
func (f *Fetcher) round(ctx context.Context, c cell.Cell, head, inbox string, want []string) (int, error) {
	if err := f.gather(ctx, inbox, want); err != nil {
		return 0, err
	}
	n, err := f.Engine.Import(ctx, c, head, inbox)
	if err == nil && n == 0 {
		err = errors.New("cellsync: import took none of the wanted objects")
	}
	return n, err
}

// gather brings every wanted object into the inbox, up to GatherWindow
// requests of at most ManyMax rids in flight at once (C5). Objects already
// sitting in the inbox from an earlier attempt are kept rather than fetched
// again (C4). An object the store does not hold is an error that names it.
func (f *Fetcher) gather(ctx context.Context, inbox string, want []string) error {
	need, err := pruneAndFilterInbox(inbox, want)
	if err != nil || len(need) == 0 {
		return err
	}
	limit := f.manyMax()
	var batches [][]string
	for len(need) > 0 {
		n := min(len(need), limit)
		batches = append(batches, need[:n])
		need = need[n:]
	}
	if len(batches) == 1 {
		return f.gatherBatch(ctx, inbox, batches[0])
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	window := f.gatherWindow()
	var mu sync.Mutex
	var firstErr error
	setErr := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
		mu.Unlock()
	}
	for len(batches) > 0 {
		mu.Lock()
		failed := firstErr != nil
		mu.Unlock()
		if failed {
			break
		}
		if err := ctx.Err(); err != nil {
			setErr(err)
			break
		}
		wave := batches[:min(len(batches), window)]
		batches = batches[len(wave):]
		var ready, wg sync.WaitGroup
		ready.Add(len(wave))
		wg.Add(len(wave))
		start := make(chan struct{})
		for _, batch := range wave {
			go func(b []string) {
				defer wg.Done()
				ready.Done()
				<-start
				if err := f.gatherBatch(ctx, inbox, b); err != nil {
					setErr(err)
				}
			}(batch)
		}
		ready.Wait()
		close(start)
		wg.Wait()
	}
	return firstErr
}

func (f *Fetcher) gatherBatch(ctx context.Context, inbox string, batch []string) error {
	for len(batch) > 0 {
		objects, err := f.getMany(ctx, batch)
		if err != nil {
			return fmt.Errorf("cellsync: fetch object %s: %w", batch[0], err)
		}
		if len(objects) == 0 {
			return fmt.Errorf("cellsync: the store answered no object for %s", batch[0])
		}
		if err := keep(inbox, objects); err != nil {
			return err
		}
		batch = batch[len(objects):]
	}
	return nil
}

func (f *Fetcher) getMany(ctx context.Context, batch []string) ([]blobstore.Object, error) {
	backoff := blobstore.RetryBackoff
	var objects []blobstore.Object
	var err error
	for attempt := 0; attempt <= blobstore.FetchRetries; attempt++ {
		if attempt > 0 {
			if sleepErr := f.sleep(ctx, backoff); sleepErr != nil {
				return nil, sleepErr
			}
			backoff *= 2
		}
		objects, err = f.Store.GetMany(ctx, batch)
		if err == nil || blobstore.Retried(err) || !blobstore.Transient(ctx, err) {
			return objects, err
		}
	}
	return objects, err
}

func (f *Fetcher) getFrame(ctx context.Context, id string) ([]byte, error) {
	backoff := blobstore.RetryBackoff
	var frame []byte
	var err error
	for attempt := 0; attempt <= blobstore.FetchRetries; attempt++ {
		if attempt > 0 {
			if sleepErr := f.sleep(ctx, backoff); sleepErr != nil {
				return nil, sleepErr
			}
			backoff *= 2
		}
		frame, err = f.Store.GetFrame(ctx, id)
		if err == nil || blobstore.Retried(err) || !blobstore.Transient(ctx, err) {
			return frame, err
		}
	}
	return frame, err
}

// pruneAndFilterInbox keeps files in inbox that belong to want and removes
// incomplete or stale files, answering the subset of want still missing from inbox.
func pruneAndFilterInbox(inbox string, want []string) ([]string, error) {
	wanted := make(map[string]bool, len(want))
	for _, rid := range want {
		wanted[rid] = true
	}
	entries, err := os.ReadDir(inbox)
	if err != nil {
		return nil, err
	}
	have := make(map[string]bool, len(entries))
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || !wanted[name] {
			_ = os.Remove(filepath.Join(inbox, name))
			continue
		}
		if info, err := e.Info(); err == nil && info.Size() > 0 {
			have[name] = true
		} else {
			_ = os.Remove(filepath.Join(inbox, name))
		}
	}
	need := make([]string, 0, len(want))
	for _, rid := range want {
		if !have[rid] {
			need = append(need, rid)
			have[rid] = true
		}
	}
	return need, nil
}

// prime reads the plan the record holds for the cell and fetches the frames
// it names, then runs one ImportPrimed over what they carried (contract §22.5).
// It is an optimisation only: a record that names no frames, a frame that is
// absent or undecodable, a store that cannot be asked, and even an import
// that refuses what priming delivered all degrade into the want loop, which
// is the completeness proof of every take. Only a local failure — the inbox —
// is an error. A refusing import is a degrade, not an error, because the loop
// fetches objects by rid through the index and may be served from a frame
// priming never named; the files priming wrote are removed first, so the
// strict import of the loop never sees them.
func (f *Fetcher) prime(ctx context.Context, c cell.Cell, head, inbox string) error {
	if f.Dir == nil && f.View == nil {
		return nil // no directory, no plan: exactly today's want loop
	}
	// A device that already holds the head wants nothing, and needs no frames.
	mainInbox := strings.TrimSuffix(inbox, ".prime")
	if _, err := os.Stat(filepath.Join(mainInbox, ".primed-"+head)); err == nil {
		return nil
	}
	stageInbox := mainInbox + ".stage-" + head
	usingStage := false
	if info, err := os.Stat(stageInbox); err == nil && info.IsDir() {
		inbox = stageInbox
		usingStage = true
	} else {
		owned, err := ensureHintInbox(filepath.Dir(inbox))
		if err != nil {
			return err
		}
		inbox = owned
		defer removeStageInbox(inbox)
	}
	// A device that already held this chat has most of its history: the plan names its whole upload history, so priming it would
	// download what it has to learn about a one-file change. The want loop
	// fetches a warm take's delta by rid.
	if f.Engine.Holds(c) {
		if usingStage {
			for {
				n, impErr := f.Engine.ImportPartial(ctx, c, head, inbox)
				if impErr != nil || n == 0 {
					break
				}
			}
			want, wErr := f.Engine.Want(ctx, c, head)
			if wErr == nil && len(want) == 0 {
				if view, vErr := f.cellView(ctx, c.ID); vErr == nil && len(view.Cell.Frames) > 0 {
					markSeenFrames(mainInbox, view.Cell.Frames)
				}
				removeStageInbox(stageInbox)
				_ = os.WriteFile(filepath.Join(mainInbox, ".primed-"+head), []byte("1"), 0o600)
				return nil
			}
			if wErr == nil && len(want) > 0 {
				if view, vErr := f.cellView(ctx, c.ID); vErr == nil && len(view.Cell.Frames) > 0 {
					seen, _ := readSeenFrames(mainInbox, stageInbox)
					var delta []string
					for _, id := range view.Cell.Frames {
						if !seen[id] {
							delta = append(delta, id)
						}
					}
					if len(delta) > 0 {
						lap := startOverlap(ctx, f.Engine, c, head, inbox)
						allLanded, fErr := f.fetchFrames(ctx, inbox, delta, lap.landed)
						lap.finish()
						if fErr != nil {
							_, _ = f.Engine.ImportPartial(context.WithoutCancel(ctx), c, head, inbox)
							return fErr
						}
						if allLanded {
							if _, impErr := f.Engine.ImportPrimed(ctx, c, head, inbox); impErr == nil {
								markSeenFrames(mainInbox, view.Cell.Frames)
								removeStageInbox(stageInbox)
								_ = os.WriteFile(filepath.Join(mainInbox, ".primed-"+head), []byte("1"), 0o600)
								return nil
							}
						}
						_, _ = f.Engine.ImportPartial(ctx, c, head, inbox)
					} else {
						if _, impErr := f.Engine.ImportPrimed(ctx, c, head, inbox); impErr == nil {
							markSeenFrames(mainInbox, view.Cell.Frames)
							removeStageInbox(stageInbox)
							_ = os.WriteFile(filepath.Join(mainInbox, ".primed-"+head), []byte("1"), 0o600)
							return nil
						}
						_, _ = f.Engine.ImportPartial(ctx, c, head, inbox)
					}
				}
			}
			removeStageInbox(stageInbox)
		}
		if f.WarmFrameHints {
			return f.primeWarm(ctx, c, head, inbox)
		}
		return nil
	}
	want, err := f.Engine.Want(ctx, c, head)
	if err != nil || len(want) == 0 {
		if usingStage {
			removeStageInbox(stageInbox)
		}
		return nil
	}
	view, err := f.cellView(ctx, c.ID)
	if err != nil || len(view.Cell.Frames) == 0 {
		if usingStage {
			removeStageInbox(stageInbox)
		}
		return nil
	}
	lap := startOverlap(ctx, f.Engine, c, head, inbox)
	allLanded, err := f.fetchFrames(ctx, inbox, view.Cell.Frames, lap.landed)
	lap.finish()
	if err != nil {
		_, _ = f.Engine.ImportPartial(context.WithoutCancel(ctx), c, head, inbox)
		return err
	}
	if !allLanded {
		_, _ = f.Engine.ImportPartial(ctx, c, head, inbox)
		if !usingStage {
			clearInbox(inbox)
		}
		return nil
	}
	if _, err := f.Engine.ImportPrimed(ctx, c, head, inbox); err != nil {
		clearInbox(inbox)
		if usingStage {
			removeStageInbox(stageInbox)
		}
		return nil
	}
	markSeenFrames(mainInbox, view.Cell.Frames)
	if usingStage {
		removeStageInbox(stageInbox)
	}
	_ = os.WriteFile(filepath.Join(mainInbox, ".primed-"+head), []byte("1"), 0o600)
	return nil
}

func removeStageInbox(inbox string) {
	_ = os.RemoveAll(inbox)
	_ = os.Remove(inbox + ".frames-seen")
}

// clearInbox removes what priming left in it: the loop's own import refuses
// files no survey wanted, and a degraded priming must not poison it.
func clearInbox(inbox string) {
	entries, err := os.ReadDir(inbox)
	if err != nil {
		return
	}
	for _, e := range entries {
		_ = os.Remove(filepath.Join(inbox, e.Name()))
	}
}

// fetchFrames writes the objects of every frame the store holds to the inbox,
// FetchWindow fetches in flight at once. A frame the store does not hold, or
// one that fails Decode, is skipped: the want loop asks for what it carried.
// Downloads race each other but writes go through sink: two frames can carry
// the same object, and one rid is written once, by one goroutine.
func (f *Fetcher) fetchFrames(ctx context.Context, inbox string, frames []string, landed func()) (bool, error) {
	alreadySeen, _ := readSeenFrames(inbox)
	need := make([]string, 0, len(frames))
	seen := make(map[string]bool, len(frames))
	for _, id := range frames {
		if seen[id] {
			continue
		}
		seen[id] = true
		if alreadySeen[id] {
			continue
		}
		need = append(need, id)
	}
	if len(need) == 0 {
		return true, nil
	}
	sink := &frameSink{written: map[string]bool{}}
	if entries, err := os.ReadDir(inbox); err == nil {
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), ".") {
				sink.written[e.Name()] = true
			}
		}
	}
	allLanded := true
	var landedMu sync.Mutex
	for len(need) > 0 && !sink.failed() {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		wave := need[:min(len(need), FetchWindow)]
		need = need[len(wave):]
		var ready, wg sync.WaitGroup
		ready.Add(len(wave))
		wg.Add(len(wave))
		start := make(chan struct{})
		for _, id := range wave {
			go func(frameID string) {
				defer wg.Done()
				ready.Done()
				<-start
				frame, err := f.getFrame(ctx, frameID)
				if err != nil {
					landedMu.Lock()
					allLanded = false
					landedMu.Unlock()
					return
				}
				_, objects, err := blobstore.Decode(frame)
				if err != nil {
					landedMu.Lock()
					allLanded = false
					landedMu.Unlock()
					return
				}
				sink.keepFrame(inbox, frameID, objects)
				landed()
			}(id)
		}
		ready.Wait()
		close(start)
		wg.Wait()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return allLanded, sink.err
}

// frameSink serializes priming writes: one rid lands once, and the first local
// failure is kept for the caller.
type frameSink struct {
	mu      sync.Mutex
	written map[string]bool
	err     error
}

func (s *frameSink) keepFrame(inbox, frameID string, objects []blobstore.Object) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return
	}
	for _, o := range objects {
		if s.written[o.RID] {
			continue
		}
		if err := writeWhole(inbox, o); err != nil {
			s.err = err
			return
		}
		s.written[o.RID] = true
	}
	markSeenFrames(inbox, []string{frameID})
}

// writeWhole puts an object in the inbox under its rid only once every byte is
// there: a partial import may list the inbox while frames still land, and it
// skips dot names, so the file is written under one and renamed.
func writeWhole(inbox string, o blobstore.Object) error {
	tmp := filepath.Join(inbox, "."+o.RID)
	if err := os.WriteFile(tmp, o.Bytes, 0o600); err != nil {
		return err
	}
	return fsreplace.Rename(tmp, filepath.Join(inbox, o.RID))
}

// failed reports whether a write has already been refused, so the spawn
// loop can stop asking for frames the inbox can no longer take.
func (s *frameSink) failed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err != nil
}

// keep writes each object atomically to the inbox under its remote id, the name
// Import reads it by, so a failed batch leaves completed objects intact for a retry.
func keep(inbox string, objects []blobstore.Object) error {
	for _, o := range objects {
		if err := writeWhole(inbox, o); err != nil {
			return err
		}
	}
	return nil
}

func (f *Fetcher) progress(have, want int) {
	if f.OnProgress != nil {
		f.OnProgress(have, want)
	}
}
