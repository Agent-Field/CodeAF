package pipeline_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

func TestStepLabelSurvivesPipelineDefinition(t *testing.T) {
	for _, name := range []string{"securityrevi", "approve"} {
		for _, label := range []string{"Security review", "", "   "} {
			t.Run(name+"/"+label, func(t *testing.T) {
				text := "pipeline:\n  issue: ["+name+"]\nsteps:\n  "+name+":\n    label: '"+label+"'\n"
				if name != "approve" { text += "    prompt: review security\n    until: clean\n" }
				file, err := pipeline.Parse(text)
				if err != nil { t.Fatal(err) }
				check := func(f pipeline.File) {
					t.Helper()
					stages := f.Recipe().For(factory.KindIssue)
					if len(stages) != 1 { t.Fatalf("steps = %+v", stages) }
					raw, _ := json.Marshal(stages[0])
					var saved struct { Name, Label string }
					_ = json.Unmarshal(raw, &saved)
					if saved.Name != name || saved.Label != strings.TrimSpace(label) { t.Errorf("definition lost label or changed id: %s", raw) }
				}
				check(file)
				raw, err := pipeline.Encode(file)
				if err != nil { t.Fatal(err) }
				if strings.TrimSpace(label) == "" && strings.Contains(string(raw), "label:") { t.Errorf("empty label written: %s", raw) }
				again, err := pipeline.Parse(string(raw))
				if err != nil { t.Fatal(err) }
				check(again)
			})
		}
	}
}
