package session

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func ledgerRows(t *testing.T, path string) []renderRow {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	var rows []renderRow
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		rows = append(rows, renderRow{})
		if err := jsonUnmarshal(line, &rows[len(rows)-1]); err != nil {
			t.Fatalf("bad row %q: %v", line, err)
		}
	}
	return rows
}

func TestRenderChargeIsKnownOnlyWhenTheProviderSaidSo(t *testing.T) {
	cost, zero, negative, nan := 0.25, 0.0, -1.0, math.NaN()
	for name, test := range map[string]struct {
		usage *ai.Usage
		want  RenderCharge
	}{
		"no usage":  {nil, RenderChargeUnknown},
		"no cost":   {&ai.Usage{}, RenderChargeUnknown},
		"positive":  {&ai.Usage{Cost: &cost}, RenderCharged},
		"zero":      {&ai.Usage{Cost: &zero}, RenderNotCharged},
		"negative":  {&ai.Usage{Cost: &negative}, RenderChargeUnknown},
		"not a num": {&ai.Usage{Cost: &nan}, RenderChargeUnknown},
	} {
		got, amount := renderCharge(test.usage)
		if got != test.want || (got == RenderCharged) != (amount != nil) {
			t.Errorf("%s: charge = %q, %v; want %q", name, got, amount, test.want)
		}
	}
}

// openForTest opens a ledger on a fresh file with a fixed clock.
func openForTest(t *testing.T) (*renderLedger, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "renders.jsonl")
	return openRenderLedger(path, "sess", time.Unix(1_700_000_000, 0)), path
}

var epoch = time.Unix(1_700_000_000, 0)

// One ending per render, whoever gets there first; the record survives a reopen
// exactly as it ended, and reopening writes nothing.
func TestRenderLedgerSealsOnceAndSurvivesReopen(t *testing.T) {
	ledger, path := openForTest(t)
	id := ledger.begin(7, "video", "film/model", "run1", "call1", epoch)
	cost := 0.5
	if !ledger.settle(id, renderPatch{state: RenderCancelled}, epoch.Add(time.Second)) {
		t.Fatal("the first ending was refused")
	}
	if ledger.settle(id, renderPatch{state: RenderFinished, committed: true, path: "/x.mp4", usage: &ai.Usage{Cost: &cost}}, epoch.Add(2*time.Second)) {
		t.Fatal("a late finish replaced the cancel")
	}
	ledger.release(id)
	ledger.close()
	if rows := ledgerRows(t, path); len(rows) != 2 {
		t.Fatalf("%d rows, want start + one end", len(rows))
	}
	reopened := openRenderLedger(path, "sess", epoch.Add(time.Hour))
	got, err := reopened.get(id)
	if err != nil || got.State != RenderCancelled || got.Upstream != "unknown" || got.Committed || got.Path != "" ||
		got.Charge != RenderChargeUnknown || got.DurationMS == nil || *got.DurationMS != 1000 ||
		got.Run != "run1" || got.Call != "call1" || got.Job != 7 || got.Model != "film/model" {
		t.Fatalf("reopened record = %+v, %v", got, err)
	}
	reopened.close()
	if rows := ledgerRows(t, path); len(rows) != 2 {
		t.Fatalf("reopening wrote %d rows, want still 2", len(rows))
	}
}

// A provider that ignores the cancel and later reports a charge: the record
// stays cancelled with no output and gains the receipt — same id, same state —
// once, and a second report changes nothing.
func TestRenderChargeAfterCancelIsAReceiptOnTheSameRecord(t *testing.T) {
	ledger, path := openForTest(t)
	var heard []RenderRecord
	ledger.notify = func(r RenderRecord) { heard = append(heard, r) }
	id := ledger.begin(1, "video", "m", "", "", epoch)
	ledger.settle(id, renderPatch{state: RenderCancelled}, epoch.Add(time.Second))
	cost, other := 0.75, 9.0
	ledger.charge(id, &ai.Usage{Cost: &cost}, epoch.Add(time.Minute))
	ledger.charge(id, &ai.Usage{Cost: &other}, epoch.Add(time.Hour))
	ledger.release(id)
	got, _ := ledger.get(id)
	if got.State != RenderCancelled || got.Committed || got.Path != "" || got.Charge != RenderCharged || got.CostUSD == nil || *got.CostUSD != cost {
		t.Fatalf("record = %+v", got)
	}
	if len(heard) != 3 || heard[2].ID != id || heard[2].State != RenderCancelled || heard[2].Charge != RenderCharged {
		t.Fatalf("events = %+v, want start, cancel, one receipt on the same id", heard)
	}
	ledger.close()
	if rows := ledgerRows(t, path); len(rows) != 3 || rows[2].Ev != renderEvCharge {
		t.Fatalf("rows = %d, want start, end, one charge", len(rows))
	}
}

// A charge reported BEFORE the ending rides into it; a provider that reports
// nothing leaves the charge unknown, never zero.
func TestRenderChargeBeforeEndingRidesIntoIt(t *testing.T) {
	ledger, _ := openForTest(t)
	failed := ledger.begin(1, "video", "m", "", "", epoch)
	cost := 0.2
	ledger.charge(failed, &ai.Usage{Cost: &cost}, epoch)
	ledger.settle(failed, renderPatch{state: RenderFailed, committed: true, err: "could not save"}, epoch.Add(time.Second))
	got, _ := ledger.get(failed)
	if got.Charge != RenderCharged || !got.Committed || got.State != RenderFailed {
		t.Fatalf("failed save after a charge = %+v", got)
	}
	silent := ledger.begin(2, "video", "m", "", "", epoch)
	ledger.settle(silent, renderPatch{state: RenderFailed, err: "provider said no"}, epoch)
	got, _ = ledger.get(silent)
	if got.Charge != RenderChargeUnknown || got.CostUSD != nil {
		t.Fatalf("a failure with no usage = %+v, want unknown", got)
	}
}

// A render the last process left running becomes interrupted ONCE.
func TestRenderLedgerMarksALeftRunningRenderInterruptedOnce(t *testing.T) {
	first, path := openForTest(t)
	id := first.begin(1, "music", "m", "", "", epoch)
	first.close()
	second := openRenderLedger(path, "sess", epoch.Add(time.Minute))
	got, _ := second.get(id)
	if got.State != RenderInterrupted || got.Charge != RenderChargeUnknown || got.Committed {
		t.Fatalf("record = %+v, want interrupted with unknown charge", got)
	}
	second.close()
	openRenderLedger(path, "sess", epoch.Add(2*time.Minute)).close()
	if rows := ledgerRows(t, path); len(rows) != 2 {
		t.Fatalf("%d rows after two reopens, want 2", len(rows))
	}
}

// Read without owning the session, an unfinished render is unknown, never running.
func TestReadRendersShowsAnUnfinishedRenderAsUnknown(t *testing.T) {
	dir := t.TempDir()
	ledger := openRenderLedger(filepath.Join(dir, placeRenders), "s", epoch)
	id := ledger.begin(1, "video", "m", "", "", epoch)
	ledger.close()
	page, err := ReadRenders(dir, 0, "")
	if err != nil || len(page.Renders) != 1 || page.Renders[0].State != RenderUnknown {
		t.Fatalf("page = %+v, %v", page, err)
	}
	if rec, err := ReadRender(dir, id); err != nil || rec.State != RenderUnknown {
		t.Fatalf("get = %+v, %v", rec, err)
	}
	if _, err := ReadRender(dir, "rnd_missing"); err != ErrRenderNotFound {
		t.Fatalf("missing id err = %v", err)
	}
}

// Cancel racing finish from many goroutines: exactly one ending, one end row.
func TestRenderLedgerCancelAndFinishRaceHasOneWinner(t *testing.T) {
	ledger, path := openForTest(t)
	for round := 0; round < 50; round++ {
		id := ledger.begin(round, "video", "m", "", "", epoch)
		var mu sync.Mutex
		wins := 0
		var wg sync.WaitGroup
		start := make(chan struct{})
		for _, state := range []RenderState{RenderCancelled, RenderFinished, RenderFailed, RenderCancelled} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if ledger.settle(id, renderPatch{state: state}, epoch) {
					mu.Lock()
					wins++
					mu.Unlock()
				}
			}()
		}
		close(start)
		wg.Wait()
		if wins != 1 {
			t.Fatalf("round %d: %d endings won", round, wins)
		}
	}
	ledger.close()
	if rows := ledgerRows(t, path); len(rows) != 100 {
		t.Fatalf("%d rows for 50 renders, want 100", len(rows))
	}
}

// THE HISTORY IS NOT TRUNCATED. Far more renders than any hot cache, written
// with endings, then read back through a reopened ledger: every one resolves by
// id, and paging by cursor visits each exactly once, newest first.
func TestRenderHistoryIsCompleteAcrossReopenAndPaging(t *testing.T) {
	ledger, path := openForTest(t)
	const total = 450
	ids := make([]string, total)
	for i := range ids {
		ids[i] = ledger.begin(i, "video", "m", "", "", epoch.Add(time.Duration(i)*time.Second))
		state := RenderFailed
		if i%2 == 0 {
			state = RenderCancelled
		}
		ledger.settle(ids[i], renderPatch{state: state, err: "boom"}, epoch.Add(time.Duration(i)*time.Second+time.Millisecond))
		ledger.release(ids[i])
	}
	ledger.close()

	reopened := openRenderLedger(path, "sess", epoch.Add(24*time.Hour))
	defer reopened.close()
	for _, probe := range []int{0, 1, 199, 200, 449} { // the oldest are far outside any 200-record window
		got, err := reopened.get(ids[probe])
		want := RenderFailed
		if probe%2 == 0 {
			want = RenderCancelled
		}
		if err != nil || got.State != want || got.Job != probe {
			t.Fatalf("record %d after reopen = %+v, %v", probe, got, err)
		}
	}
	seen := map[string]bool{}
	cursor, pages := "", 0
	for {
		page, err := reopened.list(100, cursor)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, rec := range page.Renders {
			if seen[rec.ID] || rec.State == RenderRunning || rec.State == RenderUnknown {
				t.Fatalf("duplicate or unresolved record %+v", rec)
			}
			seen[rec.ID] = true
		}
		if page.Next == "" {
			break
		}
		cursor = page.Next
	}
	if len(seen) != total || pages != 5 {
		t.Fatalf("visited %d records in %d pages, want %d in 5", len(seen), pages, total)
	}
	if _, err := reopened.get("rnd_never_existed"); err != ErrRenderNotFound {
		t.Fatalf("an id that never existed = %v, want not found after a whole-history search", err)
	}
}

// A scan that runs out of budget says so and continues from where it stopped;
// one that runs out of time is busy, never not-found.
func TestRenderScanBudgetAndDeadlineAreHonest(t *testing.T) {
	ledger, path := openForTest(t)
	var first string
	for i := 0; i < 30; i++ {
		id := ledger.begin(i, "video", "m", "", "", epoch.Add(time.Duration(i)*time.Second))
		if i == 0 {
			first = id
		}
		ledger.settle(id, renderPatch{state: RenderFailed}, epoch.Add(time.Hour))
		ledger.release(id)
	}
	ledger.close()

	store := newRenderStore(path)
	store.budget = 1 // one chunk per call
	seen, cursor, calls := map[string]bool{}, "", 0
	for {
		page, err := store.list(5, cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, rec := range page.Renders {
			seen[rec.ID] = true
		}
		calls++
		if page.Next == "" {
			break
		}
		cursor = page.Next
		if calls > 100 {
			t.Fatal("paging never finished")
		}
	}
	if len(seen) != 30 {
		t.Fatalf("budget-limited paging saw %d of 30", len(seen))
	}
	store.deadline = -time.Second
	if _, err := store.get(first); err != ErrRenderBusy {
		t.Fatalf("get past the deadline = %v, want busy", err)
	}
	if _, err := store.list(5, "garbage"); err != ErrRenderCursor {
		t.Fatalf("garbage cursor = %v", err)
	}
	if _, err := store.list(5, "r2.999999999"); err != ErrRenderCursor {
		t.Fatalf("cursor past the end = %v", err)
	}
}

// ── through the real tools ──────────────────────────────────────────────────

func renderIDOf(t *testing.T, agent *Agent) string {
	t.Helper()
	page, err := agent.Renders(10, "")
	if err != nil || len(page.Renders) == 0 {
		t.Fatalf("Renders = %+v, %v", page, err)
	}
	return page.Renders[0].ID
}

func TestGeneratedVideoLeavesAStartedThenFinishedRecord(t *testing.T) {
	media := newGatedMedia([]byte("MP4 bytes"), true)
	cost := 0.4
	media.cost = &ai.Usage{Cost: &cost}
	agent, workspace := newGatedAgent(t, media)
	runTool(t, agent, "generate_video", `{"prompt":"a ferry"}`)
	<-media.arrived

	id := renderIDOf(t, agent)
	running, _ := agent.Render(id)
	if running.State != RenderRunning || running.Kind != "video" || running.Model != "film/model" || running.Job != 1 || running.Ended != nil {
		t.Fatalf("running record = %+v", running)
	}
	close(media.release)
	<-renderJob(t, agent, 1).done

	done, _ := agent.Render(id)
	files := videoFiles(workspace)
	if done.State != RenderFinished || !done.Committed || done.Charge != RenderCharged || done.CostUSD == nil || *done.CostUSD != cost ||
		len(files) != 1 || filepath.Base(done.Path) != files[0] || done.DurationMS == nil {
		t.Fatalf("finished record = %+v, files %v", done, files)
	}
}

func TestRenderCancelNeverWaitsAndALateProviderReturnIsSilent(t *testing.T) {
	media := newGatedMedia([]byte("MP4"), false) // ignores the cancel
	agent, workspace := newGatedAgent(t, media)
	runTool(t, agent, "generate_video", `{"prompt":"a ferry"}`)
	<-media.arrived
	id := renderIDOf(t, agent)

	result, err := agent.CancelRender(id) // returns although the provider is still blocked
	if err != nil || result.Outcome != RenderOutcomeCancelled || result.Render.State != RenderCancelled ||
		result.Render.Upstream != "unknown" || result.Render.Committed || result.Render.Charge != RenderChargeUnknown {
		t.Fatalf("cancel = %+v, %v", result, err)
	}
	again, _ := agent.CancelRender(id)
	if again.Outcome != RenderOutcomeEnded {
		t.Fatalf("second cancel = %+v", again)
	}

	close(media.release) // the provider finally answers
	<-renderJob(t, agent, 1).done
	got, _ := agent.Render(id)
	if got.State != RenderCancelled || len(videoFiles(workspace)) != 0 || got.Path != "" {
		t.Fatalf("after a late return: %+v, files %v", got, videoFiles(workspace))
	}
}

func TestRenderCancelAfterCommitIsTooLateAndTheRenderFinishes(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	rendering, _, err := agent.jobs.startRender(videoLabel, "a ferry")
	if err != nil {
		t.Fatal(err)
	}
	agent.jobs.beginRender(rendering, "video", "film/model", "", "", nil)
	id := rendering.renderID
	writing, release := make(chan struct{}), make(chan struct{})
	done := make(chan string, 1)
	go func() {
		path, _, _ := agent.landRender(rendering, "", "a ferry", ".mp4", VideoDir(agent.config.Place, workspace), []byte("whole"),
			func(path string, data []byte) error {
				close(writing)
				<-release
				return writeFileAtomic(path, data)
			})
		done <- path
	}()
	<-writing

	result, err := agent.CancelRender(id)
	if err != nil || result.Outcome != RenderOutcomeTooLate || result.Render.State != RenderRunning {
		t.Fatalf("cancel after commit = %+v, %v", result, err)
	}
	close(release)
	path := <-done
	if !rendering.sealRender(renderPatch{state: RenderFinished, committed: true, path: path}) {
		t.Fatal("the finish was refused after a too-late cancel")
	}
	agent.jobs.finish(rendering, 0, "")
	got, _ := agent.Render(id)
	if got.State != RenderFinished || !got.Committed {
		t.Fatalf("record = %+v", got)
	}
}

func TestFailedRenderRecordsTheProvidersMessageAndNoCharge(t *testing.T) {
	media := &scriptedMedia{videoErr: context.DeadlineExceeded}
	agent, _ := newMediaAgent(t, media, nil)
	runTool(t, agent, "generate_video", `{"prompt":"a ferry"}`)
	<-renderJob(t, agent, 1).done
	got, _ := agent.Render(renderIDOf(t, agent))
	if got.State != RenderFailed || got.Committed || got.Charge != RenderChargeUnknown || got.Error == "" || got.Path != "" {
		t.Fatalf("record = %+v", got)
	}
}

func TestCancelRenderOfAnUnknownIDIsNotFound(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if _, err := agent.CancelRender("rnd_nope"); err != ErrRenderNotFound {
		t.Fatalf("err = %v", err)
	}
}

func jsonUnmarshal(line string, into any) error { return json.Unmarshal([]byte(line), into) }

// renderEventsUntil blocks on the lane until want render events have arrived;
// the 10 second guard only turns a lost event into a failure instead of a hang.
func renderEventsUntil(t *testing.T, events <-chan Event, want int) []RenderRecord {
	t.Helper()
	var heard []RenderRecord
	guard := time.After(10 * time.Second)
	for len(heard) < want {
		select {
		case event := <-events:
			if event.Kind == EventRenderUpdate && event.Render != nil {
				heard = append(heard, *event.Render)
			}
		case <-guard:
			t.Fatalf("heard %d render events, want %d: %+v", len(heard), want, heard)
		}
	}
	return heard
}

// The provider ignores the Stop and answers later WITH a charge: the record
// stays cancelled with no output and no second ending, gains the receipt, and
// the events say start, cancel, receipt — all on one id.
func TestLateChargeAfterCancelUpdatesTheSameRecordAndFiresOneReceiptEvent(t *testing.T) {
	media := newGatedMedia([]byte("MP4"), false)
	cost := 0.6
	media.cost = &ai.Usage{Cost: &cost}
	agent, workspace := newGatedAgent(t, media)
	events, stop := agent.WatchTaskUpdates()
	defer stop()

	runTool(t, agent, "generate_video", `{"prompt":"a ferry"}`)
	<-media.arrived
	id := renderIDOf(t, agent)
	if _, err := agent.CancelRender(id); err != nil {
		t.Fatal(err)
	}
	close(media.release)
	<-renderJob(t, agent, 1).done

	got, _ := agent.Render(id)
	if got.State != RenderCancelled || got.Committed || got.Path != "" || got.Charge != RenderCharged || got.CostUSD == nil || *got.CostUSD != cost || len(videoFiles(workspace)) != 0 {
		t.Fatalf("record = %+v", got)
	}
	heard := renderEventsUntil(t, events, 3)
	if len(heard) != 3 || heard[0].State != RenderRunning || heard[1].State != RenderCancelled || heard[1].Charge != RenderChargeUnknown ||
		heard[2].State != RenderCancelled || heard[2].Charge != RenderCharged {
		t.Fatalf("events = %+v", heard)
	}
	for _, event := range heard {
		if event.ID != id {
			t.Fatalf("event for another id: %+v", event)
		}
	}
	if notesContain(agent, "job 1") {
		t.Fatalf("a cancelled render announced itself: %v", sessionNotes(agent))
	}
}

// Started and finished each fire exactly once, with the run and call identity
// the tool site had.
func TestRenderEventsCarryStartedAndFinishedOnce(t *testing.T) {
	media := newGatedMedia([]byte("MP4"), true)
	agent, _ := newGatedAgent(t, media)
	events, stop := agent.WatchTaskUpdates()
	defer stop()
	runTool(t, agent, "generate_video", `{"prompt":"a ferry"}`)
	<-media.arrived
	close(media.release)
	<-renderJob(t, agent, 1).done
	heard := renderEventsUntil(t, events, 2)
	if len(heard) != 2 || heard[0].State != RenderRunning || heard[1].State != RenderFinished || heard[1].Path == "" {
		t.Fatalf("events = %+v", heard)
	}
}

// The run and call identity are the ones the tool site was handed, captured
// before the render's own background context cut it loose.
func TestRenderCapturesTheCallersRunAndCallIdentity(t *testing.T) {
	media := newGatedMedia([]byte("MP4"), true)
	agent, _ := newGatedAgent(t, media)
	ctx := withCallID(withRunIdentity(context.Background()), "call-77")
	run := runIdentity(ctx)
	tool := beltTool(t, agent, "generate_video")
	if _, isError, err := tool.Execute(ctx, json.RawMessage(`{"prompt":"a ferry"}`)); err != nil || isError {
		t.Fatalf("generate_video: %v %v", err, isError)
	}
	<-media.arrived
	got, _ := agent.Render(renderIDOf(t, agent))
	if got.Call != "call-77" || got.Run != run || run == "" {
		t.Fatalf("record = %+v, want run %q call call-77", got, run)
	}
	close(media.release)
	<-renderJob(t, agent, 1).done
}

// ── review corrections: exact truth or an honest refusal ────────────────────

// writeHistory writes n renders, each started, ended failed and (every third)
// charge-receipted, in order, and returns the file path and the ids.
func writeHistory(t *testing.T, n int) (string, []string) {
	t.Helper()
	ledger, path := openForTest(t)
	ids := make([]string, n)
	for i := range ids {
		ids[i] = ledger.begin(i, "video", "m", "", "", epoch.Add(time.Duration(i)*time.Second))
		ledger.settle(ids[i], renderPatch{state: RenderCancelled}, epoch.Add(time.Duration(i)*time.Second+time.Millisecond))
		if i%3 == 0 {
			cost := 0.1
			ledger.charge(ids[i], &ai.Usage{Cost: &cost}, epoch.Add(time.Hour))
		}
		ledger.release(ids[i])
	}
	ledger.close()
	return path, ids
}

// A deadline that expires AFTER the page's entries were chosen but BEFORE their
// latest rows were read must refuse the page. It must never answer with the
// start rows (running, uncharged) as though they were the latest truth.
func TestRenderPageThatCannotBeResolvedIsBusyNeverStale(t *testing.T) {
	path, _ := writeHistory(t, 30)
	store := newRenderStore(path)
	base := time.Unix(2_000_000_000, 0)
	clock := base
	store.now = func() time.Time { return clock }
	store.beforeResolve = func() { clock = base.Add(time.Hour) } // time runs out between the phases

	page, err := store.list(10, "")
	if err != ErrRenderBusy || len(page.Renders) != 0 {
		t.Fatalf("an unresolvable page = %+v, %v; want busy and no rows", page, err)
	}
	// The same call with time to spare resolves every entry to its ending.
	store.beforeResolve = nil
	clock = base
	page, err = store.list(10, "")
	if err != nil || len(page.Renders) != 10 {
		t.Fatalf("resolved page = %+v, %v", page, err)
	}
	for _, rec := range page.Renders {
		if rec.State != RenderCancelled {
			t.Fatalf("stale row served as truth: %+v", rec)
		}
		if rec.Job%3 == 0 && rec.Charge != RenderCharged {
			t.Fatalf("a receipted render lost its charge: %+v", rec)
		}
	}
}

// A page whose entries are further from the end of the history than the resolve
// budget allows is refused outright; a page near the end, and the cursor chain
// from it, are not — and retrying the refused cursor with room succeeds with
// nothing skipped.
func TestRenderResolveBudgetRefusesFarPagesAndTheCursorStaysComplete(t *testing.T) {
	path, ids := writeHistory(t, 450)
	info, _ := os.Stat(path)
	store := newRenderStore(path)

	whole := map[string]bool{}
	cursor := ""
	var cursors []string
	for {
		page, err := store.list(100, cursor)
		if err != nil {
			t.Fatalf("default bounds, cursor %q: %v", cursor, err)
		}
		for _, rec := range page.Renders {
			if whole[rec.ID] || rec.State != RenderCancelled {
				t.Fatalf("duplicate or stale %+v", rec)
			}
			whole[rec.ID] = true
		}
		if page.Next == "" {
			break
		}
		cursors = append(cursors, page.Next)
		cursor = page.Next
	}
	if len(whole) != len(ids) {
		t.Fatalf("paged %d of %d", len(whole), len(ids))
	}

	tight := store
	tight.resolveBudget = info.Size() / 4
	if _, err := tight.list(100, cursors[len(cursors)-1]); err != ErrRenderBusy {
		t.Fatalf("the oldest page under a tight resolve budget = %v, want busy", err)
	}
	if page, err := tight.list(100, ""); err != nil || len(page.Renders) != 100 {
		t.Fatalf("the newest page under the same budget = %d rows, %v", len(page.Renders), err)
	}
	// The refused cursor is still good: with room it resolves and the chain ends.
	if page, err := store.list(100, cursors[len(cursors)-1]); err != nil || len(page.Renders) == 0 {
		t.Fatalf("retrying the refused cursor = %+v, %v", page, err)
	}
}

// A read error or an oversize line is reported; neither is skipped.
func TestRenderScansReportOversizeLinesInsteadOfSkippingThem(t *testing.T) {
	path, _ := writeHistory(t, 5)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(strings.Repeat("x", renderMaxLine+10) + "\n"); err != nil {
		t.Fatal(err)
	}
	file.Close()

	store := newRenderStore(path)
	if _, err := store.list(10, ""); err != errRenderLine {
		t.Fatalf("list over an oversize line = %v, want the line error", err)
	}
	if _, err := store.get("rnd_anything"); err != errRenderLine {
		t.Fatalf("get over an oversize line = %v", err)
	}
	if _, err := store.unresolved(); err != errRenderLine {
		t.Fatalf("restore over an oversize line = %v", err)
	}
}

// A restore that cannot finish marks nothing and says so; the left-running
// render reads as unknown, never running, and the next open completes the
// restore exactly once.
func TestRenderRestoreThatCannotFinishMarksNothingAndRetriesOnTheNextOpen(t *testing.T) {
	first, path := openForTest(t)
	id := first.begin(1, "video", "m", "", "", epoch)
	first.close()

	store := newRenderStore(path)
	base := time.Unix(2_000_000_000, 0)
	clock := base
	store.now = func() time.Time { c := clock; clock = clock.Add(time.Hour); return c } // every look at the clock is past the deadline
	degraded := openRenderLedgerOn(store, "sess", epoch.Add(time.Minute))
	if degraded.restoreErr == nil {
		t.Fatal("a restore that ran out of time reported success")
	}
	degraded.close()
	got, err := ReadRender(filepath.Dir(path), id)
	if err != nil || got.State != RenderUnknown {
		t.Fatalf("a render the restore could not settle = %+v, %v; want unknown", got, err)
	}
	if rows := ledgerRows(t, path); len(rows) != 1 {
		t.Fatalf("a failed restore wrote %d rows, want none added", len(rows)-1)
	}

	healthy := openRenderLedger(path, "sess", epoch.Add(2*time.Minute))
	defer healthy.close()
	if healthy.restoreErr != nil {
		t.Fatal(healthy.restoreErr)
	}
	if got, _ := healthy.get(id); got.State != RenderInterrupted {
		t.Fatalf("after a clean open = %+v, want interrupted", got)
	}
	openRenderLedger(path, "sess", epoch.Add(3*time.Minute)).close()
	if rows := ledgerRows(t, path); len(rows) != 2 {
		t.Fatalf("%d rows, want start + one interrupted", len(rows))
	}
}

func TestRenderCannotStartWithoutItsJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "renders.jsonl")
	ledger := openRenderLedger(path, "s", time.Now())
	defer ledger.close()
	if id := ledger.begin(1, "video", "chosen", "run", "call", time.Now()); id != "" {
		t.Fatalf("unrecorded start admitted: %s", id)
	}
	if len(ledger.live) != 0 {
		t.Fatal("refused start retained a live render")
	}
}

func TestFailedRenderReceiptStaysOwnedUntilItCanBeSaved(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "session")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	ledger := openRenderLedger(filepath.Join(dir, "renders.jsonl"), "s", time.Now())
	defer ledger.close()
	id := ledger.begin(1, "video", "chosen", "run", "call", time.Now())
	if id == "" {
		t.Fatal("initial start refused")
	}
	_ = ledger.file.Close()
	ledger.file = nil
	if err := os.Rename(dir, dir+"-offline"); err != nil {
		t.Fatal(err)
	}
	defer os.Rename(dir+"-offline", dir)
	cost := 0.17
	ledger.settle(id, renderPatch{state: RenderFailed, usage: &ai.Usage{Cost: &cost}, err: "provider failed after charge"}, time.Now())
	ledger.release(id)
	rec, err := ledger.get(id)
	if err != nil || rec.State != RenderFailed || rec.Persistence != "unavailable" || rec.CostUSD == nil || *rec.CostUSD != cost {
		t.Fatalf("receipt lost: %#v, %v", rec, err)
	}
	if _, err := ledger.list(50, ""); !errors.Is(err, ErrRenderBusy) {
		t.Fatalf("unflushed history pretended complete: %v", err)
	}
	if next := ledger.begin(2, "video", "chosen", "run2", "call2", time.Now()); next != "" {
		t.Fatal("new paid start admitted with unflushed receipt")
	}
	if len(ledger.live) != 1 {
		t.Fatalf("failed starts grew retained records: %d", len(ledger.live))
	}
	if err := os.Rename(dir+"-offline", dir); err != nil {
		t.Fatal(err)
	}
	if next := ledger.begin(3, "video", "chosen", "run3", "call3", time.Now()); next == "" {
		t.Fatal("storage recovery did not admit a new start")
	}
	rec, err = ledger.get(id)
	if err != nil || rec.Persistence != "" || rec.CostUSD == nil || *rec.CostUSD != cost || rec.State != RenderFailed {
		t.Fatalf("saved receipt wrong: %#v, %v", rec, err)
	}
	if _, held := ledger.live[id]; held {
		t.Fatal("saved released receipt stayed pinned")
	}
}
