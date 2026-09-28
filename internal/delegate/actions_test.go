package delegate

import (
	"strings"
	"testing"
	"time"
)

// THE ACTION LOG KEEPS EVERY RECORD AS IT WAS RECEIVED, stamped with codeaf's
// own clock, in the order it arrived: a stage with its data, a step with its
// tool, step and exit, and the ending's status and sentence.
func TestTheActionLogKeepsEachRecordInTheOrderItArrived(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	exit := 1
	for _, action := range []Action{
		StageAction(at, StageRecord{Stage: "submit", Status: "frozen", Data: []byte(`{"patch_files":4}`)}),
		StepAction(at.Add(time.Second), StepRecord{Command: "bash: go test ./...", Observation: "FAIL", Tool: "bash", Step: "verify", Exit: &exit}),
		EndAction(at.Add(2*time.Second), Terminal{Status: StatusPass, Message: "submitted a change"}),
	} {
		if err := AppendAction(dir, action); err != nil {
			t.Fatal(err)
		}
	}
	actions, err := ReadActions(dir, 0)
	if err != nil || len(actions) != 3 {
		t.Fatalf("actions %+v, err %v", actions, err)
	}
	if a := actions[0]; a.Kind != ActionStage || a.Stage != "submit" || string(a.Data) != `{"patch_files":4}` || !a.At.Equal(at) {
		t.Fatalf("the stage read back as %+v", a)
	}
	if a := actions[1]; a.Kind != ActionStep || a.Tool != "bash" || a.Step != "verify" || a.Exit == nil || *a.Exit != 1 {
		t.Fatalf("the step read back as %+v", a)
	}
	if a := actions[2]; a.Kind != ActionEnd || a.Status != StatusPass || a.Message != "submitted a change" {
		t.Fatalf("the ending read back as %+v", a)
	}
	if last, _ := ReadActions(dir, 1); len(last) != 1 || last[0].Kind != ActionEnd {
		t.Fatalf("the last line = %+v", last)
	}
}

// A LINE IS WRITTEN CAPPED, and a log that is not there — a run from before the
// log existed — is no actions and no error.
func TestAnActionIsWrittenCappedAndAMissingLogIsEmpty(t *testing.T) {
	dir := t.TempDir()
	if actions, err := ReadActions(dir, 0); err != nil || len(actions) != 0 {
		t.Fatalf("a missing log read %v, %v", actions, err)
	}
	err := AppendAction(dir, Action{Kind: ActionStep, Command: "bash:\n" + strings.Repeat("x", 500),
		Observation: strings.Repeat("é", 3000), Data: []byte(`[1]`)})
	if err != nil {
		t.Fatal(err)
	}
	actions, _ := ReadActions(dir, 0)
	a := actions[0]
	if len(a.Command) > commandCap || strings.Contains(a.Command, "\n") || len(a.Observation) > turnTextCap || a.Data != nil {
		t.Fatalf("the line was written %d/%d bytes, data %s", len(a.Command), len(a.Observation), a.Data)
	}
}

// A PROGRAM WITH NO VOCABULARY IS READ PLAINLY: a stage as its name and
// status, a step as its command under its own step id with its exit said, and
// the ending as its sentence.
func TestAProgramWithNoVocabularyIsReadPlainly(t *testing.T) {
	plain := Delegate{Name: "fake"}
	at := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	exit := 2
	for _, tc := range []struct {
		action Action
		want   Shown
	}{
		{StageAction(at, StageRecord{Stage: "implement", Status: "running"}), Shown{At: at, Text: "implement · running"}},
		{StepAction(at, StepRecord{Command: "bash: go test", Step: "verify", Exit: &exit}), Shown{At: at, Step: "verify", Text: "bash: go test", Outcome: "fails · exit 2"}},
		{EndAction(at, Terminal{Status: StatusFail, Message: "it did not finish"}), Shown{At: at, Text: "it did not finish"}},
	} {
		got, ok := plain.Reader()(tc.action)
		if !ok || got != tc.want {
			t.Errorf("Show(%+v) = %+v, %v; want %+v", tc.action, got, ok, tc.want)
		}
	}
	zero := 0
	if ExitWord(&zero) != "passes" || ExitWord(nil) != "" {
		t.Fatalf("exit words = %q, %q", ExitWord(&zero), ExitWord(nil))
	}
	// A PROGRAM'S OWN VOCABULARY IS WHAT IT SAYS, and nothing with no words is
	// drawn.
	own := Delegate{Name: "fake", Present: func() ActionReader {
		return func(Action) (Shown, bool) { return Shown{Text: ""}, true }
	}}
	if _, ok := own.Reader()(StageAction(at, StageRecord{Stage: "x"})); ok {
		t.Fatal("an action with no words was shown")
	}
}
