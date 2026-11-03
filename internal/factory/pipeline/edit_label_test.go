package pipeline

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPipelineLabelEdits(t *testing.T) {
	op := func(raw string) EditOp {
		t.Helper()
		var e EditOp
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			t.Fatal(err)
		}
		return e
	}
	got := mustApply(t, editBase, op(`{"Op":"set_step","Name":"write","Label":" Security review "}`))
	if !got.Changed || len(got.Steps) != 1 || strings.Join(got.Steps[0].Fields, ",") != "label" || got.Steps[0].After.Label != "Security review" || !strings.Contains(got.Text, "label: Security review") {
		t.Fatalf("label change lost: %+v", got)
	}
	cleared := mustApply(t, got.Text, op(`{"Op":"set_step","Name":"write","Label":"  "}`))
	if !cleared.Changed || strings.Contains(cleared.Text, "label:") {
		t.Fatalf("clear lost: %+v", cleared)
	}
	unchanged := mustApply(t, editBase, op(`{"Op":"set_step","Name":"write","Label":""}`))
	if unchanged.Changed || unchanged.Text != editBase {
		t.Fatalf("empty label changed unlabelled file: %+v", unchanged)
	}
	for _, label := range []string{strings.Repeat("x", EditUntilMax+1), "a\nb", "a\x00b"} {
		raw, _ := json.Marshal(map[string]any{"Op": "set_step", "Name": "write", "Label": label})
		wantCode(t, refusal(t, editBase, op(string(raw))), EditBadValue, 0)
	}
	for _, name := range []string{"approve", "proof"} {
		raw, _ := json.Marshal(map[string]any{"Op": "set_step", "Name": name, "Label": "Read this"})
		wantCode(t, refusal(t, editBase, op(string(raw))), EditLocked, 0)
	}
	add := op(`{"Op":"add_step","Pipeline":"issue","Name":"signoff","After":"write","Kind":"gate","Label":"Security approval"}`)
	gate := mustApply(t, editBase, add)
	if !strings.Contains(gate.Text, "label: Security approval") {
		t.Fatalf("gate lost label: %s", gate.Text)
	}
}
