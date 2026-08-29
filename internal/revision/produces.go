package revision

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/plan"
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
