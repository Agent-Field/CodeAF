package reportline

import "testing"

// THE ONE READING OF A REPORT'S FIRST PROSE LINE. A leading fence is skipped
// whether a language word follows it or not, blank lines are skipped, a report
// that is nothing but a fence returns the first line inside it, and a report with
// no prose at all returns nothing — which is the surface's emptiness law, not this
// reader's. This table is the rule every surface routes through (#931), moved here
// whole from the tui3 card test it began life as (#889).
func TestFirstProseSkipsFencesBlankLinesAndWrappers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		report string
		want   string
	}{
		{"a report that opens with prose", "the seven files are written and the suite passes", "the seven files are written and the suite passes"},
		{"a leading fence is skipped", "```\nthe seven files are written and the suite passes\n```", "the seven files are written and the suite passes"},
		{"a fence's language word is skipped too", "```go\npackage main\n```\nthe suite passes", "package main"},
		{"a tilde fence is skipped too", "~~~\nthe suite passes\n~~~", "the suite passes"},
		{"a leading blank line is skipped", "\n\nthe suite passes", "the suite passes"},
		{"blank lines inside a fence are skipped", "```\n\nthe suite passes\n```", "the suite passes"},
		{"a report that is nothing but a fence returns its first line", "```\n7 files changed, 15 insertions(+)\n```", "7 files changed, 15 insertions(+)"},
		{"a closed fence with nothing inside falls on", "```\n```\nthe suite passes", "the suite passes"},
		{"an unclosed fence with nothing yet inside falls on", "```go\n\nthe suite passes", "the suite passes"},
		{"an indented fence marker is still a fence", "  ```\nthe suite passes", "the suite passes"},
		{"a marker is skipped wherever it stands, not only in front", "```\n```\n```diff\n- old\n+ new\n```", "- old"},
		{"a backtick in a backtick fence's info string is prose", "```not`a`fence\nthe suite passes", "```not`a`fence"},
		{"an empty report says nothing", "", ""},
		{"a report of blank lines says nothing", "\n \n", ""},
		{"a report of fence markers alone says nothing", "```\n", ""},
	} {
		if got := FirstProse(tc.report); got != tc.want {
			t.Errorf("%s: FirstProse(%q) = %q, want %q", tc.name, tc.report, got, tc.want)
		}
	}
}

// FENCE IS THE RENDERER'S OWN OPINION. A marker with a language word after it is
// still a marker; a backtick fence whose info string carries a backtick is prose,
// per CommonMark. These pin the detector FirstProse rests on.
func TestFenceReadsMarkersTheWayTheRendererDoes(t *testing.T) {
	for _, tc := range []struct {
		line  string
		delim string
		info  string
		ok    bool
	}{
		{"```", "```", "", true},
		{"```go", "```", "go", true},
		{"~~~", "~~~", "", true},
		{"~~~~ diff meta", "~~~~", "diff", true},
		{"````", "````", "", true},
		{"```not`a`fence", "", "", false},
		{"plain prose", "", "", false},
		{"", "", "", false},
	} {
		delim, info, ok := Fence(tc.line)
		if ok != tc.ok || delim != tc.delim || info != tc.info {
			t.Errorf("Fence(%q) = (%q, %q, %v), want (%q, %q, %v)", tc.line, delim, info, ok, tc.delim, tc.info, tc.ok)
		}
	}
}
