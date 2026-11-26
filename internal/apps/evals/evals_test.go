package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCaseOracleDoesNotTrustTheBuilder(t *testing.T) {
	cases, err := Load(strings.NewReader(`
- name: sample
  prompt: Make a release tracker.
  max_rounds: 2
  steps:
    - method: app.get
      expect:
        - path: /app/records/release
          exists: true
`))
	if err != nil || len(cases) != 1 {
		t.Fatalf("load cases = %v, %v", cases, err)
	}
	fake := &fakeApp{}
	got := Run(t.Context(), fake, cases[0])
	if got.Pass || len(got.Failures) != 1 || !strings.Contains(got.Failures[0], "/app/records/release") {
		t.Fatalf("the missing record was not reported: %+v", got)
	}
	if fake.prompt != "Make a release tracker." {
		t.Fatalf("builder saw %q", fake.prompt)
	}
	fake.reply = `{"app":{"records":{"release":{}}}}`
	got = Run(t.Context(), fake, cases[0])
	if !got.Pass {
		t.Fatalf("passing case: %+v", got)
	}
	var report, table bytes.Buffer
	if err := WriteReport(&report, &table, []Result{got}); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(report.Bytes()) || !strings.Contains(report.String(), `"tokens_in": 31`) ||
		!strings.Contains(table.String(), "sample") || !strings.Contains(table.String(), "PASS") {
		t.Fatalf("report = %s, table = %s", &report, &table)
	}
}

type fakeApp struct {
	prompt string
	reply  string
	rounds int
}

func (f *fakeApp) Build(_ context.Context, prompt string, noModel bool, _ func(BuildState) error) (BuildState, error) {
	f.prompt = prompt
	state := BuildState{App: "app-id", Rounds: 2, Checked: true}
	if f.rounds > 0 {
		state.Rounds = f.rounds
	}

	return state, nil
}

func (f *fakeApp) Call(_ context.Context, _ string, _ any) (json.RawMessage, error) {
	if f.reply == "" {
		return json.RawMessage(`{"app":{"records":{}},"builder_says":"passed"}`), nil
	}
	return json.RawMessage(f.reply), nil
}

func (f *fakeApp) Usage(context.Context) (Usage, error) {
	price := 0.01
	return Usage{Calls: 1, TokensIn: 31, TokensOut: 18, Cost: &price}, nil
}

func TestBuiltinCasesHaveIndependentChecksAndDataFixtures(t *testing.T) {
	cases, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"founder-crm", "hiring-reuses-contacts", "personal-finance-csv", "release-tracker", "change-and-revert", "deterministic-no-model", "marketing-desk"}
	if len(cases) != len(want) {
		t.Fatalf("got %d cases, want %d", len(cases), len(want))
	}
	for i, c := range cases {
		if c.Name != want[i] || c.Record.Type == "" || len(c.Record.Value) == 0 || len(c.Record.Edit) == 0 || len(c.Looks) == 0 {
			t.Errorf("incomplete platform case: %+v", c)
		}
	}
}

func TestNoModelCaseRejectsARecordedCall(t *testing.T) {
	fake := &fakeApp{reply: `{}`}
	got := Run(t.Context(), fake, Case{Name: "no-model", NoModel: true, MaxRounds: 2})
	if got.Pass || !strings.Contains(strings.Join(got.Failures, " "), "deterministic app called a model") {
		t.Fatalf("deterministic case did not reject the recorded call: %+v", got)
	}
}

func TestUndoRequiresTheTypedRefusal(t *testing.T) {
	probe := Step{Method: "data.undo", Error: "lifecycle"}
	for _, tc := range []struct {
		err  error
		pass bool
	}{
		{nil, false},
		{errors.New("lifecycle"), false},
		{CodeError("unknown_method"), false},
		{CodeError("lifecycle"), true},
	} {
		err := runSteps(t.Context(), &refusalApp{err: tc.err}, []Step{probe}, nil)
		if (err == nil) != tc.pass {
			t.Errorf("refusal %v: got %v, expected pass=%t", tc.err, err, tc.pass)
		}
	}
}

type refusalApp struct {
	fakeApp
	err error
}

func (f refusalApp) Call(context.Context, string, any) (json.RawMessage, error) {
	return []byte(`{}`), f.err
}

func TestLoadRejectsAmbiguousAssertion(t *testing.T) {
	_, err := Load(strings.NewReader(`
- name: bad-oracle
  prompt: Make a release tracker.
  max_rounds: 2
  steps:
    - method: app.get
      expect:
        - {path: /app, exists: false, equals: true}
`))
	if err == nil || !strings.Contains(err.Error(), "exactly one assertion") {
		t.Fatalf("got %v for an ambiguous assertion", err)
	}
}

func TestHiringRequiresAContactReuseOffer(t *testing.T) {
	cases, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if c.Name != "hiring-reuses-contacts" {
			continue
		}
		if problems := checkReply(json.RawMessage(`{"offers":[]}`), c.Plan); len(problems) == 0 {
			t.Fatal("the hiring oracle accepted a plan without a contact reuse offer")
		}
		if problems := checkReply(json.RawMessage(`{"offers":[{"type":"contact","count":1}]}`), c.Plan); len(problems) != 0 {
			t.Fatalf("the hiring oracle rejected its contact offer: %v", problems)
		}
		return
	}
	t.Fatal("the named hiring eval is missing")
}

func TestFinanceMustFinishWithinItsRoundBudget(t *testing.T) {
	cases, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if c.Name != "personal-finance-csv" {
			continue
		}
		if c.MaxRounds != 2 {
			t.Fatalf("finance Check budget is %d rounds, want 2", c.MaxRounds)
		}
		got := Run(t.Context(), &fakeApp{rounds: c.MaxRounds + 1}, c)
		if !strings.Contains(strings.Join(got.Failures, " "), "exceeded budget") {
			t.Fatalf("the finance oracle accepted too many Check rounds: %+v", got)
		}
		return
	}
	t.Fatal("the named finance eval is missing")
}
