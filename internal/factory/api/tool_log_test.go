package api_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestToolRunLogReadable(t *testing.T) {
	for _, home := range []string{"/Users/runner", "/home/runner"} {
		text := "bash bash " + home + "/work/repo && go test " + strings.Repeat("x", 300)
		person := "bash bash " + home + "/work/repo is what I typed"
		job := api.FromItem(factory.Item{Stream: &factory.Stream{Log: []factory.LogLine{
			{Tone: "thought", Text: text}, {Tone: "said", Text: person},
		}}})
		raw, err := json.Marshal(job.Desk.Run.Log)
		if err != nil {
			t.Fatal(err)
		}
		var lines []map[string]any
		if err := json.Unmarshal(raw, &lines); err != nil {
			t.Fatal(err)
		}
		got := lines[0]["text"].(string)
		if !strings.HasPrefix(got, "bash ~/work/repo && go test ") || len([]rune(got)) > 240 || lines[0]["kind"] != "tool" {
			t.Errorf("tool line: text=%q kind=%v", got, lines[0]["kind"])
		}
		if lines[1]["text"] != person || lines[1]["kind"] != nil {
			t.Errorf("person's words changed: %v", lines[1])
		}
	}
	if got := factory.ToolLogText("bash /Users/runner/work/repo/file", "/Users/runner/work/repo"); got != "bash file" {
		t.Errorf("checkout: %q", got)
	}
	if got := factory.ToolLogText("read /work/repository/file", "/work/repo"); got != "read /work/repository/file" {
		t.Errorf("adjacent checkout name changed: %q", got)
	}

	boundary := "bash " + strings.Repeat("x", 228) + " /Users/runner/file"
	if got := factory.ToolLogText(boundary, ""); !strings.HasSuffix(got, " ~/file") {
		t.Errorf("home must be replaced before clipping: %q", got)
	}

	report := api.FromRunReport(factory.RunReport{Log: []factory.LogLine{{Tone: "thought", Text: "bash bash /home/runner/repo && go test"}, {Tone: "tool", Text: "read ~/repo/file"}}})
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var oldReport struct {
		Log []struct {
			At    any    `json:"at"`
			Glyph string `json:"glyph,omitempty"`
			Tone  string `json:"tone,omitempty"`
			Text  string `json:"text"`
		} `json:"log"`
	}
	// Inspect the log body through the older strict schema, independently of
	// unrelated report fields which older factories already understand.
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(body["log"]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&oldReport.Log); err != nil {
		t.Fatalf("older factory refuses report log: %v", err)
	}
	restored := api.ToWorkerReport(report)
	job := api.FromItem(factory.Item{Stream: &factory.Stream{Log: restored.Log}})
	raw, err = json.Marshal(job.Desk.Run.Log)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"kind":"tool"`)) {
		t.Fatalf("tool kind lost after report: %s", raw)
	}
}
