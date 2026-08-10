package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/revision"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// seedGeneralist writes n ordinary leaves at one cost, so the median the
// boundary check reads is a known number.
func seedGeneralist(t *testing.T, settings config.Config, cost float64) {
	t.Helper()
	measured, err := profile.Load(settings.ProfileDir, settings.Model, exec.LinearSubharness)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < profile.MinSamples; index++ {
		measured.Add(profile.Record{Title: "ordinary leaf", Size: "atomic",
			Turns: 6, Tokens: 40_000, Cost: cost, Verdict: provider.VerdictVerifiedSuccess})
	}
	if err := measured.Save(); err != nil {
		t.Fatal(err)
	}
}

// The boundary-too-low half of the evidence, and the only half no single worker
// can produce: a specialist run that came in under the generalist's median leaf
// is a specialist that was reached for when the ordinary worker would have done.
//
// The comparison is written against "any worker that is not the baseline",
// because that is what it actually is. Nothing here may ask which specialist it
// is looking at.
func TestARecordUnderTheGeneralistsMedianSaysTheBoundaryMaySitTooHigh(t *testing.T) {
	settings := config.Config{ProfileDir: t.TempDir(), Model: "worker/model"}
	seedGeneralist(t, settings, 0.50)

	cheap := profile.Record{Title: "typo fix", Size: "atomic", Turns: 3, Tokens: 9_000, Cost: 0.02}
	noted := withBoundaryEvidence(settings, settings.Model, "swe", cheap)
	if len(noted.Calibration) != 1 {
		t.Fatalf("a specialist run under the median said nothing: %#v", noted.Calibration)
	}
	if !strings.Contains(noted.Calibration[0], "the boundary may sit too high") {
		t.Fatalf("the note does not say what it is evidence of: %q", noted.Calibration[0])
	}

	// A specialist run that cost more than the median is ordinary and silent.
	dear := profile.Record{Title: "parser bug", Size: "atomic", Turns: 30, Tokens: 400_000, Cost: 4.10}
	if got := withBoundaryEvidence(settings, settings.Model, "swe", dear); len(got.Calibration) != 0 {
		t.Fatalf("an expensive specialist run was flagged as cheap: %#v", got.Calibration)
	}

	// The generalist is never compared against itself: that is the baseline the
	// whole comparison is made of, and a note on it would be a ruler calibrating
	// against its own median.
	if got := withBoundaryEvidence(settings, settings.Model, exec.LinearSubharness, cheap); len(got.Calibration) != 0 {
		t.Fatalf("the generalist was measured against itself: %#v", got.Calibration)
	}

	// And with no measured generalist there is no median, so there is nothing
	// honest to say — the first specialist run in a fresh install stays quiet.
	fresh := config.Config{ProfileDir: t.TempDir(), Model: "worker/model"}
	if got := withBoundaryEvidence(fresh, fresh.Model, "swe", cheap); len(got.Calibration) != 0 {
		t.Fatalf("a comparison was made against nothing: %#v", got.Calibration)
	}
}

// The whole of signal two, end to end on the path a compiler-chosen leaf takes:
// what the worker said about its own fit, the marker that says it got the work
// because the generalist could not, and the cross-worker comparison — all three
// in one record, in the file that belongs to the worker that ran it.
func TestCalibrationReachesTheProfileRecordOfTheWorkerThatRanIt(t *testing.T) {
	defer exec.ForgetSubharnesses()
	exec.RegisterSubharness(exec.SubharnessInfo{Name: "swe", Purpose: "software engineering taken whole"})
	settings := config.Config{ProfileDir: t.TempDir(), Model: "worker/model"}
	seedGeneralist(t, settings, 0.50)

	node := store.Node{ID: "leaf", Title: "fix the parser", Brief: "fix it", Subharness: "swe"}
	outcome := &exec.Outcome{
		Turns: 4, Stop: exec.StopDone, Verdict: provider.VerdictVerifiedSuccess,
		Calibration: []string{"the engine judged this goal small enough to run whole (root-cut: xs)"},
	}
	outcome.Usage.PromptTokens = 8_000
	outcome.Usage.Cost = 0.03

	record, ok := recordSingleLeaf(settings, settings.Model, node, outcome, exec.LinearSubharness)
	if !ok {
		t.Fatal("the specialist's leaf was not recorded")
	}
	if record.EscalatedFrom != exec.LinearSubharness {
		t.Fatalf("escalated-from = %q — the record does not say the generalist tried first", record.EscalatedFrom)
	}
	if record.Cost != 0.03 {
		t.Fatalf("cost = %v — a record priced at zero can never be compared with anything", record.Cost)
	}
	joined := strings.Join(record.Calibration, "\n")
	if !strings.Contains(joined, "root-cut: xs") {
		t.Fatalf("the worker's own note did not survive: %#v", record.Calibration)
	}
	if !strings.Contains(joined, "the boundary may sit too high") {
		t.Fatalf("the cross-worker comparison was not made: %#v", record.Calibration)
	}
	if !record.Boundary() {
		t.Fatal("a record carrying both kinds of evidence does not read as boundary evidence")
	}

	// And it landed in the specialist's file, not the generalist's.
	specialist, err := profile.Load(settings.ProfileDir, settings.Model, "swe")
	if err != nil || len(specialist.Records) != 1 {
		t.Fatalf("the specialist's profile holds %d records (%v)", len(specialist.Records), err)
	}
	generalist, err := profile.Load(settings.ProfileDir, settings.Model, exec.LinearSubharness)
	if err != nil || len(generalist.Records) != profile.MinSamples {
		t.Fatalf("the generalist's profile grew a specialist's leaf: %d records (%v)", len(generalist.Records), err)
	}
}

// A judgement may only name a worker this build can construct. Everything else
// — a hallucinated name, a malformed reply, silence — is the default worker,
// which is the same degradation the compile path makes on the same question.
func TestOnlyARegisteredWorkerSurvivesAJudgementsReply(t *testing.T) {
	defer exec.ForgetSubharnesses()
	exec.RegisterSubharness(exec.SubharnessInfo{Name: "swe", Purpose: "software engineering taken whole"})
	for _, testCase := range []struct{ reply, want string }{
		{`{"worker":"swe"}`, "swe"},
		{"here you go:\n{\"worker\": \"swe\"}\nthanks", "swe"},
		{`{"worker":"reviewer"}`, ""},
		{`{"worker":""}`, ""},
		{`{"worker":"linear"}`, ""},
		{`{"done":true}`, ""},
		{"not json at all", ""},
		{"", ""},
	} {
		if got := revision.DecodeWorkerChoice(testCase.reply); got != testCase.want {
			t.Fatalf("revision.DecodeWorkerChoice(%q) = %q, want %q", testCase.reply, got, testCase.want)
		}
	}
}

// The additive law at the two judgements that may now name a worker: with no
// specialist registered the menu is empty, the brief is empty, and the prompt
// those judges send is the prompt they have always sent.
func TestAJudgementGetsAMenuOnlyWhenThereIsOne(t *testing.T) {
	if got := revision.WorkerChoiceBrief(exec.MenuTextExcept(exec.LinearSubharness)); got != "" {
		t.Fatalf("a baseline build hands its judges a menu:\n%s", got)
	}
	defer exec.ForgetSubharnesses()
	exec.RegisterSubharness(exec.SubharnessInfo{Name: "swe", Purpose: "software engineering taken whole"})

	brief := revision.WorkerChoiceBrief(exec.MenuTextExcept(exec.LinearSubharness))
	if !strings.Contains(brief, "swe — software engineering taken whole") {
		t.Fatalf("the judge was not shown the specialist:\n%s", brief)
	}
	if !strings.Contains(brief, `"worker":"<name>"`) {
		t.Fatalf("the judge was not told how to answer:\n%s", brief)
	}
	// No self-rung: the specialist's own failed leaf is handed nothing.
	if got := revision.WorkerChoiceBrief(exec.MenuTextExcept("swe")); got != "" {
		t.Fatalf("a failed swe leaf was offered swe again:\n%s", got)
	}
	// And with no menu the judge is never called at all.
	if got := revision.JudgeRetryWorker(nil, config.Config{}, nil, store.Node{}, exec.Task{},
		&exec.Outcome{}, nil, "", ""); got != "" {
		t.Fatalf("a judge with no menu answered %q", got)
	}
}

// A floating alias is a real model id everywhere in aforge and is not one in the
// catalog a separate engine prices its calls from. Resolving it here is what
// kept a live run from dying at startup with $0 spent; passing an unknown id
// through untouched is what keeps a model nobody chose out of the pools.
func TestTheEngineIsHandedAConcreteModelId(t *testing.T) {
	client := &http.Client{Transport: voiceRoundTripFunc(func(*http.Request) (*http.Response, error) {
		payload := `{"data":[
			{"id":"~vendor/model-latest","canonical_slug":"vendor/model-2026-08-01",
			 "architecture":{"input_modalities":["text"],"output_modalities":["text"]}},
			{"id":"vendor/model-2026-08-01",
			 "architecture":{"input_modalities":["text"],"output_modalities":["text"]}},
			{"id":"vendor/plain",
			 "architecture":{"input_modalities":["text"],"output_modalities":["text"]}}
		]}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(payload))}, nil
	})}
	models := catalog.Load(context.Background(), catalog.Options{
		BaseURL: "https://example.invalid/api/v1", Dir: t.TempDir(), HTTPClient: client,
	})
	for _, testCase := range []struct{ asked, want string }{
		{"~vendor/model-latest", "vendor/model-2026-08-01"},
		{"vendor/model-latest", "vendor/model-2026-08-01"},
		{"vendor/plain", "vendor/plain"},
		{"vendor/nobody-has-heard-of-this", "vendor/nobody-has-heard-of-this"},
		{"", ""},
	} {
		if got := engineModelID(models, testCase.asked); got != testCase.want {
			t.Fatalf("engineModelID(%q) = %q, want %q", testCase.asked, got, testCase.want)
		}
	}
	// No catalog is no resolution, never a refusal and never a substitution.
	if got := engineModelID(nil, "~vendor/model-latest"); got != "~vendor/model-latest" {
		t.Fatalf("without a catalog the id was rewritten to %q", got)
	}
}
