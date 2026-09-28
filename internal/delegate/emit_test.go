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
	exit := 2
	_ = emitter.Stage(StageRecord{Stage: "implement", Status: "running", Data: []byte(`{"attempt":1}`)})
	_ = emitter.Stage(StageRecord{Stage: "ship", Status: "unchanged", Data: []byte(`"not an object"`)})
	_ = emitter.Step(StepRecord{Command: "bash:   go test\n./...", Observation: "ok", Tool: "bash", Step: "explore", Exit: &exit})
	_ = emitter.Terminal(Ending{Status: StatusPass, Message: "submitted", CostUSD: 0.42, Claim: "tests pass", Observed: "3 of 3 commands passed", Extra: map[string]any{"claim": "overridden?", "commits": 4}})
	_ = emitter.Terminal(Ending{Status: StatusFail, Message: "never written"})
	sink := &recorder{}
	reading, err := Read(&stdout, sink)
	if err != nil {
		t.Fatal(err)
	}
	if reading.Hello == nil || reading.Hello.Protocol != ProtocolVersion || reading.LastStage != "ship" || reading.Steps != 1 {
		t.Fatalf("reading = %+v", reading)
	}
	if sink.steps[0] != "bash: go test ./...→ok" {
		t.Fatalf("step = %q", sink.steps[0])
	}
	if step := sink.stepRecords[0]; step.Tool != "bash" || step.Step != "explore" || step.Exit == nil || *step.Exit != 2 {
		t.Fatalf("step record = %+v, want its tool, step and exit through the wire", step)
	}
	if string(sink.stageRecords[0].Data) != `{"attempt":1}` || sink.stageRecords[1].Data != nil {
		t.Fatalf("stage data = %s and %s, want the object and nothing for the string", sink.stageRecords[0].Data, sink.stageRecords[1].Data)
	}
	if strings.Contains(stdout.String(), "not an object") {
		t.Fatalf("the emitter wrote data the reader would drop:\n%s", stdout.String())
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
