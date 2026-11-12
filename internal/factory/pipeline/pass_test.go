package pipeline

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// WHO MAY PASS AN APPROVAL IS A FILE KEY on a person step, read, written and
// compiled to the runner's stage; an unreadable one is a person's alone.
func TestAPersonStepSaysWhoMayPassIt(t *testing.T) {
	const text = `pipeline:
  issue: [plan, approve, write, signoff, proof]
steps:
  approve:
    pass: person
  signoff:
    kind: gate
    prompt: read it
    pass: manager when the change touches 3 files or fewer
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
	pass := map[string]string{}
	for _, st := range f.Recipe().For(factory.KindIssue) {
		pass[st.Name] = st.Pass
	}
	if pass["approve"] != "person" || pass["signoff"] != "manager when the change touches 3 files or fewer" || pass["plan"] != "" {
		t.Fatalf("%v", pass)
	}
	data, err := Encode(f)
	if err != nil || !strings.Contains(string(data), "pass: person") {
		t.Fatalf("%v\n%s", err, data)
	}
	if back, _ := Parse(string(data)); back.Steps["approve"].Pass != "person" || back.Steps["signoff"].Pass != pass["signoff"] {
		t.Fatalf("%+v", back.Steps)
	}
	if !RuntimeKeysIn("pipeline:\n  issue: [approve]\nsteps:\n  approve:\n    pass: manager\n") {
		t.Fatal("pass is a key an older reader would drop")
	}
	odd, _ := Parse("pipeline:\n  issue: [approve]\nsteps:\n  approve:\n    pass: anyone\n")
	if len(odd.Problems) == 0 || odd.Steps["approve"].Pass != factory.PassPerson {
		t.Fatalf("an unreadable pass must be a person's: %+v %+v", odd.Problems, odd.Steps["approve"])
	}
	chat, _ := Parse("pipeline:\n  issue: [plan]\nsteps:\n  plan:\n    prompt: x\n    pass: manager\n")
	if len(chat.Problems) == 0 || chat.Steps["plan"].Pass != "" {
		t.Fatalf("pass on a chat: %+v", chat.Problems)
	}
}

func TestSetPassSaysWhoMayPassAnApproveStep(t *testing.T) {
	word := func(s string) *string { return &s }
	// A FIXED APPROVE STEP IS A PERSON'S: the edit is refused, not ignored.
	if _, err := ApplyEdits(editBase, []EditOp{{Op: OpSetPass, Name: "approve", Pass: word("manager")}}); err == nil || !strings.Contains(err.Error(), "fixed by the repository") {
		t.Fatalf("a fixed approve step was given to the manager: %v", err)
	}
	base := strings.Replace(editBase, "fixed: [approve, proof]", "fixed: [proof]", 1)
	got := mustApply(t, base, EditOp{Op: OpSetPass, Name: "approve", Pass: word("Manager when  the plan names one file")})
	back := reread(t, got.Text)
	if back.Steps["approve"].Pass != "manager when the plan names one file" || !got.SettingsEdited {
		t.Fatalf("%+v %+v", back.Steps["approve"], got.Settings)
	}
	found := false
	for _, s := range got.Settings {
		if s.Setting == SettingPass && s.After == "approve: manager when the plan names one file" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the preview does not say the pass changed: %+v", got.Settings)
	}
	for name, op := range map[string]EditOp{
		"a chat":        {Op: OpSetPass, Name: "write", Pass: word("person")},
		"a bad word":    {Op: OpSetPass, Name: "approve", Pass: word("anyone")},
		"no condition":  {Op: OpSetPass, Name: "approve", Pass: word("manager when")},
		"no pass":       {Op: OpSetPass, Name: "approve"},
		"not in a list": {Op: OpSetPass, Name: "approve2", Pass: word("person")},
	} {
		if _, err := ApplyEdits(base, []EditOp{op}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// A NEWLY ADDED APPROVE STEP IS A PERSON'S unless the edit says otherwise.
func TestANewlyAddedApproveStepIsOnlyAPersons(t *testing.T) {
	after, kind, prompt := "write", string(factory.StageGate), "read the change"
	got, err := ApplyEdits(editBase, []EditOp{{Op: OpAddStep, Name: "signoff", Pipeline: "issue", After: &after, Kind: &kind, Prompt: &prompt}})
	if err != nil {
		t.Fatal(err)
	}
	if back := reread(t, got.Text); back.Steps["signoff"].Pass != factory.PassPerson {
		t.Fatalf("a new approve step must be a person's: %+v\n%s", back.Steps["signoff"], got.Text)
	}
	manager := "manager"
	got, err = ApplyEdits(editBase, []EditOp{{Op: OpAddStep, Name: "signoff", Pipeline: "issue", After: &after, Kind: &kind, Prompt: &prompt, Pass: &manager}})
	if err != nil {
		t.Fatal(err)
	}
	if back := reread(t, got.Text); back.Steps["signoff"].Pass != factory.PassManager {
		t.Fatalf("an added approve step keeps the pass it was given: %+v", back.Steps["signoff"])
	}
}
