package revision

// ── the rules the person set, held against what the run changed ──────────────
//
// A CONSTRAINT IS A LAW OF THE RUN AND NEVER ADVICE. Every other finding in this
// gate is a reading: of the request, of the repository, of two photographs
// subtracted. This one is arithmetic over the person's own sentence — they said
// the run may change no files, or may change only these, and the workspace's own
// before-and-after list either agrees or it does not.
//
// It is held before a model is bought for the reason the regression is: the
// answer is already known and a judge's cost would buy nothing. It is held
// ABOVE the regression because it is the only finding here whose standard is the
// person's own words rather than a property of the tree — a run that broke a
// rule while leaving a green repository behind has still done the one thing it
// was told not to do.
//
// The measured case is #427: `aforge do "…report the final line it prints.
// Change no files."` did exactly that in its first leaf, and the repair nodes
// its own review spliced behind it wrote a test into an existing `_test.go` and
// a shell script at the workspace root. Twice out of two. Nothing in the gate
// read the constraint, nothing in the growth rules would refuse a round for it,
// and two of those rules actively rewarded the writing — "changed nothing on
// disk" was read as a repair that had not repaired.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// FindingConstraint names this measurement in the one stable word every other
// finding is named in, so a reader comparing rounds compares a kind and a list
// rather than two paragraphs. See Judgment.Finding.
const FindingConstraint = "constraint"

// constraintFilesNamed bounds how many changed files one broken rule spells
// out. It carries regressionsNamed's figure for regressionsNamed's reason: a
// run that broke "change no files" by running a build changed four hundred of
// them, and a gap that listed all four hundred is a gap nobody reads. The rest
// are counted.
const constraintFilesNamed = 8

// constraintBreak is one rule and the files the run changed in spite of it. It
// is the single measurement both renderings below are made from, because a
// finding computed twice is a finding whose two accounts eventually disagree.
type constraintBreak struct {
	rule  plan.Constraint
	files []string
}

// HoldConstraints is the mechanical half of the gate's newest law: the rules
// this delivery broke, each as the person's own words followed by the files the
// run changed in spite of them.
//
// It is exported because it is the measurement, and the record the store keeps
// (store.DeliveryGate.Constraint) is exactly what it returns. Nil is the answer
// for every job whose request stated no mechanical rule, which is nearly all of
// them, and nil is also the answer when nothing was changed at all — a run told
// to change nothing that changed nothing has kept the rule, which is the whole
// point of stating it.
func HoldConstraints(evidence Evidence) []string {
	breaks := constraintBreaks(evidence)
	if len(breaks) == 0 {
		return nil
	}
	broken := make([]string, 0, len(breaks))
	for _, entry := range breaks {
		broken = append(broken, entry.rule.Text+" — "+constraintFiles(entry.files))
	}
	return broken
}

// ConstraintsHeld is that measurement as a verdict, in the shape every other
// mechanical finding in this file takes, so the flow the gate already owns runs
// on it unchanged.
//
// SOURCED AND MECHANICAL, AND BOTH ARE TRUE OF IT. Mechanical, because the
// evidence is the filesystem and no refusal of a citation makes a written file
// unwritten. Sourced, because there is no citation to weigh in the first place:
// the quote IS the person's own sentence, carried verbatim from their request
// through the compile that could only keep what it could quote — so the
// grounding invariant every model judge's finding answers to is satisfied here
// by construction rather than by a check.
//
// ok is false — and the gate judges exactly as it did before this existed —
// whenever no rule was stated, no rule is mechanically readable, or no rule was
// broken.
func ConstraintsHeld(evidence Evidence) (judgment Judgment, ok bool) {
	breaks := constraintBreaks(evidence)
	if len(breaks) == 0 {
		return Judgment{}, false
	}
	broken := HoldConstraints(evidence)
	// The FIRST broken rule is the one the sentence quotes and the one a person
	// reads on the stream. Every broken rule is on the record either way; what
	// the line needs is one rule said whole, because a line that named three
	// rules at once is a line that names none of them clearly.
	first := breaks[0]
	quotes := make([]string, 0, len(breaks))
	for _, entry := range breaks {
		quotes = append(quotes, entry.rule.Text)
	}
	gap := fmt.Sprintf("The work broke a rule the person set: %q (%s).", first.rule.Text,
		constraintFileCount(first.files))
	gap += "\nFiles changed: " + constraintFiles(first.files) + "."
	if len(breaks) > 1 {
		gap += fmt.Sprintf("\nAnd %d more rule(s) of theirs, on the record.", len(breaks)-1)
	}
	return Judgment{
		Pass: false, Gaps: gap, Quote: first.rule.Text, Citations: quotes,
		Constraint: broken, Mechanical: true, Sourced: true, Checked: true,
		Finding: FindingConstraint,
	}, true
}

// constraintBreaks is the one reading: which stated rules a mechanical
// arithmetic can settle, and which files settle them against.
func constraintBreaks(evidence Evidence) []constraintBreak {
	changed := constraintChanges(evidence)
	if len(changed) == 0 {
		return nil
	}
	var breaks []constraintBreak
	for _, rule := range evidence.Constraints {
		var offending []string
		switch rule.Kind {
		case plan.ConstraintNoWrites:
			// The rule is that nothing changed, so every file that changed is
			// evidence of breaking it.
			offending = changed
		case plan.ConstraintPathsOnly:
			// A place the rule names can only be recognised relative to the
			// workspace, so a record with no workspace to be relative to holds
			// nothing here. That is the fail-safe direction: an unreadable rule
			// is one the leaf and the judge still see, and never one this
			// arithmetic guesses at.
			if strings.TrimSpace(evidence.Workspace) == "" {
				continue
			}
			for _, file := range changed {
				if !underAnyConstraintPath(file, rule.Paths) {
					offending = append(offending, file)
				}
			}
		default:
			// ConstraintOther, and every kind normalization has folded into it.
			// It is shown to the judge as the standard beside the request and is
			// settled by a reader, because arithmetic over a file list cannot
			// settle it and pretending otherwise would fail deliveries for
			// rules this code had misread.
			continue
		}
		if len(offending) == 0 {
			continue
		}
		breaks = append(breaks, constraintBreak{rule: rule, files: offending})
	}
	return breaks
}

// constraintChanges is what the run left behind, as the workspace-relative
// names a person would recognise.
//
// Two things are dropped, and each is dropped because it is not a change the
// person forbade. A path outside the workspace is the harness's own machinery by
// construction — the scratch directory that holds spilled observations, turn
// traces and job logs is deliberately outside the root (exec.Workspace.scratch)
// — and a person forbidding writes forbade writes to their own tree, not to our
// bookkeeping. And a path under a directory verify.SkipTree names is the same
// fact one level in: our dot-directories, the tooling's, and the dependency
// installs a `pip install` leaves behind.
//
// A PATH THE RECORD HOLDS IS A CHANGE WHETHER OR NOT IT IS STILL THERE. This is
// the one door that must not ask the disk whether the file exists, and it is the
// opposite of every other reading in this package: elsewhere a missing file
// convicts a claim that it was written, and here the question is whether the run
// touched the tree at all. A run told to change nothing that DELETED a file
// changed the tree in the loudest way there is, and a dangling symlink is the
// same event wearing a stat error. Only a directory is skipped, and only when
// the stat succeeded well enough to say so — a directory is where files live and
// never a file the run wrote.
func constraintChanges(evidence Evidence) []string {
	root := strings.TrimSpace(evidence.Workspace)
	seen := make(map[string]bool, len(evidence.Artifacts))
	changed := make([]string, 0, len(evidence.Artifacts))
	for _, path := range evidence.Artifacts {
		if path = strings.TrimSpace(path); path == "" {
			continue
		}
		name := filepath.ToSlash(path)
		if root != "" {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				continue
			}
			name = filepath.ToSlash(relative)
			if name == "." || outsideWorkspace(name) {
				continue
			}
		}
		if skippedConstraintPath(name) {
			continue
		}
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		changed = append(changed, name)
	}
	// Sorted because the record's own order is the order leaves happened to land
	// in, and a rule broken by the same two files must read the same way twice.
	sort.Strings(changed)
	return changed
}

// outsideWorkspace reads a workspace-relative path and answers whether it climbs
// out of the workspace. It tests the SEGMENT and never the prefix, because a
// file a person legitimately keeps at the root of their tree may perfectly well
// be called `..config` — and the prefix test rejected every one of them, which
// would have let a run told to change nothing quietly change any dotfile whose
// name began with two dots.
//
// The containment is lexical: a workspace reached through a symlink is not
// resolved here, and that is deliberate — it is how the artifact record spells
// its paths, and two spellings of one place is the drift this reading cannot
// afford.
func outsideWorkspace(relative string) bool {
	return relative == ".." || strings.HasPrefix(relative, "../")
}

// skippedConstraintPath answers whether any directory on the way to this file is
// somebody else's, on verify.SkipTree's one list.
func skippedConstraintPath(name string) bool {
	segments := strings.Split(name, "/")
	for _, segment := range segments[:max(len(segments)-1, 0)] {
		if segment != "" && verify.SkipTree(segment) {
			return true
		}
	}
	return false
}

// underAnyConstraintPath answers whether a changed file is inside one of the
// places the rule allows. A named file is itself, and a named directory is
// everything beneath it — which is how a person means it when they write
// "only touch docs/".
func underAnyConstraintPath(file string, paths []string) bool {
	for _, allowed := range paths {
		if file == allowed || strings.HasPrefix(file, strings.TrimSuffix(allowed, "/")+"/") {
			return true
		}
	}
	return false
}

// constraintFiles names the files, bounded, in the shape the regression finding
// names its checks.
func constraintFiles(files []string) string {
	named := files
	if len(named) > constraintFilesNamed {
		named = named[:constraintFilesNamed]
	}
	listed := strings.Join(named, ", ")
	if len(files) > len(named) {
		listed += fmt.Sprintf(", and %d more", len(files)-len(named))
	}
	return listed
}

// constraintFileCount is the same fact as a count, for the one line a person
// watching the run reads.
func constraintFileCount(files []string) string {
	if len(files) == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", len(files))
}

// ConstraintsBlock is the rules a reader has to settle, put in front of the
// judge as the standard beside the request.
//
// Only the ones no arithmetic could settle are here: a mechanical rule was
// already held above, and showing a judge a rule this gate has just cleared
// would invite it to convict on a second reading of an answered question.
// Empty — every job whose request stated no such rule — renders nothing at all.
func ConstraintsBlock(constraints []plan.Constraint) string {
	var judged []plan.Constraint
	for _, constraint := range constraints {
		if constraint.Kind == plan.ConstraintOther {
			judged = append(judged, constraint)
		}
	}
	return plan.ConstraintsBlock("Rules the person set, in their own words. "+
		"A rule they set and the work broke is a gap, and you quote the rule", judged)
}

// ConstraintQuoted stamps a MODEL JUDGE's refusal that turns out to be about a
// rule the person set, so that it is treated as one.
//
// The mechanical door above settles the two readings arithmetic can settle and
// returns before a model is bought. Everything else — "don't use the network",
// "keep it under two hundred words", "don't delete anything" — is shown to the
// judge as the standard beside the request, and the judge can and does convict
// on it. Until this, such a verdict was an ordinary gap: it bought the repair
// round and it bought the remainder, which is precisely the thing a broken rule
// may never buy. The finding was the right one and only its KIND was wrong.
//
// The match is the containment rule this program already uses for a behaviour
// (behaviourNamed), and it runs one way for the same reason: a quote that is
// part of a stated rule is that rule, quoted shorter, and a model asked for a
// verbatim span may reasonably give less than the whole of one. A quote that
// CONTAINS a rule is a longer span of the request that happens to have a rule
// inside it, and reading that as a broken rule would refuse rounds for gaps that
// are nothing of the sort.
//
// Only ConstraintOther is matched, and that is not an oversight. A mechanical
// rule was already held above and found KEPT — that is why there was a model
// round at all — so a judge convicting on one is convicting on a question this
// gate has already answered against it, and the honest reading of that verdict
// is whatever else it says.
func ConstraintQuoted(judgment Judgment, constraints []plan.Constraint) Judgment {
	if judgment.Pass || len(judgment.Constraint) > 0 {
		return judgment
	}
	quote := constraintFold(judgment.Quote)
	if quote == "" {
		return judgment
	}
	for _, constraint := range constraints {
		if constraint.Kind != plan.ConstraintOther {
			continue
		}
		rule := constraintFold(constraint.Text)
		if rule == "" || !strings.Contains(rule, quote) {
			continue
		}
		judgment.Finding = FindingConstraint
		judgment.Constraint = []string{constraint.Text}
		return judgment
	}
	return judgment
}

// constraintFold is one sentence's identity for the purposes of matching it
// against another: its words, whitespace-normalized and case-folded. It is the
// same reading head.keepStatedConstraints holds the compiler to and
// plan.NormalizeConstraints dedupes on, so a rule kept there, deduped there and
// matched here agree about what "the same words" means.
func constraintFold(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}
