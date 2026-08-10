package golden

import (
	"strings"
	"testing"
)

func TestUnifiedDiffIsReadable(t *testing.T) {
	want := "same\nold value \nlast\n"
	got := "same\nnew  value\nlast\n"
	diff := unifiedDiff("testdata/golden/example.txt", want, got)
	for _, fragment := range []string{
		"--- testdata/golden/example.txt",
		"+++ rendered",
		"@@ -1,3 +1,3 @@",
		"-old·value·",
		"+new··value",
		"(spaces: ·",
	} {
		if !strings.Contains(diff, fragment) {
			t.Errorf("diff does not contain %q:\n%s", fragment, diff)
		}
	}
	if line, column := firstDifference(want, got); line != 2 || column != 1 {
		t.Errorf("firstDifference = line %d, column %d; want line 2, column 1", line, column)
	}
}

func TestVisibleMarkersIncludeControls(t *testing.T) {
	if got, want := markInvisible(" a\tb\x1b[31m"), "·a⇥b␛[31m"; got != want {
		t.Errorf("markInvisible = %q; want %q", got, want)
	}
}
