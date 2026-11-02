package api_test

import (
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
}
