package verify

// A suite that failed to collect, read the way ofetch's nemotron n1 run read it.

import (
	"strings"
	"testing"
)

// vitestCollectionFailure is what `pnpm exec vitest run --coverage
// --reporter=json` printed on that run: no JSON at all, because the runner never
// got as far as running a check. Every line of it is prose about an import that
// would not resolve.
const vitestCollectionFailure = ` RUN  v0.34.6 /app

Failed to load url ./circuit-breaker (resolved id: ./circuit-breaker) in /app/test/index.test.ts. Does the file exist?

 FAIL  test/index.test.ts [ test/index.test.ts ]
Error: Failed to load url ./circuit-breaker (resolved id: ./circuit-breaker)

 Test Files  1 failed (1)
      Tests  no tests
`

// A NAME COMES FROM THE RUNNER'S OWN TEST-RECORD GRAMMAR, NEVER FROM A SENTENCE.
//
// ofetch's nemotron n1 gate on task-2 reads `This work broke checks that were
// passing before it: to.` — a check called `to`, subtracted against a baseline
// that had named 28. It came out of the dotnet/xunit line `^\s*(?:Failed|X)\s+
// (\S+)\s` meeting the English sentence `Failed to load url ./circuit-breaker`.
// After an English word, `\S+` matches an English word.
func TestAnErrorSentenceIsNotACheckName(t *testing.T) {
	if named := ReportedTests(vitestCollectionFailure); len(named) > 0 {
		t.Errorf("a suite that ran no check named %v", named)
	}
	if red := FailingTests(vitestCollectionFailure); len(red) > 0 {
		t.Errorf("a sentence about a missing import was read as %v failing checks: %v",
			len(red), red)
	}
	// And the runner this vocabulary is actually for still reads, because its
	// own names are fully qualified — which is the grammar the rule is now
	// written in.
	dotnet := "  Failed Ofetch.Tests.CircuitBreakerTests.OpensAfterThreshold [12 ms]\n" +
		"  Passed Ofetch.Tests.CircuitBreakerTests.ClosesOnSuccess [3 ms]\n"
	if red := FailingTests(dotnet); len(red) != 1 ||
		red[0] != "Ofetch.Tests.CircuitBreakerTests.OpensAfterThreshold" {
		t.Errorf("a real dotnet failure stopped being read: %v", red)
	}
	if named := ReportedTests(dotnet); len(named) != 2 {
		t.Errorf("a real dotnet roster stopped being read: %v", named)
	}
}

// A SUITE THAT FAILED TO COLLECT IS NOT A SUITE THAT WENT RED, and a runner told
// to print a machine-readable report prints one whenever it ran its tests at all.
// No report means it never got that far.
func TestASuiteThatFailedToCollectIsNotOneRedCheck(t *testing.T) {
	reported, failing, read := FormatNodeJSON.Read(vitestCollectionFailure)
	if read {
		t.Fatalf("the json reader claimed a record it cannot have found: %v %v", reported, failing)
	}
	// The whole of the fall, as RunReading records it.
	result := Result{Strategy: Strategy{Read: FormatNodeJSON}}
	if !read {
		result.ReadAsPlain = true
		if result.Strategy.Read != FormatPlain {
			result.Uncollected = true
			result.Error = runnerTrouble(vitestCollectionFailure)
		}
	}
	if !result.Uncollected {
		t.Fatal("a runner that produced no report was recorded as having produced one")
	}
	if !strings.Contains(result.Error, "Failed to load url") {
		t.Errorf("the record does not carry the runner's own words: %q", result.Error)
	}
	if strings.HasPrefix(result.Error, "RUN") {
		t.Errorf("the runner's start-up banner was quoted as the trouble: %q", result.Error)
	}

	// AND IT IS NEVER SUBTRACTED AGAINST A READING THAT DID COLLECT.
	reading := Reading{
		Taken: true, AfterTaken: true,
		Before: Result{Reported: []string{"ofetch 404", "ofetch calls hooks"}},
		After:  Result{Uncollected: true, Reported: []string{"to"}, Failing: []string{"to"}},
	}
	if broke := reading.Regressed(); len(broke) > 0 {
		t.Errorf("a suite that never ran a check convicted the work of breaking %v", broke)
	}
	if gone := reading.Vanished(); len(gone) > 0 {
		t.Errorf("a suite that never ran a check reported %d checks as deleted", len(gone))
	}
	// A baseline that could not collect cannot acquit either.
	blind := Reading{
		Taken: true, AfterTaken: true,
		Before: Result{Uncollected: true},
		After:  Result{Reported: []string{"ofetch 404"}, Failing: []string{"ofetch 404"}},
	}
	if broke := blind.Regressed(); len(broke) > 0 {
		t.Errorf("a baseline that never ran a check was subtracted from: %v", broke)
	}

	// A suite that RAN and went red is untouched by any of this.
	real := Reading{
		Taken: true, AfterTaken: true,
		Before: Result{Reported: []string{"ofetch 404", "ofetch calls hooks"}},
		After: Result{
			Reported: []string{"ofetch 404", "ofetch calls hooks"},
			Failing:  []string{"ofetch calls hooks"},
		},
	}
	if broke := real.Regressed(); len(broke) != 1 || broke[0] != "ofetch calls hooks" {
		t.Errorf("a real regression stopped being one: %v", broke)
	}
}
