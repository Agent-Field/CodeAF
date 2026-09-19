package tui3

import (
	"strings"
	"testing"
)

func TestChatPreviewThenOpenKeepsThePath(t *testing.T) {
	lab := newLiveLab(t)
	a := lab.open()
	a.width, a.height = 160, 40
	a.folders = billingSecurityFolders()
	a.showPage(pageFolders)
	if !a.pointFolderPlaceID("col-billing") {
		t.Fatal("could not stand on Billing")
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
	if !strings.Contains(frame, "Porting the Resume Picker") {
		t.Fatalf("chat preview hid the title:\n%s", frame)
	}
	if !strings.Contains(frame, folderOpenChatWord) {
		t.Fatalf("chat preview hid %q:\n%s", folderOpenChatWord, frame)
	}
	path := a.folderSheet.selectedPath
	a.enterFolderPlace()
	if a.at(pageFolders) {
		t.Fatal("Enter on a chat stayed on Folders")
	}
	a.showPage(pageFolders)
	if a.folderSheet.selectedPath != path && a.folderSheet.selectedID != "aaaa000000000001" {
		t.Fatalf("return lost the chat path: id=%q path=%q want %q", a.folderSheet.selectedID, a.folderSheet.selectedPath, path)
	}
}

func TestFolderDetailsAreTruthfulAndActionable(t *testing.T) {
	fake := billingSecurityFolders()
	fake.guidance = map[string][]FolderInstruction{
		"col-billing": {{ScopeID: "col-billing", Text: "file invoices here", Origin: "person"}},
	}
	a := foldersPlaceApp(t, fake)
	a.width, a.height = 160, 40
	if !a.pointFolderPlaceID("col-billing") {
		t.Fatal("could not stand on Billing")
	}
	a.enterFolderPlace()
	a.refreshFolderPlace()
	a.folderSheet.reading.works = []ExecWork{{Title: "port the picker", State: "running", SourceRef: "aaaa000000000001"}}
	a.rebuildFolderDetails()
	a.rebuildFolderColumns()
	frame := foldersPlaceText(a)
	for _, word := range []string{"Billing", "file invoices here", folderManageWord, "Porting the Resume Picker"} {
		if !strings.Contains(frame, word) {
			t.Fatalf("folder details hid %q:\n%s", word, frame)
		}
	}
	if strings.Contains(frame, "no instructions") || strings.Contains(frame, "manager") {
		t.Fatalf("details invented empty or manager chrome:\n%s", frame)
	}
}

func TestCompactActivityHasWhyAndUndo(t *testing.T) {
	a := foldersPlaceApp(t, billingSecurityFolders())
	a.folderSheet.change = FolderChange{Action: "Added to", FolderName: "Billing", CollectionID: "col-billing", RefID: "aaaa000000000001"}
	a.refreshFolderPlace()
	frame := foldersPlaceText(a)
	want := "Added to Billing · Why · Undo"
	if !strings.Contains(frame, want) {
		t.Fatalf("compact activity hid %q:\n%s", want, frame)
	}
	foundUndo, foundWhy := false, false
	for _, v := range a.folderPlaceVerbs() {
		if v.key == 'u' && v.word == folderUndoWord {
			foundUndo = true
		}
		if v.key == 'w' {
			foundWhy = true
		}
	}
	if !foundUndo || !foundWhy {
		t.Fatalf("Why/Undo chords missing: why=%v undo=%v", foundWhy, foundUndo)
	}
	a.undoFolderChange()
	fake := a.folders.(*fakeFolders)
	if len(fake.removes) != 1 || fake.removes[0] != [2]string{"col-billing", "aaaa000000000001"} {
		t.Fatalf("Undo wrote %v", fake.removes)
	}
}

func TestOrganizeThisChatIsVisibleAndCoalesces(t *testing.T) {
	fake := billingSecurityFolders()
	a := foldersPlaceApp(t, fake)
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
	a.refreshFolderPlace()
	if !strings.Contains(foldersPlaceText(a), folderOrganizeThisWord) {
		t.Fatalf("chat details hid %q:\n%s", folderOrganizeThisWord, foldersPlaceText(a))
	}
	a.organizeThisChat()
	a.organizeThisChat()
	if fake.thisChats != 2 || fake.thisChatID != "aaaa000000000001" {
		t.Fatalf("Organize this chat calls=%d id=%q", fake.thisChats, fake.thisChatID)
	}
	if fake.organize.JobID != "job-this" {
		t.Fatalf("second Organize this chat did not coalesce: %+v", fake.organize)
	}
}

func TestStaleDetailDoesNotOverwriteANewSelection(t *testing.T) {
	a := foldersPlaceApp(t, billingSecurityFolders())
	a.compose.setText("keep this sentence")
	if !a.pointFolderPlaceID("col-billing") {
		t.Fatal("could not stand on Billing")
	}
	a.refreshFolderPlace()
	req := folderDetailReq{
		selectedID: a.folderSheet.selectedID, selectedPath: a.folderSheet.selectedPath,
		gen: a.folderSheet.detailGen,
	}
	stale := folderDetailView{id: "col-billing", lines: []string{"stale billing"}}
	if !a.pointFolderPlaceID("col-security") {
		t.Fatal("could not stand on Security")
	}
	a.rememberFolderPlace()
	a.folderSheet.detailGen++
	if a.applyFolderDetail(req, stale) {
		t.Fatal("stale detail overwrote the new selection")
	}
	if a.folderSheet.selectedID != "col-security" {
		t.Fatalf("apply moved selection to %q", a.folderSheet.selectedID)
	}
	if a.compose.String() != "keep this sentence" {
		t.Fatalf("stale detail wiped the composer: %q", a.compose.String())
	}
	for _, line := range a.folderSheet.detail.lines {
		if line == "stale billing" {
			t.Fatal("stale detail lines landed on the new selection")
		}
	}
}

func TestManageThisFolderOpensAnOrdinaryChat(t *testing.T) {
	lab := newLiveLab(t)
	a := lab.app(lab.mine)
	a.start = func(workspace string) (Conversation, error) {
		return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: lab.mine, Workspace: workspace}, nil
	}
	a.width, a.height = 160, 40
	a.folders = billingSecurityFolders()
	a.showPage(pageFolders)
	if !a.pointFolderPlaceID("col-billing") {
		t.Fatal("could not stand on Billing")
	}
	a.manageThisFolder()
	if !a.startingChat() {
		t.Fatal("Manage this folder did not open an ordinary start page")
	}
	if a.pendingFolder != "col-billing" {
		t.Fatalf("Manage this folder pendingFolder=%q", a.pendingFolder)
	}
}

func TestNilSeamRefusesNewDetailDoors(t *testing.T) {
	a := foldersPlaceApp(t, nil)
	a.manageThisFolder()
	if a.folderSheet.note != folderUnwiredWord {
		t.Fatalf("Manage this folder on a nil seam said %q", a.folderSheet.note)
	}
	a.folderSheet.note = ""
	a.organizeThisChat()
	if a.folderSheet.note != folderUnwiredWord {
		t.Fatalf("Organize this chat on a nil seam said %q", a.folderSheet.note)
	}
}

func TestFolderDetailsFunctionsStayUnderTheCeiling(t *testing.T) {
	assertFolderComplexity(t, "folderdetails.go")
}

func TestDetailsDoNotReadTheStoreInView(t *testing.T) {
	fake := billingSecurityFolders()
	a := foldersPlaceApp(t, fake)
	a.width = 160
	a.refreshFolderPlace()
	reads := fake.reads
	fake.freeze()
	foldersPlaceText(a)
	a.moveFolderPlace(1)
	foldersPlaceText(a)
	if fake.reads != reads {
		t.Fatalf("View or a cursor move read Folders (%d -> %d)", reads, fake.reads)
	}
}
