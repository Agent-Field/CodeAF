package revision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// The behaviours textual s13's request stated, in the request's own words.
var (
	expandPoint = plan.Point{
		Behaviour: "RichLog.write(expand=True) no longer preserves full-width justified rendering with current Rich",
		Quote:     "RichLog.write(expand=True) no longer preserves full-width justified rendering with current Rich",
	}
	reflowPoint = plan.Point{
		Behaviour: "RichLog.write(..., expand=True) must honor expansion and justification for existing expanded entries after min_width changes",
		Quote:     "RichLog.write(..., expand=True) must honor expansion and justification for deferred writes, explicit writes, and existing expanded entries after resizes or min_width changes",
	}
	scrollPoint = plan.Point{
		Behaviour: "Normal scrolling must still update the visible viewport and vertical scrollbar position",
		Quote:     "Normal scrolling must still update the visible viewport and vertical scrollbar position",
	}
)

func TestObservablesAreNamesSpelledOrNamesBound(t *testing.T) {
	named := Observables(expandPoint.Quote, verify.SurfaceIndex{})
	for _, want := range []string{"richlog.write", "expand"} {
		if !contains(named, want) {
			t.Errorf("observables of the expand behaviour should carry %q, got %v", want, named)
		}
	}
	// A signature binds its parameter the same way a call binds its argument.
	if named := Observables("follow_end(animate: bool = False)", verify.SurfaceIndex{}); !contains(named, "animate") ||
		!contains(named, "follow_end") {
		t.Errorf("a signature's parameter is an observable: %v", named)
	}
	// A SENTENCE WITH NO IDENTIFIER IN IT NAMES NOTHING, which is what keeps the
	// door shut on every prose behaviour a request states.
	if named := Observables(scrollPoint.Quote, verify.SurfaceIndex{}); len(named) != 0 {
		t.Errorf("prose named observables: %v", named)
	}
	// And a colon in prose is not a binding.
	if named := Observables("it must post only when the boolean actually changes", verify.SurfaceIndex{}); len(named) != 0 {
		t.Errorf("prose named observables: %v", named)
	}
}

// The measured failure: a check that calls write(expand=True) and asserts only
// that some lines exist. Every name is spelled; nothing is weighed.
func TestACheckThatNamesTheBehaviourAndAssertsNothingIsWeaklyExercised(t *testing.T) {
	root := suiteWith(t, `
async def test_rich_log_write_expand_preserves_full_width_justified() -> None:
    """RichLog.write(expand=True) no longer preserves full-width justified rendering."""

    class TestApp(App):
        def compose(self) -> ComposeResult:
            yield RichLog()

    async with TestApp().run_test(size=(80, 24)) as pilot:
        rich_log = pilot.app.query_one(RichLog)
        rich_log.write("short", expand=True)
        await pilot.pause()
        assert len(rich_log.lines) > 0
        for strip in rich_log.lines:
            assert strip.cell_length >= 5
`)
	weighed := WeighAssertions(root, "job", []string{"tests/test_log.py"},
		[]plan.Point{expandPoint},
		[]store.ExercisedPoint{{Point: expandPoint.Behaviour,
			Check: "tests/test_log.py::test_rich_log_write_expand_preserves_full_width_justified"}})
	if len(weighed[0].Unasserted) == 0 {
		t.Fatalf("a check that asserts none of the behaviour's observables was let through")
	}
	if !contains(weighed[0].Unasserted, "expand") {
		t.Errorf("the finding should name expand, got %v", weighed[0].Unasserted)
	}
	if weighed[0].Check == "" {
		t.Errorf("the pairing itself is real and must be kept as evidence")
	}
}

// And the check the hidden suite actually wanted: one that weighs the width the
// entries reflow to after min_width changes.
func TestACheckThatAssertsAnObservableIsExercised(t *testing.T) {
	root := suiteWith(t, `
async def test_rich_log_expand_entries_reflow_after_min_width_change() -> None:
    async with TestApp().run_test(size=(80, 24)) as pilot:
        rich_log = pilot.app.query_one(RichLog)
        rich_log.write("short", expand=True)
        rich_log.min_width = 40
        await pilot.pause()
        assert rich_log.min_width == 40
        assert rich_log.lines[0].cell_length == 40, "write(expand=True) must reflow entries"
`)
	weighed := WeighAssertions(root, "job", []string{"tests/test_log.py"},
		[]plan.Point{reflowPoint},
		[]store.ExercisedPoint{{Point: reflowPoint.Behaviour,
			Check: "tests/test_log.py::test_rich_log_expand_entries_reflow_after_min_width_change"}})
	if len(weighed[0].Unasserted) != 0 {
		t.Fatalf("a check that asserts an observable was called weak: %v", weighed[0].Unasserted)
	}
}

// A behaviour with no identifier in it is not this door's business, however
// little the check asserts.
func TestAProsePointIsLeftToTheMappingItAlreadyHad(t *testing.T) {
	root := suiteWith(t, `
async def test_log_normal_scrolling() -> None:
    assert True
`)
	weighed := WeighAssertions(root, "job", []string{"tests/test_log.py"},
		[]plan.Point{scrollPoint},
		[]store.ExercisedPoint{{Point: scrollPoint.Behaviour,
			Check: "tests/test_log.py::test_log_normal_scrolling"}})
	if len(weighed[0].Unasserted) != 0 {
		t.Fatalf("a prose behaviour was judged by the assertion door: %v", weighed[0].Unasserted)
	}
}

// Every silence favours the check: a file the run did not write, and a check the
// reader cannot find, both leave the pairing exactly as the mapping answered it.
func TestTheDoorStaysShutWhereNothingCouldBeRead(t *testing.T) {
	root := suiteWith(t, "async def test_other() -> None:\n    assert True\n")
	mapping := []store.ExercisedPoint{{Point: expandPoint.Behaviour,
		Check: "tests/test_log.py::test_rich_log_write_expand"}}
	if weighed := WeighAssertions(root, "job", []string{"tests/test_log.py"},
		[]plan.Point{expandPoint}, mapping); len(weighed[0].Unasserted) != 0 {
		t.Errorf("a check the file does not declare was judged: %v", weighed[0].Unasserted)
	}
	if weighed := WeighAssertions(root, "job", nil,
		[]plan.Point{expandPoint}, mapping); len(weighed[0].Unasserted) != 0 {
		t.Errorf("a check outside the run's own record was judged: %v", weighed[0].Unasserted)
	}
}

// EACH observable, not any one of them. A check that weighs one of the things a
// behaviour names says nothing about the others.
func TestEveryObservableMustBeAsserted(t *testing.T) {
	root := suiteWith(t, `
async def test_rich_log_write_expand() -> None:
    rich_log.write("short", expand=True)
    assert rich_log.write is not None
`)
	weighed := WeighAssertions(root, "job", []string{"tests/test_log.py"},
		[]plan.Point{expandPoint},
		[]store.ExercisedPoint{{Point: expandPoint.Behaviour,
			Check: "tests/test_log.py::test_rich_log_write_expand"}})
	// The assertion names RichLog.write through the instance and says nothing
	// about expand, so expand is what remains.
	if got := weighed[0].Unasserted; len(got) != 1 || got[0] != "expand" {
		t.Fatalf("the observable nothing asserted was not named alone: %v", got)
	}
}

// The tree is the vocabulary: a person writing "vertical scrollbar position" has
// named ScrollBar.position, and textual s16 asserted scroll_y a hundred times
// without ever reading it.
func TestAnObservableCanBeNamedInTheRequestsOwnWords(t *testing.T) {
	surface := verify.Surface{"src/scrollbar.py": []verify.Declaration{
		{Name: "ScrollBar"}, {Name: "ScrollBar.position"}, {Name: "ScrollUp"}}}
	named := Observables(scrollPoint.Quote, verify.IndexSurface(surface))
	if len(named) != 1 || named[0] != "scrollbar.position" {
		t.Fatalf("the spoken name was not read: %v", named)
	}
	// A SINGLE WORD IS A WORD, and a bare type is what a check builds rather
	// than what it weighs: neither becomes an observable.
	spoken := Observables("RichLog still snaps back after users scroll up, unlike Log",
		verify.IndexSurface(verify.Surface{"a.py": []verify.Declaration{
			{Name: "ScrollUp"}, {Name: "RichLog"}, {Name: "Log"}}}))
	if len(spoken) != 0 {
		t.Errorf("prose was promoted to names of the tree: %v", spoken)
	}
}

// A hyphen is not an identifier character, so a hyphenated compound is English —
// unless the person wrote it as a selector or the tree declares it.
func TestAHyphenatedCompoundIsNotAnObservable(t *testing.T) {
	if named := Observables("preserves full-width justified rendering", verify.SurfaceIndex{}); len(named) != 0 {
		t.Errorf("an English compound became an observable: %v", named)
	}
	if named := Observables("Buttons #follow-log and #write-expanded", verify.SurfaceIndex{}); len(named) != 2 {
		t.Errorf("a selector is a name and was dropped: %v", named)
	}
}

func TestCheckLeafIsTheDeclarationARunnerNames(t *testing.T) {
	for identity, want := range map[string]string{
		"tests/test_log.py::test_follow":          "test_follow",
		"tests/test_log.py::test_follow[case-2]":  "test_follow",
		"test/log.test.ts > follow > restores it": "restores it",
		"TestFollowEnd/restores":                  "TestFollowEnd",
		"tests/test_log.py":                       "",
	} {
		if got := checkLeaf(identity); got != want {
			t.Errorf("checkLeaf(%q) = %q, want %q", identity, got, want)
		}
	}
}

// The finding a weak pairing produces: it names the behaviour, names the
// observable nothing asserted, and asks for the assertion rather than for
// another check.
func TestTheWeakFindingNamesTheObservableAndBuysARound(t *testing.T) {
	grounds := Grounds{Intent: expandPoint.Quote}
	mapping := []store.ExercisedPoint{{Point: expandPoint.Behaviour,
		Check: "tests/test_log.py::test_expand", Unasserted: []string{"expand", "min_width"}}}
	finding, open := Unexercised([]plan.Point{expandPoint}, mapping, grounds)
	if !open {
		t.Fatalf("a weakly exercised behaviour did not raise a finding")
	}
	for _, want := range []string{"asserted by no check", "expand, min_width", expandPoint.Behaviour} {
		if !strings.Contains(finding.Gaps, want) {
			t.Errorf("the gap should carry %q:\n%s", want, finding.Gaps)
		}
	}
	if len(finding.Unasserted) != 1 || len(finding.Unexercised) != 0 {
		t.Errorf("the two halves of the measurement were mixed: %+v", finding)
	}
	// The citation is the behaviour, never the sentence this program wrote
	// around it.
	if len(finding.Citations) != 1 || finding.Citations[0] != expandPoint.Behaviour {
		t.Errorf("the finding cited its own wording: %v", finding.Citations)
	}
	// And it stands on its own after a refused judgement, with both halves.
	if rebuilt := finding.measuredHalf(); len(rebuilt.Unasserted) != 1 {
		t.Errorf("the measured half lost the weak finding: %+v", rebuilt)
	}
	gate := store.DeliveryGate{Pass: true, Unasserted: finding.Unasserted}
	if gate.Whole() {
		t.Errorf("a pass over a behaviour nothing asserts was called whole")
	}
}

func suiteWith(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tests"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tests", "test_log.py"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
