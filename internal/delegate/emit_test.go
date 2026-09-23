package delegate

import (
	"bytes"
	"strings"
	"testing"
)

// What the emitter writes is what the reader reads: one package, one spelling.
func TestTheEmitterWritesWhatTheReaderReads(t *testing.T) {
	var stdout bytes.Buffer
	emitter := NewEmitter(&stdout)
	_ = emitter.Hello("senior-dev", []string{"implement", "submit"})
	_ = emitter.Stage("implement", "running")
	_ = emitter.Step("bash:   go test\n./...", "ok")
	_ = emitter.Terminal(Ending{Status: StatusPass, Message: "submitted", CostUSD: 0.42, Claim: "tests pass", Observed: "3 of 3 commands passed", Extra: map[string]any{"claim": "overridden?", "commits": 4}})
	_ = emitter.Terminal(Ending{Status: StatusFail, Message: "never written"})
	sink := &recorder{}
	reading, err := Read(&stdout, sink)
	if err != nil {
		t.Fatal(err)
	}
	if reading.Hello == nil || reading.Hello.Protocol != ProtocolVersion || reading.LastStage != "implement" || reading.Steps != 1 {
		t.Fatalf("reading = %+v", reading)
	}
	if sink.steps[0] != "bash: go test ./...→ok" {
		t.Fatalf("step = %q", sink.steps[0])
	}
	end := reading.Terminal
	if end == nil || end.Status != StatusPass || end.Claim() != "tests pass" || end.Observed() != "3 of 3 commands passed" {
		t.Fatalf("terminal = %+v", end)
	}
	if cost, _ := end.CostUSD(); cost != 0.42 {
		t.Fatalf("cost = %v", cost)
	}
	if reading.Ignored != 0 || strings.Contains(stdout.String(), "never written") {
		t.Fatalf("a second terminal was written")
	}
}
