package session

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// gatedImages is an image provider that says when a request arrived and then
// waits to be released — or, when it obeys, for the request's context. It
// counts every request, so a test can prove none was sent.
type gatedImages struct {
	*scriptedMedia
	arrived chan struct{}
	release chan struct{}
	obeyCtx bool
	calls   atomic.Int32
	reply   *provider.ImageResponse
	err     error
}

func newGatedImages(reply *provider.ImageResponse, obeyCtx bool) *gatedImages {
	return &gatedImages{scriptedMedia: &scriptedMedia{}, arrived: make(chan struct{}, 4), release: make(chan struct{}), obeyCtx: obeyCtx, reply: reply}
}

func (g *gatedImages) GenerateImage(ctx context.Context, _ provider.ImageRequest) (*provider.ImageResponse, error) {
	g.calls.Add(1)
	g.arrived <- struct{}{}
	if g.obeyCtx {
		select {
		case <-g.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	} else {
		<-g.release
	}
	return g.reply, g.err
}

func usageOf(value float64) *ai.Usage { return &ai.Usage{Cost: &value} }

func onePNG(t *testing.T) []provider.GeneratedImage {
	return []provider.GeneratedImage{{Base64: base64.StdEncoding.EncodeToString(pngOfSize(t, 2, 2))}}
}

func attemptAgent(t *testing.T, media MediaGenerator) (*Agent, string) {
	t.Helper()
	return newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Media = media
		c.MediaModel = mediaModels(map[string]string{modalityImage: "paint/model"})
	})
}

// callImage runs generate_image as dispatch would: with the call's own id and
// the media-attempt mark planted.
func callImage(t *testing.T, ctx context.Context, agent *Agent, args string) (string, bool, bool) {
	t.Helper()
	ctx, mark := withMediaAttempt(withCallID(ctx, "img_call"))
	out, bad, err := beltTool(t, agent, "generate_image").Execute(ctx, json.RawMessage(args))
	if err != nil {
		t.Fatalf("harness error: %v", err)
	}
	return out, bad, mark.Load()
}

func onlyRender(t *testing.T, agent *Agent) RenderRecord {
	t.Helper()
	page, err := agent.Renders(10, "")
	if err != nil || len(page.Renders) != 1 {
		t.Fatalf("want exactly one render record: %+v %v", page, err)
	}
	return page.Renders[0]
}

func TestASavedImageIsOneFinishedAttemptWithTheRequestedModelAndExactIdentity(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		usage  *ai.Usage
		charge RenderCharge
	}{
		{"receipt", usageOf(0.04), RenderCharged},
		// An absent figure is unknown, never a zero.
		{"no receipt", nil, RenderChargeUnknown},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			media := &scriptedMedia{base64: base64.StdEncoding.EncodeToString(pngOfSize(t, 2, 2)), usage: testCase.usage}
			agent, _ := attemptAgent(t, media)
			ctx := withRunIdentity(context.Background())
			out, bad, marked := callImage(t, ctx, agent, `{"prompt":"a lighthouse"}`)
			if bad || !marked {
				t.Fatalf("bad=%v marked=%v out=%s", bad, marked, out)
			}
			got := onlyRender(t, agent)
			if got.Kind != "image" || got.Job != 0 || got.State != RenderFinished || !got.Committed || got.Model != "paint/model" ||
				got.Run != runIdentity(ctx) || got.Call != "img_call" || got.Charge != testCase.charge || got.Path != "" || got.Upstream != "" {
				t.Fatalf("record = %+v", got)
			}
		})
	}
}

func TestAProviderFailureWithAReceiptIsFailedChargedAndUncommitted(t *testing.T) {
	media := newGatedImages(&provider.ImageResponse{Usage: usageOf(0.02)}, true)
	media.err = errors.New("content policy")
	close(media.release)
	agent, _ := attemptAgent(t, media)
	out, bad, marked := callImage(t, context.Background(), agent, `{"prompt":"x"}`)
	if !bad || !marked || !strings.Contains(out, "content policy") {
		t.Fatalf("bad=%v marked=%v out=%s", bad, marked, out)
	}
	got := onlyRender(t, agent)
	if got.State != RenderFailed || got.Committed || got.Charge != RenderCharged || *got.CostUSD != 0.02 || !strings.Contains(got.Error, "content policy") {
		t.Fatalf("record = %+v", got)
	}
}

func TestAnEmptyReplyKeepsItsChargeAndSavesNothing(t *testing.T) {
	media := newGatedImages(&provider.ImageResponse{Usage: usageOf(0.03)}, true)
	close(media.release)
	agent, _ := attemptAgent(t, media)
	if _, bad, _ := callImage(t, context.Background(), agent, `{"prompt":"x"}`); !bad {
		t.Fatal("an empty reply succeeded")
	}
	got := onlyRender(t, agent)
	if got.State != RenderFailed || got.Committed || got.Charge != RenderCharged {
		t.Fatalf("record = %+v", got)
	}
}

func TestABatchWithOneUnsavedVariantFinishesCommittedWithNoPath(t *testing.T) {
	good := base64.StdEncoding.EncodeToString(pngOfSize(t, 2, 2))
	rig := newBatchRig(t, []string{good, "!!not base64!!", good}, usageOf(0.09))
	if _, bad := rig.run(t, `{"prompt":"a sheet","n":3}`); bad {
		t.Fatal("two saved variants reported as a failure")
	}
	got := onlyRender(t, rig.agent)
	if got.State != RenderFinished || !got.Committed || got.Path != "" || got.Charge != RenderCharged || got.Call != "call_sheet" {
		t.Fatalf("record = %+v", got)
	}
}

// THE START IS SAVED OR NOTHING IS SENT. A journal that cannot take the start
// row refuses the call before the provider is asked, writes no record, and
// does not mark the call as having one.
func TestAnAttemptThatCannotBeJournaledSendsNoRequest(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		spoil func(*Agent)
	}{
		{"journal unwritable", func(agent *Agent) {
			ledger := agent.jobs.renderLedger()
			ledger.mu.Lock()
			ledger.store.path = t.TempDir() // a directory cannot be appended to
			ledger.mu.Unlock()
		}},
		{"session closed", func(agent *Agent) { agent.jobs.renderLedger().close() }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			media := newGatedImages(&provider.ImageResponse{Data: onePNG(t)}, true)
			close(media.release)
			agent, _ := attemptAgent(t, media)
			testCase.spoil(agent)
			out, bad, marked := callImage(t, context.Background(), agent, `{"prompt":"x"}`)
			if !bad || marked || media.calls.Load() != 0 || !strings.HasPrefix(out, "Could not start the image") || !strings.Contains(out, "No request was sent") {
				t.Fatalf("bad=%v marked=%v calls=%d out=%s", bad, marked, media.calls.Load(), out)
			}
		})
	}
}

// Arguments the tool turns away never become an attempt.
func TestARejectedImageCallMakesNoRecord(t *testing.T) {
	agent, _ := attemptAgent(t, &scriptedMedia{})
	if _, bad, marked := callImage(t, context.Background(), agent, `{"prompt":"x","n":11}`); !bad || marked {
		t.Fatalf("bad=%v marked=%v", bad, marked)
	}
	if page, _ := agent.Renders(10, ""); len(page.Renders) != 0 {
		t.Fatalf("a rejected call left a record: %+v", page.Renders)
	}
}

func TestAnAdmissionRefusalMarksItsCall(t *testing.T) {
	agent := refusingImageAgent(t, &scriptedMedia{})
	if _, bad, marked := callImage(t, context.Background(), agent, `{"prompt":"x"}`); !bad || !marked {
		t.Fatalf("bad=%v marked=%v", bad, marked)
	}
}

// startGated begins an image call on its own goroutine and waits until the
// provider has the request; done carries the tool's answer.
func startGated(t *testing.T, ctx context.Context, agent *Agent, media *gatedImages) (RenderRecord, <-chan string) {
	t.Helper()
	done := make(chan string, 1)
	go func() {
		out, _, _ := callImage(t, ctx, agent, `{"prompt":"a ferry"}`)
		done <- out
	}()
	<-media.arrived
	return onlyRender(t, agent), done
}

func TestStoppingAnImageBeforeItsReplySealsCancelledAndSignalsTheCall(t *testing.T) {
	media := newGatedImages(&provider.ImageResponse{Data: onePNG(t)}, true)
	agent, workspace := attemptAgent(t, media)
	events, stop := agent.WatchTaskUpdates()
	defer stop()
	running, done := startGated(t, context.Background(), agent, media)
	if running.State != RenderRunning {
		t.Fatalf("running = %+v", running)
	}
	answer, err := agent.CancelRender(running.ID)
	if err != nil || answer.Outcome != RenderOutcomeCancelled || answer.Render.State != RenderCancelled ||
		answer.Render.Upstream != "unknown" || answer.Render.Charge != RenderChargeUnknown || answer.Render.Committed {
		t.Fatalf("cancel = %+v %v", answer, err)
	}
	out := <-done
	if !strings.Contains(out, "stopped") || !strings.Contains(out, "unknown") {
		t.Fatalf("tool said %q", out)
	}
	again, err := agent.CancelRender(running.ID)
	if err != nil || again.Outcome != RenderOutcomeEnded {
		t.Fatalf("second cancel = %+v %v", again, err)
	}
	if files := imageFilesUnder(t, workspace); len(files) != 0 {
		t.Fatalf("a stopped image was saved: %v", files)
	}
	heard := renderEventsUntil(t, events, 2)
	if heard[1].State != RenderCancelled {
		t.Fatalf("events = %+v", heard)
	}
}

// The provider ignores the Stop and answers later with a picture and a charge:
// nothing is saved, the record stays cancelled, and the receipt lands on it as
// one charge row — never a second ending.
func TestALateChargeAfterAnImageStopIsAReceiptNotASecondEnding(t *testing.T) {
	media := newGatedImages(&provider.ImageResponse{Data: onePNG(t), Usage: usageOf(0.05)}, false)
	path := filepath.Join(t.TempDir(), "renders.jsonl")
	agent, workspace := attemptAgent(t, media)
	agent.jobs.rendersOnce.Do(func() {
		agent.jobs.renders = openRenderLedger(path, "s", time.Now())
		agent.jobs.renders.notify = agent.emitRenderUpdate
	})
	events, stop := agent.WatchTaskUpdates()
	defer stop()
	running, done := startGated(t, context.Background(), agent, media)
	if answer, _ := agent.CancelRender(running.ID); answer.Outcome != RenderOutcomeCancelled {
		t.Fatalf("cancel = %+v", answer)
	}
	close(media.release)
	<-done
	got, _ := agent.Render(running.ID)
	if got.State != RenderCancelled || got.Charge != RenderCharged || *got.CostUSD != 0.05 || got.Committed {
		t.Fatalf("record = %+v", got)
	}
	if files := imageFilesUnder(t, workspace); len(files) != 0 {
		t.Fatalf("a stopped image was saved: %v", files)
	}
	heard := renderEventsUntil(t, events, 3)
	if heard[1].State != RenderCancelled || heard[1].Charge != RenderChargeUnknown || heard[2].State != RenderCancelled || heard[2].Charge != RenderCharged {
		t.Fatalf("events = %+v", heard)
	}
	rows := map[string]int{}
	data, _ := os.ReadFile(path)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		row, _ := parseRenderRow([]byte(line))
		rows[row.Ev]++
	}
	if rows[renderEvStart] != 1 || rows[renderEvEnd] != 1 || rows[renderEvCharge] != 1 {
		t.Fatalf("rows = %v\n%s", rows, data)
	}
}

// A Stop after the bytes were committed comes too late and the picture lands.
func TestStoppingAnImageAfterCommitIsTooLate(t *testing.T) {
	agent, workspace := attemptAgent(t, &scriptedMedia{})
	media := newGatedImages(&provider.ImageResponse{Data: onePNG(t)}, true)
	close(media.release)
	saving, release := make(chan string, 1), make(chan struct{})
	ctx, _ := withMediaAttempt(withCallID(withRunIdentity(context.Background()), "img_call"))
	done := make(chan bool, 1)
	go func() {
		_, bad := GenerateImage(ctx, ImageGen{
			Client: media, DefaultModel: "paint/model", Workspace: workspace, Directory: filepath.Join(workspace, "pics"),
			attempt: agent.beginImageAttempt,
			RecordMedia: func(path, _ string, _ MediaFact) {
				saving <- path
				<-release
			},
		}, GenerateImageArgs{Prompt: "a ferry"})
		done <- bad
	}()
	<-saving
	running := onlyRender(t, agent)
	answer, err := agent.CancelRender(running.ID)
	if err != nil || answer.Outcome != RenderOutcomeTooLate || answer.Render.State != RenderRunning {
		t.Fatalf("cancel = %+v %v", answer, err)
	}
	close(release)
	if bad := <-done; bad {
		t.Fatal("a committed image failed")
	}
	if got := onlyRender(t, agent); got.State != RenderFinished || !got.Committed {
		t.Fatalf("record = %+v", got)
	}
}

// A turn that ends under the call stops only the waiting: cancelled, upstream
// unknown, and no charge claimed either way.
func TestATurnStopUnderAnImageIsCancelledWithTheChargeUnknown(t *testing.T) {
	media := newGatedImages(&provider.ImageResponse{Data: onePNG(t)}, true)
	agent, _ := attemptAgent(t, media)
	ctx, cancel := context.WithCancel(context.Background())
	_, done := startGated(t, ctx, agent, media)
	cancel()
	<-done
	got := onlyRender(t, agent)
	if got.State != RenderCancelled || got.Upstream != "unknown" || got.Charge != RenderChargeUnknown || got.Committed {
		t.Fatalf("record = %+v", got)
	}
}

// A process that dies mid-call leaves a start row; the next owner records it
// interrupted once, and a reader that does not own the session says unknown.
func TestAnImageAttemptLeftRunningIsInterruptedAtTheNextOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "renders.jsonl")
	first := openRenderLedgerUsing(newRenderStore(path), "s", time.Now(), newRenderRetention(4))
	_, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	id, err := first.beginWith(0, modalityImage, "paint/model", "run", "call", cancel, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rec, _ := ReadRender(filepath.Dir(path), id); rec.State != RenderUnknown {
		t.Fatalf("a non-owner read = %+v", rec)
	}
	// The process is gone: nothing settles or releases the first ledger.
	second := openRenderLedgerUsing(newRenderStore(path), "s", time.Now(), newRenderRetention(4))
	defer second.close()
	got, err := second.get(id)
	if err != nil || got.State != RenderInterrupted || got.Kind != "image" || got.Charge != RenderChargeUnknown {
		t.Fatalf("restored = %+v %v", got, err)
	}
	if outcome, held := second.stopAttempt(id); held {
		t.Fatalf("a restored record was stoppable: %v", outcome)
	}
}

// Concurrent Stop and commit race on one lock: exactly one of cancelled and
// too_late, never both and never a second ending.
func TestAStopAndACommitHaveExactlyOneWinner(t *testing.T) {
	for round := 0; round < 200; round++ {
		ledger := openRenderLedgerUsing(newRenderStore(""), "s", time.Now(), newRenderRetention(4))
		ctx, cancel := context.WithCancelCause(context.Background())
		id, err := ledger.beginWith(0, modalityImage, "m", "r", "c", cancel, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		var committed bool
		var outcome RenderOutcome
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); committed = ledger.commitAttempt(id) }()
		go func() { defer wg.Done(); outcome, _ = ledger.stopAttempt(id) }()
		wg.Wait()
		switch {
		case committed && outcome == RenderOutcomeTooLate && ctx.Err() == nil:
		case !committed && outcome == RenderOutcomeCancelled && errors.Is(context.Cause(ctx), errRenderStopped):
		default:
			t.Fatalf("round %d: committed=%v outcome=%v cause=%v", round, committed, outcome, context.Cause(ctx))
		}
		ledger.settle(id, renderPatch{state: RenderFinished, committed: true}, time.Now())
		ledger.release(id)
		ledger.close()
	}
}

func imageFilesUnder(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, ".png") {
			found = append(found, path)
		}
		return nil
	})
	return found
}

// A saved harness's belt is built on an agent with no ledger by construction,
// like the command line's door: its picture is sent and simply not recorded.
// Refusing it would make every harness that draws fail for want of a journal it
// was never meant to have.
func TestAHarnessBeltPaintsWithoutALedger(t *testing.T) {
	media := newGatedImages(&provider.ImageResponse{Data: onePNG(t)}, true)
	close(media.release)
	workspace := t.TempDir()
	var paint func(context.Context, json.RawMessage) (string, bool, error)
	for _, tool := range HarnessBelt(workspace, HarnessBeltSeams{Media: media, MediaModel: mediaModels(map[string]string{modalityImage: "paint/model"})}) {
		if tool.Name == "generate_image" {
			paint = tool.Execute
		}
	}
	if paint == nil {
		t.Fatal("no generate_image on the harness belt")
	}
	ctx, mark := withMediaAttempt(context.Background())
	out, bad, err := paint(ctx, json.RawMessage(`{"prompt":"a harbour"}`))
	if err != nil || bad || media.calls.Load() != 1 || mark.Load() {
		t.Fatalf("out=%q bad=%v err=%v calls=%d marked=%v", out, bad, err, media.calls.Load(), mark.Load())
	}
}

// A STOP-WORK ROUND IS NOT THE END OF THE CONVERSATION. It shuts the registry
// and a fresh message reopens it; the render ledger was closed by that round
// and never reopened, which refused every later picture. It stays open now.
func TestAnImageAfterStopWorkAndAFreshMessageIsRecorded(t *testing.T) {
	media := &scriptedMedia{base64: base64.StdEncoding.EncodeToString(pngOfSize(t, 2, 2))}
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("ok"), nil },
	}}, func(c *Config) {
		c.Media = media
		c.MediaModel = mediaModels(map[string]string{modalityImage: "paint/model"})
	})
	if _, bad, _ := callImage(t, context.Background(), agent, `{"prompt":"before"}`); bad {
		t.Fatal("the first picture failed")
	}
	if err := agent.StopWork(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "work stop to settle", func() bool { agent.mu.Lock(); defer agent.mu.Unlock(); return !agent.workStopping })
	events, err := agent.Submit(context.Background(), "fresh request")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	out, bad, marked := callImage(t, context.Background(), agent, `{"prompt":"after"}`)
	if bad || !marked {
		t.Fatalf("a picture after stop-work: bad=%v marked=%v %s", bad, marked, out)
	}
	page, err := agent.Renders(10, "")
	if err != nil || len(page.Renders) != 2 || page.Renders[0].State != RenderFinished {
		t.Fatalf("history after stop-work: %+v %v", page, err)
	}
}

// A picture still with the provider when a stop-work round runs keeps its
// ending on disk: nothing is left retained in the process for want of a
// journal, and the history stays readable.
func TestAStopWorkRoundUnderAnImageLeavesItsEndingSaved(t *testing.T) {
	media := newGatedImages(&provider.ImageResponse{Data: onePNG(t)}, false)
	path := filepath.Join(t.TempDir(), "renders.jsonl")
	agent, _ := attemptAgent(t, media)
	agent.jobs.rendersOnce.Do(func() {
		agent.jobs.renders = openRenderLedger(path, "s", time.Now())
		agent.jobs.renders.notify = agent.emitRenderUpdate
	})
	running, done := startGated(t, context.Background(), agent, media)
	if err := agent.StopWork(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "work stop to settle", func() bool { agent.mu.Lock(); defer agent.mu.Unlock(); return !agent.workStopping })
	close(media.release)
	<-done
	got, err := agent.Render(running.ID)
	if err != nil || got.State == RenderRunning || got.Persistence != "" || processRenderHistory.hasPending(path) {
		t.Fatalf("ending = %+v %v pending=%v", got, err, processRenderHistory.hasPending(path))
	}
	if _, err := agent.Renders(10, ""); err != nil {
		t.Fatalf("history after the round: %v", err)
	}
}

// A conversation that closed before its ledger was ever opened refuses a
// picture all the same: the ledger opened afterwards is a closed one.
func TestAClosedSessionRefusesAnImageEvenBeforeItsLedgerOpened(t *testing.T) {
	media := newGatedImages(&provider.ImageResponse{Data: onePNG(t)}, true)
	close(media.release)
	agent, _ := attemptAgent(t, media)
	tool := beltTool(t, agent, "generate_image")
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, mark := withMediaAttempt(withCallID(context.Background(), "late"))
	out, bad, err := tool.Execute(ctx, json.RawMessage(`{"prompt":"x"}`))
	if err != nil || !bad || mark.Load() || media.calls.Load() != 0 || !strings.Contains(out, "No request was sent") {
		t.Fatalf("out=%q bad=%v err=%v marked=%v calls=%d", out, bad, err, mark.Load(), media.calls.Load())
	}
}

// A Stop for a picture that has ended answers already_ended, never "its job is
// not in this session" — job 0 is no job. This covers the record read as ended;
// an attempt released between CancelRender's read and its stop takes the
// re-read branch in render_app.go, which no test reaches deterministically.
func TestCancellingAnImageThatJustEndedIsAlreadyEnded(t *testing.T) {
	agent, _ := attemptAgent(t, &scriptedMedia{base64: base64.StdEncoding.EncodeToString(pngOfSize(t, 2, 2))})
	callImage(t, context.Background(), agent, `{"prompt":"x"}`)
	got := onlyRender(t, agent)
	answer, err := agent.CancelRender(got.ID)
	if err != nil || answer.Outcome != RenderOutcomeEnded {
		t.Fatalf("cancel = %+v %v", answer, err)
	}
}
