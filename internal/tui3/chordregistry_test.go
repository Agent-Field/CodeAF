package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ── THE CHORD REGISTRY: WHO CLAIMS WHICH CHORD, IN WHICH CONTEXT ─────────────
//
// A chord is a claim on the keyboard, and two handlers that claim the same
// chord in the same context are a bug that no single file can see. This test
// holds the one table of claims and fails three ways:
//
//  1. a chord constant declared in the package that the table does not name,
//     so a new chord cannot land without saying where it works;
//  2. a table row whose name is not a chord constant, or whose context is
//     empty, so the table cannot drift from the source;
//  3. two rows with the same chord in one context, or a row in [ctxAny]
//     beside any other row with that chord.
//
// A context is the surface that owns the keyboard when the chord arrives. The
// same chord may live in two different contexts (alt+m is the devices row on
// home, the team manager in a conversation, and "mark" in the folder picker,
// and each is shut off where the others are open); it may not live twice in
// one.

const (
	ctxAny          = "any"
	ctxHome         = "home"
	ctxBox          = "box"
	ctxConversation = "conversation"
	ctxRail         = "rail"
	ctxFolderPicker = "folder-picker"
	ctxModelList    = "model-list"
	ctxFiles        = "files-sheet"
	ctxProgram      = "program-room"
	ctxTaskRecord   = "task-record"
	ctxWall         = "wall"
	ctxTeamsPage    = "teams-page"
	ctxComposer     = "composer-layer"
	ctxQuestion     = "question"
	ctxSpell        = "spell-out"
	ctxCopyMode     = "copy-mode"
	ctxStanding     = "standing"
	// ctxTabs is the rung that reads the three tab chords under every modal and
	// over the box (input.go): a modal that edits with the same chord wins.
	ctxTabs = "chat-tabs"
)

// chordClaim is one handler's claim: the constant that spells the chord, and
// every context that handler answers it in.
type chordClaim struct {
	name string
	ctxs []string
}

var chordRegistry = []chordClaim{
	{"addMachineKey", []string{ctxHome}},
	{"approvalKey", []string{ctxBox}},
	{"bargeKey", []string{ctxConversation}},
	{"briefFoldKey", []string{ctxConversation}},
	{"closeTabChord", []string{ctxTabs}},
	{"deviceKey", []string{ctxHome}},
	{"effortKey", []string{ctxBox}},
	{"filesCopyKey", []string{ctxFiles}},
	{"filesRevealKey", []string{ctxFiles}},
	{"folderHiddenKey", []string{ctxFolderPicker}},
	{"folderMarkKey", []string{ctxFolderPicker}},
	{"folderPaneKey", []string{ctxFolderPicker}},
	{"folderWideKey", []string{ctxFolderPicker}},
	{"jumpKey", []string{ctxConversation}},
	{"newChatChord", []string{ctxTabs}},
	{"pickerSortBackChord", []string{ctxModelList}},
	{"pickerSortKeyChord", []string{ctxModelList}},
	{"placeMapKey", []string{ctxHome, ctxTeamsPage}},
	{"programCallsKey", []string{ctxProgram}},
	{"projectKey", []string{ctxBox, ctxComposer}},
	{"questionChipKey", []string{ctxQuestion}},
	{"railStowKey", []string{ctxRail}},
	{"refreshModelsKey", []string{ctxModelList}},
	{"reopenTabChord", []string{ctxTabs}},
	{"resumeKey", []string{ctxHome}},
	{"resumeSkipKey", []string{ctxHome}},
	{"selectKey", []string{ctxCopyMode}},
	{"spellOutKey", []string{ctxSpell}},
	{"standMarkKey", []string{ctxStanding}},
	{"steerKeySuper", []string{ctxConversation}},
	{"taskSheetKey", []string{ctxTaskRecord}},
	{"teamManagerKey", []string{ctxConversation}},
	{"trafficKey", []string{ctxConversation}},
	{"wallOpenKey", []string{ctxWall}},
}

// chordLiterals are chords handled by a string literal in a switch rather than
// a constant, so the source scan cannot find them. They are claims all the
// same, and they are checked against the constants.
var chordLiterals = []struct{ chord, ctx string }{
	{"alt+o", ctxConversation},  // input.go: open the visible picture
	{"alt+o", ctxComposer},      // composerlayer.go: the composer picker
	{"alt+i", ctxConversation},  // input.go: toggle pictures
	{"alt+g", ctxHome},          // regroup the chat list by project
	{"ctrl+g", ctxConversation}, // input.go: send the running command to the background
	{"ctrl+t", ctxModelList},    // palette.go: walk the row's effort
	{"ctrl+w", ctxModelList},    // palette.go: delete a word of the search
	{"ctrl+t", ctxHome},         // homedraft.go: effort in the home box
	{"ctrl+q", ctxConversation}, // follow-up after the work
	{"ctrl+o", ctxHome},         // home.go: open the folder
	{"ctrl+y", ctxHome},         // home.go: copy the path
	{"ctrl+x", ctxHome},         // home.go: stop
}

var chordShape = regexp.MustCompile(`^((ctrl|alt|super|shift)\+)+([a-z0-9.,]|enter)$`)

// declaredChords is every string constant in the package's non-test files whose
// value has the shape of a modifier chord, by name.
func declaredChords(t *testing.T) map[string]string {
	t.Helper()
	isSource := func(fi os.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }
	pkgs, err := parser.ParseDir(token.NewFileSet(), ".", isSource, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			collectChordConsts(f, found)
		}
	}
	return found
}

// collectChordConsts adds every top-level const in f whose value is a chord.
func collectChordConsts(f *ast.File, into map[string]string) {
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, n := range vs.Names {
				if v, ok := chordValue(vs, i); ok {
					into[n.Name] = v
				}
			}
		}
	}
}

func chordValue(vs *ast.ValueSpec, i int) (string, bool) {
	if i >= len(vs.Values) {
		return "", false
	}
	lit, ok := vs.Values[i].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(lit.Value)
	return v, err == nil && chordShape.MatchString(v)
}

func TestChordRegistryHasNoClashes(t *testing.T) {
	values := declaredChords(t)
	owners := map[string][]string{} // chord -> "ctx name"
	for _, c := range chordRegistry {
		chord, ok := values[c.name]
		if !ok {
			t.Errorf("registry row %s is not a chord constant in this package", c.name)
			continue
		}
		if len(c.ctxs) == 0 {
			t.Errorf("registry row %s names no context", c.name)
		}
		for _, ctx := range c.ctxs {
			owners[chord] = append(owners[chord], ctx+" "+c.name)
		}
	}
	for _, l := range chordLiterals {
		owners[l.chord] = append(owners[l.chord], l.ctx+" literal")
	}
	for chord, claims := range owners {
		if clash := clashIn(claims); clash != "" {
			t.Errorf("%s is claimed twice in one context: %s", chord, clash)
		}
	}
}

func TestEveryChordConstantIsRegistered(t *testing.T) {
	registered := map[string]bool{}
	for _, c := range chordRegistry {
		registered[c.name] = true
	}
	var missing []string
	for name := range declaredChords(t) {
		if !registered[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("chord constants missing from chordRegistry (add each with its context): %v", missing)
	}
}

// clashIn names the first pair of "context who" claims that share a context,
// or where either one is in [ctxAny]; "" when there is none.
func clashIn(claims []string) string {
	for i, a := range claims {
		for _, b := range claims[i+1:] {
			ca, cb := strings.Fields(a)[0], strings.Fields(b)[0]
			if ca == cb || ca == ctxAny || cb == ctxAny {
				return a + " / " + b
			}
		}
	}
	return ""
}

func TestTheClashCheckSeesADuplicate(t *testing.T) {
	if clashIn([]string{"home a", "home b"}) == "" {
		t.Fatal("two claims in one context were not reported")
	}
	if clashIn([]string{"home a", "any b"}) == "" {
		t.Fatal("a claim beside an any-context claim was not reported")
	}
	if clashIn([]string{"home a", "box b"}) != "" {
		t.Fatal("claims in different contexts were reported")
	}
}
