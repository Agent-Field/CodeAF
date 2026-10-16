package session

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// gatedMedia is a video provider whose every call is held on a barrier the test
// owns. Nothing here sleeps: a call announces it has arrived, then waits to be
// released, and honours its context only when obeyCtx says a real client would.
type gatedMedia struct {
	*scriptedMedia
	arrived chan struct{}
	release chan struct{}
	obeyCtx bool
	video   []byte
	cost    *ai.Usage
	mu      sync.Mutex
	calls   int
}

func newGatedMedia(video []byte, obeyCtx bool) *gatedMedia {
	return &gatedMedia{scriptedMedia: &scriptedMedia{}, arrived: make(chan struct{}, 16), release: make(chan struct{}), obeyCtx: obeyCtx, video: video}
}

func (g *gatedMedia) GenerateVideo(ctx context.Context, _ provider.VideoRequest) (*provider.VideoResponse, error) {
	g.mu.Lock()
	g.calls++
	g.mu.Unlock()
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
	return &provider.VideoResponse{Video: g.video, Usage: g.cost}, nil
}

func newGatedAgent(t *testing.T, media *gatedMedia) (*Agent, string) {
	t.Helper()
	return newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Media = media
		config.MediaModel = mediaModels(map[string]string{modalityVideo: "film/model"})
	})
}

func renderJob(t *testing.T, agent *Agent, id int) *job {
	t.Helper()
	found := agent.jobs.find(id)
	if found == nil {
		t.Fatalf("no job %d", id)
	}
	return found
}

func videoFiles(workspace string) []string {
	entries, _ := os.ReadDir(filepath.Join(workspace, ".codeaf", "video"))
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// Stop accepted while the provider is still working: nothing is written, the
// reply says what is and is not known, and the render reads as stopped.
func TestStopBeforeTheProviderAnswersSavesNothingAndDoesNotClaimAProviderCancel(t *testing.T) {
	media := newGatedMedia([]byte("MP4"), true)
	agent, workspace := newGatedAgent(t, media)
	runTool(t, agent, "generate_video", `{"prompt":"a ferry"}`)
	<-media.arrived

	reply, _ := runTool(t, agent, "jobs", `{"action":"kill","id":1}`)
	<-renderJob(t, agent, 1).done
	if !strings.Contains(reply, "no video was saved") || !strings.Contains(reply, "not told to cancel") {
		t.Fatalf("kill said %q", reply)
	}
	if strings.Contains(reply, "cancelled at the provider") {
		t.Fatalf("kill claimed an upstream cancel: %q", reply)
	}
	if files := videoFiles(workspace); len(files) != 0 {
		t.Fatalf("a stopped render left %v", files)
	}
	if got := renderJob(t, agent, 1).info().state; got != jobKilled {
		t.Fatalf("state = %v, want killed", got)
	}
	if notesContain(agent, "job 1") {
		t.Fatalf("a stopped render reported itself: %v", sessionNotes(agent))
	}
	if media.calls != 1 {
		t.Fatalf("provider called %d times, want exactly 1", media.calls)
	}
}

// The provider ignores the cancel and answers anyway AFTER the stop was
// accepted: the bytes are dropped unwritten, not published.
func TestStopAcceptedBeforeBytesArriveNeverWritesAFileEvenIfTheProviderFinishes(t *testing.T) {
	media := newGatedMedia([]byte("MP4"), false)
	cost := 0.25
	media.cost = &ai.Usage{Cost: &cost}
	agent, workspace := newGatedAgent(t, media)
	runTool(t, agent, "generate_video", `{"prompt":"a ferry"}`)
	<-media.arrived

	runTool(t, agent, "jobs", `{"action":"kill","id":1}`)
	close(media.release)
	<-renderJob(t, agent, 1).done

	if files := videoFiles(workspace); len(files) != 0 {
		t.Fatalf("a file was published after an accepted stop: %v", files)
	}
	if notesContain(agent, "job 1") {
		t.Fatalf("note after stop: %v", sessionNotes(agent))
	}
}

// Finished first, stop second: the file is there, the stop is told it was too
// late, and the model still gets its note.
func TestStopAfterTheRenderLandedIsToldSoAndTheFileStays(t *testing.T) {
	media := newGatedMedia([]byte("MP4 bytes"), true)
	agent, workspace := newGatedAgent(t, media)
	runTool(t, agent, "generate_video", `{"prompt":"a ferry"}`)
	<-media.arrived
	close(media.release)
	<-renderJob(t, agent, 1).done

	reply, _ := runTool(t, agent, "jobs", `{"action":"kill","id":1}`)
	if !strings.Contains(reply, "already finished") {
		t.Fatalf("late kill said %q", reply)
	}
	files := videoFiles(workspace)
	if len(files) != 1 {
		t.Fatalf("files = %v, want the one render", files)
	}
	if !notesContain(agent, "job 1 finished") {
		t.Fatalf("no completion note: %v", sessionNotes(agent))
	}
}

// A stop arriving DURING the write loses: the file is complete, the reply does
// not say nothing was saved, and the job is not marked killed.
func TestStopDuringTheWriteDoesNotClaimNothingWasSaved(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	rendering, _, err := agent.jobs.startRender(videoLabel, "a ferry")
	if err != nil {
		t.Fatalf("startRender: %v", err)
	}
	writing := make(chan struct{})
	finishWrite := make(chan struct{})
	type landed struct {
		path    string
		landing renderLanding
		err     error
	}
	result := make(chan landed, 1)
	go func() {
		path, landing, err := agent.landRender(rendering, "", "a ferry", ".mp4", VideoDir(agent.config.Place, workspace), []byte("whole"),
			func(path string, data []byte) error {
				close(writing)
				<-finishWrite
				return writeFileAtomic(path, data)
			})
		result <- landed{path, landing, err}
	}()
	<-writing

	reply, isError := agent.jobs.kill(context.Background(), rendering.id)
	if isError || !strings.Contains(reply, "had already finished rendering") || strings.Contains(reply, "no video was saved") {
		t.Fatalf("mid-write kill said %q", reply)
	}
	close(finishWrite)
	got := <-result
	if got.landing != renderLanded || got.err != nil {
		t.Fatalf("landing = %v, %v", got.landing, got.err)
	}
	data, err := os.ReadFile(got.path)
	if err != nil || !bytes.Equal(data, []byte("whole")) {
		t.Fatalf("saved file = %q, %v", data, err)
	}
	rendering.mu.Lock()
	killed := rendering.killRequested
	rendering.mu.Unlock()
	if killed {
		t.Fatal("a committed render was marked killed, which would swallow its note")
	}
}

// A failed save leaves no empty placeholder behind and no temp file.
func TestFailedSaveLeavesNoPlaceholder(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	rendering, _, err := agent.jobs.startRender(videoLabel, "a ferry")
	if err != nil {
		t.Fatal(err)
	}
	_, landing, err := agent.landRender(rendering, "", "a ferry", ".mp4", VideoDir(agent.config.Place, workspace), []byte("x"),
		func(string, []byte) error { return os.ErrPermission })
	if landing != renderNotSaved || err == nil {
		t.Fatalf("landing = %v, %v", landing, err)
	}
	if files := videoFiles(workspace); len(files) != 0 {
		t.Fatalf("failed save left %v", files)
	}
}

// Renders are admitted up to the cap; the next is refused with the reason, and
// stopping one gives its place back.
func TestRenderAdmissionIsBoundedAndReleasedOnSettle(t *testing.T) {
	media := newGatedMedia([]byte("MP4"), true)
	agent, _ := newGatedAgent(t, media)
	for i := 1; i <= jobRenderCap; i++ {
		if reply, isError := runTool(t, agent, "generate_video", `{"prompt":"a ferry"}`); isError {
			t.Fatalf("render %d refused: %s", i, reply)
		}
		<-media.arrived
	}
	reply, isError := runTool(t, agent, "generate_video", `{"prompt":"one too many"}`)
	if !isError || !strings.Contains(reply, "renders are already running") {
		t.Fatalf("over the cap: %q (error=%v)", reply, isError)
	}
	if media.calls != jobRenderCap {
		t.Fatalf("provider saw %d calls, want %d", media.calls, jobRenderCap)
	}

	runTool(t, agent, "jobs", `{"action":"kill","id":1}`)
	<-renderJob(t, agent, 1).done
	if reply, isError := runTool(t, agent, "generate_video", `{"prompt":"now there is room"}`); isError {
		t.Fatalf("a freed place was not reusable: %s", reply)
	}
	<-media.arrived
	for id := 2; id <= jobRenderCap+2; id++ {
		runTool(t, agent, "jobs", `{"action":"kill","id":`+itoa(id)+`}`)
	}
}

// A failed render lists as failed, is called once, and is not retried.
func TestFailedRenderListsAsFailedAndIsNeverRetried(t *testing.T) {
	media := &scriptedMedia{videoErr: os.ErrDeadlineExceeded}
	agent, _ := newMediaAgent(t, media, nil)
	runTool(t, agent, "generate_video", `{"prompt":"a ferry"}`)
	<-renderJob(t, agent, 1).done
	listed, _ := runTool(t, agent, "jobs", `{"action":"list"}`)
	if !strings.Contains(listed, "job 1 · video · failed") {
		t.Fatalf("list = %q, want a failed row", listed)
	}
	if media.films() != 1 {
		t.Fatalf("provider called %d times, want 1", media.films())
	}
}

// ── the process-wide admission ──────────────────────────────────────────────

// A stopped render whose provider ignores the cancel still holds its place.
// The row says stopped at once; the place comes back only when the provider
// call actually returns.
func TestStoppedRenderKeepsItsProcessPlaceUntilTheProviderReturns(t *testing.T) {
	media := newGatedMedia([]byte("MP4"), false) // ignores the cancel
	agent, _ := newGatedAgent(t, media)
	agent.jobs.gate = newRenderGate(jobRenderCap)

	for i := 1; i <= jobRenderCap; i++ {
		if reply, isError := runTool(t, agent, "generate_video", `{"prompt":"a ferry"}`); isError {
			t.Fatalf("render %d refused: %s", i, reply)
		}
		<-media.arrived
	}
	// The stop is accepted and delivered exactly as `jobs kill` does, without
	// its two-second wait for a row that an ignoring provider never settles.
	for id := 1; id <= jobRenderCap; id++ {
		stopped := renderJob(t, agent, id)
		if claimed, _ := stopped.requestKillRender(); !claimed {
			t.Fatalf("stop on job %d not accepted", id)
		}
		stopped.signal(syscall.SIGTERM)
	}
	reply, isError := runTool(t, agent, "generate_video", `{"prompt":"replacement"}`)
	if !isError || !strings.Contains(reply, "in this process") {
		t.Fatalf("a replacement was admitted while stopped providers still ran: %q", reply)
	}
	if media.calls != jobRenderCap {
		t.Fatalf("provider calls = %d, want %d", media.calls, jobRenderCap)
	}

	// One provider actually returns: exactly one place frees.
	media.release <- struct{}{}
	waitGateFree(t, agent.jobs.gate, 1)
	if reply, isError := runTool(t, agent, "generate_video", `{"prompt":"replacement"}`); isError {
		t.Fatalf("a returned provider did not free its place: %s", reply)
	}
	<-media.arrived
	if _, isError := runTool(t, agent, "generate_video", `{"prompt":"another"}`); !isError {
		t.Fatal("only one place had freed")
	}
	close(media.release)
}

// waitGateFree blocks until the gate has n free places, by polling the gate's
// own state with a yield — the release happens on a goroutine the test cannot
// otherwise observe.
func waitGateFree(t *testing.T, gate *renderGate, n int) {
	t.Helper()
	for len(gate.places) > cap(gate.places)-n {
		runtime.Gosched()
	}
}

// Two agents share three places, not six.
func TestProcessRenderCapIsSharedAcrossAgents(t *testing.T) {
	gate := newRenderGate(jobRenderCap)
	media := newGatedMedia([]byte("MP4"), true)
	first, _ := newGatedAgent(t, media)
	second, _ := newGatedAgent(t, media)
	first.jobs.gate, second.jobs.gate = gate, gate

	admitted := 0
	for _, agent := range []*Agent{first, second, first, second} {
		if _, isError := runTool(t, agent, "generate_video", `{"prompt":"a ferry"}`); !isError {
			admitted++
			<-media.arrived
		}
	}
	if admitted != jobRenderCap {
		t.Fatalf("admitted %d across two agents, want %d", admitted, jobRenderCap)
	}
	for _, agent := range []*Agent{first, second} {
		for _, one := range agent.jobs.all() {
			runTool(t, agent, "jobs", `{"action":"kill","id":`+itoa(one.id)+`}`)
		}
	}
	waitGateFree(t, gate, jobRenderCap)
}

// The place comes back on a provider failure, a committed save, and a stop that
// the provider honours.
func TestProcessPlaceIsReleasedOnEveryRoadOut(t *testing.T) {
	gate := newRenderGate(1)
	for name, scenario := range map[string]func(t *testing.T) (*Agent, func()){
		"failure": func(t *testing.T) (*Agent, func()) {
			agent, _ := newMediaAgent(t, &scriptedMedia{videoErr: os.ErrDeadlineExceeded}, nil)
			return agent, func() {}
		},
		"commit": func(t *testing.T) (*Agent, func()) {
			agent, _ := newMediaAgent(t, &scriptedMedia{video: []byte("MP4")}, nil)
			return agent, func() {}
		},
		"honoured stop": func(t *testing.T) (*Agent, func()) {
			media := newGatedMedia([]byte("MP4"), true)
			agent, _ := newGatedAgent(t, media)
			return agent, func() {
				<-media.arrived
				runTool(t, agent, "jobs", `{"action":"kill","id":1}`)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			agent, after := scenario(t)
			agent.jobs.gate = gate
			if reply, isError := runTool(t, agent, "generate_video", `{"prompt":"a ferry"}`); isError {
				t.Fatalf("refused: %s", reply)
			}
			after()
			<-renderJob(t, agent, 1).done
			waitGateFree(t, gate, 1)
		})
	}
}

// A refusal from the per-registry cap or a failed start gives the place back.
func TestRefusedStartDoesNotLeakAProcessPlace(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	gate := newRenderGate(jobRenderCap + 2)
	agent.jobs.gate = gate
	for i := 0; i < jobRenderCap; i++ {
		if _, _, err := agent.jobs.startRender(videoLabel, "p"); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := agent.jobs.startRender(videoLabel, "p"); err != errRenderCap {
		t.Fatalf("err = %v, want the per-agent cap", err)
	}
	if len(gate.places) != jobRenderCap {
		t.Fatalf("gate holds %d places, want %d (the refusal leaked one)", len(gate.places), jobRenderCap)
	}
	agent.jobs.shutdown(0)
	if _, _, err := agent.jobs.startRender(videoLabel, "p"); err == nil {
		t.Fatal("a closed registry admitted a render")
	}
	if len(gate.places) != jobRenderCap {
		t.Fatalf("a closed-registry refusal leaked a place: %d", len(gate.places))
	}
}

// Concurrent admissions never exceed the gate, and every place is returned.
func TestRenderGateAccountingUnderContention(t *testing.T) {
	gate := newRenderGate(jobRenderCap)
	var held, peak, wins int32
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !gate.tryAcquire() {
				return
			}
			now := atomic.AddInt32(&held, 1)
			for {
				old := atomic.LoadInt32(&peak)
				if now <= old || atomic.CompareAndSwapInt32(&peak, old, now) {
					break
				}
			}
			atomic.AddInt32(&wins, 1)
			atomic.AddInt32(&held, -1)
			gate.release()
		}()
	}
	wg.Wait()
	if peak > int32(jobRenderCap) || len(gate.places) != 0 {
		t.Fatalf("peak %d (cap %d), %d places still held", peak, jobRenderCap, len(gate.places))
	}
}
