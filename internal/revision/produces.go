package revision

import (
	"os"
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
	gap := strings.Join(missing, ", ")
	// The quote is the names verbatim, the same span a judged gap would carry:
	// it is what the repair path grounds against the ask, so a file the person
	// named buys its round and a file the plan named alone is refused on the
	// same invariant that already bounds every other gap.
	return Judgment{Pass: false, Gaps: gap, Quote: gap, Checked: true}, true
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
		name := strings.TrimSpace(produces)
		if name == "" {
			continue
		}
		matched := NamedFiles(name)
		// One entry, and the entry is the name rather than a sentence holding
		// one: "the write-up" names nothing, "see report.md" mentions one, and
		// only "report.md" is a file this gate may settle against the disk.
		if len(matched) != 1 || !sameFile(matched[0], name) {
			continue
		}
		files = append(files, matched[0])
	}
	return files
}

// sameFile reports whether two spellings name one file, after the cleaning
// NamedFiles already applies. It is the test that the entry is the name
// itself rather than a sentence containing it.
func sameFile(a, b string) bool {
	return strings.EqualFold(fileKey(a), fileKey(b))
}

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
