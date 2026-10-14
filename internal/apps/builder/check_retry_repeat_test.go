package builder

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func TestRepeatedFindingQuotesValueAndLastChange(t *testing.T) {
	issue := spec.Issue{File: "screens/a.json", Path: "/body/props/gap", Rule: "schema",
		Message: `value must be one of "sm", "md"; fix: set screens/a.json:/body/props/gap to exactly one of "sm", "md"`}
	report := bridge.CheckReport{Round: 1, Issues: []spec.Issue{issue}}
	round1 := spec.Files{"screens/a.json": []byte(`{"body":{"props":{"gap":"large"}}}`)}
	round2 := spec.Files{"screens/a.json": []byte(`{"body":{"props":{"gap":"big"}}}`)}

	m := &roundMemory{}
	if first := m.formatReport(report, round1); strings.Contains(first, "REPEATED") {
		t.Fatalf("first round marked repeated: %s", first)
	}
	m.remember(report, round1)
	again := m.formatReport(bridge.CheckReport{Round: 2, Issues: []spec.Issue{issue}}, round2)
	for _, want := range []string{"REPEATED from round 1", `value now "big"`, `changed it from "large"`, `one of "sm", "md"`} {
		if !strings.Contains(again, want) {
			t.Errorf("repeat line lacks %q:\n%s", want, again)
		}
	}
	other := spec.Issue{File: "app.json", Path: "/name", Rule: "schema", Message: "x"}
	if line := m.formatReport(bridge.CheckReport{Issues: []spec.Issue{other}}, round2); strings.Contains(line, "REPEATED") {
		t.Errorf("new finding marked repeated: %s", line)
	}
	if same := m.note(issue, round1); !strings.Contains(same, "left it unchanged") {
		t.Errorf("unchanged value note: %s", same)
	}
}

func TestValueAtReadsYAMLAndAbsent(t *testing.T) {
	files := spec.Files{"runs/r.yaml": []byte("run: r\nsteps:\n  - agent: hi\n")}
	if got := valueAt(files, "runs/r.yaml", "/steps/0/agent"); got != `"hi"` {
		t.Errorf("yaml value = %s", got)
	}
	if got := valueAt(files, "runs/r.yaml", "/steps/3"); got != "(absent)" {
		t.Errorf("absent = %s", got)
	}
}
