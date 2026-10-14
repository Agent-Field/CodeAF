package builder

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/draft"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// pollEvery is how often the journal's size is read when no file watch could be
// set, and coalesce is how long a burst of appends is let to finish before one
// render answers all of it.
const (
	pollEvery = 250 * time.Millisecond
	coalesce  = 40 * time.Millisecond
)

// phaseRank orders the draft's phases, so "the plan is ready or further" is one
// comparison.
var phaseRank = map[draft.Phase]int{
	draft.PhasePlanning: 0, draft.PhasePlanned: 1, draft.PhaseBuilding: 2, draft.PhaseDone: 3,
}

func phaseAtLeast(have, want draft.Phase) bool { return phaseRank[have] >= phaseRank[want] }

// feed turns the journal into what the owner sees: for each new entry one draft
// event and one step line, and after each burst one render written to the app's
// draft. The tail goroutine and the owner's edits both call pump, so an edit
// shows at once and no entry is published twice.
type feed struct {
	s      *Service
	b      *build
	drafts *draft.Store

	mu        sync.Mutex
	seq       int
	files     spec.Files
	announced bool
	plan      []byte // the last plan.json written while the plan was ready
}

// newFeed starts after the entries the journal already holds, so a restart does
// not replay the build to the desk; the first pump still renders them.
func newFeed(s *Service, b *build, drafts *draft.Store) *feed {
	f := &feed{s: s, b: b, drafts: drafts}
	if entries, err := drafts.Entries(0); err == nil && len(entries) > 0 {
		f.seq = entries[len(entries)-1].Seq
	}
	if st, err := drafts.State(); err == nil {
		f.announced = phaseAtLeast(st.Phase, draft.PhasePlanned)
	}
	return f
}

// pump publishes the entries since the last pump and writes the render. It is
// safe to call from several goroutines and cheap when nothing is new.
func (f *feed) pump(ctx context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entries, err := f.drafts.Entries(f.seq)
	if err != nil {
		return
	}
	for _, e := range entries {
		f.publish(e)
		f.seq = e.Seq
	}
	st, err := f.drafts.State()
	if err != nil {
		return
	}
	f.flush(ctx, st)
	f.announcePlan(st)
}

func (f *feed) publish(e draft.Entry) {
	f.s.all.Hub.Publish(bridge.TopicDraft(f.b.app), bridge.DraftEvent{
		Seq: e.Seq, Kind: e.Piece.Kind, ID: e.Piece.ID, Line: e.Line, Author: string(e.Author), At: e.At,
	})
	if step, ok := stepOf(e); ok && strings.TrimSpace(e.Line) != "" {
		f.s.step(f.b, step, bridge.BuildRunning, e.Line)
	}
}

// stepOf puts an entry on the Building row it belongs to; the phase marker is
// not work and shows no line.
func stepOf(e draft.Entry) (bridge.BuildStep, bool) {
	switch {
	case e.Piece.Kind == "phase":
		return "", false
	case e.Piece.Kind == "name", strings.HasPrefix(e.Piece.Kind, "plan."):
		return bridge.StepPlan, true
	}
	return bridge.StepWrite, true
}

// flush renders the draft and writes the files that changed. The plan card is
// kept current from the same fold, with the owner's own words as plan.ask.
func (f *feed) flush(ctx context.Context, st draft.State) {
	files := f.s.render(f.b, st)
	if plan, err := decodePlan(string(files[spec.PlanFile])); err == nil {
		f.b.setPlan(plan)
	}
	files = f.holdPlan(st, files)
	if sameFiles(f.files, files) {
		return
	}
	if err := f.s.all.Store.WriteFiles(ctx, f.b.app, files); err == nil {
		f.files = files
	}
}

// holdPlan keeps plan.json off the app's draft until the plan is ready. The
// desk prefers plan.json over the streamed lines, so a nearly empty one fetched
// early would freeze the card. While a replan runs, the last ready plan.json
// stays until the new plan is ready.
func (f *feed) holdPlan(st draft.State, files spec.Files) spec.Files {
	held := spec.Files{}
	for path, data := range files {
		held[path] = data
	}
	if phaseAtLeast(st.Phase, draft.PhasePlanned) {
		f.plan = files[spec.PlanFile]
		return held
	}
	delete(held, spec.PlanFile)
	if f.plan != nil {
		held[spec.PlanFile] = f.plan
	}
	return held
}

// announcePlan shows the plan card the first time the draft's plan is ready.
func (f *feed) announcePlan(st draft.State) {
	if f.announced || !phaseAtLeast(st.Phase, draft.PhasePlanned) {
		return
	}
	f.announced = true
	f.s.emitPlan(f.b, f.b.currentPlan())
}

// render is the pure render of a draft with the facts of the create sheet.
func (s *Service) render(b *build, st draft.State) spec.Files {
	return draft.Render(st, s.defaults(b))
}

// defaults are the create-sheet facts a render takes from the build, not the
// worker.
func (s *Service) defaults(b *build) draft.Defaults {
	return draft.Defaults{
		AppID: b.app, Space: personalSpace, Ask: b.currentPlan().Ask,
		AgentModel: b.agentModel, BudgetMonth: b.budget, Kit: kitVersion(), Tint: b.tintChoice(),
	}
}

func kitVersion() int {
	kit, err := spec.LoadKit()
	if err != nil {
		return 0
	}
	return kit.Kit
}

func sameFiles(a, b spec.Files) bool {
	if len(a) != len(b) {
		return false
	}
	for path, data := range a {
		if !bytes.Equal(data, b[path]) {
			return false
		}
	}
	return true
}

// watch tails the journal until ctx ends, then reads it one last time. A
// worker's last verb can land the instant before it exits, and the file watch
// can be a moment behind it; the final read is what makes the render complete.
func (f *feed) watch(ctx context.Context) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	wake, release := journalWake(f.drafts.Dir())
	go func() {
		defer close(done)
		defer release()
		for {
			select {
			case <-ctx.Done():
				return
			case <-wake:
				pause(ctx, coalesce)
				f.pump(ctx)
			}
		}
	}()
	return func() {
		cancel()
		<-done
		f.pump(context.WithoutCancel(ctx))
	}
}

func pause(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// journalWake signals on a channel whenever the journal may have grown. It
// watches the folder with fsnotify, and when that cannot be set it reads the
// journal's size every pollEvery instead.
func journalWake(dir string) (wake <-chan struct{}, release func()) {
	ch := make(chan struct{}, 1)
	ping := func() {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	quit := make(chan struct{})
	if watcher, err := fsnotify.NewWatcher(); err == nil {
		if err = watcher.Add(dir); err == nil {
			go relay(watcher, ping, quit)
			return ch, func() { close(quit); _ = watcher.Close() }
		}
		_ = watcher.Close()
	}
	go pollSize(filepath.Join(dir, draft.JournalFile), ping, quit)
	return ch, func() { close(quit) }
}

func relay(watcher *fsnotify.Watcher, ping func(), quit <-chan struct{}) {
	for {
		select {
		case <-quit:
			return
		case ev, ok := <-watcher.Events:
			if !ok {
				return
			}
			if filepath.Base(ev.Name) == draft.JournalFile {
				ping()
			}
		case _, ok := <-watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

func pollSize(journal string, ping func(), quit <-chan struct{}) {
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	var size int64 = -1
	for {
		select {
		case <-quit:
			return
		case <-tick.C:
			if info, err := os.Stat(journal); err == nil && info.Size() != size {
				size = info.Size()
				ping()
			}
		}
	}
}
