package exec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The brief offers a file; it does not ask for one. The measured cost of the
// old wording — "a standalone document goes to NN-title.md", handed to every
// leaf unconditionally — was six numbered process artifacts left in a person's
// project by one job, only one of which anyone might have wanted.
func TestTheBriefOffersAFileOnlyToTheWorkThatOwnsTheDeliverable(t *testing.T) {
	linear := NewLinear(&scriptedCompleter{}, workspace(t), nil, 10, 1_000_000, time.Minute)
	brief := linear.brief(Task{Brief: "review the diff", OutputHint: "07-review.md"})

	if !strings.Contains(brief, "07-review.md") {
		t.Fatalf("the deliverable owner was never told where a file would go:\n%s", brief)
	}
	// The invitation is conditioned on the person's own ask, and the final
	// message is named as the deliverable before any path is mentioned.
	for _, clause := range []string{
		"your final message is the deliverable",
		"only when they asked for a file",
	} {
		if !strings.Contains(strings.ToLower(brief), clause) {
			t.Fatalf("the brief lost the condition %q:\n%s", clause, brief)
		}
	}
	if strings.Contains(brief, "a standalone document goes to") {
		t.Fatalf("the unconditional invitation is still in the brief:\n%s", brief)
	}
	if hint := strings.Index(brief, "07-review.md"); hint < strings.Index(brief, "final message is the deliverable") {
		t.Fatalf("the path is offered before the deliverable is named:\n%s", brief)
	}
}

// A deliverable owner with no offered address is not off the hook — it is the
// exact seat from which a worker invented `answer.txt`, filed the analysis in
// it, and lost the cell to a pointer. The in-channel law rides the brief even
// when there is no path to condition it on.
func TestALeafWithNoAddressStillCarriesTheInChannelLaw(t *testing.T) {
	linear := NewLinear(&scriptedCompleter{}, workspace(t), nil, 10, 1_000_000, time.Minute)
	brief := linear.brief(Task{Brief: "compare march orders against february"})

	for _, clause := range []string{
		"your final message is the deliverable",
		"a message that says where the answer lives instead of carrying it has delivered nothing",
	} {
		if !strings.Contains(strings.ToLower(brief), clause) {
			t.Fatalf("with no output hint the brief lost %q:\n%s", clause, brief)
		}
	}
}

// An intermediate leaf is working for the work that comes after it. It is
// offered no deliverable path at all: its handoff is its final message, and
// the only address it gets is the run's own scratch.
func TestAnIntermediateLeafIsOfferedScratchAndNotADocument(t *testing.T) {
	space := workspace(t)
	scratch := t.TempDir()
	space = space.WithScratch(scratch)
	linear := NewLinear(&scriptedCompleter{}, space, nil, 10, 1_000_000, time.Minute)

	_, shown, err := space.ScratchPath(SuggestPath(70, "read diff"))
	if err != nil {
		t.Fatal(err)
	}
	brief := linear.brief(Task{Brief: "read the diff", OutputHint: shown, Intermediate: true})

	if !strings.Contains(brief, scratch) {
		t.Fatalf("the intermediate hint does not point at scratch:\n%s", brief)
	}
	if strings.Contains(brief, space.Root()) {
		t.Fatalf("an intermediate leaf was invited into the workspace:\n%s", brief)
	}
	for _, clause := range []string{
		"read by the work that comes after you",
		"do not write a document",
	} {
		if !strings.Contains(strings.ToLower(brief), clause) {
			t.Fatalf("the intermediate brief lost %q:\n%s", clause, brief)
		}
	}
}

// The other half of the same move: a path handed out must be a path that can be
// written. Scratch moved out of the workspace is exactly the case that matters —
// an errand working in someone's project — and a refusal there would send the
// file straight back beside their files.
func TestScratchIsWritableWhenItSitsOutsideTheWorkspace(t *testing.T) {
	space := workspace(t)
	scratch := t.TempDir()
	space = space.WithScratch(scratch)

	full, shown, err := space.ScratchPath("144-synthesis.md")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := space.Resolve(shown)
	if err != nil {
		t.Fatalf("the workspace refused the path it handed out: %v", err)
	}
	if resolved != full {
		t.Fatalf("resolved %q, want %q", resolved, full)
	}
	if err := os.WriteFile(resolved, []byte("notes"), 0o600); err != nil {
		t.Fatal(err)
	}

	// It is a working file and not a product: nothing recorded there may reach
	// the person as something the job made for them.
	space.Record("1", resolved)
	if got := space.Artifacts("1"); len(got) != 0 {
		t.Fatalf("a scratch file was announced as a deliverable: %v", got)
	}
	// And somewhere genuinely outside both roots is still refused.
	if _, err := space.Resolve(filepath.Join(t.TempDir(), "elsewhere.md")); err == nil {
		t.Fatal("a path outside the workspace and outside scratch was admitted")
	}
}
