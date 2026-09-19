package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestWideFoldersDrawRootFolderSubfolderAndDetails(t *testing.T) {
	a := foldersPlaceApp(t, billingSecurityFolders())
	a.width, a.height = 160, 40
	if !a.pointFolderPlaceID("col-billing") {
		t.Fatal("could not stand on Billing")
	}
	a.refreshFolderPlace()
	frame := foldersPlaceText(a)
	for _, word := range []string{"Root", "Billing", "Receipts", "details", folderManageWord} {
		if !strings.Contains(frame, word) {
			t.Fatalf("wide columns hid %q:\n%s", word, frame)
		}
	}
	if !strings.Contains(frame, folderDrillHint) && !strings.Contains((placeFolders{}).hint(a), folderDrillHint) {
		t.Fatalf("wide Folders hid %q", folderDrillHint)
	}
	if !strings.Contains((placeFolders{}).hint(a), folderShiftActionsHint) {
		t.Fatalf("hint hid %q: %q", folderShiftActionsHint, (placeFolders{}).hint(a))
	}
}

func TestSharedFolderKeepsOneIdentityAndAlsoIn(t *testing.T) {
	a := foldersPlaceApp(t, billingSecurityFolders())
	a.width, a.height = 160, 40
	if !a.pointFolderPlaceID("col-billing") {
		t.Fatal("could not stand on Billing")
	}
	a.enterFolderPlace()
	if !a.pointFolderPlaceID("col-receipts") {
		t.Fatal("could not stand on Receipts via Billing")
	}
	a.rememberFolderPlace()
	billingPath := a.folderSheet.selectedPath
	a.escFolderPlace()
	if !a.pointFolderPlaceID("col-security") {
		t.Fatal("could not stand on Security")
	}
	a.enterFolderPlace()
	if !a.pointFolderPlaceID("col-receipts") {
		t.Fatal("could not stand on Receipts via Security")
	}
	a.rememberFolderPlace()
	if a.folderSheet.selectedID != "col-receipts" {
		t.Fatalf("shared Receipts lost its id: %q", a.folderSheet.selectedID)
	}
	if a.folderSheet.selectedPath == billingPath {
		t.Fatal("selecting Receipts from Security kept the Billing path")
	}
	if !a.pointFolderPlaceID("col-billing") {
		a.leaveFolderPlace()
		a.refreshFolderPlace()
		if !a.pointFolderPlaceID("col-billing") {
			t.Fatal("could not return to Billing")
		}
	}
	a.enterFolderPlace()
	for i, stop := range a.folderSheet.stops {
		if stop.kind == folderStopChat && stop.id == "aaaa000000000001" {
			a.folderSheet.cursor = i
			a.rememberFolderPlace()
			break
		}
	}
	a.refreshFolderPlace()
	frame := foldersPlaceText(a)
	if !strings.Contains(frame, folderAlsoInWord) {
		t.Fatalf("shared chat hid Also in:\n%s", frame)
	}
}

func TestNarrowAndWideResizeKeepTheSelectedPath(t *testing.T) {
	a := foldersPlaceApp(t, billingSecurityFolders())
	a.width, a.height = 80, 24
	if !a.pointFolderPlaceID("col-billing") {
		t.Fatal("could not stand on Billing at 80-col")
	}
	a.rememberFolderPlace()
	a.compose.setText("keep this sentence")
	a.width, a.height = 160, 40
	a.refreshFolderPlace()
	if a.compose.String() != "keep this sentence" {
		t.Fatalf("wide resize wiped the composer: %q", a.compose.String())
	}
	stop, ok := a.folderPlaceCursor()
	if !ok || stop.id != "col-billing" {
		t.Fatalf("wide resize moved selection to %+v", stop)
	}
	a.width, a.height = 80, 24
	a.refreshFolderPlace()
	stop, ok = a.folderPlaceCursor()
	if !ok || stop.id != "col-billing" {
		t.Fatalf("80-col resize moved selection to %+v", stop)
	}
	if strings.Contains(foldersPlaceText(a), "Receipts") {
		t.Fatal("80-col Root listed a nested folder")
	}
}

func TestDeepPathWindowsOlderColumnsAndKeepsABreadcrumb(t *testing.T) {
	fake := deepFolderChain()
	a := foldersPlaceApp(t, fake)
	a.width, a.height = 120, 40
	walk := []string{"col-a", "col-b", "col-c", "col-d"}
	for _, id := range walk {
		if !a.pointFolderPlaceID(id) {
			t.Fatalf("could not stand on %s", id)
		}
		a.enterFolderPlace()
	}
	a.refreshFolderPlace()
	if a.folderSheet.windowFrom <= 0 {
		t.Fatalf("deep path did not window older columns: cols=%d windowFrom=%d", len(a.folderSheet.cols), a.folderSheet.windowFrom)
	}
	if crumb := a.folderColumnCrumb(); crumb == "" || !strings.Contains(crumb, folderRootTitle) {
		t.Fatalf("windowed columns hid the breadcrumb: %q", crumb)
	}
}

func TestRightDrillsAndShiftRightOpensTheStrip(t *testing.T) {
	a := foldersPlaceApp(t, billingSecurityFolders())
	a.width, a.height = 160, 40
	if !a.pointFolderPlaceID("col-billing") {
		t.Fatal("could not stand on Billing")
	}
	drive(t, a, key("right"))
	if a.folderSheet.open != "col-billing" {
		t.Fatalf("→ did not drill into Billing, open=%q strip=%v", a.folderSheet.open, a.strip.open)
	}
	if a.strip.open {
		t.Fatal("→ on Folders opened the verb strip")
	}
	if !a.pointFolderPlaceID("col-receipts") {
		t.Fatal("could not stand on Receipts after drill")
	}
	drive(t, a, key("shift+right"))
	if !a.strip.open {
		t.Fatal("shift+→ did not open the verb strip")
	}
}

func TestNewFolderNestsInTheSelectedFolder(t *testing.T) {
	fake := billingSecurityFolders()
	a := foldersPlaceApp(t, fake)
	if !a.pointFolderPlaceID("col-billing") {
		t.Fatal("could not stand on Billing")
	}
	a.enterFolderPlace()
	a.folderSheet.cursor = 0
	a.enterFolderPlace()
	if a.folderSheet.naming == nil {
		t.Fatal("New folder did not open a name box")
	}
	a.folderSheet.naming.setText("Invoices")
	drive(t, a, key("enter"))
	if len(fake.createsIn) != 1 || fake.createsIn[0] != [2]string{"Invoices", "col-billing"} {
		t.Fatalf("visible New folder wrote %v, want Invoices in Billing", fake.createsIn)
	}
}

func TestCoordinateSelectedUsesGNotC(t *testing.T) {
	a, fake := collabLab(t)
	fake.marks = []CollabMark{{RefID: "aaaa000000000001", Title: "Porting the Resume Picker"}}
	a.folders = billingSecurityFolders()
	a.showPage(pageFolders)
	a.readCollab()
	a.refreshFolderPlace()
	foundG, foundC := false, false
	for _, v := range a.folderPlaceVerbs() {
		if v.key == 'g' && v.word == folderCoordinateAction {
			foundG = true
		}
		if v.key == 'c' && v.word == folderCoordinateAction {
			foundC = true
		}
		if v.key == 'c' && v.word == folderNewFolderWord {
			continue
		}
	}
	if !foundG {
		t.Fatal("marked chats hid g Coordinate selected")
	}
	if foundC {
		t.Fatal("Coordinate selected stole c from New folder")
	}
}

func TestAddExistingChatsHookIsPreserved(t *testing.T) {
	called := 0
	folderAddStart = func(*app) tea.Cmd {
		called++
		return nil
	}
	defer func() { folderAddStart = (*app).beginFolderAdd }()
	a := foldersPlaceApp(t, billingSecurityFolders())
	frame := foldersPlaceText(a)
	if !strings.Contains(frame, folderAddExistingWord) {
		t.Fatalf("hook present but %q is missing:\n%s", folderAddExistingWord, frame)
	}
	found := false
	for i, stop := range a.folderSheet.stops {
		if stop.kind == folderStopAddExisting {
			a.folderSheet.cursor = i
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Add existing chats was not a restable row")
	}
	a.enterFolderPlace()
	if called != 1 {
		t.Fatalf("beginFolderAdd hook called %d times", called)
	}
	for _, v := range a.folderPlaceVerbs() {
		if v.key == 'b' && v.word == folderAddExistingWord {
			return
		}
	}
	t.Fatal("hook hid the b chord")
}

func TestFoldersColumnFunctionsStayUnderTheCeiling(t *testing.T) {
	assertFolderComplexity(t, "foldercolumns.go")
}

func deepFolderChain() *fakeFolders {
	a := FolderView{ID: "col-a", Name: "Alpha", Lifecycle: "active"}
	b := FolderView{ID: "col-b", Name: "Beta", Lifecycle: "active", ParentIDs: []string{"col-a"}}
	c := FolderView{ID: "col-c", Name: "Gamma", Lifecycle: "active", ParentIDs: []string{"col-b"}}
	d := FolderView{ID: "col-d", Name: "Delta", Lifecycle: "active", ParentIDs: []string{"col-c"}}
	e := FolderView{ID: "col-e", Name: "Epsilon", Lifecycle: "active", ParentIDs: []string{"col-d"}}
	return &fakeFolders{
		root: FolderRoot{Folders: []FolderView{a, b, c, d, e}},
		members: map[string][]FolderPlacement{
			"col-a": {{CollectionID: "col-a", RefID: "col-b", Title: "Beta", Kind: folderCollectionKind}},
			"col-b": {{CollectionID: "col-b", RefID: "col-c", Title: "Gamma", Kind: folderCollectionKind}},
			"col-c": {{CollectionID: "col-c", RefID: "col-d", Title: "Delta", Kind: folderCollectionKind}},
			"col-d": {{CollectionID: "col-d", RefID: "col-e", Title: "Epsilon", Kind: folderCollectionKind}},
		},
	}
}

func assertFolderComplexity(t *testing.T, name string) {
	t.Helper()
	set := token.NewFileSet()
	source, err := parser.ParseFile(set, name, nil, 0)
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

func TestRightOnAChatFocusesDetails(t *testing.T) {
	a := foldersPlaceApp(t, billingSecurityFolders())
	a.width, a.height = 80, 24
	if !a.pointFolderPlaceID("col-billing") {
		t.Fatal("could not stand on Billing")
	}
	a.enterFolderPlace()
	for i, stop := range a.folderSheet.stops {
		if stop.kind == folderStopChat {
			a.folderSheet.cursor = i
			a.rememberFolderPlace()
			break
		}
	}
	drive(t, a, key("right"))
	if !a.folderSheet.detailFocus {
		t.Fatal("→ on a chat did not focus details")
	}
	if a.strip.open {
		t.Fatal("→ on a chat opened the strip")
	}
	drive(t, a, key("left"))
	if a.folderSheet.detailFocus {
		t.Fatal("← from details did not return to the column")
	}
}
