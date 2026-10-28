package verify

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The two readings this whole package exists for are readings of a real
// runner's real output, so the fixtures are cut from the graded logs of the
// 2026-08-28 sweep rather than invented here. Both are the same defect: a patch
// that deleted an attribute the repository already had, every hidden test
// failing on SETUP, and the leaf's own narrow tests green throughout.
func TestTheFailuresAPytestSuitePrintedAreTheOnesReadBackOutOfIt(t *testing.T) {
	for _, sample := range []struct {
		fixture string
		want    []string
	}{{
		// bench/deepswe/results/igel-persist-feature-schema-…-s2/verifier.log:
		// AttributeError: <class 'igel.igel.Igel'> has no attribute
		// 'results_path', raised in the fixture, so every test errors at setup
		// and pytest reports them as ERROR rather than FAILED.
		fixture: "pytest-igel-setup-errors.txt",
		want: []string{
			"tests/test_igel/test_feature_schema_persistence.py::test_fit_persists_feature_schema_and_description_metadata",
			"tests/test_igel/test_feature_schema_persistence.py::test_fit_rejects_unknown_include_columns",
			"tests/test_igel/test_feature_schema_persistence.py::test_predict_applies_feature_schema_and_preserves_selected_feature_order",
			"tests/test_igel/test_feature_schema_persistence.py::test_predict_uses_duplicate_alias_when_canonical_feature_is_missing",
		},
	}, {
		// bench/deepswe/results/textual-richlog-follow-state-…-s1/verifier.log:
		// 'RichLog' object has no attribute '_size_known', raised inside the
		// widget itself, so pytest reports FAILED and truncates the first
		// line's reason to "- Att...".
		fixture: "pytest-textual-attribute-error.txt",
		want: []string{
			"tests/test_rich_log_follow_state.py::test_log_exposes_follow_api",
			"tests/test_rich_log_follow_state.py::test_rich_log_exposes_follow_api",
			"tests/test_rich_log_follow_state.py::test_rich_log_follow_changed_posts_only_when_boolean_changes",
			"tests/test_rich_log_follow_state.py::test_widgets_start_following_end_when_auto_scroll_is_enabled",
		},
	}} {
		raw, err := os.ReadFile(filepath.Join("testdata", sample.fixture))
		if err != nil {
			t.Fatal(err)
		}
		if got := FailingTests(string(raw)); !reflect.DeepEqual(got, sample.want) {
			t.Errorf("%s: FailingTests = %#v, want %#v", sample.fixture, got, sample.want)
		}
	}
}

func TestEveryTestRunnersOwnFailureVocabularyIsRead(t *testing.T) {
	// The extractor is the whole of the language independence: the caller runs
	// the checks, this only reads what they printed.
	for name, sample := range map[string]struct {
		output string
		want   string
	}{
		"go":       {"--- FAIL: TestThing (0.00s)\n    thing_test.go:9: nope\n", "TestThing"},
		"pytest":   {"FAILED tests/test_api.py::test_headers - AssertionError\n", "tests/test_api.py::test_headers"},
		"unittest": {"FAIL: test_headers (tests.api.ApiCase)\n", "test_headers (tests.api.ApiCase)"},
		"jest":     {"  ✕ renders the header (12 ms)\n", "renders the header"},
		"cargo":    {"test parser::tests::commas ... FAILED\n", "parser::tests::commas"},
		"maven":    {"[ERROR] ApiTest.headers Time elapsed: 0.1 s <<< FAILURE!\n", "ApiTest.headers"},
		"tap":      {"not ok 3 - the header is set\n", "the header is set"},
	} {
		names := FailingTests(sample.output)
		found := false
		for _, got := range names {
			if got == sample.want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: FailingTests(%q) = %#v, want %q among them",
				name, sample.output, names, sample.want)
		}
	}
	if names := FailingTests("ok  \texample.test/pkg\t0.10s\nPASS\n"); len(names) != 0 {
		t.Errorf("a green run named failures: %#v", names)
	}
}

// The subtraction, in both directions at once. A repository carrying a
// pre-existing red test must not fail every correct patch that passes through
// it — that is a measured, repeated way of throwing finished work away — and a
// check the work turned red must be named however much else was already red
// around it.
func TestOnlyACheckThatWasGreenBeforeAndIsRedAfterIsNamedANewFailure(t *testing.T) {
	before := []string{
		"tests/test_igel/test_feature_schema_persistence.py::test_fit_rejects_unknown_include_columns",
	}
	after := []string{
		"tests/test_igel/test_feature_schema_persistence.py::test_fit_rejects_unknown_include_columns",
		"tests/test_igel/test_predict.py::test_predict_reads_results_path",
	}
	want := []string{"tests/test_igel/test_predict.py::test_predict_reads_results_path"}
	if got := NewFailures(before, after); !reflect.DeepEqual(got, want) {
		t.Errorf("NewFailures = %#v, want exactly the check this work turned red: %#v", got, want)
	}
}

func TestACheckThatWasAlreadyRedBeforeTheWorkIsNamedByNobody(t *testing.T) {
	same := []string{"TestFailGenFishCompletionFile", "tests/test_api.py::test_headers"}
	if got := NewFailures(same, same); got != nil {
		t.Errorf("NewFailures over one repository's own pre-existing reds = %#v, want nil", got)
	}
	// Nobody looked before, so nothing can be subtracted and everything red now
	// is new. The exit statuses are the caller's to weigh; this only subtracts.
	if got := NewFailures(nil, same); !reflect.DeepEqual(got, same) {
		t.Errorf("NewFailures against no baseline = %#v, want all of %#v", got, same)
	}
}

// Order-stable and deduped: the same two readings must subtract to the same
// list every time, or a run comparing them disagrees with itself.
func TestTheSubtractionIsOrderStableAndNamesNothingTwice(t *testing.T) {
	after := []string{"b", "a", "b", "c", "a"}
	want := []string{"b", "a", "c"}
	if got := NewFailures([]string{"z"}, after); !reflect.DeepEqual(got, want) {
		t.Errorf("NewFailures = %#v, want %#v in the after reading's own order", got, want)
	}
}

// A SLASH MEANS A PATH, AND A PATH IS NOT QUALIFIED BY ITS DOTS. The last dotted
// segment of a name is a class or a package the check hangs off — but only where
// the name is a qualified name at all. In a path that dot is an extension or a
// package's own name, and reducing it left one reading identified as `test` and
// another as `py`.
func TestAPathIsNotQualifiedByItsDots(t *testing.T) {
	for name, want := range map[string]string{
		// Paths and package names, which keep every byte they arrived with.
		"example.com/widget.test": "example.com/widget.test",
		"tests/x.py":              "tests/x.py",
		"example.com/circuit":     "example.com/circuit",
		// A file named with no directory in front of it is still a file.
		"x.py": "x.py",
		// And the qualifications that DO reduce, including the subtest — which
		// is read off before any of this, or its own slash would keep it.
		"TestX/sub":                  "TestX",
		"example.com/pkg.TestX/sub":  "TestX",
		"pkg.Class.method":           "method",
		"Ofetch.Tests.Api.OpensOnce": "OpensOnce",
	} {
		if got := CheckIdentity(name); got != want {
			t.Errorf("CheckIdentity(%q) = %q, want %q", name, got, want)
		}
	}
}

// Playwright's list, line and dot reporters name red checks in a closing `N
// failed` section and nowhere else in a form that is stable between runs. The
// fixtures are real output of @playwright/test 1.64.1 against a four-check
// spec, with the absolute workspace path replaced by /work/app.
func TestTheChecksPlaywrightReportedAsFailedAreTheOnesReadBackOutOfIt(t *testing.T) {
	inTwoBrowsers := []string{
		"[chromium] › tests/e2e/login.spec.ts › login › rejects a wrong password",
		"[chromium] › tests/e2e/login.spec.ts › shows the footer",
		"[firefox] › tests/e2e/login.spec.ts › login › rejects a wrong password",
		"[firefox] › tests/e2e/login.spec.ts › shows the footer",
	}
	for _, sample := range []struct {
		fixture string
		want    []string
	}{{
		// The list reporter with two projects: one test in two browsers is
		// two checks, so the project stays in the name.
		fixture: "playwright-list-projects.txt",
		want:    inTwoBrowsers,
	}, {
		// The same suite after a line was added above every check. Playwright
		// prints each check at a new line and column, and a name that kept them
		// would read two unchanged red checks as two new failures.
		fixture: "playwright-list-projects-moved.txt",
		want:    inTwoBrowsers,
	}, {
		// The line reporter with no projects configured prints no brackets.
		fixture: "playwright-line.txt",
		want: []string{
			"tests/e2e/login.spec.ts › login › rejects a wrong password",
			"tests/e2e/login.spec.ts › shows the footer",
		},
	}, {
		// A check that failed and then passed on retry gets a numbered failure
		// banner like the red ones, and the summary lists it under `1 flaky`.
		// It passed, so it is not read as failing.
		fixture: "playwright-list-flaky.txt",
		want: []string{
			"[chromium] › tests/e2e/login.spec.ts › login › rejects a wrong password",
			"[chromium] › tests/e2e/login.spec.ts › shows the footer",
		},
	}} {
		got := FailingTests(fixture(t, sample.fixture))
		if !reflect.DeepEqual(got, sample.want) {
			t.Errorf("%s: read %q, want %q", sample.fixture, got, sample.want)
		}
	}
}

// Only playwright's own location grammar loses its numbers: a spec file inside
// a ` › ` chain. rspec names a red example by file and line, and that line is
// the example's identity to rspec.
func TestOnlyPlaywrightsMovingLocationIsStrippedFromAName(t *testing.T) {
	// The leading `./` is trimmed by normalizeTestName as it always was; the
	// line number is what this asserts.
	if got := FailingTests("rspec ./spec/api_spec.rb:12 # Api returns headers\n"); !reflect.DeepEqual(got, []string{"/spec/api_spec.rb:12"}) {
		t.Errorf("an rspec location was rewritten: %q", got)
	}
	// mocha also numbers its failures `1) ...`, but prints no `N failed`
	// section in this shape, so nothing of it is read as a playwright check.
	mocha := "  2 passing (5ms)\n  1 failing\n\n  1) Array\n       #indexOf()\n         returns -1:\n     AssertionError\n"
	if got := FailingTests(mocha); len(got) != 0 {
		t.Errorf("mocha's numbered failure list was read as playwright checks: %q", got)
	}
}
