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
	mu       sync.Mutex
	once     sync.Once
	spoke    chan struct{}
	hello    *Hello
	stages   []string
	spend    []float64
	steps    []string
	terminal *Terminal
}

func newRecorder() *recorder { return &recorder{spoke: make(chan struct{})} }

func (r *recorder) Hello(h Hello) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hello = &h
}

func (r *recorder) Stage(stage, status string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stages = append(r.stages, stage+"·"+status)
	if r.spoke != nil {
		r.once.Do(func() { close(r.spoke) })
	}
}
func (r *recorder) Spend(usd float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spend = append(r.spend, usd)
}
func (r *recorder) Step(command, observation string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, command+"→"+observation)
}
func (r *recorder) Terminal(t Terminal) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.terminal = &t
}

// A recorded senior-dev stream, taken from EVENTS-CONTRACT.md's shapes, read
// through the one generic reader: the stages reach the live step, the spend
// reaches the bank, the steps reach the page, the terminal is the result, and
// every bus payload passes through untouched.
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
	if reading.SpendUSD != 0.0213 || reading.Steps != 2 {
		t.Fatalf("spend %.4f steps %d, want 0.0213 and 2", reading.SpendUSD, reading.Steps)
	}
	// Three bus payloads are on the stream; they are ignored, not failed.
	if reading.Ignored != 3 {
		t.Fatalf("ignored = %d, want the three bus payloads", reading.Ignored)
	}
	if got := strings.Join(sink.stages, " "); !strings.Contains(got, "implement·running") || !strings.Contains(got, "verification·pass") {
		t.Fatalf("stages = %q", got)
	}
	// The spend is told three times and never goes down; the repeat is told
	// again at the same figure, which a bank reads as no delta.
	if len(sink.spend) != 3 || sink.spend[0] != 0.0101 || sink.spend[2] != 0.0213 {
		t.Fatalf("spend told = %v", sink.spend)
	}
	if sink.steps[0] != "bash: go test ./...→ok  \tpkg\t0.3s" || sink.steps[1] != "edit: internal/auth/middleware.go→" {
		t.Fatalf("steps told = %q", sink.steps)
	}
	// The terminal's optional keys read in senior-dev's spelling.
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

func TestTheReaderKeepsSpendFromFallingAndTakesOneTerminal(t *testing.T) {
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
	if len(sink.spend) != 2 || sink.spend[1] != 0.5 {
		t.Fatalf("spend told = %v, want the second reading held at the first's high water", sink.spend)
	}
	if sink.terminal == nil || sink.terminal.Message != "first" {
		t.Fatalf("terminal = %+v, want the first one only", sink.terminal)
	}
	// The second terminal, the stray line and the unknown type are the three
	// ignored lines; the empty line is nothing.
	if reading.Ignored != 3 {
		t.Fatalf("ignored = %d", reading.Ignored)
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
