package tui3

// folderpick_test.go is the /folder picker: the layers it lays candidates down
// in, the frecency that orders them inside a layer, the morph from a filter
// into columns the moment somebody types a path, the law that esc changes
// nothing, and the two roads that avoid the picker entirely — a folder offered
// by `@`, and a folder handed to `/attach`.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// folderLab is a surface standing in a real directory with a shape under it:
// `here/deep`, `here/other`, and a sibling the world knows about.
func folderLab(t *testing.T) (*app, string) {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"here/deep", "here/other", "sibling", "here/.hidden", "here/node_modules"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	a := newApp(t.Context(), Options{Agent: &fakeAgent{model: "m"}, Workspace: filepath.Join(root, "here")})
	a.width, a.height = 100, 24
	a.pal = newPalette(tokens.ANSI256, false)
	a.entries = nil
	// The world is set by hand so the candidate build cannot reach a disk this
	// test does not own: [app.composerDestinations] reads the cached world when
	// it holds anything at all.
	a.home.world = session.World{Projects: []session.Project{
		{Path: filepath.Join(root, "sibling")},
		{Path: filepath.Join(root, "here")},
	}}
	a.touch()
	return a, root
}

// The layers are a ladder: what this conversation touched comes above what the
// world knows about, which comes above the background index — whatever any of
// them was picked before.
func TestTheFolderPickerLaysItsCandidatesDownInLayers(t *testing.T) {
	a, root := folderLab(t)
	// One tool call, pointed at a file inside `here/deep`. That is the TOUCHED
	// layer, read off the entries this surface already drew.
	a.entries = []entry{{
		kind: entryTool, tool: "read",
		detail: toolDetail{Args: `{"path":"deep/notes.md"}`},
	}}
	a.folderStore.Roots = []string{filepath.Join(root, "indexed")}

	got := a.folderCandidates()
	if len(got) < 4 {
		t.Fatalf("the layers produced %d candidates: %+v", len(got), got)
	}
	want := []struct {
		path  string
		layer folderLayer
	}{
		{filepath.Join(root, "here", "deep"), folderTouched},
		{filepath.Join(root, "here"), folderProject},
		{filepath.Join(root, "sibling"), folderProject},
		{filepath.Join(root, "indexed"), folderIndexed},
	}
	for at, expect := range want {
		if got[at].path != expect.path {
			t.Fatalf("row %d is %s, want %s", at, got[at].path, expect.path)
		}
		if got[at].layer != expect.layer {
			t.Fatalf("row %d (%s) is layer %d, want %d", at, got[at].path, got[at].layer, expect.layer)
		}
	}

	// And nothing is offered twice: `here` is the workspace AND a project AND
	// the parent of the touched directory.
	seen := map[string]bool{}
	for _, cand := range got {
		if seen[cand.path] {
			t.Fatalf("%s is on the list twice", cand.path)
		}
		seen[cand.path] = true
	}
}

// Inside one layer the order is frecency — recency × frequency of prior picks —
// and a directory nobody has chosen falls back on the order its source meant.
func TestFrecencyOrdersTheRowsInsideALayer(t *testing.T) {
	a, root := folderLab(t)
	sibling := filepath.Join(root, "sibling")
	here := filepath.Join(root, "here")
	// `here` leads its layer on source order alone.
	if first := a.folderCandidates()[0]; first.path != here {
		t.Fatalf("with no picks the first row is %s, want %s", first.path, here)
	}
	// Two picks on the sibling, half an hour ago, lift it over its neighbour.
	a.folderStore.Picks = map[string]folderPickCount{
		sibling: {N: 2, At: a.now().Add(-30 * time.Minute)},
	}
	a.folder.start(a.folderCandidates(), a.tilde)
	if first, ok := a.folder.here(); !ok || first != sibling {
		t.Fatalf("after two recent picks the first row is %q, want %s", first, sibling)
	}
	// And ONE pick this hour outranks those two once they are a month old: an
	// old habit is not this afternoon's, which is the whole of what the
	// "recency ×" half buys.
	a.folderStore.Picks[sibling] = folderPickCount{N: 2, At: a.now().Add(-30 * 24 * time.Hour)}
	a.folderStore.Picks[here] = folderPickCount{N: 1, At: a.now().Add(-10 * time.Minute)}
	a.folder.start(a.folderCandidates(), a.tilde)
	if first, ok := a.folder.here(); !ok || first != here {
		t.Fatalf("after two stale picks the first row is %q, want %s", first, here)
	}
}

func TestFrecencyWeighsRecencyAgainstFrequency(t *testing.T) {
	now := time.Now()
	if got := folderFrecency(folderPickCount{}, now); got != 0 {
		t.Fatalf("a directory nobody picked scores %v, want 0", got)
	}
	fresh := folderFrecency(folderPickCount{N: 1, At: now.Add(-time.Minute)}, now)
	stale := folderFrecency(folderPickCount{N: 3, At: now.Add(-30 * 24 * time.Hour)}, now)
	if fresh <= stale {
		t.Fatalf("one pick a minute ago (%v) must beat three a month ago (%v)", fresh, stale)
	}
}

// Free words filter; a path browses. The sheet OPENS on the columns
// ([app.contextStart]) and a word is what puts the ranked list in front of them.
func TestTypingAPathMorphsTheListIntoColumns(t *testing.T) {
	a, root := folderLab(t)
	cmd := a.openFolderPick("")
	if cmd == nil {
		t.Fatal("opening the picker asks for the store and the facts")
	}
	// The command is RUN, because the sheet opens browsing now and the level it
	// opened on is asked for by that command — dropping it would leave the
	// columns marked as being read and never read.
	settleFolder(t, a, cmd)
	if !a.folder.open {
		t.Fatal("/folder opened nothing")
	}
	if !a.folder.browsing {
		t.Fatal("the chooser opens on the columns, not on a list")
	}

	// A word puts the list up, and narrows it.
	typeFolder(t, a, "sibl")
	if a.folder.browsing {
		t.Fatal("a bare word turned into a browse")
	}
	if here, ok := a.folder.here(); !ok || filepath.Base(here) != "sibling" {
		t.Fatalf("the filter left the cursor on %q", here)
	}

	// A path morphs it, and the columns read exactly one level.
	drive(t, a, key("ctrl+u"))
	typeFolder(t, a, root+"/here/")
	if !a.folder.browsing {
		t.Fatal("a path did not morph the list into columns")
	}
	if a.folder.cols.dir != filepath.Join(root, "here") {
		t.Fatalf("the columns are on %s, want %s", a.folder.cols.dir, filepath.Join(root, "here"))
	}
	if want := []string{"deep", "other"}; strings.Join(a.folder.cols.here.names, ",") != strings.Join(want, ",") {
		t.Fatalf("the middle column is %v, want %v — directories only, dot and skipped ones pruned", a.folder.cols.here.names, want)
	}
	// The parent column is the level above, and it knows which of its rows we
	// are standing in.
	if a.folder.cols.upAt < 0 || a.folder.cols.up.names[a.folder.cols.upAt] != "here" {
		t.Fatalf("the parent column does not mark `here`: %v at %d", a.folder.cols.up.names, a.folder.cols.upAt)
	}

	// → walks in, ← walks back out and leaves the cursor where it came from.
	drive(t, a, key("right"))
	if a.folder.cols.dir != filepath.Join(root, "here", "deep") {
		t.Fatalf("→ landed on %s", a.folder.cols.dir)
	}
	drive(t, a, key("left"))
	if a.folder.cols.dir != filepath.Join(root, "here") {
		t.Fatalf("← landed on %s", a.folder.cols.dir)
	}
	if got, _ := a.folder.here(); got != filepath.Join(root, "here", "deep") {
		t.Fatalf("← left the cursor on %q, not on the folder it came out of", got)
	}

	// And the columns draw as columns: no borders, the names, and nothing wider
	// than the frame. The measure is CELLS and not bytes — the sheet's own
	// punctuation is multibyte, so a byte count would fail a row that fits.
	for _, line := range chooserRows(t, a, -1, "") {
		if ansi.StringWidth(line) > a.width {
			t.Fatalf("a column row runs past the frame: %q", plain(line))
		}
	}
	if !strings.Contains(plain(strings.Join(a.folder.rows(a.width, 6, a.pal, a.styler(), -1, ""), "\n")), "deep") {
		t.Fatal("the columns are not drawing the directories they read")
	}
}

func TestAPathShapeIsTheOnlyThingThatBrowses(t *testing.T) {
	for _, typed := range []string{"/", "/tmp", "~", "~/code", "./here", "../up", ".."} {
		if !folderPathish(typed) {
			t.Errorf("%q should browse", typed)
		}
	}
	for _, typed := range []string{"", "code", "aforge-v2", "a/b"} {
		if folderPathish(typed) {
			t.Errorf("%q should filter, not browse", typed)
		}
	}
}

// The palette's law, kept: esc leaves the draft, the workspace and the
// conversation exactly as they were.
func TestEscLeavesTheFolderPickerHavingChangedNothing(t *testing.T) {
	a, _ := folderLab(t)
	typeInto(t, a, "half a sentence")
	draft, workspace := a.input.String(), a.workspace
	notes := len(a.entries)

	a.openFolderPick("")
	typeFolder(t, a, "sibl")
	drive(t, a, key("down"))
	drive(t, a, key("esc"))

	if a.folder.open {
		t.Fatal("esc left the picker open")
	}
	if a.input.String() != draft {
		t.Fatalf("esc came back to the draft %q, want %q", a.input.String(), draft)
	}
	if a.workspace != workspace {
		t.Fatalf("esc moved the workspace to %s", a.workspace)
	}
	if a.placeChosen != "" {
		t.Fatalf("esc chose %s", a.placeChosen)
	}
	if len(a.entries) != notes {
		t.Fatalf("esc wrote %d lines into the conversation", len(a.entries)-notes)
	}
}

// enter hands the directory to the ONE seam every road onto a folder comes out
// of, and writes the pick down so the next open leads with it.
func TestEnterReportsTheChosenPlaceAndRemembersThePick(t *testing.T) {
	a, root := folderLab(t)
	a.openFolderPick("")
	typeFolder(t, a, "sibl")
	drive(t, a, key("enter"))

	want := filepath.Join(root, "sibling")
	if a.placeChosen != want {
		t.Fatalf("enter chose %q, want %s", a.placeChosen, want)
	}
	if a.folder.open {
		t.Fatal("enter left the picker open")
	}
	if pick := a.folderStore.Picks[want]; pick.N != 1 || pick.At.IsZero() {
		t.Fatalf("the pick was not written down: %+v", pick)
	}
	if got := plain(lastNote(t, a)); !strings.Contains(got, "folder · ") || !strings.Contains(got, "sibling") {
		t.Fatalf("the line said %q", got)
	}
}

// A row whose directory has since gone says so and takes nothing — the one stat
// on this surface, spent on the keystroke that commits.
func TestEnterOnAFolderThatIsGoneSaysSoAndChoosesNothing(t *testing.T) {
	a, root := folderLab(t)
	gone := filepath.Join(root, "vanished")
	a.folder.start([]folderCand{{path: gone, show: gone}}, a.tilde)
	drive(t, a, key("enter"))
	if a.placeChosen != "" {
		t.Fatalf("a folder that is not there was chosen: %s", a.placeChosen)
	}
	if got := plain(lastNote(t, a)); !strings.HasPrefix(got, folderGoneWord) {
		t.Fatalf("the refusal said %q, want it to start %q", got, folderGoneWord)
	}
}

// The third column is what the machine knows, and the emptiness law runs
// through it: a plain folder with nothing in it says `folder` and stops.
func TestWhatTheMachineKnowsAboutAFolder(t *testing.T) {
	root := t.TempDir()
	empty := filepath.Join(root, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := folderFactsOf(empty); len(got) != 1 || got[0] != "folder" {
		t.Fatalf("an empty folder knows %v, want exactly [folder] — never `0 files`", got)
	}

	full := filepath.Join(root, "full")
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one.txt", "two.txt", ".hidden"} {
		if err := os.WriteFile(filepath.Join(full, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := folderFactsOf(full); len(got) != 1 || got[0] != "folder · 2 files" {
		t.Fatalf("a folder with two files knows %v", got)
	}

	// AGENTS.md is its own line, and only when it is there.
	if err := os.WriteFile(filepath.Join(full, "AGENTS.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := folderFactsOf(full)
	if len(got) != 2 || got[1] != "AGENTS.md" {
		t.Fatalf("AGENTS.md is not said: %v", got)
	}
	if folderFactsOf(filepath.Join(root, "nowhere")) != nil {
		t.Fatal("a directory that is not there knows something")
	}
}

// ── the roads that avoid the picker ─────────────────────────────────────────

// The @ list offers directories as well as files, marked, and choosing one
// writes the path into the sentence exactly as a file does.
func TestTheAtListOffersFoldersAndSaysWhichRowsThoseAre(t *testing.T) {
	a := completionApp(t, "internal/tui3/app.go", "cmd/aforge/main.go", ".git/config")
	typeInto(t, a, "look at @internal")
	if !a.comp.loaded {
		t.Fatal("the walk did not land")
	}
	folders := 0
	for _, path := range a.comp.all {
		if isFolderPath(path) {
			folders++
		}
		if strings.HasPrefix(path, ".git/") {
			t.Fatalf("the walk offered %s", path)
		}
	}
	if folders == 0 {
		t.Fatal("the walk offers no directories at all")
	}
	if !strings.Contains(strings.Join(a.comp.all, " "), "internal/tui3/") {
		t.Fatalf("internal/tui3/ is not on offer: %v", a.comp.all)
	}

	// The row says what it is, and the row for a file does not.
	drawn := plain(strings.Join(a.overlayRows(a.width, a.overlayHeight()), "\n"))
	if !strings.Contains(drawn, folderTag) {
		t.Fatalf("no row is marked %q:\n%s", folderTag, drawn)
	}

	// And choosing one inserts the path the way choosing a file does.
	typeInto(t, a, "/tui3")
	drive(t, a, key("enter"))
	if got := a.input.String(); got != "look at @internal/tui3/" {
		t.Fatalf("choosing a folder inserted %q", got)
	}
}

// /attach on a directory used to refuse. It is the folder door now, and a file
// handed to the same command is still a file on the tray.
func TestAttachOnAFolderTakesTheFolderDoorInsteadOfRefusing(t *testing.T) {
	a, _, dir := fileLab(t, "", map[string]int{"server.log": 8, "sub/inner.txt": 8})
	a.entries = nil
	a.attachFilePath("sub")
	if want := filepath.Join(dir, "sub"); a.placeChosen != want {
		t.Fatalf("/attach on a folder chose %q, want %s", a.placeChosen, want)
	}
	if len(a.chips) != 0 {
		t.Fatalf("/attach on a folder put %d things on the tray", len(a.chips))
	}
	if got := plain(lastNote(t, a)); strings.Contains(got, "is a folder · attach a file") {
		t.Fatalf("the old refusal is still there: %q", got)
	}

	a.entries = nil
	a.attachFilePath("server.log")
	if len(a.chips) != 1 {
		t.Fatalf("/attach on a file put %d things on the tray", len(a.chips))
	}
}

// Over a connection the folders this process can read are the laptop's, so the
// picker refuses in one sentence rather than offering somewhere the work cannot
// go.
func TestTheFolderPickerRefusesOverAConnection(t *testing.T) {
	a, _, _ := fileLab(t, "somewhere", nil)
	a.entries = nil
	if cmd := a.openFolderPick(""); cmd != nil {
		t.Fatal("a hosted /folder started work")
	}
	if a.folder.open {
		t.Fatal("a hosted /folder opened the picker")
	}
	if got := plain(lastNote(t, a)); got != folderRemoteWord {
		t.Fatalf("the refusal said %q", got)
	}
}

// typeFolder types into the picker's own box, one key at a time, the way
// [typeInto] types into the draft.
func typeFolder(t *testing.T, a *app, text string) {
	t.Helper()
	for _, r := range text {
		drive(t, a, key(string(r)))
	}
}

// The actual chooser must use the typo-aware ranker, not just carry its helpers.
func TestTheFolderBrowserFindsATransposedProjectName(t *testing.T) {
	var picker folderPick
	picker.start([]folderCand{
		{path: "/code/aforge", show: "~/code/aforge", layer: folderProject},
		{path: "/notes", show: "~/notes", layer: folderProject},
	}, "/home/person")
	picker.filter.setText("afroge")
	picker.rank()
	path, ok := picker.here()
	if !ok || path != "/code/aforge" {
		t.Fatalf("transposed query selected %q, %v; want /code/aforge", path, ok)
	}
}
