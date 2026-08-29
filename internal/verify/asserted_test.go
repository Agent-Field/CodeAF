package verify

import (
	"strings"
	"testing"
)

// The shape textual s13 wrote: a check that CALLS the behaviour and asserts
// something else entirely. The reader has to file the assertions under the test
// and not under the App subclass the test declares inside its own body.
func TestPythonAssertionsBelongToTheOutermostDefinition(t *testing.T) {
	body := `
import pytest
from textual.app import App

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
`
	read := AssertionsIn("tests/test_log.py", body)
	text, found := read.Text("test_rich_log_write_expand_preserves_full_width_justified")
	if !found {
		t.Fatalf("the test declaration was not found: %v", read)
	}
	for _, want := range []string{"rich_log.lines", "strip.cell_length"} {
		if !strings.Contains(text, want) {
			t.Errorf("assertion text should carry %q, got %q", want, text)
		}
	}
	// THE CALL IS NOT AN ASSERTION. `expand=True` is on the write, which is
	// setup, and the whole of the door downstream turns on this line.
	if strings.Contains(text, "expand") {
		t.Errorf("assertion text carries the call's own words: %q", text)
	}
	if _, found := read.Text("compose"); found {
		t.Errorf("a definition nested inside the check was read as a check of its own")
	}
}

func TestPythonAssertionsReadTheShapesASuiteSpellsThemIn(t *testing.T) {
	body := `
class TestFollow:
    def test_raises(self):
        with pytest.raises(ValueError):
            widget.follow_end(animate=True)

    def test_multiline(self):
        assert (
            widget.scroll_y
            == widget.max_scroll_y
        )

def test_after_the_class():
    self.assertEqual(widget.is_following_end, True)
`
	read := AssertionsIn("tests/test_log.py", body)
	for name, want := range map[string]string{
		"test_raises":          "pytest.raises",
		"test_multiline":       "max_scroll_y",
		"test_after_the_class": "is_following_end",
	} {
		text, found := read.Text(name)
		if !found {
			t.Fatalf("%s was not found: %v", name, read)
		}
		if !strings.Contains(text, want) {
			t.Errorf("%s should carry %q, got %q", name, want, text)
		}
	}
}

func TestGoAssertionsCarryTheConditionTheReportGuards(t *testing.T) {
	body := `package thing

func TestFollowEnd(t *testing.T) {
	got := Follow()
	if got.ScrollY != want.MaxScrollY {
		t.Fatalf("scroll: %v", got)
	}
}

func TestHelper(t *testing.T) {
	require.Equal(t, 4, bar.Position)
}
`
	read := AssertionsIn("pkg/thing_test.go", body)
	text, found := read.Text("TestFollowEnd")
	if !found || !strings.Contains(text, "MaxScrollY") {
		t.Fatalf("the condition beside the report was lost: %q found=%v", text, found)
	}
	if text, found := read.Text("TestHelper"); !found || !strings.Contains(text, "bar.Position") {
		t.Errorf("an assertion library call was not read: %q found=%v", text, found)
	}
}

func TestScriptAssertionsAreKeyedByTheCaseName(t *testing.T) {
	body := `
describe('follow', () => {
  it('restores follow at the end', () => {
    render();
    expect(log.isFollowingEnd).toBe(true);
  });
  test("does not snap back", () => {
    log.write('x');
  });
});
`
	read := AssertionsIn("test/log.test.ts", body)
	if text, found := read.Text("restores follow at the end"); !found ||
		!strings.Contains(text, "isFollowingEnd") {
		t.Errorf("the expect was not read: %q found=%v", text, found)
	}
	// Found and empty is the answer for a case that asserts nothing, and it is a
	// different answer from not found at all.
	if text, found := read.Text("does not snap back"); !found || strings.TrimSpace(text) != "" {
		t.Errorf("a case that asserts nothing should be found and empty: %q found=%v", text, found)
	}
}

func TestAssertionsAreSilentWhereThereIsNoReader(t *testing.T) {
	if read := AssertionsIn("suite/checks.rb", "assert_equal 1, 2\n"); read != nil {
		t.Errorf("a language with no reader answered: %v", read)
	}
	if _, found := AssertionsIn("tests/test_log.py", "def helper():\n    pass\n").
		Text("test_missing"); found {
		t.Errorf("a check the file does not declare was reported found")
	}
}
