package tui3

import "testing"

// THE WALL READS THE FRONT ONLY WHEN IT MOVED. A second look at a front whose
// entries have not changed is not a reason to read its whole transcript
// again; a new entry is.
func TestTheWallReadsTheFrontOnlyWhenItMoved(t *testing.T) {
	a, _, _ := tabApp(t)
	if !a.wallFrontMoved() {
		t.Fatal("the first look at the front did not count as a change")
	}
	if a.wallFrontMoved() {
		t.Fatal("an unchanged front counted as moved")
	}
	a.entries = append(a.entries, entry{kind: entryNote, text: "something new"})
	if !a.wallFrontMoved() {
		t.Fatal("a new entry in front did not count as moved")
	}
}
