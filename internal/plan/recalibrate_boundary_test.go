package plan

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/profile"
)

// The additive law where a prompt is built: with no specialist registered, the
// system message of a recalibration call is the constant and nothing else. A
// byte added here would be a byte added to every process that has never had a
// second worker.
func TestRecalibratePromptIsByteIdenticalWithoutASpecialist(t *testing.T) {
	if got := specialistPreamble(PurposeFor(LinearSubharness)); got != "" {
		t.Fatalf("the generalist's ruler is rewritten with a specialist preamble:\n%s", got)
	}
	if got := specialistPreamble(PurposeFor("")); got != "" {
		t.Fatalf("an unnamed worker grew a preamble:\n%s", got)
	}
	if got := specialistPreamble(PurposeFor("nobody-registered-this")); got != "" {
		t.Fatalf("an unregistered worker grew a preamble:\n%s", got)
	}
}

// And what a specialist's ruler is rewritten with: its own purpose, so the model
// knows which worker's capacity it is describing. Without it the prompt's own
// second paragraph — "that agent works alone and in order, with tools" — is the
// only description in the call, and it describes the wrong worker.
func TestARewrittenSpecialistRulerIsToldWhatTheWorkerIsFor(t *testing.T) {
	defer ForgetSubharnesses()
	UseSubharness(Subharness{Name: "swe", Purpose: "an end-to-end software-engineering pipeline"}, "swe ruler")

	if got := PurposeFor("swe"); got != "an end-to-end software-engineering pipeline" {
		t.Fatalf("purpose = %q", got)
	}
	preamble := specialistPreamble(PurposeFor("swe"))
	if !strings.Contains(preamble, "an end-to-end software-engineering pipeline") {
		t.Fatalf("the worker's purpose is not in its own recalibration prompt:\n%s", preamble)
	}
	if !strings.Contains(preamble, "not the default one") {
		t.Fatalf("the prompt never says this is a specialist:\n%s", preamble)
	}
	// The ruler that is rewritten is that worker's own, seated from its prior.
	if got := AnchorsFor("swe"); got != "swe ruler" {
		t.Fatalf("anchors in force for swe = %q", got)
	}
	if AnchorsFor(LinearSubharness) == "swe ruler" {
		t.Fatal("registering a specialist replaced the generalist's ruler")
	}
}

// The boundary evidence: what a worker said about its own fit, and who tried
// the work before it, rendered where the model rewriting the ruler will read
// it. A generalist profile carries none of either and renders nothing at all.
func TestBoundaryEvidenceRendersWhatTheWorkerNoticed(t *testing.T) {
	generalist := &profile.Profile{}
	generalist.Add(profile.Record{Title: "ordinary", Size: "atomic", Turns: 6, Tokens: 40_000})
	if got := boundaryEvidence(generalist); got != "" {
		t.Fatalf("a profile with nothing to say said:\n%s", got)
	}

	specialist := &profile.Profile{}
	specialist.Add(
		profile.Record{Title: "typo fix", Size: "atomic", Turns: 3, Tokens: 9_000,
			Calibration: []string{"the engine judged this goal small enough to run whole (root-cut: xs)"}},
		profile.Record{Title: "parser bug", Size: "atomic", Turns: 20, Tokens: 300_000,
			EscalatedFrom: "linear"},
	)
	rendered := boundaryEvidence(specialist)
	for _, want := range []string{
		"root-cut: xs",
		"the linear worker tried this first and could not finish it",
		"TOO SMALL example is set too low", // the preamble teaching how to read the notes
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("boundary evidence is missing %q:\n%s", want, rendered)
		}
	}

	// The same notes travel with a record picked into one of the three bands,
	// so a reader never sees the cost without the observation beside it.
	var banded strings.Builder
	appendCalibrationEvidence(&banded, "finished quickly", profile.Record{
		Title: "typo fix", Turns: 3, Tokens: 9_000,
		Calibration: []string{"far inside this worker's envelope"},
	})
	if !strings.Contains(banded.String(), "far inside this worker's envelope") {
		t.Fatalf("a banded record dropped its own note:\n%s", banded.String())
	}
}

// The pick itself: newest first, bounded, and only records that say something.
func TestBoundaryEvidencePicksTheNewestObservations(t *testing.T) {
	measured := &profile.Profile{}
	measured.Add(profile.Record{Title: "quiet", Size: "atomic", Turns: 5, Tokens: 10_000})
	for index := 0; index < boundaryEvidenceCount+3; index++ {
		measured.Add(profile.Record{
			Title: "noted", Size: "atomic", Turns: 5, Tokens: 10_000,
			Calibration: []string{"note " + string(rune('a'+index))},
		})
	}
	picked := measured.BoundaryEvidence(boundaryEvidenceCount)
	if len(picked) != boundaryEvidenceCount {
		t.Fatalf("picked %d, want %d", len(picked), boundaryEvidenceCount)
	}
	last := boundaryEvidenceCount + 2
	if picked[0].Calibration[0] != "note "+string(rune('a'+last)) {
		t.Fatalf("the newest observation is not first: %#v", picked[0].Calibration)
	}
	for _, record := range picked {
		if !record.Boundary() {
			t.Fatalf("a record with nothing to say was picked: %#v", record)
		}
	}
}
