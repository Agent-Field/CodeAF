package exec

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// THE LEAF'S GRANT IS ONE NUMBER AND NOBODY SPELLS IT TWICE.
//
// [DefaultLeafTokens] is calibrated against measured work, and recalibrating it
// is a reading somebody takes and writes down. That reading is worthless if a
// second copy of the figure sits at a door: `aforge exec`, `aforge run` and the
// chat surface each carried their own `150000`, and chat's even carried a
// comment promising it "mirrors the headless run defaults exactly" — which is
// an intention where this repository's one-source-of-truth law wants an
// interpolation. Four numbers had to be moved together, by somebody who knew
// all four were there, and the help text at a door could tell a person a figure
// the loop had stopped using.
//
// IT READS EVERY INTEGER LITERAL AND NOT ONLY DECLARED ONES. Its first draft
// inspected `*ast.ValueSpec`, which would have caught exactly ONE of the four
// copies in the story above: the other three were call arguments —
// `newCountFlag(flags, "token-budget", 150000, …)` and `flags.Int("token-budget",
// 150000, …)` — and a flag default is the most likely place for this figure to
// be written a second time, because that is where a door states what it does
// when nobody says otherwise. A law that misses the shape it was written for is
// decoration.
//
// WHERE IT LOOKS, AND WHY NOT EVERYWHERE. A door that states a default is the
// shape this law is about: `--token-budget`'s default, the chat surface's own
// constant, a test that asserts what a door does when nobody says otherwise.
// Inside the package that OWNS the constant, an integer equal to it is usually
// a fixture — `NewLinear(client, space, nil, 50, 150_000, time.Minute)` is a
// test choosing a size, not a second opinion about the default — and there are
// thirteen of them. Policing those would force every fixture to move with a
// recalibration and would say nothing true about drift. So: every file under
// the surfaces, and only the non-test files of internal/exec.
//
// WHAT IT CANNOT SEE, SAID PLAINLY RATHER THAN GUESSED AT. It watches the
// CURRENT figure, so a copy left behind by a recalibration — a door still
// saying what the loop used to do — is invisible to it until somebody sweeps
// for the old number by hand, and a recalibration owes that sweep. Watching the
// past figures instead was tried and withdrawn: 400,000 was a grant once and is
// also an ordinary round number, and the law then failed an unrelated spend
// receipt and an unrelated context length for spelling it. A law that cries
// wolf is worse than one with a stated blind spot. It is also blind to a figure
// written in hex or in some other underscore grouping, and to the manual's own
// prose, which is markdown and out of an AST law's reach — internal/manual's
// TestEveryFigureAChatPageQuotesComesFromTheCodeThatOwnsIt already owns that
// half, checking the sentence adaptive-runs.md writes the grant into against
// `aforge exec --token-budget`'s default. A second law for the same claim was
// written here and deleted: two gates on one figure is the drift this file
// exists to stop, wearing a test's clothes.
func TestTheLeafGrantIsSpelledOnce(t *testing.T) {
	root := repoRoot(t)
	watched := map[string]bool{}
	for _, spelling := range goSpellings(DefaultLeafTokens) {
		watched[spelling] = true
	}
	// The declaration of the constant itself is the one place the figure belongs,
	// and this file names every past figure on purpose.
	exempt := map[string]bool{
		filepath.Join(root, "internal", "exec", "linear.go"):             false,
		filepath.Join(root, "internal", "exec", "leafgrant_law_test.go"): true,
	}
	for _, place := range leafGrantWatched {
		walkGoFiles(t, filepath.Join(root, place.dir), func(path string, file *ast.File, fset *token.FileSet) {
			if exempt[path] || (place.doorsOnly && strings.HasSuffix(path, "_test.go")) {
				return
			}
			// The constant's own declaration, by position, so the exemption is
			// the declaration and never the whole file it lives in.
			var declared token.Pos
			ast.Inspect(file, func(n ast.Node) bool {
				spec, ok := n.(*ast.ValueSpec)
				if !ok {
					return true
				}
				for i, name := range spec.Names {
					if name.Name == "DefaultLeafTokens" && i < len(spec.Values) {
						declared = spec.Values[i].Pos()
					}
				}
				return true
			})
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.INT || lit.Pos() == declared {
					return true
				}
				if !watched[lit.Value] {
					return true
				}
				where := fset.Position(lit.Pos())
				t.Errorf("the leaf's grant is spelled again as %s at %s:%d\n"+
					"read it from exec.DefaultLeafTokens — the figure is calibrated in one place, and a copy "+
					"of it is a door telling somebody what the loop does when it no longer does that",
					lit.Value, strings.TrimPrefix(where.Filename, root+"/"), where.Line)
				return true
			})
		})
	}
}

// THE HEADLESS REFERENCE STATES THE GRANT TOO, AND IT IS NOT MARKDOWN THIS
// PACKAGE MAY IGNORE.
//
// docs/HEADLESS.md's flag table gives a default for every flag `aforge exec`
// and `aforge run` take, and the `--token-budget` row is the grant written out
// for somebody reading the reference instead of the help. It is the SEVENTH
// place this figure has lived, and it was the one still saying 150000 after
// #918 wrote the constant once and after internal/manual's truth table took
// custody of the chat corpus's copy — found by sweeping for the old number by
// hand, which is the blind spot the law above states plainly and this one
// closes for the one document where it bit.
//
// It checks the flag's own row and not merely the page, because a reference
// mentions a number in several sentences and only one of them is the default a
// reader will type. The row's shape is the gate: a `--token-budget N` cell
// followed by the default in backticks. If the table is reshaped the law goes
// red rather than quiet, and reshaping it is a deliberate act that can afford
// to edit one line here.
func TestTheHeadlessReferenceStatesTheGrantTheDoorsApply(t *testing.T) {
	const page = "docs/HEADLESS.md"
	path := filepath.Join(repoRoot(t), filepath.FromSlash(page))
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s, so this law cannot see what it tells a reader: %v", page, err)
	}
	row := regexp.MustCompile("`--token-budget N`[^|]*\\|\\s*`(\\d+)`")
	match := row.FindStringSubmatch(string(text))
	if match == nil {
		t.Fatalf("%s no longer has a --token-budget row with a number for its default.\n"+
			"Teach this law the table's new shape rather than dropping the check, or the grant "+
			"stops being answerable to the reference a person reads instead of --help", page)
	}
	if want := strconv.Itoa(DefaultLeafTokens); match[1] != want {
		t.Errorf("%s says --token-budget defaults to %s; exec.DefaultLeafTokens is %s.\n"+
			"A reference that states a default is read by somebody deciding whether to pass the flag at all",
			page, match[1], want)
	}
}

// leafGrantWatched is where a spelling of the grant is a door restating a
// default rather than a test choosing a size. doorsOnly excludes the tests of
// the package that owns the constant, for the reason in the law's head comment.
var leafGrantWatched = []struct {
	dir       string
	doorsOnly bool
}{
	{dir: "cmd/codeaf"},
	{dir: "internal/session"},
	{dir: "internal/exec", doorsOnly: true},
}

// goSpellings is an integer as Go's integer literals may spell it: the plain
// digits, and the underscore grouping a person writes a large constant in.
// A figure written in hex or in some other grouping is out of this law's reach
// and is named in its head comment rather than guessed at here.
func goSpellings(value int) []string {
	plain := strconv.Itoa(value)
	grouped := plain
	for at := len(plain) - 3; at > 0; at -= 3 {
		grouped = grouped[:at] + "_" + grouped[at:]
	}
	if grouped == plain {
		return []string{plain}
	}
	return []string{plain, grouped}
}

// walkGoFiles parses every Go file under dir and hands it over. A file it
// cannot parse is reported rather than skipped: a law that goes quiet on a
// broken file is a law with a hole in it that nobody can see.
func walkGoFiles(t *testing.T, dir string, visit func(string, *ast.File, *token.FileSet)) {
	t.Helper()
	fset := token.NewFileSet()
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Errorf("cannot read %s, so this law cannot see what it spells: %v", path, parseErr)
			return nil
		}
		visit(path, file, fset)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
}
