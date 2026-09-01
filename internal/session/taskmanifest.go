package session

import (
	"fmt"
	"strings"
)

// ── THE CHECK READS THE GROUND, NOT A DIFF ──────────────────────────────────
//
// A checker was handed one sentence about the tree it was standing in — "its
// changes are staged, so `git diff --cached` shows all of them, new files
// included" — and that sentence was not true of the run it was written for. The
// files the work turned on were UNTRACKED: nothing had added them, so no diff
// showed them, and the reading that decided whether the work held never saw the
// core of it. It read the parts that happened to be tracked and judged the whole
// against them.
//
// THE LAW: A CHECK IS REACHED AGAINST THE GROUND'S FULL MANIFEST — what is
// staged, what is changed and not staged, and what is not tracked at all — and
// never against a diff alone. A diff is a view of the tracked half of a tree; it
// is the right thing to read and it is the wrong thing to survey by, because the
// one file it cannot show is the new one nobody added, which is exactly the file
// a piece of work is most likely to turn on.
//
// This is one function and one call site on purpose. It is the seam the checker
// asks the ground through, so a lane that changes what the checker CONSUMES has
// one thing to change and a lane that changes how the ground is READ has one
// place to change it.

const (
	// groundManifestHeading opens the block. It names the exact command, because
	// a reader that wants more than the manifest holds should be able to run the
	// same thing itself rather than guess at the flags.
	groundManifestHeading = "THE GROUND, AS GIT SEES IT (`git status --porcelain --untracked-files=all`):"
	// groundManifestUntracked is the sentence that makes the listing worth
	// carrying. A checker that reads `??` as noise is a checker back where it
	// started, so the block says outright what no diff will show it.
	groundManifestUntracked = "Lines beginning `??` are UNTRACKED: no diff shows them and `git diff --cached` " +
		"does not either. If any of them matter to the acceptance, open them with read."
	// groundManifestClean is what the block says about a tree git reports nothing
	// about. It is said rather than left silent because "the ground is clean" and
	// "nobody looked" are the same blank page, and only one of them is a finding.
	groundManifestClean = "Git reports no change in this tree at all — nothing staged, nothing modified, nothing untracked."
	// groundManifestLines bounds the listing. A hundred paths is a large change
	// and far more than a verdict is ever reached on; past it the block says how
	// many it did not name, so a reader knows it is looking at a prefix and can
	// run the command itself.
	groundManifestLines = 100
	groundManifestMore  = "…and %d more paths git reports here."
)

// groundManifest is the full state of the tree a check is reached in, as the
// checker reads it. It answers "" for a directory that is not a repository —
// there is no manifest to give, the packet's own sentence already says the
// workspace is not one, and an empty string renders as nothing, which is this
// codebase's emptiness law applied to a block of evidence.
//
// IT IS READ FROM THE GROUND AND NOT FROM THE WORK'S OWN COPY. Those are two
// different trees whenever the check is reached in a clean restore
// ([auditGroundFor]), and the packet's WHERE YOU ARE paragraph is a claim about
// the ground — so a manifest of anywhere else would be evidence about a tree the
// reader is not standing in.
//
// The private metadata directory is excluded exactly as [leftBehind] and
// [worktreeDirt] exclude it: a job's own log is this program's droppings and
// never the work's.
func groundManifest(dir string) string {
	out, err := git(dir, "status", "--porcelain", "--untracked-files=all",
		"--", ".", ":(exclude)"+aforgeDroppings)
	if err != nil {
		return ""
	}
	lines := nonEmptyLines(out)
	var body strings.Builder
	body.WriteString(groundManifestHeading + "\n")
	if len(lines) == 0 {
		body.WriteString(groundManifestClean + "\n")
		return body.String()
	}
	shown := lines
	if len(shown) > groundManifestLines {
		shown = shown[:groundManifestLines]
	}
	for _, line := range shown {
		body.WriteString(line + "\n")
	}
	if left := len(lines) - len(shown); left > 0 {
		body.WriteString(fmt.Sprintf(groundManifestMore, left) + "\n")
	}
	body.WriteString(groundManifestUntracked + "\n")
	return body.String()
}
