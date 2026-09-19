package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func foldersPlaceApp(t *testing.T, fake *fakeFolders) *app {
	t.Helper()
	lab := newLiveLab(t)
	a := lab.open()
	a.width, a.height = 120, 40
	a.folders = nil
	if fake != nil {
		a.folders = fake
	}
	if cmd := a.showPage(pageFolders); cmd != nil {
		runCmd(cmd)
	}
	if !a.at(pageFolders) {
		t.Fatal("the folders place did not open")
	}
	return a
}

func foldersPlaceText(a *app) string {
	lines, _, _, _ := a.folderPlaceFrame(a.width, a.height)
	return plain(strings.Join(lines, "\n"))
}

func TestFoldersIsARegisteredBarPlace(t *testing.T) {
	if placeBarPlaces != 5 {
		t.Fatalf("placeBarPlaces is %d, want 5", placeBarPlaces)
	}
	if got := pages(); len(got) != 8 || got[4] != pageFolders {
		t.Fatalf("placeOrder is %v, want folders at alt+5", got)
	}
	if id, ok := placeDigit("alt+5"); !ok || id != pageFolders {
		t.Fatalf("alt+5 reaches %v %v, want folders", id, ok)
	}
	if id, ok := placeDigit("alt+6"); !ok || id != pageStanding {
		t.Fatalf("alt+6 reaches %v %v, want standing", id, ok)
	}
	if id, ok := placeDigit("alt+8"); !ok || id != pageSearch {
		t.Fatalf("alt+8 reaches %v %v, want search", id, ok)
	}
	if placeFor(pageFolders) == nil || pageFolders.word() != "folders" {
		t.Fatal("pageFolders is not registered as folders")
	}
}

func TestBareFoldersOpensThePlaceAndFolderStaysFilesystem(t *testing.T) {
	a := newTestApp(nil)
	a.width, a.height = 120, 40
	a.slash("/folders")
	if a.page != pageFolders {
		t.Fatalf("/folders opened %q, want folders", a.page.word())
	}
	b, _ := folderLab(t)
	settleFolder(t, b, b.slash("/folder"))
	if b.page == pageFolders {
		t.Fatal("/folder opened the Folders place")
	}
	if !b.folder.open {
		t.Fatal("/folder did not open the filesystem browser")
	}
	for _, word := range []string{"/place", "/dir"} {
		c, _ := folderLab(t)
		settleFolder(t, c, c.slash(word))
		if c.page == pageFolders {
			t.Fatalf("%s opened the Folders place", word)
		}
		if !c.folder.open {
			t.Fatalf("%s did not open the filesystem browser", word)
		}
	}
}

func TestEmptyFoldersPlaceWhispersAndShowsActions(t *testing.T) {
	a := foldersPlaceApp(t, &fakeFolders{})
	frame := foldersPlaceText(a)
	if !strings.Contains(frame, "folders") {
		t.Fatalf("empty Folders place dropped the heading:\n%s", frame)
	}
	if !strings.Contains(frame, folderWhisperWord) {
		t.Fatalf("empty Folders place dropped the whisper:\n%s", frame)
	}
	if strings.Contains(frame, "no folders yet") {
		t.Fatalf("empty Folders place broke the emptiness law:\n%s", frame)
	}
	for _, word := range []string{folderNewFolderWord, folderNewChatAction, folderOrganizeWord} {
		if !strings.Contains(frame, word) {
			t.Fatalf("empty Folders place hid %q:\n%s", word, frame)
		}
	}
}

func TestUnfiledRootChatsDrawWhenTheFolderListIsEmpty(t *testing.T) {
	fake := &fakeFolders{
		root: FolderRoot{
			Unfiled: []FolderPlacement{{RefID: "aaaa000000000001", Title: "porting the resume picker"}},
		},
	}
	a := foldersPlaceApp(t, fake)
	frame := foldersPlaceText(a)
	if !strings.Contains(frame, folderWhisperWord) {
		t.Fatalf("unfiled Root hid the empty-folder whisper:\n%s", frame)
	}
	if !strings.Contains(frame, "porting the resume picker") {
		t.Fatalf("unfiled Root chat is missing:\n%s", frame)
	}
}

func TestNilFoldersPlaceRefusesTheVisibleActions(t *testing.T) {
	a := foldersPlaceApp(t, nil)
	frame := foldersPlaceText(a)
	if !strings.Contains(frame, folderUnwiredWord) {
		t.Fatalf("nil Folders hid the refusal:\n%s", frame)
	}
	if strings.Contains(frame, folderWhisperWord) {
		t.Fatalf("nil Folders masqueraded as an empty working store:\n%s", frame)
	}
	a.folderSheet.cursor = 0
	a.enterFolderPlace()
	if a.folderSheet.note != folderUnwiredWord {
		t.Fatalf("New folder on a nil seam said %q", a.folderSheet.note)
	}
	a.folderSheet.note = ""
	a.folderSheet.cursor = 2
	a.enterFolderPlace()
	if a.folderSheet.note != folderUnwiredWord {
		t.Fatalf("Organize on a nil seam said %q", a.folderSheet.note)
	}
}

func TestFoldersPlaceIsSequentialAtEightyColumns(t *testing.T) {
	a := foldersPlaceApp(t, billingSecurityFolders())
	a.width, a.height = 80, 24
	a.refreshFolderPlace()
	frame := foldersPlaceText(a)
	if !strings.Contains(frame, "Billing") || !strings.Contains(frame, "Security") {
		t.Fatalf("80-col Folders root is missing collections:\n%s", frame)
	}
	if strings.Contains(frame, "Receipts") {
		t.Fatalf("80-col Root listed a nested folder:\n%s", frame)
	}
	if !a.pointFolderPlaceID("col-billing") {
		t.Fatal("could not stand on Billing")
	}
	a.enterFolderPlace()
	frame = foldersPlaceText(a)
	if a.folderSheet.open != "col-billing" {
		t.Fatalf("enter did not drill into Billing, open=%q", a.folderSheet.open)
	}
	if !strings.Contains(frame, folderBackWord) {
		t.Fatalf("80-col drill-in has no back row:\n%s", frame)
	}
	a.escFolderPlace()
	if a.folderSheet.open != "" {
		t.Fatalf("esc back left open %q", a.folderSheet.open)
	}
}

func TestFoldersPlaceKeepsSelectionAndComposerAcrossABeat(t *testing.T) {
	fake := billingSecurityFolders()
	a := foldersPlaceApp(t, fake)
	if !a.pointFolderPlaceID("col-billing") {
		t.Fatal("could not stand on Billing")
	}
	a.rememberFolderPlace()
	a.compose.setText("keep this sentence")
	fake.root.Folders = append(fake.root.Folders, FolderView{ID: "col-ops", Name: "Ops", Lifecycle: "active"})
	a.refreshFolderPlace()
	if a.compose.String() != "keep this sentence" {
		t.Fatalf("beat wiped the composer: %q", a.compose.String())
	}
	stop, ok := a.folderPlaceCursor()
	if !ok || stop.kind != folderStopFolder || stop.id != "col-billing" {
		t.Fatalf("beat moved the selection to %+v", stop)
	}
}

func TestFoldersPlaceDoesNotReadTheStoreInView(t *testing.T) {
	fake := billingSecurityFolders()
	a := foldersPlaceApp(t, fake)
	foldersPlaceText(a)
	reads := fake.reads
	fake.freeze()
	foldersPlaceText(a)
	a.moveFolderPlace(1)
	foldersPlaceText(a)
	if fake.reads != reads {
		t.Fatalf("View or a cursor move read Folders (%d -> %d)", reads, fake.reads)
	}
}

func TestFoldersPlaceOrganizeDrawsTheJob(t *testing.T) {
	fake := billingSecurityFolders()
	a := foldersPlaceApp(t, fake)
	for i, stop := range a.folderSheet.stops {
		if stop.kind == folderStopOrganize {
			a.folderSheet.cursor = i
			a.rememberFolderPlace()
			break
		}
	}
	a.enterFolderPlace()
	if fake.organizes != 1 || fake.organize.State != "queued" {
		t.Fatalf("Organize existing chats did not start a job: %+v calls=%d", fake.organize, fake.organizes)
	}
	frame := foldersPlaceText(a)
	if !strings.Contains(frame, "queued") {
		t.Fatalf("Folders place hid the job state:\n%s", frame)
	}
	if !strings.Contains(frame, folderCancelAction) {
		t.Fatalf("a live job hid cancel:\n%s", frame)
	}
	a.folderPlaceOrganize()
	if fake.organizes != 2 || fake.organize.JobID != "job-org" {
		t.Fatalf("second Organize did not coalesce: %+v calls=%d", fake.organize, fake.organizes)
	}
}

func TestFoldersHeadingOnHomeOpensThePlace(t *testing.T) {
	lab := newLiveLab(t)
	a := lab.open()
	a.folders = billingSecurityFolders()
	a.readHomeFolders()
	a.home.build()
	homeText(a)
	at := -1
	for i, line := range a.home.lines {
		if line.foldersHeadStop() {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatal("the folders heading is not a stop")
	}
	a.home.cursor = at
	a.homeEnter()
	if !a.at(pageFolders) {
		t.Fatalf("enter on the folders heading opened %q", a.page.word())
	}
}

func TestFoldersPlaceNewChatMayStartAtRoot(t *testing.T) {
	lab := newLiveLab(t)
	started := 0
	a := lab.app(lab.mine)
	a.start = func(workspace string) (Conversation, error) {
		started++
		return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: lab.mine, Workspace: workspace}, nil
	}
	a.width, a.height = 120, 40
	a.folders = billingSecurityFolders()
	a.showPage(pageFolders)
	a.folderSheet.cursor = 1
	a.enterFolderPlace()
	if !a.startingChat() {
		t.Fatal("New chat did not open the start page")
	}
	if a.pendingFolder != "" {
		t.Fatalf("New chat at Root set pendingFolder %q", a.pendingFolder)
	}
	if a.at(pageFolders) {
		t.Fatal("New chat left the Folders place up")
	}
}

func TestFoldersPlaceFunctionsStayUnderTheCeiling(t *testing.T) {
	set := token.NewFileSet()
	source, err := parser.ParseFile(set, "place_folders.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range source.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		got := collabComplexity(function.Body)
		if got > 15 {
			t.Errorf("%s is %d, ceiling is 15", function.Name.Name, got)
		}
	}
}

func TestSlashFoldersOrganizeLandsOnThePlace(t *testing.T) {
	a := newTestApp(nil)
	a.width, a.height = 120, 40
	a.folders = billingSecurityFolders()
	a.slash("/folders organize")
	if a.page != pageFolders {
		t.Fatalf("/folders organize opened %q", a.page.word())
	}
	if a.folders.(*fakeFolders).organizes != 1 {
		t.Fatal("/folders organize did not start the survey")
	}
}

func TestFoldersPlaceNewFolderNamesAndCreates(t *testing.T) {
	fake := billingSecurityFolders()
	a := foldersPlaceApp(t, fake)
	a.folderSheet.cursor = 0
	a.enterFolderPlace()
	if a.folderSheet.naming == nil {
		t.Fatal("New folder did not open a name box")
	}
	drive(t, a, key("O"), key("p"), key("s"), key("enter"))
	if fake.creates != 1 {
		t.Fatalf("enter on the name box created %d folders", fake.creates)
	}
	if a.folderSheet.naming != nil {
		t.Fatal("create left the name box up")
	}
}

func TestJumpWordsMatchThePlaceCount(t *testing.T) {
	want := "1…" + itoa(len(placeOrder))
	if !strings.Contains(chordJumpWords, want) {
		t.Fatalf("chordJumpWords is %q, want it to name %s", chordJumpWords, want)
	}
	if _, ok := placeDigit("alt+" + itoa(len(placeOrder)+1)); ok {
		t.Fatal("a digit past the last place still reaches a room")
	}
}

func TestFoldersPlaceEscFromRootLeavesThePlace(t *testing.T) {
	a := foldersPlaceApp(t, billingSecurityFolders())
	drive(t, a, key("esc"))
	if a.at(pageFolders) {
		t.Fatal("esc from Root left the Folders place up")
	}
}

func TestAClickOnAFoldersRowDrillsInLikeEnter(t *testing.T) {
	click := foldersPlaceApp(t, billingSecurityFolders())
	enter := foldersPlaceApp(t, billingSecurityFolders())
	if !click.pointFolderPlaceID("col-billing") || !enter.pointFolderPlaceID("col-billing") {
		t.Fatal("could not stand on Billing")
	}
	y := -1
	_, hits, _, _ := click.folderPlaceFrame(click.width, click.height)
	for row, at := range hits {
		if at == click.folderSheet.cursor {
			y = row
			break
		}
	}
	if y < 0 {
		t.Fatal("Billing was not on the frame")
	}
	drive(t, click, tea.MouseClickMsg{X: 4, Y: y, Button: tea.MouseLeft})
	drive(t, enter, key("enter"))
	if click.folderSheet.open != "col-billing" || enter.folderSheet.open != "col-billing" {
		t.Fatalf("click open=%q enter open=%q", click.folderSheet.open, enter.folderSheet.open)
	}
}
