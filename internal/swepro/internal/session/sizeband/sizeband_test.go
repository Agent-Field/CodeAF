package sizeband

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// Translation of src/session/size-band.test.ts — verbatim describe/test names
// as Go subtest paths, same assertion semantics. estimateSizeBand never throws,
// so there is no TS-throw-to-Go-error mapping to make here.

func base() EstimateSizeBandInput {
	return EstimateSizeBandInput{Description: "", Tags: []string{}}
}

func fanIn(n float64) *float64 { return &n }

// repeatJoin mirrors `Array(n).fill(x).join(sep)`.
func repeatJoin(x string, n int, sep string) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = x
	}
	return strings.Join(parts, sep)
}

var bandIndexOrder = []string{"xs", "s", "m", "l", "xl"}

// bandIndex mirrors the tests' `["xs","s","m","l","xl"].indexOf(band)`,
// including the -1 for an unknown band.
func bandIndex(band SizeBand) int {
	for i, b := range bandIndexOrder {
		if b == string(band) {
			return i
		}
	}
	return -1
}

func expectBand(t *testing.T, got, want SizeBand) {
	t.Helper()
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEstimateSizeBandExplicitTagDominance(t *testing.T) {
	t.Run("each scope tag maps to its band, regardless of other signals", func(t *testing.T) {
		bigDescription := strings.Repeat("x", 5000) + strings.Repeat("\n", 60) +
			repeatJoin("- [ ] item", 20, "\n")
		expectBand(t, EstimateSizeBand(EstimateSizeBandInput{Description: bigDescription, Tags: []string{"scope:tiny"}, DependencyFanIn: fanIn(50)}), "xs")
		expectBand(t, EstimateSizeBand(EstimateSizeBandInput{Description: bigDescription, Tags: []string{"scope:small"}, DependencyFanIn: fanIn(50)}), "s")
		expectBand(t, EstimateSizeBand(EstimateSizeBandInput{Description: bigDescription, Tags: []string{"scope:medium"}, DependencyFanIn: fanIn(50)}), "m")
		expectBand(t, EstimateSizeBand(EstimateSizeBandInput{Description: bigDescription, Tags: []string{"scope:large"}, DependencyFanIn: fanIn(50)}), "l")
	})

	t.Run("scope:trivial is accepted as a compat alias for xs", func(t *testing.T) {
		in := base()
		in.Tags = []string{"scope:trivial"}
		expectBand(t, EstimateSizeBand(in), "xs")
	})

	t.Run("is case-insensitive and tolerates surrounding tags", func(t *testing.T) {
		in := base()
		in.Tags = []string{"agent:fixer", "scope:LARGE", "risk:high"}
		expectBand(t, EstimateSizeBand(in), "l")
	})

	t.Run("first recognized scope tag wins", func(t *testing.T) {
		in := base()
		in.Tags = []string{"scope:small", "scope:large"}
		expectBand(t, EstimateSizeBand(in), "s")
	})

	t.Run("unknown/malformed scope tag falls through to static scoring, not left as scope", func(t *testing.T) {
		// No other signal present either -> hits the no-signal default, same as
		// a genuinely absent scope tag would.
		in := base()
		in.Tags = []string{"scope:gigantic"}
		expectBand(t, EstimateSizeBand(in), "m")
		in.Tags = []string{"scope:"}
		expectBand(t, EstimateSizeBand(in), "m")
	})
}

func TestEstimateSizeBandNoSignalDefault(t *testing.T) {
	t.Run("empty description, no tags, no fan-in defaults to m", func(t *testing.T) {
		expectBand(t, EstimateSizeBand(EstimateSizeBandInput{Description: "", Tags: []string{}}), "m")
		expectBand(t, EstimateSizeBand(EstimateSizeBandInput{Description: "", Tags: []string{}, DependencyFanIn: nil}), "m")
		expectBand(t, EstimateSizeBand(EstimateSizeBandInput{Description: "", Tags: []string{"agent:fixer"}}), "m")
	})

	t.Run("dependencyFanIn: 0 counts as no signal too", func(t *testing.T) {
		expectBand(t, EstimateSizeBand(EstimateSizeBandInput{Description: "", Tags: []string{}, DependencyFanIn: fanIn(0)}), "m")
	})
}

func TestEstimateSizeBandStaticScoringFromRealSignals(t *testing.T) {
	t.Run("a short, real description with no other signal scores below the no-signal default", func(t *testing.T) {
		expectBand(t, EstimateSizeBand(EstimateSizeBandInput{Description: "Fix a typo in the README.", Tags: []string{}}), "xs")
	})

	t.Run("a longer description with a few acceptance criteria and one file lands at m", func(t *testing.T) {
		description := strings.Join([]string{
			"Refactor the retry loop in the client so it backs off exponentially.",
			"",
			"file_scope: src/client/retry.ts",
			"",
			"- [ ] backoff doubles each attempt",
			"- [ ] capped at 30s",
			"- [ ] unit tests cover the cap",
		}, "\n")
		expectBand(t, EstimateSizeBand(EstimateSizeBandInput{Description: description, Tags: []string{}}), "m")
	})

	t.Run("a large multi-file spec with many criteria and high fan-in lands at xl", func(t *testing.T) {
		prose := strings.Repeat("This task touches the scheduler, the router, and the merge coordinator. ", 30)
		description := strings.Join([]string{
			prose,
			"",
			"file_scope: src/session/plandb-scheduler.ts, src/router/state.ts, src/session/merge-coordinator.ts, src/session/leaf-merger.ts, src/session/review-gate.ts, src/tool/task.ts, src/plandb/types.ts, src/session/replan-gate.ts",
			"",
			repeatJoin("- [ ] criterion", 10, "\n"),
		}, "\n")
		expectBand(t, EstimateSizeBand(EstimateSizeBandInput{Description: description, Tags: []string{}, DependencyFanIn: fanIn(12)}), "xl")
	})
}

func TestEstimateSizeBandMonotonicity(t *testing.T) {
	t.Run("growing description length alone never decreases the band", func(t *testing.T) {
		lengths := []int{10, 100, 300, 800, 2000}
		prev := -1
		for _, l := range lengths {
			band := EstimateSizeBand(EstimateSizeBandInput{Description: strings.Repeat("a", l), Tags: []string{}})
			idx := bandIndex(band)
			if idx < prev {
				t.Fatalf("len=%d: idx %d < prev %d", l, idx, prev)
			}
			prev = idx
		}
	})

	t.Run("growing acceptance criteria count alone never decreases the band", func(t *testing.T) {
		base := "A real task description that is long enough to carry some criteria below it in detail."
		prev := -1
		for _, n := range []int{0, 1, 3, 6, 10} {
			description := base + "\n" + repeatJoin("- [ ] criterion", n, "\n")
			idx := bandIndex(EstimateSizeBand(EstimateSizeBandInput{Description: description, Tags: []string{}}))
			if idx < prev {
				t.Fatalf("n=%d: idx %d < prev %d", n, idx, prev)
			}
			prev = idx
		}
	})

	t.Run("growing file_scope width alone never decreases the band", func(t *testing.T) {
		files := []string{
			"src/a.ts",
			"src/a.ts, src/b.ts",
			"src/a.ts, src/b.ts, src/c.ts, src/d.ts",
			"src/a.ts, src/b.ts, src/c.ts, src/d.ts, src/e.ts, src/f.ts, src/g.ts, src/h.ts",
		}
		prev := -1
		for _, scope := range files {
			description := fmt.Sprintf("Some real work description here.\n\nfile_scope: %s", scope)
			idx := bandIndex(EstimateSizeBand(EstimateSizeBandInput{Description: description, Tags: []string{}}))
			if idx < prev {
				t.Fatalf("scope=%q: idx %d < prev %d", scope, idx, prev)
			}
			prev = idx
		}
	})

	t.Run("growing dependencyFanIn alone never decreases the band", func(t *testing.T) {
		description := "Some real work description here that stands on its own."
		prev := -1
		for _, f := range []float64{0, 1, 3, 6, 12} {
			idx := bandIndex(EstimateSizeBand(EstimateSizeBandInput{Description: description, Tags: []string{}, DependencyFanIn: fanIn(f)}))
			if idx < prev {
				t.Fatalf("fanIn=%v: idx %d < prev %d", f, idx, prev)
			}
			prev = idx
		}
	})
}

func TestEstimateSizeBandStabilityUnderSmallJitter(t *testing.T) {
	// `core.slice(0, Math.floor(core.length * f))` — the cores below are pure
	// ASCII, so a UTF-16 slice and a byte slice coincide.
	sliceTo := func(s string, f float64) string {
		return s[:int(math.Floor(float64(len(s))*f))]
	}

	t.Run("±10% description length jitter around a mid-size description gives the same band", func(t *testing.T) {
		core := strings.Repeat("Update the config loader to support a new environment override. ", 4)
		want := EstimateSizeBand(EstimateSizeBandInput{Description: core, Tags: []string{}})
		shorter := EstimateSizeBand(EstimateSizeBandInput{Description: sliceTo(core, 0.9), Tags: []string{}})
		longer := EstimateSizeBand(EstimateSizeBandInput{Description: core + sliceTo(core, 0.1), Tags: []string{}})
		expectBand(t, shorter, want)
		expectBand(t, longer, want)
	})

	t.Run("±10% jitter around a large description gives the same band", func(t *testing.T) {
		core := strings.Repeat("This change spans several modules and needs careful sequencing. ", 40)
		want := EstimateSizeBand(EstimateSizeBandInput{Description: core, Tags: []string{}})
		shorter := EstimateSizeBand(EstimateSizeBandInput{Description: sliceTo(core, 0.9), Tags: []string{}})
		longer := EstimateSizeBand(EstimateSizeBandInput{Description: core + sliceTo(core, 0.1), Tags: []string{}})
		expectBand(t, shorter, want)
		expectBand(t, longer, want)
	})

	t.Run("adding one extra acceptance-criterion line to a list of five doesn't flip the band", func(t *testing.T) {
		description := "A task with several concrete acceptance criteria to verify.\n"
		five := description + repeatJoin("- [ ] criterion", 5, "\n")
		six := description + repeatJoin("- [ ] criterion", 6, "\n")
		expectBand(t,
			EstimateSizeBand(EstimateSizeBandInput{Description: six, Tags: []string{}}),
			EstimateSizeBand(EstimateSizeBandInput{Description: five, Tags: []string{}}))
	})
}

// ---------------------------------------------------------------------------
// P4: prose-spec size signals (clause density + section count).

// proseSpec builds a realistic multi-subsystem prose spec with none of the
// machine-readable markers (checkboxes/file_scope) the legacy signals key on.
func proseSpec(sectionCount int) string {
	parts := make([]string, sectionCount)
	for i := range parts {
		s := fmt.Sprintf("Subsystem%d", i+1)
		parts[i] = fmt.Sprintf("## %s\n\nThe %s must handle the primary input path. It should validate all inputs and ", s, s) +
			"reject malformed data. The component shall support incremental operation and must preserve " +
			fmt.Sprintf("ordering. Values that overflow are normalized. The `%s` interface accepts a ", strings.ToLower(s)) +
			"config. If the input is empty it falls back to the default. Errors combine into a single report."
	}
	return strings.Join(parts, "\n\n")
}

func TestEstimateSizeBandP4ProseSpecSignals(t *testing.T) {
	t.Run("a large multi-section, many-clause prose spec scores l (decomposes)", func(t *testing.T) {
		spec := proseSpec(12)
		// No checkboxes, no file_scope — the pre-P4 estimator capped this at "m".
		if strings.Contains(spec, "- [ ]") {
			t.Fatalf("spec unexpectedly contains a checkbox")
		}
		if strings.Contains(spec, "file_scope:") {
			t.Fatalf("spec unexpectedly contains file_scope:")
		}
		expectBand(t, EstimateSizeBand(EstimateSizeBandInput{Description: spec, Tags: []string{}}), "l")
	})

	t.Run("a very large, many-subsystem prose spec scores xl", func(t *testing.T) {
		expectBand(t, EstimateSizeBand(EstimateSizeBandInput{Description: proseSpec(18), Tags: []string{}}), "xl")
	})

	t.Run("large prose band feeds the gate: bandWithinReliable(l, l cold-start) no longer root-cuts via shouldRootCut", func(t *testing.T) {
		got := bandIndex(EstimateSizeBand(EstimateSizeBandInput{Description: proseSpec(12), Tags: []string{}}))
		if got < bandIndex("l") {
			t.Fatalf("band index %d < %d", got, bandIndex("l"))
		}
	})
}

func TestEstimateSizeBandP4KeepsSmallMediumByteIdentical(t *testing.T) {
	t.Run("a short prose task with no markers still scores below the no-signal default", func(t *testing.T) {
		small := "Fix the retry backoff in the client so it doubles each attempt and caps at 30 seconds."
		expectBand(t, EstimateSizeBand(EstimateSizeBandInput{Description: small, Tags: []string{}}), "xs")
	})

	t.Run("a modest multi-sentence prose paragraph stays s/m, not l", func(t *testing.T) {
		med := "Refactor the config loader to support environment overrides. It should read from a file, " +
			"then apply env vars on top, and validate the merged result. Add a helper to resolve nested keys."
		got := bandIndex(EstimateSizeBand(EstimateSizeBandInput{Description: med, Tags: []string{}}))
		if got >= bandIndex("l") {
			t.Fatalf("band index %d >= %d", got, bandIndex("l"))
		}
	})

	t.Run("a mid-size 6-section spec stays m (conservative — does not over-decompose)", func(t *testing.T) {
		parts := make([]string, 6)
		for i := range parts {
			s := fmt.Sprintf("Subsystem%d", i+1)
			parts[i] = fmt.Sprintf("## %s\n\nThe %s must handle input. It should validate and reject bad data. It shall preserve order.", s, s)
		}
		expectBand(t, EstimateSizeBand(EstimateSizeBandInput{Description: strings.Join(parts, "\n\n"), Tags: []string{}}), "m")
	})

	t.Run("double-count guard: a checkbox-heavy task is NOT re-scored as prose clauses", func(t *testing.T) {
		// 12 checkboxes already feed the acceptance signal; countSpecClauses
		// also counts them as bullets. Without the subtraction guard this would
		// double to band l. It must stay m.
		description := "Implement the following checklist items:\n" + repeatJoin("- [ ] criterion", 12, "\n")
		expectBand(t, EstimateSizeBand(EstimateSizeBandInput{Description: description, Tags: []string{}}), "m")
	})
}

func TestEstimateSizeBandP4ThresholdBoundaries(t *testing.T) {
	t.Run("section count just below the first section bucket adds nothing", func(t *testing.T) {
		// 4 `##` sections (< SECTIONS_SEVERAL=5) with terse prose stays small.
		parts := make([]string, 4)
		for i := range parts {
			parts[i] = fmt.Sprintf("## S%d\n\nTerse note here.", i+1)
		}
		got := bandIndex(EstimateSizeBand(EstimateSizeBandInput{Description: strings.Join(parts, "\n\n"), Tags: []string{}}))
		if got >= bandIndex("l") {
			t.Fatalf("band index %d >= %d", got, bandIndex("l"))
		}
	})

	t.Run("the prose-clause signal is monotone: more subsystems never lowers the band", func(t *testing.T) {
		prev := -1
		for _, n := range []int{1, 3, 6, 9, 12, 18} {
			cur := bandIndex(EstimateSizeBand(EstimateSizeBandInput{Description: proseSpec(n), Tags: []string{}}))
			if cur < prev {
				t.Fatalf("n=%d: idx %d < prev %d", n, cur, prev)
			}
			prev = cur
		}
	})
}
