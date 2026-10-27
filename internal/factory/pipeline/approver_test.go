package pipeline

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// WHO AN APPROVAL WAITS ON IS A FILE KEY on a person step, read, written and
// compiled to the runner's stage; on any other step it is a problem and dropped.
func TestAPersonStepNamesWhoItWaitsOn(t *testing.T) {
	const text = `pipeline:
  issue: [plan, approve, write, signoff, proof]
steps:
  approve:
    who: "@Rel"
  signoff:
    kind: gate
    prompt: read it
    who: operator
  plan:
    prompt: plan it
  write:
    prompt: write it
  proof:
    prompt: prove it
`
	f, err := Parse(text)
	if err != nil || len(f.Problems) > 0 {
		t.Fatalf("%v %v", err, f.Problems)
	}
	whom := map[string]string{}
	for _, st := range f.Recipe().For(factory.KindIssue) {
		whom[st.Name] = st.Who
	}
	if whom["approve"] != "@rel" || whom["signoff"] != "operator" || whom["plan"] != "" {
		t.Fatalf("%v", whom)
	}
	data, err := Encode(f)
	if err != nil || !strings.Contains(string(data), "who: '@rel'") && !strings.Contains(string(data), `who: "@rel"`) {
		t.Fatalf("%v\n%s", err, data)
	}
	if back, _ := Parse(string(data)); back.Steps["approve"].Who != "@rel" || back.Steps["signoff"].Who != "operator" {
		t.Fatalf("%+v", back.Steps)
	}
	if !RuntimeKeysIn(text) {
		t.Fatal("who is a key an older reader would drop")
	}
	bad, _ := Parse("pipeline:\n  issue: [plan]\nsteps:\n  plan:\n    prompt: x\n    who: \"@sam\"\n")
	if len(bad.Problems) == 0 || bad.Steps["plan"].Who != "" {
		t.Fatalf("who on a chat: %+v %+v", bad.Problems, bad.Steps["plan"])
	}
	if odd, _ := Parse("pipeline:\n  issue: [approve]\nsteps:\n  approve:\n    who: everyone\n"); len(odd.Problems) == 0 || odd.Steps["approve"].Who != "" {
		t.Fatal("an unreadable who is never anyone")
	}
}

func TestSetApproverNamesWhoAPersonStepWaitsOn(t *testing.T) {
	who := func(s string) *string { return &s }
	got := mustApply(t, editBase, EditOp{Op: OpSetApprover, Name: "approve", Who: who("@Rel")})
	back := reread(t, got.Text)
	if back.Steps["approve"].Who != "@rel" || !got.SettingsEdited || len(got.Settings) != 1 || got.Settings[0].Setting != SettingApprovers || got.Settings[0].After != "approve: @rel" {
		t.Fatalf("%+v %+v", back.Steps["approve"], got.Settings)
	}
	// Back to the job's person writes nothing for approve.
	again := mustApply(t, got.Text, EditOp{Op: OpSetApprover, Name: "approve", Who: who("")})
	if strings.Contains(again.Text, "who:") {
		t.Fatal(again.Text)
	}
	for name, op := range map[string]EditOp{
		"a chat":        {Op: OpSetApprover, Name: "write", Who: who("@rel")},
		"a bad word":    {Op: OpSetApprover, Name: "approve", Who: who("everyone")},
		"no who":        {Op: OpSetApprover, Name: "approve"},
		"not in a list": {Op: OpSetApprover, Name: "approve2", Who: who("@rel")},
	} {
		if _, err := ApplyEdits(editBase, []EditOp{op}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
