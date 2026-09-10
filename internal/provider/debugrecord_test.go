package provider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/trace"
)

// recordingRun turns the debug record on for ONE run and points the state
// root at a disposable folder, so a test cannot write into whoever ran it
// and cannot leave the process-wide switch on for the tests that follow.
func recordingRun(t *testing.T) context.Context {
	t.Helper()
	t.Setenv("AFORGE_HOME", t.TempDir())
	ctx := trace.Begin(context.Background())
	if trace.EnableRun(ctx) == "" {
		t.Fatal("EnableRun did not take this run")
	}
	return ctx
}

func TestACompletedCallWritesItsBodiesIntoTheDebugRecord(t *testing.T) {
	ctx := recordingRun(t)
	client, _ := newTestClient(t, Config{
		SupportsParameter: func(string, string) (bool, bool) { return true, true },
	})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello from the record")); err != nil {
		t.Fatal(err)
	}

	folder := filepath.Join(trace.Dir(trace.RunFrom(ctx)), trace.CallsDirName)
	entries, err := os.ReadDir(folder)
	if err != nil {
		t.Fatalf("the debug record wrote no calls folder: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("one completion should leave one body file, got %d", len(entries))
	}
	raw, err := os.ReadFile(filepath.Join(folder, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("the body file is not JSON: %v\n%s", err, raw)
	}
	if document["kind"] != "call" {
		t.Fatalf("kind = %v, want call", document["kind"])
	}
	if document["model"] != "sim/model" {
		t.Errorf("model = %v, want sim/model", document["model"])
	}
	if document["call"] == "" || document["call"] == nil {
		t.Error("the body file named no call id")
	}
	if !strings.Contains(string(raw), "hello from the record") {
		t.Errorf("the request body did not keep what went out:\n%s", raw)
	}
	if !strings.Contains(string(raw), "ok") {
		t.Errorf("the response body did not keep what came back:\n%s", raw)
	}
}

func TestACallWithTheRecordOffWritesNoBodyFile(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	ctx := trace.Begin(context.Background())
	client, _ := newTestClient(t, Config{})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(trace.Dir(trace.RunFrom(ctx)), trace.CallsDirName)
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Fatalf("the record was off and still wrote %s (%v)", folder, err)
	}
}

func TestALaneChoiceIsADecisionInTheDebugRecord(t *testing.T) {
	ctx := recordingRun(t)
	recordLaneChoice(ctx, "sim/model", lanes.Choice{
		Order:  []string{"together", "fireworks"},
		Ignore: []string{"friendli"},
		Why:    "together has been the fastest this week",
	})
	event := onlyEvent(t, ctx)
	if event["kind"] != "decision" || event["decision"] != "lane" {
		t.Fatalf("event = %v, want a lane decision", event)
	}
	if event["choice"] != "together" {
		t.Errorf("choice = %v, want together", event["choice"])
	}
	if event["subject"] != "sim/model" {
		t.Errorf("subject = %v, want the model", event["subject"])
	}
	alts, _ := event["alternatives"].([]any)
	if len(alts) != 2 || alts[0] != "fireworks" || alts[1] != "not friendli" {
		t.Errorf("alternatives = %v, want fireworks and not friendli", alts)
	}
	if event["reason"] != "together has been the fastest this week" {
		t.Errorf("reason = %v", event["reason"])
	}
}

func TestAFiredHedgeAndARefusedHedgeAreBothDecisions(t *testing.T) {
	ctx := recordingRun(t)
	race := &hedgeRace{base: ctx, model: "sim/model"}
	race.recordHedge("together", "the first machine went quiet", true)
	race.recordHedge("fireworks", "budget", false)

	events := allEvents(t, ctx)
	if len(events) != 2 {
		t.Fatalf("got %d events, want a fired hedge and a refused one", len(events))
	}
	if events[0]["kind"] != "decision" || events[0]["decision"] != "hedge" {
		t.Fatalf("first event = %v, want a hedge decision", events[0])
	}
	if events[0]["choice"] != "together" {
		t.Errorf("fired choice = %v, want together", events[0]["choice"])
	}
	if events[1]["choice"] != "no second machine" {
		t.Errorf("refused choice = %v, want no second machine", events[1]["choice"])
	}
	if events[1]["reason"] != "budget" {
		t.Errorf("refused reason = %v, want budget", events[1]["reason"])
	}
}

func onlyEvent(t *testing.T, ctx context.Context) map[string]any {
	t.Helper()
	events := allEvents(t, ctx)
	if len(events) != 1 {
		t.Fatalf("got %d events, want one", len(events))
	}
	return events[0]
}

func allEvents(t *testing.T, ctx context.Context) []map[string]any {
	t.Helper()
	path := filepath.Join(trace.Dir(trace.RunFrom(ctx)), trace.EventsFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the debug record wrote no events file: %v", err)
	}
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("an events line is not JSON: %v\n%s", err, line)
		}
		events = append(events, event)
	}
	return events
}
