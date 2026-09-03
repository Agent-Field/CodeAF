package verify

import (
	"os"
	"reflect"
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

// A CHECK HAS ONE IDENTITY, whichever reader named it.
//
// The two readers here read the same check out of two places and cannot spell
// it the same way: a runner prints the node-id path, the class, the package and
// the subtest it ran under, and the source those names come from holds none of
// them. Every pair below is ONE check read both ways, and the gate unions the
// two readings — the checks a change declared with the checks the suite
// reported — so a pair that does not reduce to one identity is a behaviour
// counted as covered twice and a deletion named twice.
func TestOneCheckIsOneIdentityWhicheverReaderNamedIt(t *testing.T) {
	for _, one := range []struct {
		runner   string
		output   string
		source   string
		identity string
	}{{
		// Go names the subtest under the function; the file holds the function.
		runner:   "go",
		output:   "--- PASS: TestOpensAfterThreshold/half_open_probe (0.01s)\n",
		source:   "func TestOpensAfterThreshold(t *testing.T) {\n\tt.Run(\"half open probe\", nil)\n}\n",
		identity: "TestOpensAfterThreshold",
	}, {
		// pytest names the file and the class in front of the check.
		runner:   "pytest",
		output:   "PASSED tests/state/test_cooldown.py::CooldownCase::test_cooldown_expires\n",
		source:   "class CooldownCase:\n    def test_cooldown_expires(self):\n        assert True\n",
		identity: "test_cooldown_expires",
	}, {
		// junit/testng, whose runners name the class and whose annotation names
		// only the method. Both of the shapes maven and gradle print reduce to
		// the method, which is the one the declaration can be read for.
		runner: "junit",
		output: "[ERROR] ApiTest.testHeadersAreSet Time elapsed: 0.1 s <<< FAILURE!\n" +
			"com.example.ApiTest > testHeadersAreSet PASSED\n",
		source:   "public class ApiTest {\n  @Test\n  public void testHeadersAreSet() {\n  }\n}\n",
		identity: "testHeadersAreSet",
	}, {
		// An rspec description is written once and printed back unchanged, and
		// the identity must leave it exactly as its author spelled it.
		runner:   "rspec",
		output:   " ✓ resets consecutive failures on success\n",
		source:   "  it 'resets consecutive failures on success' do\n    expect(true)\n  end\n",
		identity: "resets consecutive failures on success",
	}} {
		reported := ReportedTests(one.output)
		if len(reported) == 0 {
			t.Fatalf("%s: the runner's own output named no check at all", one.runner)
		}
		for _, name := range reported {
			if got := CheckIdentity(name); got != one.identity {
				t.Errorf("%s: the runner named %q, whose identity is %q, want %q",
					one.runner, name, got, one.identity)
			}
		}
		declared := DeclaredChecks(one.source)
		if len(declared) != 1 || CheckIdentity(declared[0]) != one.identity {
			t.Errorf("%s: the source declares %#v, want the one identity %q",
				one.runner, declared, one.identity)
		}
		// And the union the gate actually takes: both readings of one check,
		// and one check comes out.
		if union := UniqueChecks(append(declared, reported...)); len(union) != 1 {
			t.Errorf("%s: one check read twice was unioned as %#v", one.runner, union)
		}
	}
}

// The order is the order of the text. A file that spells its live checks `it`
// and its skipped ones `xit` is read by two of the shapes, and a reader that
// walked the shapes one at a time — or sorted at the end — reported an order
// nobody wrote.
func TestTheChecksASourceDeclaresComeBackInTheOrderItDeclaresThem(t *testing.T) {
	source := `describe("circuit breaker", () => {
  it("opens after five failures", () => {})
  xit("keeps the half-open slot across internal retries", () => {})
  it("resets consecutive failures on success", () => {})
  it("opens after five failures", () => {})
})
`
	want := []string{
		"opens after five failures",
		"keeps the half-open slot across internal retries",
		"resets consecutive failures on success",
	}
	if got := DeclaredChecks(source); !reflect.DeepEqual(got, want) {
		t.Errorf("DeclaredChecks = %#v, want the file's own order without repeats: %#v",
			got, want)
	}
}

// A ` > ` A RUNNER PRINTED IS NESTING; A ` > ` AN AUTHOR TYPED IS TWO WORDS.
//
// The reader that splits a check off the headings it sits under was splitting
// declarations too, so a suite holding `it("renders a > b")` declared a check
// called `b` — a name nothing in any output matches, in the roster the gate maps
// behaviours against and in the sentence that says a check was deleted.
func TestADescriptionIsNotANestingChainWhenItIsReadOutOfSource(t *testing.T) {
	source := "describe(\"markdown\", () => {\n  it(\"renders a > blockquote\", () => {})\n})\n"
	declared := DeclaredChecks(source)
	want := []string{"renders a > blockquote"}
	if !reflect.DeepEqual(declared, want) {
		t.Errorf("DeclaredChecks = %#v, want the description whole: %#v", declared, want)
	}
	// The runner's own reading of that same check is the last segment of what
	// it printed, because a title holding ` > ` cannot be told from a chain once
	// it has been printed. That is the ambiguity CheckIdentity states, and it
	// runs in the safe direction: the two readings meet at the same segment, so
	// the check unions as one check and not as two.
	banner := "spec/markdown.test.ts > markdown > renders a > blockquote"
	if got := CheckIdentity(banner); got != "blockquote" {
		t.Errorf("the runner's own name %q reduced to %q, want its last segment %q",
			banner, got, "blockquote")
	}
	if union := UniqueChecks([]string{declared[0], banner}); len(union) != 1 {
		t.Errorf("one check declared and printed was unioned as %#v", union)
	}
	// And the far end of the same ambiguity, which costs nothing: a title whose
	// last segment is one character has no readable identity — a one-letter name
	// is not a name — so the union keeps both readings rather than losing
	// either. A CHECK THE GATE STOPS SEEING IS THE ONE THING THIS MAY NOT DO.
	if got := CheckIdentity("spec > renders a > b"); got != "" {
		t.Errorf("a one-character segment was read as the identity %q", got)
	}
	if union := UniqueChecks([]string{"renders a > b", "spec > renders a > b"}); len(union) != 2 {
		t.Errorf("a name with no readable identity was dropped from %#v", union)
	}
}
