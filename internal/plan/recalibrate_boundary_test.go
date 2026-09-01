package plan

import (
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/profile"
)

// The recalibration call's system message is the constant and nothing else, and
// it is pinned byte for byte. It is the prompt that rewrites the ruler every
// plan is then judged against, so a sentence added here moves every size
// judgment the harness will ever make, silently and everywhere.
func TestRecalibratePromptIsByteIdentical(t *testing.T) {
	want, err := os.ReadFile("testdata/recalibrate_prompt_baseline.golden")
	if err != nil {
		t.Fatal(err)
	}
	if recalibratePrompt != string(want) {
		t.Fatalf("the recalibrate prompt drifted from its pinned bytes:\n%s",
			diffLine(recalibratePrompt, string(want)))
	}
}

// The boundary evidence: what the worker said about its own fit for the work it
// was given, rendered where the model rewriting the ruler will read it. A
// profile with nothing to say renders nothing at all.
func TestBoundaryEvidenceRendersWhatTheWorkerNoticed(t *testing.T) {
	generalist := &profile.Profile{}
	generalist.Add(profile.Record{Title: "ordinary", Size: "atomic", Turns: 6, Tokens: 40_000})
	if got := boundaryEvidence(generalist); got != "" {
		t.Fatalf("a profile with nothing to say said:\n%s", got)
	}

	noticing := &profile.Profile{}
	noticing.Add(
		profile.Record{Title: "typo fix", Size: "atomic", Turns: 3, Tokens: 9_000,
			Calibration: []string{"far inside this leaf's envelope (root-cut: xs)"}},
		profile.Record{Title: "parser bug", Size: "atomic", Turns: 20, Tokens: 300_000,
			Calibration: []string{"at the very top of this leaf's envelope"}},
	)
	rendered := boundaryEvidence(noticing)
	for _, want := range []string{
		"root-cut: xs",
		"at the very top of this leaf's envelope",
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
