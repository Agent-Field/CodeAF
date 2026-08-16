package exec

import "testing"

// The title is for reading and the key is for telling nodes apart. Conflating
// the two is what let a fan-out overwrite itself: titles are clipped to 48
// characters before they ever reach here, and sibling parts of one ask share
// their opening words, so two distinct nodes present one identical title.
func TestTwoNodesWithOneClippedTitleDoNotShareAFile(t *testing.T) {
	const clipped = "Research and write a one-page brief on the"
	first := SuggestPathFor("task-12-n1", clipped)
	second := SuggestPathFor("task-12-n2", clipped)
	if first == second {
		t.Fatalf("both nodes were sent to %q — whichever finishes last wins and the other is lost", first)
	}
	for _, path := range []string{first, second} {
		if path == ".md" || path == "-.md" {
			t.Fatalf("path %q names nothing", path)
		}
	}
}

// A key that survives slugging to nothing must not take the title's place, and
// a title that does must still leave a readable name.
func TestASuggestedPathIsNeverJustTheKeyOrJustAnExtension(t *testing.T) {
	if got := SuggestPathFor("", "read the diff"); got != "read-the-diff.md" {
		t.Fatalf("keyless path = %q", got)
	}
	if got := SuggestPathFor("task-9", "///"); got != "task-9-output.md" {
		t.Fatalf("titleless path = %q", got)
	}
}

// The numeric form is the plan graph's, where the id genuinely is unique per
// node, and it is unchanged: the spelling appears in prompts and in prior
// runs' workspaces.
func TestTheNumericSuggestionKeepsItsSpelling(t *testing.T) {
	if got := SuggestPath(7, "pr 482 code review"); got != "07-pr-482-code-review.md" {
		t.Fatalf("SuggestPath = %q, want the spelling it has always had", got)
	}
}
