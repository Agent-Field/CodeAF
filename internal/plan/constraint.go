package plan

// A constraint is a rule the person stated about what the run may or may not
// DO, as distinct from what it must produce.
//
// This is the fact nothing in the system held. The compiled brief carries a
// goal, assumptions, scale, a contract and parts; the acceptance checklist
// holds the behaviours the request states about the RESULT. "Change no files"
// is neither of those — it is a rule about the run itself — so it survived only
// as prose inside the goal, and prose is not a thing a gate can hold anything
// to. A headless errand told "Change no files." ran the command it was asked
// to run, reported the line it was asked to report, and was then sent back by
// its own review to write a test and a shell script into the workspace (#427).
//
// So a constraint is a field, on the brief and on the spec of EVERY node of the
// job, and it is held mechanically against what the run left behind. The two
// halves of that sentence are why the kinds below exist: `no_writes` and
// `paths_only` are the readings the delivery gate can settle from the
// workspace's own before-and-after list without paying a model, and `other` is
// every rule that needs a reader — shown to the judge as the standard beside
// the request, never settled by arithmetic.
//
// THE PERSON'S OWN WORDS ARE THE WHOLE OF WHAT IS HELD. Text is verbatim, the
// compiler may only keep a constraint it can quote out of the instruction (see
// head.keepStatedConstraints), and the gate's refusal quotes it back — so the
// grounding invariant every other finding in this program answers to is
// satisfied here by construction rather than by a citation check.

import (
	"path/filepath"
	"strings"
)

// Constraint is one such rule: the person's words, the mechanical reading of
// them, and the paths that reading is about when it has any.
type Constraint struct {
	// Text is the person's own words for this rule, verbatim. It is what the
	// leaf is shown, what the gate quotes when it refuses, and the only thing
	// anybody is ever held to.
	Text string `json:"text"`
	// Kind is the mechanical reading: one of the three constants below. An
	// unrecognised kind normalizes to ConstraintOther, which is the weaker
	// claim — a rule nothing mechanical settles is still a rule the leaf reads
	// and the judge is shown.
	Kind string `json:"kind"`
	// Paths are the places a ConstraintPathsOnly rule allows, in the person's
	// own spelling, cleaned to workspace-relative slash paths. Empty on every
	// other kind.
	Paths []string `json:"paths,omitempty"`
}

// The three readings a constraint can carry.
//
// Two of them are mechanical because two of them are answerable from the
// workspace's own before-and-after list, which the run already takes for other
// reasons: whether anything changed, and whether what changed is inside a named
// set. Everything else — do not use the network, do not delete, keep it under
// two hundred words — is ConstraintOther and is judged by a reader, because
// arithmetic over a file list cannot settle it and a mechanism that pretended
// otherwise would fail deliveries for rules it had misread.
const (
	ConstraintNoWrites  = "no_writes"
	ConstraintPathsOnly = "paths_only"
	ConstraintOther     = "other"
)

// ConstraintsHeading is what a leaf reads above its own assignment. It is a
// constant because the leaf's brief, the repair round's input and the spec's
// own render all write it, and three spellings of one law is three laws.
const ConstraintsHeading = "Rules the person set, never broken"

// ConstraintsOutrankHeading is the same list said to a worker that is holding
// somebody else's finding as well: a repair round is handed a reviewer's gap
// and told to close it, and the one thing it may not do is close it by breaking
// a rule. The heading says which of the two wins before the worker reads either.
const ConstraintsOutrankHeading = "Rules the person set, which outrank anything below"

// NormalizeConstraints bounds and cleans what a model returned, on the same
// terms NormalizeDone and NormalizeAcceptance use: a rule that cannot be read
// is dropped rather than refused, and an unrecognised reading falls back to the
// weaker one.
//
// A ConstraintPathsOnly rule whose path list is empty after cleaning becomes
// ConstraintOther, and that is the fail-safe direction rather than tidiness:
// "only these paths" with no paths reads mechanically as "no paths at all",
// which would fail every delivery of a job whose model wrote the kind and
// forgot the list. The rule is still shown to the leaf and to the judge in the
// person's own words; only the arithmetic is declined.
func NormalizeConstraints(constraints []Constraint) []Constraint {
	clean := make([]Constraint, 0, len(constraints))
	seen := map[string]bool{}
	for _, constraint := range constraints {
		text := strings.TrimSpace(constraint.Text)
		if text == "" {
			continue
		}
		key := constraintKey(text)
		if seen[key] {
			// One rule said twice is one rule, and two copies of it would quote
			// the same sentence twice in every refusal it ever earns.
			continue
		}
		seen[key] = true
		kind := strings.ToLower(strings.TrimSpace(constraint.Kind))
		paths := cleanConstraintPaths(constraint.Paths)
		switch kind {
		case ConstraintNoWrites:
			paths = nil
		case ConstraintPathsOnly:
			if len(paths) == 0 {
				kind = ConstraintOther
			}
		default:
			kind, paths = ConstraintOther, nil
		}
		clean = append(clean, Constraint{Text: text, Kind: kind, Paths: paths})
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
}

// constraintKey is one rule's identity for the purposes of saying it twice:
// its words, whitespace-normalized and case-folded. It is the same reading
// NormalizeAcceptance dedupes quotes on, and the same one the compiler's
// substring guard uses, so a rule kept there and a rule deduped here agree.
func constraintKey(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

// cleanConstraintPaths turns what a model wrote into paths the gate can compare
// against the record: trimmed, slash-spelled, and relative to the workspace,
// because that is what the record's own relative spelling is.
func cleanConstraintPaths(paths []string) []string {
	clean := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		path = filepath.ToSlash(filepath.Clean(path))
		if path = strings.TrimPrefix(path, "./"); path == "" || path == "." {
			continue
		}
		clean = append(clean, path)
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
}

// ConstraintLines is the rules as a person reads them: one line per rule, the
// text verbatim, and nothing added. Every renderer that shows constraints —
// the spec, the leaf's brief, the repair round's input — writes these lines, so
// there is one wording of the law and no surface can paraphrase it.
func ConstraintLines(constraints []Constraint) []string {
	lines := make([]string, 0, len(constraints))
	for _, constraint := range constraints {
		if text := strings.TrimSpace(constraint.Text); text != "" {
			lines = append(lines, "- "+text)
		}
	}
	return lines
}

// ConstraintsBlock is those lines under a heading, or the empty string when
// there are no rules. Empty renders as nothing at all, which is the emptiness
// law and also the whole compatibility story: a job whose request stated no
// rule sends exactly the bytes it sent before this existed.
func ConstraintsBlock(heading string, constraints []Constraint) string {
	lines := ConstraintLines(constraints)
	if len(lines) == 0 {
		return ""
	}
	return heading + ":\n" + strings.Join(lines, "\n")
}

// RulesAbove puts the rules in front of whatever a worker was going to be told,
// which is the only placement that means anything: a rule read after the
// assignment it governs is a rule the assignment has already argued with.
// Returns body unchanged when there are no rules.
func RulesAbove(constraints []Constraint, body string) string {
	return rulesAbove(ConstraintsHeading, constraints, body)
}

// RulesOutranking is the same placement for the one text where the worker is
// holding two orders at once: a repair round is handed a reviewer's gap and
// told to close it, and the heading says which of the two wins before it has
// read either. Returns body unchanged when there are no rules.
func RulesOutranking(constraints []Constraint, body string) string {
	return rulesAbove(ConstraintsOutrankHeading, constraints, body)
}

func rulesAbove(heading string, constraints []Constraint, body string) string {
	block := ConstraintsBlock(heading, constraints)
	if block == "" {
		return body
	}
	if strings.TrimSpace(body) == "" {
		return block
	}
	return block + "\n\n" + body
}

// SetConstraints stamps the job's rules on EVERY node's spec, and that is the
// one way it differs from SetAcceptance beside it.
//
// The checklist belongs to whoever hands the finished thing over, because it is
// the list of behaviours the whole request states and a contributing worker was
// never asked for those. A CONSTRAINT IS A PROPERTY OF THE JOB AND NOT OF ANY
// ONE NODE OF IT. The person who said "change no files" said it about the run,
// so every worker the run ever starts is under it — including the ones spliced
// later by a repair round or a remainder, which is exactly where #427's files
// were written.
func (g *Graph) SetConstraints(constraints []Constraint) {
	if g == nil || len(constraints) == 0 {
		return
	}
	for index := range g.Nodes {
		g.Nodes[index].Spec.Constraints = constraints
	}
}
