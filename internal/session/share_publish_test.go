package session

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// shareWorld is a conversation that can share: its index, its folders, and the
// registered share_deliverable tool off its own belt.
type shareWorld struct {
	agent     *Agent
	workspace string
	index     string
	session   string
}

func newShareWorld(t *testing.T) *shareWorld {
	t.Helper()
	agent, workspace, index := deliverableAgent(t, false)
	return &shareWorld{agent: agent, workspace: canonicalFile(workspace), index: index, session: agent.journalID()}
}

// shareCall is one dispatch: its own run and call ids, under a message the
// person actually sent, the way the loop stamps a tool call.
func (w *shareWorld) shareCall(t *testing.T, call string) context.Context {
	t.Helper()
	return withCallID(withRunIdentity(askingContext(t, w.agent, "send me the results")), call)
}

func (w *shareWorld) share(t *testing.T, ctx context.Context, op string, paths ...string) (ShareResult, bool) {
	t.Helper()
	args, _ := json.Marshal(map[string]any{"operation_id": op, "paths": paths})
	out, bad, err := beltTool(t, w.agent, "share_deliverable").Execute(ctx, args)
	if err != nil {
		t.Fatalf("share returned a harness error: %v", err)
	}
	var result ShareResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("share answered something that is not a receipt: %q", out)
	}
	return result, bad
}

func (w *shareWorld) file(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(w.workspace, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func indexLines(t *testing.T, index string) int {
	t.Helper()
	raw, err := os.ReadFile(index)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Count(raw, []byte("\n"))
}

func shareSoon() time.Time { return time.Now().Add(5 * time.Second) }

// TestShareDeliverableRecordsEveryKindThroughTheRegisteredTool is the road a
// model actually takes: the belt's own tool, one call, six kinds. Every file is
// on the real paged listing and the exact lookup as shared — never made — and
// only the picture, the sound and the video are media sources.
func TestShareDeliverableRecordsEveryKindThroughTheRegisteredTool(t *testing.T) {
	w := newShareWorld(t)
	film := mp4TestFile(mp4TestBox("mvhd", mp4TestMovieHeader(600, 3000)), mp4Track("vide", 640, 360, false))
	files := map[string][]byte{
		"chart.png":  pngOfSize(t, 7, 5),
		"voice.wav":  rangeWAV(250),
		"clip.mp4":   film,
		"report.pdf": []byte("%PDF-1.7\n1 0 obj\n%%EOF\n"),
		"notes.md":   []byte("# Notes\nresults\n"),
		"data.bin":   {0, 1, 2, 3, 0xfe, 0xff},
	}
	want := map[string]string{"chart.png": "image", "voice.wav": "audio", "clip.mp4": "video", "report.pdf": shareKindFile, "notes.md": DeliverableMarkdown, "data.bin": shareKindFile}
	var paths []string
	for name, data := range files {
		w.file(t, name, data)
		paths = append(paths, name) // relative: resolved from the workspace
	}
	ctx := w.shareCall(t, "call-share-1")
	result, bad := w.share(t, ctx, "share-all-kinds", paths...)
	if bad || result.Status != ShareOpAccepted || result.Replayed || len(result.RecordedIDs) != len(files) {
		t.Fatalf("share = %+v (bad %v)", result, bad)
	}
	turn, run := askedMessageID(ctx), runIdentity(ctx)
	if turn == "" || run == "" {
		t.Fatal("the dispatch carried no message or run to attribute to")
	}
	listed := map[string]DeliverableRow{}
	for _, row := range listDeliverables(t, w.index, DeliverableQuery{Sessions: map[string]bool{w.session: true}}, 2) {
		listed[row.Artifact.Title] = row
	}
	for _, file := range result.Files {
		name := filepath.Base(file.Path)
		row, ok := listed[name]
		if !ok {
			t.Fatalf("%s is not on the paged listing", name)
		}
		fact := row.Fact
		if row.Kind != want[name] || fact.Origin != DeliverOriginSharedByAgent || fact.Authored || fact.State != "" || fact.Kept != nil {
			t.Fatalf("%s listed as %+v kind %s", name, fact, row.Kind)
		}
		if fact.Turn != turn || fact.Run != run || fact.Call != "call-share-1" || fact.Version != file.Version {
			t.Fatalf("%s attributed to %q/%q/%q, want %q/%q/call-share-1", name, fact.Turn, fact.Run, fact.Call, turn, run)
		}
		if row.Artifact.Path != filepath.Join(w.workspace, name) {
			t.Fatalf("%s recorded at %s, not where it is", name, row.Artifact.Path)
		}
		w.checkMedia(t, name, row, file)
	}
	if got, err := FindDeliverable(w.index, w.session, result.RecordedIDs[0], shareSoon()); err != nil || got.ID != result.RecordedIDs[0] {
		t.Fatalf("exact lookup: %v", err)
	}
	for name, data := range files {
		if got, _ := os.ReadFile(filepath.Join(w.workspace, name)); !bytes.Equal(got, data) {
			t.Fatalf("%s was changed by sharing it", name)
		}
	}
}

// checkMedia says a shared media file is a shared media source with only the
// facts its header gave, and that every other file is no media source at all.
func (w *shareWorld) checkMedia(t *testing.T, name string, row DeliverableRow, file ShareResultFile) {
	t.Helper()
	media := row.Artifact.Media
	if mediaKindOf(row.Fact.File.MIME) == "" {
		if media != nil || file.Media != "" {
			t.Fatalf("%s is not media and carries a media fact %+v", name, media)
		}
		return
	}
	if media == nil || file.Media != media.ID {
		t.Fatalf("%s carries no media source", name)
	}
	source, err := FindMedia(w.index, w.session, media.ID, shareSoon())
	if err != nil {
		t.Fatalf("%s: media.get %v", name, err)
	}
	fact := source.Fact
	if fact.Origin != MediaShared || fact.Model != "" || fact.Charge != "" || fact.CostUSD != nil || fact.Change != "" || fact.Declared != "" || fact.Batch != "" {
		t.Fatalf("%s media claims more than sharing: %+v", name, fact)
	}
	if fact.Tool != row.Fact.Call || fact.Run != row.Fact.Run || fact.Message != row.Fact.Turn || fact.SHA256 != row.Fact.File.SHA256 || fact.MIME != row.Fact.File.MIME {
		t.Fatalf("%s media disagrees with its deliverable: %+v vs %+v", name, fact, row.Fact)
	}
	if live := ResolveMedia(source, w.agent.deliverableRoots()); live.State != MediaAvailable {
		t.Fatalf("%s media is %s, not available", name, live.State)
	}
	page, err := ListMedia(w.index, w.session, "", MediaPageMax, shareSoon())
	if err != nil {
		t.Fatal(err)
	}
	for _, listed := range page.Rows {
		if listed.Fact.ID == media.ID {
			return
		}
	}
	t.Fatalf("%s is not on media.list", name)
}

// TestShareReplayAnswersTheOriginalAndNeverRecordsTwice: the same operation_id
// from a later call is the ORIGINAL receipt — its ids, its media ids, its
// provenance — and not one more line on the index.
func TestShareReplayAnswersTheOriginalAndNeverRecordsTwice(t *testing.T) {
	w := newShareWorld(t)
	w.file(t, "chart.png", pngOfSize(t, 3, 3))
	first, _ := w.share(t, w.shareCall(t, "call-1"), "op-replay", "chart.png")
	lines := indexLines(t, w.index)
	again, bad := w.share(t, w.shareCall(t, "call-2"), "op-replay", "chart.png")
	if bad || again.Status != ShareOpAccepted || !again.Replayed {
		t.Fatalf("replay = %+v", again)
	}
	if sharePlanJSON(t, again.Files) != sharePlanJSON(t, first.Files) || sharePlanJSON(t, again.RecordedIDs) != sharePlanJSON(t, first.RecordedIDs) {
		t.Fatalf("replay answered a different receipt:\n%+v\n%+v", again, first)
	}
	if indexLines(t, w.index) != lines {
		t.Fatal("a replay appended to the index")
	}
	row, err := FindDeliverable(w.index, w.session, first.RecordedIDs[0], shareSoon())
	if err != nil || row.Fact.Call != "call-1" {
		t.Fatalf("the replay re-attributed the row: %+v %v", row.Fact, err)
	}
	conflict, _ := w.share(t, w.shareCall(t, "call-3"), "op-replay", "other.png")
	if conflict.Status != shareResultConflict || indexLines(t, w.index) != lines {
		t.Fatalf("a different payload under the same id = %+v", conflict)
	}
}

// planCrashed is an operation that wrote its full plan and then died before
// any append, as a crash or a lost process leaves it.
func (w *shareWorld) planCrashed(t *testing.T, op string, paths ...string) (shareTarget, ShareReceipt) {
	t.Helper()
	target, ok := w.agent.shareTarget(w.shareCall(t, "call-crashed"))
	if !ok {
		t.Fatal("no target")
	}
	asked, err := target.absolute(paths)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := AdmitShareOperation(filepath.Dir(target.index), target.session, op, asked, target.prov, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Release()
	receipt, _ := claim.frozen()
	if reason, ok := target.plan(context.Background(), claim, receipt); !ok {
		t.Fatalf("plan: %s", reason)
	}
	planned, _ := claim.frozen()
	return target, planned
}

// TestShareRetryAfterACrashAppendsThePlanNotTheFileAsItIsNow: a plan written
// and never appended is appended on the retry from the PLAN — the bytes the
// first attempt read, under the first attempt's ids and call — even though the
// file was rewritten in between.
func TestShareRetryAfterACrashAppendsThePlanNotTheFileAsItIsNow(t *testing.T) {
	w := newShareWorld(t)
	w.file(t, "chart.png", pngOfSize(t, 4, 4))
	_, planned := w.planCrashed(t, "op-crash", "chart.png")
	if indexLines(t, w.index) != 0 {
		t.Fatal("a plan appended before it was settled")
	}
	w.file(t, "chart.png", pngOfSize(t, 9, 9)) // edited between crash and retry
	retry, _ := w.share(t, w.shareCall(t, "call-retry"), "op-crash", "chart.png")
	if retry.Status != ShareOpAccepted || !retry.Replayed {
		t.Fatalf("retry = %+v", retry)
	}
	row, err := FindDeliverable(w.index, w.session, planned.Planned[0].ID, shareSoon())
	if err != nil {
		t.Fatal(err)
	}
	if want := planned.Planned[0].Row; row.Fact.Version != want.Deliver.Version || row.Fact.Call != "call-crashed" || row.Artifact.Media.ID != want.Media.ID {
		t.Fatalf("the retry recorded the file as it is now, or as the retry: %+v", row.Fact)
	}
	if indexLines(t, w.index) != 1 {
		t.Fatalf("index holds %d lines, want 1", indexLines(t, w.index))
	}
}

// TestShareRetryAfterAnUnsettledAppendReconcilesExactly: rows that reached the
// index while the receipt was never settled are FOUND by exact id on the
// retry and not appended again; only the missing one is.
func TestShareRetryAfterAnUnsettledAppendReconcilesExactly(t *testing.T) {
	w := newShareWorld(t)
	w.file(t, "a.md", []byte("# a\n"))
	w.file(t, "b.md", []byte("# b\n"))
	target, planned := w.planCrashed(t, "op-unsettled", "a.md", "b.md")
	if err := appendShareRow(target.index, *planned.Planned[0].Row); err != nil {
		t.Fatal(err)
	}
	retry, _ := w.share(t, w.shareCall(t, "call-retry"), "op-unsettled", "a.md", "b.md")
	if retry.Status != ShareOpAccepted || len(retry.RecordedIDs) != 2 {
		t.Fatalf("retry = %+v", retry)
	}
	if indexLines(t, w.index) != 2 {
		t.Fatalf("the retry appended a row the index already held: %d lines", indexLines(t, w.index))
	}
}

// TestShareNeverAppendsOnASearchThatCouldNotFinish: a reconcile whose read did
// not finish is not absence. Nothing is appended and the answer is unknown.
func TestShareNeverAppendsOnASearchThatCouldNotFinish(t *testing.T) {
	w := newShareWorld(t)
	w.file(t, "a.md", []byte("# a\n"))
	target, planned := w.planCrashed(t, "op-unknown", "a.md")
	// Some other conversation's history is there to be read.
	other := Artifact{Path: "/x/other.md", Title: "other.md", Session: "elsewhere", Kind: DeliverableMarkdown, Created: time.Now()}
	if !RecordArtifact(w.index, other) {
		t.Fatal("seed")
	}
	lines := indexLines(t, w.index)
	row := *planned.Planned[0].Row
	if present, known := shareRowPresent(w.index, w.session, row, time.Now().Add(-time.Second)); present || known {
		t.Fatal("a search past its deadline claimed to know")
	}
	// A call with no deadline has no bounded search to make, and makes none.
	recorded, reason := target.record(context.Background(), w.session, []Artifact{row}, false)
	if len(recorded) != 0 || reason != shareRetryCheck || indexLines(t, w.index) != lines {
		t.Fatalf("recorded %v (%s) from a search that could not finish", recorded, reason)
	}
}

// TestShareIsBusyWhileAnotherAttemptHoldsTheOperation: no wait, no queue, no
// row, and the holder's receipt untouched.
func TestShareIsBusyWhileAnotherAttemptHoldsTheOperation(t *testing.T) {
	w := newShareWorld(t)
	path := w.file(t, "a.md", []byte("# a\n"))
	holder, err := AdmitShareOperation(filepath.Dir(w.index), w.session, "op-busy", []string{path}, ShareProvenance{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()
	got, bad := w.share(t, w.shareCall(t, "call-2"), "op-busy", "a.md")
	if !bad || got.Status != shareResultBusy || indexLines(t, w.index) != 0 {
		t.Fatalf("busy = %+v", got)
	}
}

// TestShareCancelledBeforeThePlanStaysRetryable: running out of time is not a
// refusal; the same id shares the file once the call has time again.
func TestShareCancelledBeforeThePlanStaysRetryable(t *testing.T) {
	w := newShareWorld(t)
	w.file(t, "a.md", []byte("# a\n"))
	ctx, cancel := context.WithCancel(w.shareCall(t, "call-1"))
	cancel()
	got, _ := w.share(t, ctx, "op-cancel", "a.md")
	if got.Status != ShareOpPending || got.Reason != shareRetryTime || indexLines(t, w.index) != 0 {
		t.Fatalf("cancelled = %+v", got)
	}
	again, _ := w.share(t, w.shareCall(t, "call-2"), "op-cancel", "a.md")
	if again.Status != ShareOpAccepted {
		t.Fatalf("retry after cancel = %+v", again)
	}
}

// TestShareRefusesWhatItCannotHonestlyRecord: outside the folders, a folder, a
// link, a file missing — all refused, nothing appended, and refused stays.
func TestShareRefusesWhatItCannotHonestlyRecord(t *testing.T) {
	w := newShareWorld(t)
	outside := filepath.Join(t.TempDir(), "outside.md")
	os.WriteFile(outside, []byte("x"), 0o600)
	w.file(t, "real.md", []byte("# real\n"))
	if err := os.Symlink(filepath.Join(w.workspace, "real.md"), filepath.Join(w.workspace, "link.md")); err != nil {
		t.Fatal(err)
	}
	os.Mkdir(filepath.Join(w.workspace, "folder"), 0o700)
	for name, path := range map[string]string{"outside": outside, "link": "link.md", "folder": "folder", "missing": "nope.md"} {
		got, bad := w.share(t, w.shareCall(t, "call-"+name), "op-"+name, path)
		if !bad || got.Status != ShareOpRefused || len(got.RecordedIDs) != 0 {
			t.Fatalf("%s = %+v", name, got)
		}
	}
	if indexLines(t, w.index) != 0 {
		t.Fatal("a refusal appended")
	}
	mixed, _ := w.share(t, w.shareCall(t, "call-mixed"), "op-mixed", "real.md", "nope.md")
	if mixed.Status != ShareOpRefused || indexLines(t, w.index) != 0 {
		t.Fatalf("a share is all or none, got %+v", mixed)
	}
}

// TestSharePublisherAppendsOnlyTheDurablePlan is the causal proof for the
// publisher itself: the exported Receipt view and the rows PlannedRows handed
// out are both edited before the append, and the index holds the durable
// plan's exact bytes and the original provenance anyway.
func TestSharePublisherAppendsOnlyTheDurablePlan(t *testing.T) {
	w := newShareWorld(t)
	w.file(t, "chart.png", pngOfSize(t, 5, 5))
	target, planned := w.planCrashed(t, "op-tamper", "chart.png")
	want := sharePlanJSON(t, planned.Planned[0].Row)
	claim, err := AdmitShareOperation(filepath.Dir(target.index), target.session, "op-tamper", []string{planned.Planned[0].Path}, ShareProvenance{Call: "later"}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	mutateSharePlanRowForTest(claim.Receipt.Planned[0].Row)
	claim.Receipt.Provenance.Call, claim.Receipt.Status = "forged", ShareOpPending
	handed, _ := claim.PlannedRows()
	mutateSharePlanRowForTest(&handed[0])
	ctx, cancel := context.WithDeadline(context.Background(), claim.deadline)
	defer cancel()
	result := target.settle(ctx, claim, ShareResult{OperationID: "op-tamper"})
	if result.Status != ShareOpAccepted {
		t.Fatalf("settle = %+v", result)
	}
	raw, err := os.ReadFile(w.index)
	if err != nil {
		t.Fatal(err)
	}
	var appended Artifact
	if err := json.Unmarshal(bytes.TrimSpace(raw), &appended); err != nil {
		t.Fatal(err)
	}
	if got := sharePlanJSON(t, appended); got != want {
		t.Fatalf("the append followed an edited view:\n got %s\nwant %s", got, want)
	}
	loaded, _ := LoadShareReceipt(filepath.Dir(w.index), w.session, "op-tamper")
	if loaded.Provenance.Call != "call-crashed" || result.Files[0].Path != appended.Path {
		t.Fatalf("receipt provenance %+v, result %+v", loaded.Provenance, result.Files)
	}
}

// TestShareAppendRefusesRatherThanDropsAndEndsAPartialTail: the strict append
// never writes a row without its fact, and a line a crashed writer left
// without its newline does not swallow the shared row.
func TestShareAppendRefusesRatherThanDropsAndEndsAPartialTail(t *testing.T) {
	w := newShareWorld(t)
	w.file(t, "a.md", []byte("# a\n"))
	_, planned := w.planCrashed(t, "op-append", "a.md")
	row := *planned.Planned[0].Row
	big := row
	big.Title = strings.Repeat("t", deliverRowMax)
	if err := appendShareRow(w.index, big); err == nil || indexLines(t, w.index) != 0 {
		t.Fatal("an over-size shared row was written (or written without its fact)")
	}
	bare := row
	bare.Deliver = nil
	if err := appendShareRow(w.index, bare); err == nil {
		t.Fatal("a shared row without its fact was written")
	}
	if err := os.WriteFile(w.index, []byte(`{"path":"/x","title":"half`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := appendShareRow(w.index, row); err != nil {
		t.Fatal(err)
	}
	if present, known := shareRowPresent(w.index, w.session, row, shareSoon()); !present || !known {
		t.Fatal("the row appended after a partial tail is not found")
	}
}

// shareWorker is a task worker built the way the owning conversation builds
// one, standing in its own worktree, with the share verb off its own belt.
func shareWorker(t *testing.T, w *taskWorld) *Agent {
	t.Helper()
	worker, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = w.worktree
		c.InTask = true
		c.deliverSink = w.sink
	})
	return worker
}

func workerShare(t *testing.T, worker *Agent, call, op string, paths ...string) ShareResult {
	t.Helper()
	args, _ := json.Marshal(map[string]any{"operation_id": op, "paths": paths})
	ctx := withCallID(withRunIdentity(context.Background()), call)
	out, _, err := beltTool(t, worker, "share_deliverable").Execute(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	var result ShareResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("not a receipt: %q", out)
	}
	return result
}

// TestShareFromATaskBranchSnapshotsIntoTheOwningConversation: a worker's file
// lives only in a tree that is not the conversation's to keep, so it is copied
// — in the same pass that hashes it — into the conversation's own artifacts
// folder, recorded there as the conversation's with the node's id and asking
// message, the original named and untouched, and still available after the
// worktree is gone.
func TestShareFromATaskBranchSnapshotsIntoTheOwningConversation(t *testing.T) {
	w := newTaskWorld(t, "task/share")
	worker := shareWorker(t, w)
	pdf := []byte("%PDF-1.7\n%%EOF\n")
	picture := pngOfSize(t, 6, 2)
	os.WriteFile(filepath.Join(w.worktree, "report.pdf"), pdf, 0o600)
	os.WriteFile(filepath.Join(w.worktree, "chart.png"), picture, 0o600)
	result := workerShare(t, worker, "call-worker", "op-task", "report.pdf", "chart.png")
	if result.Status != ShareOpAccepted {
		t.Fatalf("worker share = %+v", result)
	}
	artifacts := canonicalFile(w.owner.config.Place.Artifacts())
	for _, file := range result.Files {
		row, err := FindDeliverable(w.index, w.sessionID, file.ID, shareSoon())
		if err != nil {
			t.Fatal(err)
		}
		fact := row.Fact
		if row.Artifact.Session != w.sessionID || fact.Task != strconv.FormatUint(w.node.id, 10) || fact.Turn != "scope#3" || fact.Call != "call-worker" {
			t.Fatalf("worker row is not the conversation's under the node: %+v", fact)
		}
		original := filepath.Join(w.worktree, filepath.Base(file.Path))
		if fact.Source == nil || fact.Source.Canonical != original || file.Original != original {
			t.Fatalf("the original is not named: %+v", fact.Source)
		}
		if filepath.Dir(filepath.Dir(row.Artifact.Path)) != filepath.Join(artifacts, shareSharedFolder) || filepath.Base(filepath.Dir(row.Artifact.Path)) != file.ID {
			t.Fatalf("the copy is at %s, not in the owner's artifacts folder under its id", row.Artifact.Path)
		}
		if fact.Authored || fact.State != "" || fact.Branch != "" {
			t.Fatalf("a copied share claims more than sharing: %+v", fact)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(w.worktree, "report.pdf")); !bytes.Equal(got, pdf) {
		t.Fatal("the original was changed")
	}
	if err := os.RemoveAll(w.worktree); err != nil {
		t.Fatal(err)
	}
	for _, file := range result.Files {
		row, _ := FindDeliverable(w.index, w.sessionID, file.ID, shareSoon())
		if live := ResolveDeliverable(row, w.owner.deliverableRoots()); live.State != MediaAvailable {
			t.Fatalf("%s is %s once the worktree is gone", file.Path, live.State)
		}
		if file.Media != "" {
			source, err := FindMedia(w.index, w.sessionID, file.Media, shareSoon())
			if err != nil || source.Fact.Origin != MediaShared || ResolveMedia(source, w.owner.deliverableRoots()).State != MediaAvailable {
				t.Fatalf("the shared picture is not an available media source: %+v %v", source.Fact, err)
			}
		}
	}
}

// TestShareRefusedFromATaskBranchLeavesNoCopy: a refusal before the plan
// removes every copy it took, because no row can name one.
func TestShareRefusedFromATaskBranchLeavesNoCopy(t *testing.T) {
	w := newTaskWorld(t, "task/refuse")
	worker := shareWorker(t, w)
	os.WriteFile(filepath.Join(w.worktree, "a.md"), []byte("# a\n"), 0o600)
	result := workerShare(t, worker, "call-1", "op-refuse", "a.md", "missing.md")
	if result.Status != ShareOpRefused {
		t.Fatalf("result = %+v", result)
	}
	shared := filepath.Join(w.owner.config.Place.Artifacts(), shareSharedFolder)
	entries, _ := os.ReadDir(shared)
	if len(entries) != 0 {
		t.Fatalf("a refused share left %d copies", len(entries))
	}
	if indexLines(t, w.index) != 0 {
		t.Fatal("a refused share appended")
	}
}
