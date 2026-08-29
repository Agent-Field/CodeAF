package revision

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// MissingProduces is the mechanical half of the delivery gate: the one fact a
// judge should never be paid to discover, settled before a model round is
// bought.
//
// When the plan's stopping criterion names files — and only then; a file name
// is never inferred from prose — every named file must be on disk and
// non-empty before a judge is asked anything. A missing or empty named file is
// a gate failure that names the absent files verbatim, in the same shape a
// judged gap takes, so the one-round repair flow the gate already owns runs on
// it unchanged. The model judge is skipped for that round: its cost buys
// nothing when the absence is a fact about the filesystem rather than a
// question about the text.
//
// ONE CITATION PER MISSING FILE. That is the whole of what this gate claims,
// and for a while it was not what it said. The gap was one comma-joined string
// handed to a rule that asks whether a citation is a verbatim span of the ask,
// and a list of five paths is a verbatim span of nothing — so a gate that had
// correctly found five promised files absent was refused as an invention, no
// repair round was bought, and a run that wrote no file at all settled as done.
// The list is what makes the sentence below true per file, which is how it was
// always meant to read: a file the person named buys its round, and a file the
// plan named alone is refused, on the same invariant that already bounds every
// other gap. See AdmitGapCitation, which weighs each citation on its own and
// admits a file the ask names under either spelling.
//
// Mechanical is set because a refusal downstream means something different
// here than it does for a judge's opinion. A judge can be wrong about whether
// a deliverable answers the ask; nothing can be wrong about whether a file is
// on disk. Refusing this gap declines to buy a repair round and settles
// nothing about whether the work landed — see deliveredWhole in
// cmd/aforge/do.go, which is where the difference is spent.
//
// ok is false — and the caller judges exactly as it did before this existed —
// when the criterion named no files, or when every named file is present and
// non-empty. The path is byte-identical to before in both cases: the judgment
// is the model's, the spend is the model's, and nothing here ran.
func MissingProduces(done plan.Done, artifacts []string) (judgment Judgment, ok bool) {
	var missing []string
	for _, name := range producesFiles(done) {
		if !producedNonEmpty(name, artifacts) {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return Judgment{}, false
	}
	gap := joinCitations(missing)
	return Judgment{
		Pass: false, Gaps: gap, Quote: gap,
		Citations: missing, Mechanical: true, Checked: true,
	}, true
}

// producesFiles lists the produces entries that name a file rather than an
// abstract output.
//
// An entry counts only when it IS a path with an extension — "report.md" or
// "docs/summary.txt" — and never when it merely mentions a file inside a
// sentence, so a deliverable name is never inferred from prose. The plan's own
// structured field is the sole source; the request's prose is not read here,
// because the request names what the person wants and the plan names what done
// means, and the gap between the two is the planner's to close.
func producesFiles(done plan.Done) []string {
	var files []string
	for _, produces := range done.Produces {
		if name, ok := citedFile(produces); ok {
			files = append(files, name)
		}
	}
	return files
}

// citedFile reads a piece of text that is supposed to BE a file name and
// returns the name it is, or reports that it is not one.
//
// It is the same question in both places that ask it: a produces entry the gate
// may settle against the disk, and a citation the grounding rule may weigh as a
// file identity rather than as a span of prose. The answer is yes only when the
// text names exactly one file AND is that name rather than a sentence holding
// it — "the write-up" names nothing, "see report.md" mentions one, and only
// "report.md" is a name. Anything looser and a gate would infer a deliverable
// from prose, which is precisely what neither caller is allowed to do.
func citedFile(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	matched := NamedFiles(text)
	if len(matched) != 1 || !sameFile(matched[0], text) {
		return "", false
	}
	return matched[0], true
}

// sameFile reports whether two spellings are one spelling, after the cleaning
// NamedFiles already applies. It is the strict test — no directory is inferred
// and none is dropped — and it is what citedFile uses to say that an entry is
// the name itself rather than a sentence containing it.
func sameFile(a, b string) bool {
	return fileKey(a) == fileKey(b)
}

// namedAs is this package's one law about when a spelling of a file name is
// satisfied by another string, stated in the direction the delivery record asks
// it: does this candidate path answer to that name?
//
// A name carrying a directory names that place: "docs/memo.md" is answered by a
// path ending in docs/memo.md and by nothing else, which is exactly the case a
// leaf lost when it wrote the right content at the wrong address. A bare name
// names the file wherever it landed, because the person who wrote "report.md"
// said nothing about which directory.
//
// It lives here rather than inside ProducedFile because two questions turn out
// to be this question: whether a run produced the file a plan promised, and
// whether the file a review is missing is the file the person asked for. They
// were answered by two different rules for a while, and the second one — plain
// string equality — is what refused a person's own "breakpoints_test.go"
// against the plan's "internal/tui3/breakpoints_test.go".
func namedAs(named, candidate string) bool {
	want, have := fileKey(named), fileKey(filepath.ToSlash(candidate))
	if want == "" || have == "" {
		return false
	}
	if have == want || strings.HasSuffix(have, "/"+want) {
		return true
	}
	return !strings.Contains(want, "/") && path.Base(have) == want
}

// namesSameFile is that law asked without a direction, which is the shape the
// grounding rule needs: two pieces of text, either of which may be the more
// specific spelling, naming one file. The person may write the bare name and
// the plan resolve it, or the person may write the path and a review shorten
// it; both are the same file, and neither is an invention.
func namesSameFile(a, b string) bool {
	return namedAs(a, b) || namedAs(b, a)
}

// fileKey is the form every comparison above is made in: trimmed of
// surrounding space, of a leading slash, of a leading "./", and lowercased,
// because a name is a name whichever case it was typed in.
func fileKey(s string) string {
	return strings.ToLower(strings.TrimPrefix(strings.Trim(strings.TrimSpace(s), "/"), "./"))
}

// producedNonEmpty is ProducedFile with the size the gate cares about: a file
// the worker left at zero bytes is a file that was not written, and a gate
// that counted it as produced would pass a deliverable that exists only as a
// name on disk.
func producedNonEmpty(named string, artifacts []string) bool {
	found, ok := ProducedFile(named, artifacts)
	if !ok {
		return false
	}
	info, err := os.Stat(found)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Size() > 0
}

// regressionsNamed bounds how many failing checks one finding spells out. A
// worker that breaks an import breaks every test in the file, and a gap that
// listed four hundred of them is a gap no repair can read and no person can
// take in; the rest are counted, which is the same shape describeFailing uses
// on the other side of the same measurement. Eight is what a person reads
// before their eye slides off a list.
const regressionsNamed = 8

// Regressions is the second mechanical half of the delivery gate, and the one
// whose evidence comes from the WORLD rather than from the plan.
//
// A check that passed before this work and fails after it is a fact the run
// measured for itself, twice, with the project's own command. It is a gate
// failure that names the failing checks verbatim, in the same shape a judged
// gap takes, so the one-round repair flow the gate already owns runs on it
// unchanged — and the model judge is skipped for that round, because its cost
// buys nothing when the failure is a fact about a test runner's output rather
// than a question about the text.
//
// SOURCED, NOT MECHANICAL, AND THE DIFFERENCE IS WHERE THE EVIDENCE CAME FROM.
// A mechanical gap reads the plan's promises against the disk; refusing its
// citation is possible and merely pointless. This gap has no citation to weigh
// at all: the request never said "and do not break the tests", because nobody
// has to. Grounding it would refuse it every single time, which is exactly the
// shape of the three graded runs that shipped patches deleting attributes their
// repositories already had while their own narrow tests stayed green
// (2026-08-28, bench/deepswe; docs/design/gate/SETTLEMENT.md §4).
//
// ok is false — and the caller judges exactly as it did before this existed —
// when nothing was measured or nothing turned red. A worker that cannot take
// two readings hands back nil, which reads as no claim and never as no
// regression.
func Regressions(regressed []string) (judgment Judgment, ok bool) {
	broke := make([]string, 0, len(regressed))
	for _, name := range regressed {
		if name = strings.TrimSpace(name); name != "" {
			broke = append(broke, name)
		}
	}
	if len(broke) == 0 {
		return Judgment{}, false
	}
	named := broke
	if len(named) > regressionsNamed {
		named = named[:regressionsNamed]
	}
	gap := "This work broke checks that were passing before it: " + joinCitations(named) + "."
	if len(broke) > len(named) {
		gap += fmt.Sprintf(" And %d more.", len(broke)-len(named))
	}
	gap += " They were measured twice with the project's own command, before the work and after it."
	return Judgment{
		Pass: false, Gaps: gap, Quote: joinCitations(named),
		Citations: named, Sourced: true, Checked: true,
	}, true
}

// OwnChecksFailing is the finding for a leaf whose OWN new checks are red.
//
// It is what the regression finding used to say instead, in words that were not
// true. A run writes checks — that is most of what a run does — and happy-dom's
// nemotron n1 run rewrote the file its reading was scoped to, taking it from 4
// checks to 33 with eighteen of the new ones red. Both readings ran the
// identical command, so nothing looked widened and nothing looked wrong, and the
// gate failed the delivery with `This work broke checks that were passing before
// it: IntersectionObserver initial observation queuing …` — naming checks that
// did not exist when the baseline was taken — while the grader scored that same
// tree 9 of 9.
//
// A LEAF WHOSE OWN CHECKS ARE RED HAS NOT FINISHED; A LEAF THAT TURNED SOMEBODY
// ELSE'S CHECK RED HAS BROKEN THE REPOSITORY. Both are worth a round and only
// one of them is a regression, so they are two findings and the words say which.
//
// SOURCED, like the regression and the removed name, and for the same reason:
// there is no citation to weigh. The person asked for the behaviour; that the
// checks written for it do not pass is a measurement of the repository rather
// than a reading of the request.
func OwnChecksFailing(failing []string) (judgment Judgment, ok bool) {
	red := make([]string, 0, len(failing))
	for _, name := range failing {
		if name = strings.TrimSpace(name); name != "" {
			red = append(red, name)
		}
	}
	if len(red) == 0 {
		return Judgment{}, false
	}
	named := red
	if len(named) > regressionsNamed {
		named = named[:regressionsNamed]
	}
	gap := "The checks this work wrote fail: " + joinCitations(named) + "."
	if len(red) > len(named) {
		gap += fmt.Sprintf(" And %d more.", len(red)-len(named))
	}
	gap += " They were not in the project's roster when this job started, so they are this " +
		"work's own — nothing was broken by them being red, and nothing is finished while " +
		"they are. Make them pass, or say in the deliverable why a check this work added " +
		"is expected to fail."
	return Judgment{
		Pass: false, Gaps: gap, Quote: joinCitations(named),
		Citations: named, OwnFailing: red, Sourced: true, Checked: true,
	}, true
}

// RemovedPublicNames is the symbol-level half of the same measurement, and the
// half a test suite structurally cannot make.
//
// A CHECK IS EVIDENCE THAT SOMETHING IS EXERCISED; IT IS NOT EVIDENCE THAT
// NOTHING ELSE EXISTS. igel s11 deleted eight public class attributes off `Igel`
// — `results_path` among them — and moved them onto instances set in `__init__`.
// No check that project owns touches any of them, so the reading of the finished
// tree came back an IMPROVEMENT: named 2 → 14, red 2 → 0. Every one of the
// twenty-four hidden tests failed at setup on `Igel.results_path`, and the run
// held not one word about it. There was nothing wrong with the reading. The
// question it answers is simply not this one.
//
// SOURCED, NOT MECHANICAL, for the reason Regressions is: there is no citation
// to weigh. The request never said "and do not delete the class's public
// attributes", because nobody has to, and a door that asked for a quotation
// would refuse this finding every single time.
//
// It names the removal and not a remedy. Putting the name back and keeping the
// new arrangement are both answers, and which one is right is the repair round's
// business — this says only what the world lost.
//
// ok is false when nothing was measured or nothing was lost. A worker that
// cannot take two readings of the tree hands back nil, which reads as no claim
// and never as nothing removed.
func RemovedPublicNames(removed []string) (judgment Judgment, ok bool) {
	gone := make([]string, 0, len(removed))
	for _, name := range removed {
		if name = strings.TrimSpace(name); name != "" {
			gone = append(gone, name)
		}
	}
	if len(gone) == 0 {
		return Judgment{}, false
	}
	named := gone
	if len(named) > regressionsNamed {
		named = named[:regressionsNamed]
	}
	gap := "This work removed a public name that existed before it: " + joinCitations(named) + "."
	if len(gone) > len(named) {
		gap += fmt.Sprintf(" And %d more.", len(gone)-len(named))
	}
	gap += " The public surface of the files this work changed was read twice, before the " +
		"work and after it, and these names are in the first reading and not the second. " +
		"Anything outside this run that used them is broken by it. Restore them, or say " +
		"in the deliverable why removing them was what the request asked for."
	return Judgment{
		Pass: false, Gaps: gap, Quote: joinCitations(named),
		Citations: named, Sourced: true, Checked: true,
	}, true
}

// producedSweepLimit bounds the walk that settles a named file against the
// world. It is internal/exec's producedScanLimit read from the other end and
// carries the same figure for the same reason: a workspace is usually a handful
// of files, a person's repository is not, and a walk whose size is nobody's plan
// stops rather than going on forever. Past it the answer is whatever was found,
// which is the narrower answer and never a wrong one.
const producedSweepLimit = 6000

// completeAgainstTheWorld settles every file this delivery is ABOUT against the
// tree it was produced in, and adds what it finds to the record.
//
// FAILSAFE clause 2, applied to the last record in this gate that was still an
// account rather than an observation. What a run left behind is answered by the
// filesystem, and the artifact registry is a report of it: a leaf that landed
// under another node's key, a file a background job wrote after the leaf's own
// sweep, a path recorded by a worker this process never held. igel s6 is the
// measured case — the request asked for `feature_schema.joblib`, the gate said
// "nothing of that name was left behind", and `model_results/feature_schema.joblib`
// was on disk and in the graded patch. The judge then convicted a correct
// deliverable of not having written it.
//
// A FILE ANYWHERE UNDER THE WORKSPACE THAT ANSWERS TO THE NAME IS PRODUCED, and
// namedAs is what "answers to the name" means — the same law, read by the same
// code, that decides it for the registry. A bare name is answered wherever the
// file landed and a name carrying a directory is answered only at that place, so
// a request naming `feature_schema.joblib` is closed by the nested path and a
// request naming `docs/memo.md` is not closed by one in `notes/`.
//
// NOTHING HERE LOOKS AT WHAT IS INSIDE A FILE. A joblib, a compiled model, a PNG
// are deliverables exactly as a Markdown file is; the registry has always known
// that and this is the reading that has to agree with it. Only emptiness
// disqualifies, and it disqualifies at the caller that cares (producedNonEmpty).
//
// It is asked ONLY about names this delivery already holds — what the request
// named and what the plan promised — so it is one bounded walk that answers a
// closed question, never a re-inventory of the tree.
func (e *Evidence) completeAgainstTheWorld() {
	root := strings.TrimSpace(e.Workspace)
	if root == "" {
		return
	}
	var wanted []string
	for _, name := range append(append([]string{}, e.Named...), producesFiles(e.Done)...) {
		if fileKey(name) == "" {
			continue
		}
		if _, held := ProducedFile(name, e.Artifacts); held {
			continue
		}
		wanted = append(wanted, name)
	}
	if len(wanted) == 0 {
		return
	}
	found := sweepFor(root, wanted)
	if len(found) == 0 {
		return
	}
	e.Artifacts = append(append([]string{}, e.Artifacts...), found...)
}

// sweepFor is that one walk: every path under root that answers to one of these
// names, as an absolute path in stable order.
func sweepFor(root string, wanted []string) []string {
	var found []string
	visited := 0
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if visited++; visited > producedSweepLimit {
			return fs.SkipAll
		}
		if entry.IsDir() {
			// The harness's own machinery and somebody else's installed
			// packages are not this run's deliverables, and a node_modules is
			// forty thousand entries of proof that the walk must not enter them.
			if path != root && verify.SkipTree(entry.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		relative = filepath.ToSlash(relative)
		for _, name := range wanted {
			if namedAs(name, relative) {
				found = append(found, path)
				break
			}
		}
		return nil
	})
	sort.Strings(found)
	return found
}
