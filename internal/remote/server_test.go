package remote

// The engine is tested over a pair of in-memory pipes with a scripted agent
// behind it: no ssh, no provider, no session file. That is the whole point of
// [WrappedAgent] being an interface — the protocol is a thing you can drive, and every
// question this file asks ("does an event survive JSON", "do two streams tear
// each other's lines") is a question about the protocol and not about a model.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// TestMain silences the fault log. A recovered panic writes its stack through
// the standard logger (internal/guard), and one of the tests below causes one
// deliberately; the stack is not a failure and should not read like one.
func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	os.Exit(m.Run())
}

// ── the scripted agent ──────────────────────────────────────────────────────

type fakeAgent struct {
	mu sync.Mutex

	streams  []chan session.Event
	sent     []string
	marked   []string
	follows  []string
	steered  []string
	images   []session.Image
	tasks    []string
	planners []string

	model  string
	window int
	levels map[string]string

	interrupts int
	compacts   int
	closes     int

	consents  []string
	standings []session.StandingAnswer
	harnesses []string
	connects  []string
	connected []string

	title      string
	usage      session.Usage
	tokens     int
	transcript []session.DisplayEntry
	earlier    []session.DisplayEntry
	points     []session.RewindPoint
	dropped    []session.DisplayEntry

	// prefill is a turn that is ALREADY OVER by the time it is handed back: the
	// events sit in the channel and the channel is closed. It is the shape that
	// catches a pump overtaking the result that names its stream.
	prefill []session.Event

	failing     error
	compactBy   error
	rewindBy    error
	panicking   bool
	taskJournal string
	cancelled   []string
}

func (f *fakeAgent) TaskJournal(uint64) string { return f.taskJournal }

func (f *fakeAgent) SteerTask(id uint64, line string) (bool, error) {
	f.steered = append(f.steered, fmt.Sprintf("%d:%s", id, line))
	return true, f.failing
}

func (f *fakeAgent) Cancel(id string) (string, error) {
	f.cancelled = append(f.cancelled, id)
	return "stopping task 17", f.failing
}

func (f *fakeAgent) StartTask(_ context.Context, brief string) (uint64, string, error) {
	f.tasks = append(f.tasks, brief)
	return 17, "far task", f.failing
}

func (f *fakeAgent) StartPlannerRun(_ context.Context, brief, hint string) (string, string, error) {
	f.planners = append(f.planners, brief+"|"+hint)
	return "run-8", "far plan", f.failing
}

func (f *fakeAgent) JudgeDecomposable(_ context.Context, brief string) (bool, []string, string) {
	f.tasks = append(f.tasks, "judge:"+brief)
	return true, []string{"one", "two"}, "independent"
}

func (f *fakeAgent) PendingConnect() []string { return nil }

func (f *fakeAgent) open() <-chan session.Event {
	stream := make(chan session.Event, 64)
	f.streams = append(f.streams, stream)
	return stream
}

func (f *fakeAgent) Submit(_ context.Context, text string) (<-chan session.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, text)
	if f.failing != nil {
		return nil, f.failing
	}
	if f.prefill != nil {
		done := make(chan session.Event, len(f.prefill))
		for _, event := range f.prefill {
			done <- event
		}
		close(done)
		return done, nil
	}
	return f.open(), nil
}

// SubmitStanding is the marked door — a draft the person said should keep being
// true (internal/session's standing_mark.go). It is remembered separately so a
// test can tell which of the two doors one frame opened.
func (f *fakeAgent) SubmitStanding(_ context.Context, text string) (<-chan session.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.marked = append(f.marked, text)
	if f.failing != nil {
		return nil, f.failing
	}
	return f.open(), nil
}

func (f *fakeAgent) SubmitImage(_ context.Context, text string, images []session.Image) (<-chan session.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, text)
	f.images = append(f.images, images...)
	if f.failing != nil {
		return nil, f.failing
	}
	return f.open(), nil
}

func (f *fakeAgent) FollowUp(text string) (<-chan session.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.follows = append(f.follows, text)
	if f.failing != nil {
		return nil, f.failing
	}
	return f.open(), nil
}

func (f *fakeAgent) Steer(text string) (<-chan session.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.steered = append(f.steered, text)
	if f.failing != nil {
		return nil, f.failing
	}
	return f.open(), nil
}

// stream is the nth channel this agent handed out, for a test that wants to
// push events down it.
func (f *fakeAgent) stream(n int) chan session.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	if n >= len(f.streams) {
		return nil
	}
	return f.streams[n]
}

// finish ends one turn the way a real one ends, and takes it off the books so
// that Close does not end it twice.
func (f *fakeAgent) finish(stream chan session.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, held := range f.streams {
		if held == stream {
			f.streams = append(f.streams[:i], f.streams[i+1:]...)
			close(stream)
			return
		}
	}
}

func (f *fakeAgent) Interrupt() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.interrupts++
}

func (f *fakeAgent) Compact(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.compacts++
	return f.compactBy
}

func (f *fakeAgent) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closes++
	for _, stream := range f.streams {
		close(stream)
	}
	f.streams = nil
	return nil
}

func (f *fakeAgent) Model() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.model
}

func (f *fakeAgent) SetModel(model string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.model = model
}

func (f *fakeAgent) SetContextWindow(tokens int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.window = tokens
}

// THE LEVELS ARE KEYED THE WAY internal/session KEYS THEM ([session.ReasoningKey]),
// because a double that stored them under the caller's own spelling would let a
// level set from a picker row go missing from a lookup by a slug typed in
// another case — a bug the real agent does not have, invented here.
func (f *fakeAgent) ReasoningFor(model string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.panicking {
		panic("the reasoning map is not there")
	}
	return f.levels[session.ReasoningKey(model)]
}

// ReasoningLevels is the whole map, copied — the fact set's own read
// ([session.FactsOf]).
func (f *fakeAgent) ReasoningLevels() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.levels) == 0 {
		return nil
	}
	levels := make(map[string]string, len(f.levels))
	for model, level := range f.levels {
		levels[model] = level
	}
	return levels
}

func (f *fakeAgent) SetReasoningFor(model, level string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := session.ReasoningKey(model)
	if key == "" {
		return
	}
	// Absence is stored as absence, exactly as the agent stores it.
	if level == "" {
		delete(f.levels, key)
		return
	}
	if f.levels == nil {
		f.levels = map[string]string{}
	}
	f.levels[key] = level
}

func (f *fakeAgent) ResolveConsent(id uint64, allow bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.consents = append(f.consents, note("consent", id, allow, ""))
}

func (f *fakeAgent) ResolveConsentRemember(id uint64, allow bool, scope session.ConsentScope) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.consents = append(f.consents, note("remember", id, allow, string(scope)))
}

func (f *fakeAgent) ResolveStanding(id uint64, answer session.StandingAnswer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.consents = append(f.consents, note("standing", id, answer.Approved, answer.Change))
	// The answer is kept WHOLE as well as noted, because the thing worth
	// asserting about it is a pointer's three states and a note is a string.
	f.standings = append(f.standings, answer)
}

func (f *fakeAgent) ResolveHarness(id uint64, run bool, model string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.harnesses = append(f.harnesses, note("harness", id, run, model))
}

func (f *fakeAgent) ResolveConnect(id string, approve bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connects = append(f.connects, "approve:"+id+":"+boolWord(approve))
}

func (f *fakeAgent) ResolveConnectKey(id string, key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connects = append(f.connects, "key:"+id+":"+key)
}

func (f *fakeAgent) NoteConnected(service, account string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connected = append(f.connected, service+":"+account)
}

func (f *fakeAgent) Title() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.title
}

func (f *fakeAgent) Usage() session.Usage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.usage
}

func (f *fakeAgent) ContextTokens() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokens
}

func (f *fakeAgent) Transcript() []session.DisplayEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.transcript
}

func (f *fakeAgent) EarlierHistory() session.EarlierHistory {
	f.mu.Lock()
	defer f.mu.Unlock()
	return session.EarlierHistory{Entries: f.earlier}
}

func (f *fakeAgent) RewindPoints() []session.RewindPoint {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.points
}

func (f *fakeAgent) RewindAt(int) ([]session.DisplayEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rewindBy != nil {
		return nil, f.rewindBy
	}
	return f.dropped, nil
}

func note(kind string, id uint64, yes bool, extra string) string {
	out := kind + ":" + itoa(id) + ":" + boolWord(yes)
	if extra != "" {
		out += ":" + extra
	}
	return out
}

func boolWord(yes bool) string {
	if yes {
		return "yes"
	}
	return "no"
}

func itoa(id uint64) string {
	if id == 0 {
		return "0"
	}
	var digits []byte
	for id > 0 {
		digits = append([]byte{byte('0' + id%10)}, digits...)
		id /= 10
	}
	return string(digits)
}

// ── the pipe pair ───────────────────────────────────────────────────────────

// link is a surface's side of an engine: what it writes, what it reads, and the
// error the engine finally returned.
type link struct {
	t      *testing.T
	toward *io.PipeWriter
	frames chan Frame
	spare  []Frame
	served chan error
	// stated is every "facts" frame this link has passed over. THE FACT PUSH IS
	// UNSOLICITED (wire.go's version 4): it answers no call and belongs to no
	// stream, so a reader walking a turn's frames in order has to be able to
	// step past one — exactly as the real client's reader does, by kind. They
	// are kept rather than dropped so a test can assert one was sent.
	stated []Frame
}

// unparsable is the kind the reader invents for a line that is not a frame. It
// exists so that "did anything tear" is an assertion a test can make rather
// than a panic in the reader goroutine.
const unparsable = "unparsable"

func dial(t *testing.T, boot func(Hello) (*Engine, error)) *link {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	l := &link{t: t, toward: inW, frames: make(chan Frame, 256), served: make(chan error, 1)}

	go func() {
		err := Serve(inR, outW, Options{Boot: boot})
		_ = outW.Close()
		l.served <- err
	}()
	go func() {
		scan := bufio.NewScanner(outR)
		scan.Buffer(make([]byte, 0, 64*1024), frameCap)
		for scan.Scan() {
			var frame Frame
			if err := json.Unmarshal(scan.Bytes(), &frame); err != nil {
				l.frames <- Frame{Kind: unparsable, Error: scan.Text()}
				continue
			}
			l.frames <- frame
		}
		close(l.frames)
	}()

	t.Cleanup(func() { _ = inW.Close() })
	return l
}

// dialAgent is the ordinary case: one engine, one scripted agent.
func dialAgent(t *testing.T, engine *Engine) *link {
	t.Helper()
	return dial(t, func(Hello) (*Engine, error) { return engine, nil })
}

func (l *link) write(frame Frame) {
	l.t.Helper()
	line, err := json.Marshal(frame)
	if err != nil {
		l.t.Fatalf("encode frame: %v", err)
	}
	if _, err := l.toward.Write(append(line, '\n')); err != nil {
		l.t.Fatalf("write frame: %v", err)
	}
}

// recv is the next frame a caller ASKED FOR, from the stash first and then the
// wire, stepping over the engine's unsolicited fact pushes on the way.
func (l *link) recv() Frame {
	l.t.Helper()
	for {
		frame := l.recvAny()
		if frame.Kind == "facts" {
			l.stated = append(l.stated, frame)
			continue
		}
		return frame
	}
}

// recvAny is the next frame whatever it is, fact pushes included.
func (l *link) recvAny() Frame {
	l.t.Helper()
	if len(l.spare) > 0 {
		frame := l.spare[0]
		l.spare = l.spare[1:]
		return frame
	}
	select {
	case frame, ok := <-l.frames:
		if !ok {
			l.t.Fatalf("the engine closed the pipe with nothing more to say")
		}
		return frame
	case <-time.After(5 * time.Second):
		l.t.Fatalf("the engine said nothing for five seconds")
		return Frame{}
	}
}

// await walks the wire until a frame matches, stashing everything it passes so
// that a later reader still sees it in order.
func (l *link) await(match func(Frame) bool) Frame {
	l.t.Helper()
	var passed []Frame
	for {
		frame := l.recv()
		if match(frame) {
			l.spare = append(passed, l.spare...)
			return frame
		}
		passed = append(passed, frame)
	}
}

func (l *link) hello(hello Hello) Frame {
	l.t.Helper()
	l.write(Frame{Kind: "hello", Payload: raw(l.t, hello)})
	return l.recv()
}

// call makes one call and returns its result, which is the frame carrying the
// same id — events from a turn already running may arrive in between and are
// stashed rather than dropped.
func (l *link) call(id uint64, method string, payload any) Frame {
	l.t.Helper()
	frame := Frame{Kind: "call", ID: id, Method: method}
	if payload != nil {
		frame.Payload = raw(l.t, payload)
	}
	l.write(frame)
	return l.await(func(f Frame) bool { return f.Kind == "result" && f.ID == id })
}

// ok is call with the assertion every method that must not fail wants.
func (l *link) ok(id uint64, method string, payload any) Frame {
	l.t.Helper()
	result := l.call(id, method, payload)
	if result.Error != "" {
		l.t.Fatalf("%s: %s", method, result.Error)
	}
	return result
}

// end closes the surface's side and waits for the engine to finish.
func (l *link) end() error {
	l.t.Helper()
	_ = l.toward.Close()
	select {
	case err := <-l.served:
		return err
	case <-time.After(5 * time.Second):
		l.t.Fatalf("the engine did not exit")
		return nil
	}
}

func raw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	return encoded
}

func decode[T any](t *testing.T, payload json.RawMessage) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatalf("decode payload %s: %v", payload, err)
	}
	return value
}

func engineOn(agent *fakeAgent) *Engine {
	return &Engine{
		Agent:       agent,
		Workspace:   "/home/somebody/api",
		SessionFile: "/home/somebody/.aforge/v3/sessions/-home-somebody-api/one.jsonl",
		Resumed:     true,
		Note:        "session open elsewhere — started a new one",
	}
}

// ── the handshake ───────────────────────────────────────────────────────────

func TestServeWelcomesAHello(t *testing.T) {
	agent := &fakeAgent{model: "openai/gpt-5", title: "the api rewrite"}
	l := dialAgent(t, engineOn(agent))

	frame := l.hello(Hello{Version: Version, Workspace: "api"})
	if frame.Kind != "welcome" {
		t.Fatalf("first answer was %q: %s", frame.Kind, frame.Error)
	}
	welcome := decode[Welcome](t, frame.Payload)
	if welcome.Version != Version {
		t.Errorf("welcome version %d, want %d", welcome.Version, Version)
	}
	if welcome.Workspace != "/home/somebody/api" {
		t.Errorf("welcome workspace %q", welcome.Workspace)
	}
	if !strings.HasSuffix(welcome.SessionFile, "one.jsonl") || !welcome.Resumed {
		t.Errorf("welcome session %q resumed %v", welcome.SessionFile, welcome.Resumed)
	}
	if welcome.Model != "openai/gpt-5" || welcome.Title != "the api rewrite" {
		t.Errorf("welcome model %q title %q", welcome.Model, welcome.Title)
	}
	if !strings.Contains(welcome.Note, "started a new one") {
		t.Errorf("welcome note %q — the locked-file sentence has to travel", welcome.Note)
	}
	if err := l.end(); err != nil {
		t.Fatalf("serve: %v", err)
	}
}

func TestLargeFramesCompressOnlyAfterNegotiation(t *testing.T) {
	entries := []session.DisplayEntry{{Role: "assistant", Text: strings.Repeat("compressible transcript ", 4000)}}
	agent := &fakeAgent{transcript: entries}

	oldSurface := dialAgent(t, engineOn(agent))
	welcome := decode[Welcome](t, oldSurface.hello(Hello{Version: Version}).Payload)
	if welcome.Encoding != "" {
		t.Fatalf("an old surface was assigned %q", welcome.Encoding)
	}
	plain := oldSurface.ok(1, MethodTranscript, nil)
	if plain.Encoding != "" || len(plain.Payload) < compressionThreshold {
		t.Fatalf("old surface got encoding=%q payload=%d", plain.Encoding, len(plain.Payload))
	}
	_ = oldSurface.end()

	newSurface := dialAgent(t, engineOn(agent))
	welcome = decode[Welcome](t, newSurface.hello(Hello{Version: Version, Encodings: []string{frameEncodingGzip}}).Payload)
	if welcome.Encoding != frameEncodingGzip {
		t.Fatalf("negotiated encoding = %q", welcome.Encoding)
	}
	compressed := newSurface.call(1, MethodTranscript, nil)
	if compressed.Encoding != frameEncodingGzip || len(compressed.Payload) != 0 || len(compressed.Data) == 0 {
		t.Fatalf("negotiated frame = encoding %q payload %d data %d", compressed.Encoding, len(compressed.Payload), len(compressed.Data))
	}
	if err := expandFrame(&compressed); err != nil {
		t.Fatal(err)
	}
	if got := decode[[]session.DisplayEntry](t, compressed.Payload); len(got) != 1 || got[0].Text != entries[0].Text {
		t.Fatal("expanded transcript changed")
	}
	_ = newSurface.end()
}

type flushCountingWriter struct {
	bytes.Buffer
	flushes int
}

func (w *flushCountingWriter) Flush() error {
	w.flushes++
	return nil
}

func TestEveryServerFrameIsFlushed(t *testing.T) {
	out := &flushCountingWriter{}
	s := &server{out: out}
	if err := s.send(Frame{Kind: "result", ID: 1, Payload: raw(t, "ready")}); err != nil {
		t.Fatal(err)
	}
	if out.flushes != 1 {
		t.Fatalf("flushes = %d, want one for one frame", out.flushes)
	}
	if !strings.HasSuffix(out.String(), "\n") {
		t.Fatalf("frame was not completed as one line: %q", out.String())
	}
}

func TestServeRefusesAnotherVersion(t *testing.T) {
	agent := &fakeAgent{}
	l := dialAgent(t, engineOn(agent))

	frame := l.hello(Hello{Version: Version + 1})
	if frame.Kind != "fatal" {
		t.Fatalf("answer to a version mismatch was %q, want fatal", frame.Kind)
	}
	if !strings.Contains(frame.Error, "protocol") {
		t.Errorf("fatal said %q", frame.Error)
	}
	if err := l.end(); err == nil {
		t.Fatal("a refused handshake has to be an error, so the engine exits 1")
	}
	if agent.closes != 0 {
		t.Errorf("nothing was opened, so nothing should have been closed (closes=%d)", agent.closes)
	}
}

func TestServeRefusesGarbage(t *testing.T) {
	l := dialAgent(t, engineOn(&fakeAgent{}))
	if _, err := l.toward.Write([]byte("this is not a frame\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	frame := l.recv()
	if frame.Kind != "fatal" {
		t.Fatalf("answer to garbage was %q, want fatal", frame.Kind)
	}
	if err := l.end(); err == nil {
		t.Fatal("a garbled handshake has to be an error")
	}
}

func TestServeRefusesACallBeforeAHello(t *testing.T) {
	l := dialAgent(t, engineOn(&fakeAgent{}))
	l.write(Frame{Kind: "call", ID: 1, Method: MethodModel})
	if frame := l.recv(); frame.Kind != "fatal" {
		t.Fatalf("answer to a call before the hello was %q, want fatal", frame.Kind)
	}
	if err := l.end(); err == nil {
		t.Fatal("a handshake that never happened has to be an error")
	}
}

func TestServeCarriesABootFailure(t *testing.T) {
	l := dial(t, func(Hello) (*Engine, error) {
		return nil, errors.New("open /home/somebody/nowhere: no such directory")
	})
	frame := l.hello(Hello{Version: Version, Workspace: "nowhere"})
	if frame.Kind != "fatal" || !strings.Contains(frame.Error, "no such directory") {
		t.Fatalf("boot failure came back as %q / %q", frame.Kind, frame.Error)
	}
	if err := l.end(); err == nil {
		t.Fatal("a workspace that will not open has to be an error")
	}
}

// ── the methods ─────────────────────────────────────────────────────────────

func TestServeAnswersEveryMethod(t *testing.T) {
	agent := &fakeAgent{
		model:      "openai/gpt-5",
		title:      "the api rewrite",
		tokens:     4321,
		usage:      session.Usage{Input: 10, Output: 20, CostUSD: 0.5, Turns: 2},
		transcript: []session.DisplayEntry{{Role: "user", Text: "hello"}, {Role: "assistant", Text: "hi"}},
		points:     []session.RewindPoint{{Index: 3, Turn: true, Said: "hello", Entry: 1}},
		dropped:    []session.DisplayEntry{{Role: "user", Text: "hello"}},
	}
	l := dialAgent(t, engineOn(agent))
	if frame := l.hello(Hello{Version: Version}); frame.Kind != "welcome" {
		t.Fatalf("handshake: %s", frame.Error)
	}

	// The stream-openers answer with a stream id and nothing else.
	if ref := decode[StreamRef](t, l.ok(1, MethodSubmit, SubmitArgs{Text: "write the readme"}).Payload); ref.Stream == 0 {
		t.Error("Submit answered with no stream")
	}
	if ref := decode[StreamRef](t, l.ok(2, MethodFollowUp, SubmitArgs{Text: "and the changelog"}).Payload); ref.Stream == 0 {
		t.Error("FollowUp answered with no stream")
	}
	if ref := decode[StreamRef](t, l.ok(99, MethodSteer, SubmitArgs{Text: "use staging"}).Payload); ref.Stream == 0 {
		t.Error("Steer answered with no stream")
	}

	l.ok(3, MethodInterrupt, nil)
	l.ok(4, MethodCompact, nil)
	l.ok(5, MethodSetModel, "anthropic/claude")
	if model := decode[string](t, l.ok(6, MethodModel, nil).Payload); model != "anthropic/claude" {
		t.Errorf("Model answered %q after SetModel", model)
	}
	l.ok(7, MethodSetContext, 200000)
	l.ok(8, MethodSetReasoningFor, ReasoningArgs{Model: "openai/gpt-5", Level: "high"})
	if level := decode[string](t, l.ok(9, MethodReasoningFor, "openai/gpt-5").Payload); level != "high" {
		t.Errorf("ReasoningFor answered %q", level)
	}
	l.ok(10, MethodConsent, ConsentArgs{ID: 7, Allow: true})
	l.ok(11, MethodConsentRemember, ConsentArgs{ID: 8, Allow: true, Scope: session.ConsentToolSession})
	l.ok(12, MethodHarness, HarnessArgs{ID: 9, Run: true, Model: "openai/gpt-5"})
	l.ok(13, MethodConnect, ConnectArgs{ID: "google", Approve: true})
	l.ok(14, MethodConnectKey, ConnectArgs{ID: "exa", Key: "sk-live"})
	l.ok(15, MethodNoteConnected, ConnectedArgs{Service: "google", Account: "somebody@example.com"})

	if title := decode[string](t, l.ok(16, MethodTitle, nil).Payload); title != "the api rewrite" {
		t.Errorf("Title answered %q", title)
	}
	if usage := decode[session.Usage](t, l.ok(17, MethodUsage, nil).Payload); usage.Input != 10 || usage.CostUSD != 0.5 {
		t.Errorf("Usage answered %+v", usage)
	}
	if tokens := decode[int](t, l.ok(18, MethodContextTokens, nil).Payload); tokens != 4321 {
		t.Errorf("ContextTokens answered %d", tokens)
	}
	if entries := decode[[]session.DisplayEntry](t, l.ok(19, MethodTranscript, nil).Payload); len(entries) != 2 || entries[1].Text != "hi" {
		t.Errorf("Transcript answered %+v", entries)
	}
	if points := decode[[]session.RewindPoint](t, l.ok(20, MethodRewindPoints, nil).Payload); len(points) != 1 || points[0].Index != 3 {
		t.Errorf("RewindPoints answered %+v", points)
	}
	if dropped := decode[[]session.DisplayEntry](t, l.ok(21, MethodRewindAt, 3).Payload); len(dropped) != 1 {
		t.Errorf("RewindAt answered %+v", dropped)
	}

	if result := l.call(22, "Nonsense", nil); result.Error == "" {
		t.Error("a method this build does not know has to come back as an error")
	}

	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.sent) != 1 || agent.sent[0] != "write the readme" {
		t.Errorf("Submit carried %q", agent.sent)
	}
	if len(agent.follows) != 1 || agent.follows[0] != "and the changelog" {
		t.Errorf("FollowUp carried %q", agent.follows)
	}
	if len(agent.steered) != 1 || agent.steered[0] != "use staging" {
		t.Errorf("Steer carried %q", agent.steered)
	}
	if agent.interrupts != 1 || agent.compacts != 1 || agent.window != 200000 {
		t.Errorf("interrupts=%d compacts=%d window=%d", agent.interrupts, agent.compacts, agent.window)
	}
	if len(agent.consents) != 2 || agent.consents[1] != "remember:8:yes:tool-session" {
		t.Errorf("consents %q", agent.consents)
	}
	if len(agent.harnesses) != 1 || agent.harnesses[0] != "harness:9:yes:openai/gpt-5" {
		t.Errorf("harness answers %q", agent.harnesses)
	}
	if len(agent.connects) != 2 || agent.connects[1] != "key:exa:sk-live" {
		t.Errorf("connect answers %q", agent.connects)
	}
	if len(agent.connected) != 1 || agent.connected[0] != "google:somebody@example.com" {
		t.Errorf("connected notes %q", agent.connected)
	}
}

func TestServeCarriesAMethodFailure(t *testing.T) {
	agent := &fakeAgent{failing: errors.New("no key for this model"), compactBy: errors.New("nothing to compact")}
	l := dialAgent(t, engineOn(agent))
	l.hello(Hello{Version: Version})

	if result := l.call(1, MethodSubmit, SubmitArgs{Text: "go"}); result.Error != "no key for this model" {
		t.Errorf("a refused Submit came back as %q", result.Error)
	}
	if result := l.call(2, MethodCompact, nil); result.Error != "nothing to compact" {
		t.Errorf("a failed Compact came back as %q", result.Error)
	}
	// The engine is still serving: a method that failed is not a pipe that died.
	if result := l.call(3, MethodContextTokens, nil); result.Error != "" {
		t.Errorf("the engine stopped answering after a failure: %q", result.Error)
	}
}

func TestServeSurvivesAPanickingMethod(t *testing.T) {
	agent := &fakeAgent{panicking: true, model: "openai/gpt-5"}
	l := dialAgent(t, engineOn(agent))
	l.hello(Hello{Version: Version})

	result := l.call(1, MethodReasoningFor, "openai/gpt-5")
	if result.Error == "" {
		t.Fatal("a panicking method has to come back as an error")
	}
	agent.mu.Lock()
	agent.panicking = false
	agent.mu.Unlock()
	if result := l.call(2, MethodModel, nil); result.Error != "" {
		t.Errorf("the engine died of a fault it should have absorbed: %q", result.Error)
	}
}

// ── streams ─────────────────────────────────────────────────────────────────

func TestServeStreamsATurnInOrder(t *testing.T) {
	agent := &fakeAgent{}
	l := dialAgent(t, engineOn(agent))
	l.hello(Hello{Version: Version})

	ref := decode[StreamRef](t, l.ok(1, MethodSubmit, SubmitArgs{Text: "go"}).Payload)
	stream := agent.stream(0)
	stream <- session.Event{Kind: session.EventTextDelta, Text: "one"}
	stream <- session.Event{Kind: session.EventToolBegin, Tool: "read", Hint: "README.md", Args: `{"path":"README.md"}`}
	stream <- session.Event{Kind: session.EventTurnDone, Usage: session.Usage{Input: 5, Output: 6}}
	agent.finish(stream)

	first := decode[EventWire](t, l.recv().Payload).Unwire()
	if first.Kind != session.EventTextDelta || first.Text != "one" {
		t.Errorf("first event %+v", first)
	}
	second := decode[EventWire](t, l.recv().Payload).Unwire()
	if second.Tool != "read" || second.Args != `{"path":"README.md"}` {
		t.Errorf("second event %+v", second)
	}
	third := decode[EventWire](t, l.recv().Payload).Unwire()
	if third.Kind != session.EventTurnDone || third.Usage.Output != 6 {
		t.Errorf("third event %+v", third)
	}
	closed := l.recv()
	if closed.Kind != "closed" || closed.ID != ref.Stream {
		t.Errorf("the stream ended with %q id %d, want closed id %d", closed.Kind, closed.ID, ref.Stream)
	}
}

// A stream must be NAMED before it speaks. The events are already waiting in
// the channel when Submit answers, so a pump started a moment too early would
// put them on the wire ahead of the result that says which stream they belong
// to — and a surface cannot bind an event to a turn it has not been told about.
func TestServeNamesAStreamBeforeItSpeaks(t *testing.T) {
	const events = 24
	prefill := make([]session.Event, 0, events)
	for i := 0; i < events; i++ {
		prefill = append(prefill, session.Event{Kind: session.EventTextDelta, Text: itoa(uint64(i))})
	}
	agent := &fakeAgent{prefill: prefill}
	l := dialAgent(t, engineOn(agent))
	l.hello(Hello{Version: Version})

	l.write(Frame{Kind: "call", ID: 1, Method: MethodSubmit, Payload: raw(t, SubmitArgs{Text: "go"})})
	first := l.recv()
	if first.Kind != "result" {
		t.Fatalf("the first frame after the call was %q — a stream spoke before it was named", first.Kind)
	}
	ref := decode[StreamRef](t, first.Payload)
	for i := 0; i < events; i++ {
		frame := l.recv()
		if frame.Kind != "event" || frame.ID != ref.Stream {
			t.Fatalf("event %d arrived as %q on stream %d", i, frame.Kind, frame.ID)
		}
		if text := decode[EventWire](t, frame.Payload).Unwire().Text; text != itoa(uint64(i)) {
			t.Fatalf("event %d carried %q", i, text)
		}
	}
	if closed := l.recv(); closed.Kind != "closed" || closed.ID != ref.Stream {
		t.Fatalf("the stream ended with %q id %d", closed.Kind, closed.ID)
	}
}

func TestServeCarriesAnEventError(t *testing.T) {
	agent := &fakeAgent{}
	l := dialAgent(t, engineOn(agent))
	l.hello(Hello{Version: Version})
	l.ok(1, MethodSubmit, SubmitArgs{Text: "go"})

	stream := agent.stream(0)
	stream <- session.Event{Kind: session.EventError, Err: errors.New("the provider refused the request")}
	agent.finish(stream)

	event := decode[EventWire](t, l.recv().Payload).Unwire()
	if event.Err == nil {
		t.Fatal("the error did not survive the wire — an interface marshals to nothing")
	}
	if event.Err.Error() != "the provider refused the request" {
		t.Errorf("the error came back as %q", event.Err)
	}
}

func TestServeInterleavesTwoStreamsWithoutTearing(t *testing.T) {
	agent := &fakeAgent{}
	l := dialAgent(t, engineOn(agent))
	l.hello(Hello{Version: Version})

	first := decode[StreamRef](t, l.ok(1, MethodSubmit, SubmitArgs{Text: "one"}).Payload)
	second := decode[StreamRef](t, l.ok(2, MethodSubmit, SubmitArgs{Text: "two"}).Payload)
	if first.Stream == second.Stream {
		t.Fatal("two turns were given the same stream id")
	}

	const each = 40
	one, two := agent.stream(0), agent.stream(1)
	var writers sync.WaitGroup
	writers.Add(2)
	for _, pair := range []struct {
		stream chan session.Event
		word   string
	}{{one, "one"}, {two, "two"}} {
		go func(stream chan session.Event, word string) {
			defer writers.Done()
			for i := 0; i < each; i++ {
				// The text is long on purpose: a short frame could tear and
				// still parse, and the question here is whether one writer can
				// land inside another's line.
				stream <- session.Event{Kind: session.EventTextDelta, Text: word + strings.Repeat("·", 200) + itoa(uint64(i))}
			}
			agent.finish(stream)
		}(pair.stream, pair.word)
	}
	writers.Wait()

	counts := map[uint64]int{}
	closes := map[uint64]bool{}
	for len(closes) < 2 {
		frame := l.recv()
		if frame.Kind == unparsable {
			t.Fatalf("a line did not parse — two writers tore one frame: %.120q", frame.Error)
		}
		switch frame.Kind {
		case "event":
			event := decode[EventWire](t, frame.Payload).Unwire()
			// Every stream's events arrive in ITS OWN order, whatever order the
			// two streams arrive in relative to each other.
			if want := itoa(uint64(counts[frame.ID])); !strings.HasSuffix(event.Text, want) {
				t.Fatalf("stream %d event %d was %q", frame.ID, counts[frame.ID], event.Text)
			}
			counts[frame.ID]++
		case "closed":
			closes[frame.ID] = true
		default:
			t.Fatalf("unexpected %q frame mid-stream", frame.Kind)
		}
	}
	for id, count := range counts {
		if count != each {
			t.Errorf("stream %d delivered %d events, want %d", id, count, each)
		}
	}
}

// ── the session doors ───────────────────────────────────────────────────────

func TestServeSwapsSessions(t *testing.T) {
	first := &fakeAgent{model: "openai/gpt-5", title: "the old one"}
	second := &fakeAgent{model: "openai/gpt-5", title: "the new one"}
	third := &fakeAgent{model: "openai/gpt-5", title: "an earlier one"}

	engine := engineOn(first)
	engine.Fresh = func() (WrappedAgent, string, error) { return second, "/sessions/two.jsonl", nil }
	engine.Open = func(file string) (WrappedAgent, bool, error) { return third, true, nil }
	engine.Recent = func() []session.Summary {
		return []session.Summary{{File: "/sessions/one.jsonl", Title: "the old one", Asked: 3}}
	}

	l := dialAgent(t, engine)
	l.hello(Hello{Version: Version})

	recent := decode[[]session.Summary](t, l.ok(1, MethodSessionsRecent, nil).Payload)
	if len(recent) != 1 || recent[0].Title != "the old one" {
		t.Fatalf("Sessions.Recent answered %+v", recent)
	}

	// A turn is running on the first agent when the swap happens. Its pump must
	// go quiet: the surface has been handed a new welcome and has forgotten
	// everything before it.
	l.ok(2, MethodSubmit, SubmitArgs{Text: "go"})
	stale := first.stream(0)

	welcome := decode[Welcome](t, l.ok(3, MethodSessionNew, nil).Payload)
	if welcome.SessionFile != "/sessions/two.jsonl" || welcome.Resumed {
		t.Errorf("Session.New answered %+v", welcome)
	}
	if welcome.Title != "the new one" {
		t.Errorf("Session.New answered with the old agent's title %q", welcome.Title)
	}
	if welcome.Note != "" {
		t.Errorf("the launch note outlived the launch: %q", welcome.Note)
	}
	if first.closes != 1 {
		t.Errorf("the replaced agent was closed %d times, want once", first.closes)
	}
	select {
	case _, ok := <-stale:
		if ok {
			t.Fatal("the replaced agent's stream is still open")
		}
	case <-time.After(time.Second):
		t.Fatal("closing the replaced agent did not end its stream")
	}

	// The next call lands on the NEW agent.
	l.ok(4, MethodSubmit, SubmitArgs{Text: "again"})
	if len(second.sent) != 1 {
		t.Errorf("the call went to the old agent (new agent saw %q)", second.sent)
	}

	opened := decode[Welcome](t, l.ok(5, MethodSessionOpen, "/sessions/three.jsonl").Payload)
	if opened.SessionFile != "/sessions/three.jsonl" || !opened.Resumed || opened.Title != "an earlier one" {
		t.Errorf("Session.Open answered %+v", opened)
	}

	// Nothing the retired agents streamed reached the wire after the swap.
	if err := l.end(); err != nil {
		t.Fatalf("serve: %v", err)
	}
	for frame := range l.frames {
		if frame.Kind == "event" {
			t.Errorf("a retired stream sent an event after the swap: %s", frame.Payload)
		}
	}
	if third.closes != 1 {
		t.Errorf("the last agent was closed %d times, want once", third.closes)
	}
}

func TestServeSaysWhenASessionDoorIsMissing(t *testing.T) {
	l := dialAgent(t, engineOn(&fakeAgent{}))
	l.hello(Hello{Version: Version})
	for _, method := range []string{MethodSessionsRecent, MethodSessionNew, MethodSessionOpen} {
		if result := l.call(1, method, "/sessions/one.jsonl"); result.Error == "" {
			t.Errorf("%s answered without a door behind it", method)
		}
	}
}

// ── pictures ────────────────────────────────────────────────────────────────

func TestServeWritesAnUploadedPicture(t *testing.T) {
	workspace := t.TempDir()
	agent := &fakeAgent{}
	engine := engineOn(agent)
	engine.Workspace = workspace
	l := dialAgent(t, engine)
	l.hello(Hello{Version: Version})

	bytes := []byte("\x89PNG\r\n\x1a\n and then some pixels")
	l.ok(1, MethodSubmitImage, SubmitImageArgs{
		Text:   "what is this?",
		Images: []session.Image{{Path: "/home/somebody-else/Desktop/shot.png", MIME: "image/png", Bytes: bytes}},
	})

	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.images) != 1 {
		t.Fatalf("the agent saw %d images", len(agent.images))
	}
	image := agent.images[0]
	if !strings.HasPrefix(image.Path, workspace) {
		t.Fatalf("the picture kept the surface's path %q — the journal would name a file that is not here", image.Path)
	}
	if filepath.Ext(image.Path) != ".png" {
		t.Errorf("the picture was written as %q", filepath.Base(image.Path))
	}
	written, err := os.ReadFile(image.Path)
	if err != nil {
		t.Fatalf("read what the engine wrote: %v", err)
	}
	if string(written) != string(bytes) {
		t.Errorf("the picture on disk is not the picture that arrived")
	}
	if string(image.Bytes) != string(bytes) {
		t.Error("the bytes were dropped — the session would read the file it was just told about")
	}
}

func TestServeRefusesAPictureItCannotName(t *testing.T) {
	engine := engineOn(&fakeAgent{})
	engine.Workspace = t.TempDir()
	l := dialAgent(t, engine)
	l.hello(Hello{Version: Version})

	result := l.call(1, MethodSubmitImage, SubmitImageArgs{
		Text:   "look",
		Images: []session.Image{{Path: "notes.txt", Bytes: []byte("not a picture")}},
	})
	if result.Error == "" {
		t.Fatal("a picture with no readable type has to be refused rather than guessed at")
	}
}

// ── the exit ────────────────────────────────────────────────────────────────

func TestServeClosesTheAgentWhenThePipeDies(t *testing.T) {
	agent := &fakeAgent{}
	l := dialAgent(t, engineOn(agent))
	l.hello(Hello{Version: Version})
	l.ok(1, MethodSubmit, SubmitArgs{Text: "a long piece of work"})

	// The pipe dies mid-turn, which is the case that matters: the journal is
	// the only record, and only Close flushes it.
	if err := l.end(); err != nil {
		t.Fatalf("an ordinary hang-up has to be exit 0, got %v", err)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if agent.interrupts != 1 {
		t.Errorf("the turn was interrupted %d times, want once", agent.interrupts)
	}
	if agent.closes != 1 {
		t.Errorf("the agent was closed %d times, want once — THE JOURNAL IS THE SAFETY", agent.closes)
	}
}

func TestServeExitsQuietlyWhenNobodySaidHello(t *testing.T) {
	agent := &fakeAgent{}
	l := dialAgent(t, engineOn(agent))
	if err := l.end(); err != nil {
		t.Fatalf("a surface that hung up before the hello is not an error, got %v", err)
	}
	if agent.closes != 0 {
		t.Errorf("nothing was opened, so nothing should have been closed (closes=%d)", agent.closes)
	}
}

// A picture that arrives for a BORROWED session lands in the session's own
// artifacts directory and never in the repository the engine is standing in
// (docs/CHAT-V3.md, Decision 26). It earns NO row in the deliverables index:
// a paste is the person's input, not something made for them, and /files
// buried under a person's own clipboard would answer nothing.
func TestServeLandsAnUploadedPictureInTheSessionFolder(t *testing.T) {
	workspace := t.TempDir()
	folder := filepath.Join(t.TempDir(), "0123456789abcdef")
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")

	agent := &fakeAgent{}
	engine := engineOn(agent)
	engine.Workspace = workspace
	engine.Place = session.Place{Dir: folder, Workspace: workspace}
	l := dialAgent(t, engine)
	l.hello(Hello{Version: Version})

	raw := []byte("\x89PNG\r\n\x1a\n and then some pixels")
	l.ok(1, MethodSubmitImage, SubmitImageArgs{
		Text:   "what is this?",
		Images: []session.Image{{Path: "/home/somebody-else/Desktop/shot.png", MIME: "image/png", Bytes: raw}},
	})

	agent.mu.Lock()
	path := agent.images[0].Path
	agent.mu.Unlock()

	if got, want := filepath.Dir(path), engine.Place.Artifacts(); got != want {
		t.Fatalf("the picture landed in %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".aforge-v3")); !os.IsNotExist(err) {
		t.Fatalf("the engine littered the workspace: %v", err)
	}
	if rows := session.ReadArtifacts(index); len(rows) != 0 {
		t.Fatalf("a paste earned %d deliverable rows, want none: %+v", len(rows), rows)
	}
}

// ── the ambient side ────────────────────────────────────────────────────────

// standingCard is one proposal with every band of the card filled, so that a
// round trip has something to lose. It is deliberately a COMPLETE item — the
// person's words, a probe with its arguments, a task action, the rails, the
// item's own present — because the question these tests ask is not "does JSON
// work" but "does anything on either of these two types quietly fail to
// travel".
func standingCard() session.StandingNotice {
	item := standing.Item{
		Schema:    standing.Schema,
		ID:        "01HQ",
		Words:     "tell me when CI goes red on main",
		Workspace: "/home/somebody/api",
		Origin: standing.Origin{
			SessionID: "one", Transcript: "/sessions/one.jsonl", TurnIDs: []string{"t1", "t2"},
		},
		When: standing.When{
			Kind:  standing.WhenProbe,
			Words: "whenever CI finishes",
			Probe: standing.Probe{
				Tool: "bash",
				Args: json.RawMessage(`{"command":"gh run list --limit 1"}`),
			},
			ProbeEvery: 10 * time.Minute,
			Hint:       "yes when any run on main shows conclusion=failure",
		},
		Does: standing.Action{
			Kind: standing.ActionTask, Brief: "find out what broke", Acceptance: "a named commit", MaxSteps: 40,
		},
		Rails:         standing.Rails{PerRunUSD: 0.05, MaxPerDay: 6, Expires: time.Now().Add(48 * time.Hour).UTC().Round(time.Second)},
		Status:        standing.StatusActive,
		Created:       time.Now().UTC().Round(time.Second),
		Updated:       time.Now().UTC().Round(time.Second),
		LastChecked:   time.Now().UTC().Round(time.Second),
		LastCheckLine: "green",
		Previous:      []string{"green", "green"},
		Runs:          2,
		SpentUSD:      0.04,
		NeedsPerson:   "the branch is gone",
	}
	return session.StandingNotice{
		ID:        7,
		Item:      item,
		WhenWords: "every ten minutes while CI is running",
		CostWords: "about $0.05 a run, at most six times a day",
		Guessed:   true,
		Options:   session.StandingOptions(item),
	}
}

// A CARD IS DATA AND THE ANSWER IS A CALL, and this is the whole seam in one
// test: the proposal goes out on a turn's stream as an ordinary event, and the
// person's answer comes back as its own frame. Neither half existed over --host
// before — the card crossed and could only be looked at.
func TestAStandingCardCrossesTheWireAndIsAnsweredBack(t *testing.T) {
	agent := &fakeAgent{}
	l := dialAgent(t, engineOn(agent))
	l.hello(Hello{Version: Version})

	ref := decode[StreamRef](t, l.ok(1, MethodSubmit, SubmitArgs{Text: "keep an eye on CI"}).Payload)
	card := standingCard()
	stream := agent.stream(0)
	stream <- session.Event{Kind: session.EventStandingProposal, Tool: "stand", Standing: &card}

	frame := l.await(func(f Frame) bool { return f.Kind == "event" && f.ID == ref.Stream })
	arrived := decode[EventWire](t, frame.Payload).Unwire()
	if arrived.Kind != session.EventStandingProposal || arrived.Standing == nil {
		t.Fatalf("the card did not arrive as a card: %+v", arrived)
	}
	// THE ITEM IS THE CARD. A surface draws it and never reshapes it, so a field
	// that fell off the wire is a band a person reads as absent.
	if !reflect.DeepEqual(*arrived.Standing, card) {
		t.Fatalf("the card changed crossing the wire:\n got %+v\nwant %+v", *arrived.Standing, card)
	}
	if len(arrived.Standing.Options) != len(card.Options) || len(card.Options) == 0 {
		t.Fatalf("the chips did not travel: %+v", arrived.Standing.Options)
	}
	if arrived.Standing.Item.When.Probe.Tool != "bash" || len(arrived.Standing.Item.When.Probe.Args) == 0 {
		t.Fatalf("the probe did not travel: %+v", arrived.Standing.Item.When)
	}

	l.ok(2, MethodStandingResolve, StandingArgs{ID: 7, Answer: session.StandingAnswer{Approved: true}})
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.standings) != 1 {
		t.Fatalf("the answer did not reach the engine's agent: %+v", agent.standings)
	}
	if !agent.standings[0].Approved {
		t.Fatalf("the answer arrived as %+v", agent.standings[0])
	}
	if len(agent.consents) != 1 || agent.consents[0] != "standing:7:yes" {
		t.Fatalf("the card the answer was for did not travel: %q", agent.consents)
	}
	stream <- session.Event{Kind: session.EventStandingUpdate, Standing: &session.StandingNotice{Update: "stood", Text: "watching CI"}}
	news := decode[EventWire](t, l.await(func(f Frame) bool {
		return f.Kind == "event" && f.ID == ref.Stream
	}).Payload).Unwire()
	if news.Standing == nil || news.Standing.Update != "stood" || news.Standing.Text != "watching CI" {
		t.Fatalf("the update line did not travel: %+v", news.Standing)
	}
}

// THE STORE THE SURFACE READS IS THE ENGINE MACHINE'S OWN, and these are the two
// doors that make that true: one workspace's items out, one item back.
func TestTheEnginesStandingStoreAnswersOverTheWire(t *testing.T) {
	root := t.TempDir()
	store, err := standing.Open(filepath.Join(root, "standing"))
	if err != nil {
		t.Fatal(err)
	}
	item := standingCard().Item
	item.Workspace = "/home/somebody/api"
	item.NeedsPerson = ""
	if _, err := store.Create(item); err != nil {
		t.Fatalf("create: %v", err)
	}

	engine := engineOn(&fakeAgent{})
	engine.StandingItems = store.ForWorkspace
	engine.StandingSave = store.Save
	l := dialAgent(t, engine)
	l.hello(Hello{Version: Version})

	items := decode[[]standing.Item](t, l.ok(1, MethodStandingItems, "/home/somebody/api").Payload)
	if len(items) != 1 || items[0].Words != item.Words {
		t.Fatalf("Standing.Items answered %+v", items)
	}
	// A workspace nothing was ever set up in is an empty list and never an
	// error: the ambient side is on, and there is simply nothing here.
	if elsewhere := decode[[]standing.Item](t, l.ok(2, MethodStandingItems, "/home/somebody/www").Payload); len(elsewhere) != 0 {
		t.Fatalf("another workspace answered %+v", elsewhere)
	}

	// The pause key: one item written back, and the store holding the change.
	paused := items[0]
	paused.Status = standing.StatusPaused
	l.ok(3, MethodStandingSave, paused)
	after, err := store.ForWorkspace("/home/somebody/api")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0].Status != standing.StatusPaused {
		t.Fatalf("the write did not land: %+v", after)
	}

	// A REFUSED WRITE IS AN ERROR AND NOT A SHRUG. The store validates what it
	// is asked to hold, and the surface prints the refusal on its own message
	// line rather than redrawing a row that was never saved.
	broken := paused
	broken.Rails.PerRunUSD = -1
	if result := l.call(4, MethodStandingSave, broken); result.Error == "" {
		t.Fatal("an item the store refuses was reported as written")
	}
}

// AN ENGINE WITH NO AMBIENT SIDE HAS NO DOOR, not an empty one. A store that
// could not be opened leaves both closures nil (cmd/aforge's engine.go), and the
// surface keeps the difference between "nothing is set up here" and "this
// machine cannot answer that at all".
func TestServeSaysWhenTheStandingDoorIsMissing(t *testing.T) {
	l := dialAgent(t, engineOn(&fakeAgent{}))
	l.hello(Hello{Version: Version})
	if result := l.call(1, MethodStandingItems, "/home/somebody/api"); result.Error == "" {
		t.Error("Standing.Items answered without a store behind it")
	}
	if result := l.call(2, MethodStandingSave, standingCard().Item); result.Error == "" {
		t.Error("Standing.Save answered without a store behind it")
	}
}
