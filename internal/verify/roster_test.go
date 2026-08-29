package verify

import (
	"os"
	"strings"
	"testing"
)

// THE LEAF'S OWN DIFF IS THE CHEAPEST HONEST ANSWER TO "WHAT DOES THIS CHECK".
//
// The fixture is the change ofetch s4 actually shipped, taken out of
// bench/deepswe/results/ofetch-…-s4/model.patch. Its deliverable opened "All 56
// tests pass (28 existing + 28 new circuit breaker tests)" and the gate passed
// it at 41 of 47 hidden tests. Nothing in the harness could see WHICH 28 checks
// the leaf had written, so the only account of coverage anybody held was that
// sentence.
func TestTheChecksAChangeWritesAreReadOutOfItsOwnDiff(t *testing.T) {
	patch := readFixture(t, "testdata/ofetch-s4-tests.patch")
	added, removed := PatchChecks(patch)

	// The leaf declared twenty-eight checks in one new file, and the count is
	// asserted as a floor rather than a figure: what matters is that the diff
	// answers the question at all, and a reader that found two of them would
	// pass an exact-count test written the other way round.
	if len(added) < 25 {
		t.Fatalf("read %d checks out of the change that wrote 28: %#v", len(added), added)
	}
	for _, want := range []string{
		"transitions from closed to open after threshold failures",
		"does not count non-listed 4xx/5xx as circuit failures",
		"counts onRequestError hook errors as circuit failures",
		"runs onRequest hook even when circuit is open",
	} {
		if !holds(added, want) {
			t.Errorf("the change declares %q and the reader did not name it", want)
		}
	}
	// A file created from nothing removes nothing. The removal half must be
	// silent here or every new test file would read as a deletion.
	if len(removed) != 0 {
		t.Errorf("a change that only adds a file removed %#v", removed)
	}
	// The describe() block is grouping and not a check: counting it would let a
	// worker satisfy a behaviour with a heading.
	if holds(added, "circuit breaker") {
		t.Error("a describe() block was counted as a check")
	}
}

// A CHECK THAT WAS THERE AND IS NOT WAS DELETED, RENAMED OR SKIPPED. Taking out
// the test that is failing is the cheapest way there is to make a suite green,
// and until this reader existed the before/after photograph could see a check
// turn red and could not see one stop existing.
func TestACheckTheChangeTakesAwayIsRead(t *testing.T) {
	patch := `diff --git a/test/circuit.test.ts b/test/circuit.test.ts
--- a/test/circuit.test.ts
+++ b/test/circuit.test.ts
@@ -1,8 +1,8 @@
 describe("circuit breaker", () => {
-  it("keeps the half-open slot across internal retries", async () => {
-    await expect(client("/x")).rejects.toThrow();
-  });
+  it("opens after five failures", async () => {
+    await expect(client("/x")).rejects.toThrow();
+  });
   it("resets consecutive failures on success", async () => {
`
	added, removed := PatchChecks(patch)
	if !holds(removed, "keeps the half-open slot across internal retries") {
		t.Errorf("the deleted check was not read as removed: %#v", removed)
	}
	if !holds(added, "opens after five failures") {
		t.Errorf("the new check was not read as added: %#v", added)
	}
	// The untouched check is context in the diff and belongs to neither side.
	if holds(added, "resets consecutive failures on success") ||
		holds(removed, "resets consecutive failures on success") {
		t.Error("a context line was scored as a change")
	}
}

// A check that only moved is not a check that was removed. Re-indenting a test
// file rewrites every one of its lines, and a reader that convicted on that
// would raise a finding for a formatter.
func TestACheckThatOnlyMovedCancels(t *testing.T) {
	patch := `--- a/a.test.ts
+++ b/b.test.ts
-  it("handles the empty case", () => {})
+    it("handles the empty case", () => {})
`
	added, removed := PatchChecks(patch)
	if len(added) != 0 || len(removed) != 0 {
		t.Errorf("a moved check was scored: added=%#v removed=%#v", added, removed)
	}
}

// The roster is what says a check EXISTS, and a runner's green lines are most of
// it. FailingTests reads only the red half, so a rule built on it alone would
// report every passing check in a project as one that had disappeared.
func TestTheRosterHoldsWhatPassedAndWhatFailed(t *testing.T) {
	output := `--- PASS: TestOpensAfterThreshold (0.01s)
--- FAIL: TestHalfOpenSlot (0.02s)
ok  	example.com/circuit	0.31s
PASSED tests/test_state.py::test_cooldown
FAILED tests/test_state.py::test_probe - AssertionError
 ✓ resets consecutive failures on success
 ✕ keeps the half-open slot across internal retries
`
	roster := ReportedTests(output)
	for _, want := range []string{
		"TestOpensAfterThreshold", "TestHalfOpenSlot",
		"tests/test_state.py::test_cooldown", "tests/test_state.py::test_probe",
		"resets consecutive failures on success",
		"keeps the half-open slot across internal retries",
	} {
		if !holds(roster, want) {
			t.Errorf("the roster does not hold %q: %#v", want, roster)
		}
	}
	// Every failing name is in the roster too, or a red check would subtract to
	// a vanished one on the next reading.
	for _, red := range FailingTests(output) {
		if !holds(roster, red) {
			t.Errorf("the roster omits the failing check %q", red)
		}
	}
}

func readFixture(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the sweep's own change is the fixture: %v", err)
	}
	return string(body)
}

func holds(names []string, want string) bool {
	for _, name := range names {
		if name == want || strings.Contains(name, want) {
			return true
		}
	}
	return false
}
