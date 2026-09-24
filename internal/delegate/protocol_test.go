package delegate

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// recorder is a Sink that keeps what it was told, in order. It is read after
// the reader is done, except for spoke, which a launch test waits on to know
// the program has said its first word.
type recorder struct {
	mu     sync.Mutex
	once   sync.Once
	spoke  chan struct{}
	hello  *Hello
	stages []string
	steps  []string
	// stageRecords and stepRecords are the records whole, for the tests of the
	// optional fields.
	stageRecords []StageRecord
	stepRecords  []StepRecord
	terminal     *Terminal
}

func newRecorder() *recorder { return &recorder{spoke: make(chan struct{})} }

func (r *recorder) Hello(h Hello) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hello = &h
}

func (r *recorder) Stage(stage StageRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stages = append(r.stages, stage.Stage+"·"+stage.Status)
	r.stageRecords = append(r.stageRecords, stage)
	if r.spoke != nil {
		r.once.Do(func() { close(r.spoke) })
	}
}
func (r *recorder) Step(step StepRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, step.Command+"→"+step.Observation)
	r.stepRecords = append(r.stepRecords, step)
}
func (r *recorder) Terminal(t Terminal) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.terminal = &t
}

// A recorded senior-dev stream, taken from EVENTS-CONTRACT.md's shapes, read
// through the one generic reader: the stages reach the live step, the steps
// reach the page, the terminal is the result, and every bus payload passes
// through untouched. The stream was recorded while the program still reported
// its own `spend`; those lines are read now as what they are — lines this
// reader does not know — because the model API meters money itself.
func TestTheReaderReplaysASeniorDevStream(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "senior-dev-stream.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	sink := &recorder{}
	reading, err := Read(strings.NewReader(string(data)), sink)
	if err != nil {
		t.Fatal(err)
	}
	if reading.Terminal == nil || reading.Terminal.Status != StatusPass {
		t.Fatalf("terminal = %+v, want the pass senior-dev wrote last", reading.Terminal)
	}
	if reading.LastStage != "agent-summary" {
		t.Fatalf("last stage = %q, want agent-summary, the stage before the terminal", reading.LastStage)
	}
	if reading.Steps != 2 {
		t.Fatalf("steps %d, want 2", reading.Steps)
	}
	// Three bus payloads and three v1 spend lines are on the stream; all six
	// are ignored, not failed.
	if reading.Ignored != 6 {
		t.Fatalf("ignored = %d, want the three bus payloads and the three spend lines", reading.Ignored)
	}
	if got := strings.Join(sink.stages, " "); !strings.Contains(got, "implement·running") || !strings.Contains(got, "verification·pass") {
		t.Fatalf("stages = %q", got)
	}
	if sink.steps[0] != "bash: go test ./...→ok  \tpkg\t0.3s" || sink.steps[1] != "edit: internal/auth/middleware.go→" {
		t.Fatalf("steps told = %q", sink.steps)
	}
	// The terminal's optional keys read in senior-dev's spelling. Its cost is
	// the program's own reading, kept on the record and never banked.
	cost, ok := sink.terminal.CostUSD()
	if !ok || cost != 0.0213 {
		t.Fatalf("terminal cost = %v %v", cost, ok)
	}
	if sink.terminal.Claim() != "tests pass" {
		t.Fatalf("claim = %q, want senior-dev's submission_reason", sink.terminal.Claim())
	}
	if sink.terminal.Observed() != "pass" {
		t.Fatalf("observed = %q, want senior-dev's own inner status", sink.terminal.Observed())
	}
}

// ONE TERMINAL, AND NO WORD OF THE PROGRAM'S ABOUT MONEY. A second terminal
// is dropped, and a v1 `spend` record is a line this reader does not know: the
// model API is where a run's money is metered, so nothing the program says
// about its own spending reaches a sink.
func TestTheReaderTakesOneTerminalAndNoSpendRecord(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"spend","cost_usd":0.5}`,
		`{"type":"spend","cost_usd":0.2}`,
		`{"type":"terminal","status":"fail","message":"first"}`,
		`{"type":"terminal","status":"pass","message":"second"}`,
		`not json at all`,
		`{"type":"something-else"}`,
		``,
	}, "\n")
	sink := &recorder{}
	reading, err := Read(strings.NewReader(stream), sink)
	if err != nil {
		t.Fatal(err)
	}
	if sink.terminal == nil || sink.terminal.Message != "first" {
		t.Fatalf("terminal = %+v, want the first one only", sink.terminal)
	}
	// The two spend lines, the second terminal, the stray line and the unknown
	// type are the five ignored lines; the empty line is nothing.
	if reading.Ignored != 5 {
		t.Fatalf("ignored = %d, want the two spend lines, the second terminal, the stray line and the unknown type", reading.Ignored)
	}
}

func TestTheReaderCapsAStepOnARuneBoundary(t *testing.T) {
	long := strings.Repeat("é", 2000)
	stream := `{"type":"step","command":"  bash:   two   words  ","observation":"` + long + `"}` + "\n"
	sink := &recorder{}
	if _, err := Read(strings.NewReader(stream), sink); err != nil {
		t.Fatal(err)
	}
	got := sink.steps[0]
	command, observation, _ := strings.Cut(got, "→")
	if command != "bash: two words" {
		t.Fatalf("command = %q, want it folded onto one line", command)
	}
	if len(observation) > observationCap || !strings.HasSuffix(observation, "é") {
		t.Fatalf("observation is %d bytes ending %q, want ≤ %d on a rune boundary", len(observation), observation[len(observation)-2:], observationCap)
	}
}

func TestObservedReadsSeniorDevsVerificationCount(t *testing.T) {
	sink := &recorder{}
	stream := `{"type":"terminal","status":"fail","message":"x","data":{"status":"fail","verification_failing":2,"verification_commands":5}}`
	if _, err := Read(strings.NewReader(stream), sink); err != nil {
		t.Fatal(err)
	}
	if got := sink.terminal.Observed(); got != "fail, verification failed 2 of 5 commands" {
		t.Fatalf("observed = %q", got)
	}
}

// THE FIRST HELLO IS THE ONE READ: it carries the protocol, the name and the
// stages, and a second is ignored for the reason a second terminal is.
func TestTheReaderTakesOneHelloWithItsStages(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"hello","protocol":2,"delegate":"senior-dev","stages":["bootstrap","implement","submit"]}`,
		`{"type":"hello","protocol":9,"delegate":"other"}`,
		`{"type":"stage","stage":"implement","status":"running"}`,
	}, "\n")
	sink := &recorder{}
	reading, err := Read(strings.NewReader(stream), sink)
	if err != nil {
		t.Fatal(err)
	}
	if reading.Hello == nil || reading.Hello.Protocol != ProtocolVersion || reading.Hello.Delegate != "senior-dev" {
		t.Fatalf("hello = %+v, want the first one", reading.Hello)
	}
	if sink.hello == nil || strings.Join(sink.hello.Stages, ",") != "bootstrap,implement,submit" {
		t.Fatalf("hello told = %+v", sink.hello)
	}
	if reading.Ignored != 1 {
		t.Fatalf("ignored = %d, want the second hello", reading.Ignored)
	}
}

// A STEP SAYS ITS TOOL, ITS STEP AND A COMMAND'S EXIT, AND A STAGE ITS DATA —
// each optional, each read forgivingly. A field of another shape than this
// reader's is left off and the record kept; data that is not an object, or is
// past the cap, is left off the stage and the stage kept; and a record that
// carries none of them reads exactly as it did before they existed.
func TestTheReaderCarriesTheOptionalFieldsAndForgivesTheirShape(t *testing.T) {
	big := `{"text":"` + strings.Repeat("x", StageDataCap) + `"}`
	stream := strings.Join([]string{
		`{"type":"step","command":"bash: go test ./...","observation":"FAIL","tool":"bash","step":"explore","exit":1}`,
		`{"type":"step","command":"bash: go build ./...","tool":"bash","step":"verify","exit":0}`,
		`{"type":"step","command":"read: a.go","tool":7,"step":{"id":"x"},"exit":"one"}`,
		`{"type":"step","command":"edit: a.go"}`,
		`{"type":"stage","stage":"submit","status":"frozen","data":{"patch_files":4,"checklist_items":5}}`,
		`{"type":"stage","stage":"verification","status":"pass","data":[1,2]}`,
		`{"type":"stage","stage":"verification","status":"pass","data":` + big + `}`,
		`{"type":"stage","stage":"bootstrap","status":"ready"}`,
	}, "\n")
	sink := &recorder{}
	reading, err := Read(strings.NewReader(stream), sink)
	if err != nil {
		t.Fatal(err)
	}
	if reading.Steps != 4 || len(sink.stageRecords) != 4 || reading.Ignored != 0 {
		t.Fatalf("steps %d, stages %d, ignored %d; want every record kept", reading.Steps, len(sink.stageRecords), reading.Ignored)
	}
	first := sink.stepRecords[0]
	if first.Tool != "bash" || first.Step != "explore" || first.Exit == nil || *first.Exit != 1 {
		t.Fatalf("first step = %+v, want its tool, its step and its exit", first)
	}
	// AN EXIT OF 0 IS A FACT, NOT AN ABSENCE.
	if second := sink.stepRecords[1]; second.Exit == nil || *second.Exit != 0 {
		t.Fatalf("second step = %+v, want exit 0 kept", second)
	}
	if odd := sink.stepRecords[2]; odd.Tool != "" || odd.Step != "" || odd.Exit != nil || odd.Command != "read: a.go" {
		t.Fatalf("a step with odd-shaped optional fields = %+v, want them left off and the step kept", odd)
	}
	if plain := sink.stepRecords[3]; plain.Tool != "" || plain.Step != "" || plain.Exit != nil {
		t.Fatalf("a step with no optional fields = %+v", plain)
	}
	if got := string(sink.stageRecords[0].Data); got != `{"patch_files":4,"checklist_items":5}` {
		t.Fatalf("stage data = %s, want the object as written", got)
	}
	for i := 1; i <= 3; i++ {
		if data := sink.stageRecords[i].Data; data != nil {
			t.Fatalf("stage %d data = %s, want none: not an object, past the cap, or never sent", i, data)
		}
	}
}
