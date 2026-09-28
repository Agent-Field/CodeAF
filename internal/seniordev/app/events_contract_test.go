//go:build !windows

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/sessioncore"
)

// The bus payloads senior-dev used to print on stdout are still published
// and still reach the run's log: a test's view of the run. In a run codeaf
// hosts there is no log on stdout (TestAHostedRunReportsOnlyStagesAndSteps).
func TestQuestionToolEventsReachTheLogAsBusPayloads(t *testing.T) {
	workspace := testRepoWithEntrypoints(t)
	var output bytes.Buffer
	runner := newPipeline(cliArgs{High: "provider/high"}, workspace, pipelineDeps{
		Backend: backendFunc(func(context.Context, turn) (turnResult, error) {
			return turnResult{}, nil
		}),
		Events: newEventWriter(&output),
	})
	defer runner.runtime.Close()
	if runner.runtime.initErr != nil {
		t.Fatal(runner.runtime.initErr)
	}

	input := json.RawMessage(`{"questions":[{"question":"Continue?","header":"Choice","options":[{"label":"Yes","description":"Continue now"}]}]}`)
	_, err := runner.runtime.registry.Execute(context.Background(), steploop.ToolCall{
		ID: "call-question", Name: "question", Input: input,
		SessionID: "ses-question", MessageID: "msg-question", Agent: "coder",
	})
	if err == nil || err.Error() != "The user dismissed this question" {
		t.Fatalf("question error = %v, want headless rejection", err)
	}

	seen := map[string]bool{}
	for _, line := range bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n")) {
		var value map[string]json.RawMessage
		if err := json.Unmarshal(line, &value); err != nil {
			t.Fatalf("event line %q: %v", line, err)
		}
		if len(value) != 3 || value["id"] == nil || value["type"] == nil || value["properties"] == nil {
			t.Fatalf("bus line keys = %v, want exactly id/type/properties", value)
		}
		var eventType string
		if err := json.Unmarshal(value["type"], &eventType); err != nil {
			t.Fatal(err)
		}
		seen[eventType] = true
	}
	if !seen["question.asked"] || !seen["question.rejected"] {
		t.Fatalf("stdout events = %v, want question.asked and question.rejected; stream=%s", seen, output.String())
	}
}

func TestPipelineStreamsBusEventsToTheLog(t *testing.T) {
	workspace := testRepoWithEntrypoints(t)
	var output bytes.Buffer
	runner := newPipeline(cliArgs{High: "provider/high"}, workspace, pipelineDeps{
		Backend: backendFunc(func(context.Context, turn) (turnResult, error) {
			return turnResult{}, nil
		}),
		Events: newEventWriter(&output),
	})
	defer runner.runtime.Close()
	if runner.runtime.initErr != nil {
		t.Fatal(runner.runtime.initErr)
	}
	if _, err := runner.runtime.durable.sessions.Create(context.Background(), sessioncore.CreateInput{
		ID: "ses_contract", Title: "contract",
	}); err != nil {
		t.Fatal(err)
	}

	lines := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("session creation lines = %d, want session.created then session.updated: %s", len(lines), output.String())
	}
	for index, wantType := range []string{"session.created", "session.updated"} {
		var value map[string]json.RawMessage
		if err := json.Unmarshal(lines[index], &value); err != nil {
			t.Fatal(err)
		}
		if len(value) != 3 || value["id"] == nil || value["type"] == nil || value["properties"] == nil {
			t.Fatalf("bus line keys = %v, want exactly id/type/properties", value)
		}
		var gotType string
		if err := json.Unmarshal(value["type"], &gotType); err != nil || gotType != wantType {
			t.Fatalf("bus line %d type = %q (%v), want %q", index, gotType, err, wantType)
		}
	}
}

// recordedHost is the part of a delegate host the event writer reports to.
type recordedHost struct {
	stages       []string
	steps        []string
	stageRecords []delegate.StageRecord
	stepRecords  []delegate.StepRecord
}

func (host *recordedHost) Stage(stage delegate.StageRecord) {
	host.stages = append(host.stages, stage.Stage+"/"+stage.Status)
	host.stageRecords = append(host.stageRecords, stage)
}

func (host *recordedHost) Step(step delegate.StepRecord) {
	host.steps = append(host.steps, step.Command)
	host.stepRecords = append(host.stepRecords, step)
}

// STDOUT IS THE PROTOCOL'S. A run codeaf hosts reports its stages and its
// finished steps and nothing else: no bus payload, no spend record, no second
// copy of a step a republished part would have made. A stage's data goes whole
// to the notes, which are stderr, for a person, and a curated copy of it rides
// the stage record; a step says its tool, the step of senior-dev's process it
// served, and a command's exit code.
func TestAHostedRunReportsOnlyStagesAndSteps(t *testing.T) {
	host := &recordedHost{}
	var notes bytes.Buffer
	writer := newRecordWriter(host, &notes)

	writer.stage("implement", "running", map[string]any{"attempt": 0})
	writer.busEvent(toolPartPayload("c1", "bash", "running", map[string]any{"command": "go test ./..."}, "", ""))
	failing := toolPartPayload("c1", "bash", "completed", map[string]any{"command": "go test ./..."}, "FAIL", "")
	failing.Properties.(map[string]any)["part"].(map[string]any)["state"].(map[string]any)["metadata"] = map[string]any{"exitCode": 1}
	writer.busEvent(failing)
	writer.busEvent(failing)
	writer.busEvent(assistantPayload("m1", "coder", 1, 2, 3, 0.01))

	if len(host.stages) != 1 || host.stages[0] != "implement/running" {
		t.Fatalf("stages = %v, want the one stage", host.stages)
	}
	if got := string(host.stageRecords[0].Data); got != `{"attempt":0}` {
		t.Fatalf("stage data = %s, want the attempt", got)
	}
	if len(host.steps) != 1 || host.steps[0] != "bash: go test ./..." {
		t.Fatalf("steps = %v, want the one finished call, once", host.steps)
	}
	if step := host.stepRecords[0]; step.Tool != "bash" || step.Step != StepExplore || step.Exit == nil || *step.Exit != 1 {
		t.Fatalf("step = %+v, want the bash tool, the explore step and exit 1", step)
	}
	if !strings.Contains(notes.String(), `implement · running {"attempt":0}`) {
		t.Fatalf("notes = %q, want the stage and its data for a person", notes.String())
	}
	if strings.Contains(notes.String(), "message.updated") || strings.Contains(notes.String(), "spend") {
		t.Fatalf("notes carry bus traffic: %q", notes.String())
	}
}
