package cellstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/executor"
)

func TestSealWatchChangesSignalQueuedSentences(t *testing.T) {
	var w SealWatch
	changes := w.Changes()
	if changes == nil || changes != w.Changes() || w.Owed() {
		t.Fatal("zero-value watch must have one stable wake channel and owe nothing")
	}
	for _, step := range []struct {
		speak   func()
		failing bool
	}{
		{func() { w.Note("file kept out") }, false},
		{func() { w.Report(errors.New("disk full")) }, true},
		{func() { w.Report(nil) }, false},
	} {
		step.speak()
		select {
		case <-changes:
		default:
			t.Fatal("queued sentence did not signal")
		}
		if !w.Owed() || w.Take() == "" {
			t.Fatal("a queued sentence must be owed until it is taken")
		}
		if w.Owed() != step.failing {
			t.Fatalf("after the sentence was taken, owed=%v; want %v (only a failure stays owed)", w.Owed(), step.failing)
		}
	}
}

func TestSealWatchChangesCoalesceABurstBeforeSubscription(t *testing.T) {
	var w SealWatch
	for range 10 {
		w.Note("file kept out")
	}
	changes := w.Changes()
	select {
	case <-changes:
	default:
		t.Fatal("news queued before subscription lost its wake")
	}
	select {
	case <-changes:
		t.Fatal("a burst must leave only one pending wake")
	default:
	}
	for range 10 {
		if !w.Owed() || w.Take() != "file kept out" {
			t.Fatal("coalescing wakes lost a sentence")
		}
	}
	if w.Owed() || w.Take() != "" {
		t.Fatal("drained watch still has a sentence")
	}
}

func TestSealWatchChangesDoNotSignalSay(t *testing.T) {
	var said []string
	w := SealWatch{Say: func(line string) { said = append(said, line) }}
	changes := w.Changes()
	w.Note("file kept out")
	w.Report(errors.New("disk full"))
	w.Report(nil)
	if len(said) != 3 || w.Take() != "" {
		t.Fatalf("Say must receive sentences without queueing them: %q", said)
	}
	select {
	case <-changes:
		t.Fatal("a sentence handed to Say woke a queue reader")
	default:
	}
}

func TestSealWatchOwedDoesNotDiscoverRecovery(t *testing.T) {
	c := newCell(t)
	if err := os.WriteFile(rel(c, ".cell/transcript.jsonl"), []byte("reply\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var w SealWatch
	w.TrackTurn(c.Root)
	w.Report(errors.New("disk full"))
	w.Take()
	if _, err := (&fakeEngine{}).engine(t).Seal(t.Context(), c, TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.Changes():
	default:
	}
	if !w.Owed() {
		t.Fatal("a failing watch is owed a look")
	}
	select {
	case <-w.Changes():
		t.Fatal("Owed discovered the recovery and queued it")
	default:
	}
	if w.Failing() || !w.Owed() || !strings.Contains(w.Take(), "sealing works again") || w.Owed() {
		t.Fatal("Failing must still discover recovery after Owed left it alone")
	}
}

func TestSealWatchConcurrentNews(t *testing.T) {
	var w SealWatch
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		for range 1000 {
			w.Note("file kept out")
		}
	}()
	go func() {
		defer workers.Done()
		for range 1000 {
			w.Owed()
			select {
			case <-w.Changes():
			default:
			}
		}
	}()
	taken := 0
	for range 1000 {
		if w.Take() != "" {
			taken++
		}
	}
	workers.Wait()
	for w.Take() != "" {
		taken++
	}
	if taken != 1000 || w.Owed() {
		t.Fatalf("concurrent reader received %d sentences, want 1000 and none pending", taken)
	}
}

func TestSealWatchFailureIsVisibleAndSaidOnce(t *testing.T) {
	var w SealWatch
	w.Report(errors.New("disk full"))
	w.Report(errors.New("disk full"))
	if !w.Failing() {
		t.Fatal("a failed seal must leave the watch failing")
	}
	first := w.Take()
	if !strings.Contains(first, "not being sealed") || !strings.Contains(first, "disk full") {
		t.Fatalf("first sentence %q", first)
	}
	if again := w.Take(); again != "" {
		t.Fatalf("the same failure said again: %q", again)
	}
	if !w.Failing() {
		t.Fatal("saying the sentence must not clear the state")
	}
}

func TestSealWatchRecoveryIsSaidAndClearsTheState(t *testing.T) {
	var w SealWatch
	w.Report(errors.New("disk full"))
	w.Take()
	w.Report(nil)
	if w.Failing() {
		t.Fatal("a seal that held must clear the state")
	}
	if got := w.Take(); !strings.Contains(got, "works again") {
		t.Fatalf("recovery sentence %q", got)
	}
	w.Report(nil)
	if got := w.Take(); got != "" {
		t.Fatalf("a healthy seal after a healthy seal said %q", got)
	}
}

func TestSealWatchCauseChangeIsSaidAgain(t *testing.T) {
	var w SealWatch
	w.Report(errors.New("disk full"))
	w.Take()
	w.Report(errors.New("engine not found"))
	if got := w.Take(); !strings.Contains(got, "different reason") || !strings.Contains(got, "engine not found") {
		t.Fatalf("cause change sentence %q", got)
	}
}

func TestSealWatchFailingAgainAfterRecoveryIsANewFailure(t *testing.T) {
	var w SealWatch
	w.Report(errors.New("disk full"))
	w.Report(nil)
	w.Take()
	w.Take()
	w.Report(errors.New("disk full"))
	if got := w.Take(); !strings.Contains(got, "not being sealed") {
		t.Fatalf("second failure sentence %q", got)
	}
}

// The recorder is what feeds the watch: a seal that fails leaves it failing,
// and the next seal that holds clears it.
func TestRecorderFeedsTheSealWatch(t *testing.T) {
	c := newCell(t)
	fail := true
	e := (&fakeEngine{}).engine(t)
	good := e.Run
	e.Run = func(ctx context.Context, d string, env []string, argv ...string) ([]byte, error) {
		if fail && len(argv) > 2 && argv[2] == "hook" {
			return nil, errors.New("disk full")
		}
		return good(ctx, d, env, argv...)
	}
	var w SealWatch
	r, err := NewRecorder(&stubExec{}, e, c, filepath.Join(t.TempDir(), "wal"), Options{Report: w.Report})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Exec(context.Background(), req(executor.NetPolicy{}, "one"), nil); err != nil {
		t.Fatal(err)
	}
	if !w.Failing() {
		t.Fatal("a failed seal must show")
	}
	fail = false
	if _, err := r.Exec(context.Background(), req(executor.NetPolicy{}, "two"), nil); err != nil {
		t.Fatal(err)
	}
	if w.Failing() {
		t.Fatal("a seal that held must clear it")
	}
}

// A turn ending reaches whoever the door named, and costs nothing when nobody
// listens.
func TestSealWatchForwardsTurnEnd(t *testing.T) {
	var w SealWatch
	w.TurnEnded() // no listener: nothing happens
	heard := 0
	w.OnTurnEnd = func() { heard++ }
	w.TurnEnded()
	w.TurnEnded()
	if heard != 2 {
		t.Fatalf("heard %d turn ends, want 2", heard)
	}
}

// Writes behind the turn-end seal are sealed once, after they stop: a burst of
// them is one seal, and nothing is sealed while a turn is running.
func TestSealWatchSealsTheTailOnceAfterTheWritesStop(t *testing.T) {
	tails := make(chan struct{}, 8)
	w := &SealWatch{Quiet: 30 * time.Millisecond, OnTail: func() { tails <- struct{}{} }}

	w.Wrote() // before any turn has ended nothing is owed: the turn's own seal is coming
	w.TurnEnded()
	for range 5 {
		w.Wrote()
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case <-tails:
	case <-time.After(2 * time.Second):
		t.Fatal("the writes behind the turn-end seal were never sealed")
	}
	select {
	case <-tails:
		t.Fatal("one burst of writes made more than one tail seal")
	case <-time.After(100 * time.Millisecond):
	}

	w.Wrote()
	w.TurnStarted() // the next turn seals its own writes
	select {
	case <-tails:
		t.Fatal("a tail seal fired inside a turn")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestSealWatchNoteIsSaidAndDoesNotFailTheState(t *testing.T) {
	var w SealWatch
	w.Note("zero.txt cannot be read here, so it is left out of the saved history until its permissions allow it")
	if w.Failing() {
		t.Fatal("a note is not a failed seal")
	}
	if got := w.Take(); !strings.Contains(got, "zero.txt") {
		t.Fatalf("note not handed to the surface: %q", got)
	}
}

func TestWithNoticesSendsTheGuardsLineToTheSurface(t *testing.T) {
	var said []string
	e := Engine{}
	WithNotices(func(line string) { said = append(said, line) })(&e)
	e.Guard.notify("one line")
	if len(said) != 1 || said[0] != "one line" {
		t.Fatalf("guard notice went elsewhere: %q", said)
	}
}

func TestWaitingArmsOnlyTheInitialIdleChat(t *testing.T) {
	tails := make(chan struct{}, 2)
	ends := 0
	watch := &SealWatch{Quiet: 10 * time.Millisecond, OnTail: func() { tails <- struct{}{} }, OnTurnEnd: func() { ends++ }}
	watch.Waiting()
	if ends != 0 {
		t.Fatal("initial waiting pretended a foreground turn ended")
	}
	watch.Wrote()
	select {
	case <-tails:
	case <-time.After(time.Second):
		t.Fatal("initial idle write was not sealed")
	}
	watch.TurnStarted()
	watch.Waiting() // Startup notification cannot rearm a running foreground turn.
	watch.Wrote()
	select {
	case <-tails:
		t.Fatal("waiting rearmed an active turn")
	case <-time.After(50 * time.Millisecond):
	}
	watch.TurnEnded()
	if ends != 1 {
		t.Fatal("actual turn end was lost")
	}
}

// Contract A3 keeps an unrelated seal from announcing recovery of this chat.
func TestSealWatchWaitsForItsOwnTurnAndPublish(t *testing.T) {
	c := newCell(t)
	transcript := rel(c, ".cell/transcript.jsonl")
	if err := os.WriteFile(transcript, []byte("the unsaved reply\n"), 0600); err != nil {
		t.Fatal(err)
	}
	engine := (&fakeEngine{}).engine(t)
	engine.Identity = Identity{Device: "this-computer", Fence: 1}
	var watch SealWatch
	watch.TrackTurn(c.Root)
	watch.Report(errors.New("turn seal failed"))
	watch.Take()
	other := newCell(t)
	if _, err := engine.Seal(t.Context(), other, TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	watch.Report(nil)
	if !watch.Failing() || watch.Take() != "" {
		t.Fatal("unrelated successful seal claimed recovery")
	}
	saved, err := engine.Seal(t.Context(), c, TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	watch.Report(nil)
	if !watch.Failing() || watch.Take() != "" {
		t.Fatal("unpublished turn claimed recovery")
	}
	if err := NotePublished(c, saved.Turn.ID); err != nil {
		t.Fatal(err)
	}
	watch.Report(nil)
	if watch.Failing() || !strings.Contains(watch.Take(), "sealing works again") || watch.Take() != "" {
		t.Fatal("own saved and published turn did not say recovery once")
	}
	watch.Report(nil)
	if watch.Take() != "" {
		t.Fatal("recovery repeated")
	}
}

// Contract A3 also waits for publication when startup sealed before its first fence.
func TestSealWatchStartupTurnStillWaitsForPublish(t *testing.T) {
	c := newCell(t)
	if err := os.WriteFile(rel(c, ".cell/transcript.jsonl"), []byte("startup reply\n"), 0600); err != nil {
		t.Fatal(err)
	}
	release := RequirePublication(c)
	defer release()
	var watch SealWatch
	watch.TrackTurn(c.Root)
	watch.Report(errors.New("startup turn failed"))
	watch.Take()
	saved, err := (&fakeEngine{}).engine(t).Seal(t.Context(), c, TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	watch.Report(nil)
	if !watch.Failing() || watch.Take() != "" {
		t.Fatal("startup seal claimed recovery before publishing")
	}
	if err := NotePublished(c, saved.Turn.ID); err != nil {
		t.Fatal(err)
	}
	if watch.Failing() || !strings.Contains(watch.Take(), "sealing works again") {
		t.Fatal("published startup turn did not recover")
	}
}
