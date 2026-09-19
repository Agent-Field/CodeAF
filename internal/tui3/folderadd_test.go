package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

func folderAddSessions() []Session {
	return []Session{
		{
			Title: "copper receipt lookup",
			Last:  "filed the August invoice",
			File:  filepath.Join("/saved", "cccc000000000001", "transcript.jsonl"),
		},
		{
			Title: "security review notes",
			Last:  "rotate the keys",
			File:  filepath.Join("/saved", "cccc000000000002", "transcript.jsonl"),
		},
		{
			Title: "porting the resume picker",
			Last:  "already in Billing",
			File:  filepath.Join("/saved", "aaaa000000000001", "transcript.jsonl"),
		},
	}
}

func folderAddApp(t *testing.T) (*app, *fakeFolders) {
	t.Helper()
	fake := billingSecurityFolders()
	a := foldersPlaceApp(t, fake)
	a.recentSessions = func() []Session { return folderAddSessions() }
	a.resume = func(string) (Agent, error) {
		t.Fatal("Add existing chats opened a conversation; it must not merge histories")
		return nil, nil
	}
	return a, fake
}

func standInBilling(t *testing.T, a *app) {
	t.Helper()
	if !a.pointFolderPlaceID("col-billing") {
		t.Fatal("could not stand on Billing")
	}
	a.enterFolderPlace()
	if strings.TrimSpace(a.folderSheet.open) != "col-billing" {
		t.Fatalf("open folder %q, want Billing", a.folderSheet.open)
	}
}

func TestAddExistingChatsIsARestableAndAChord(t *testing.T) {
	a, _ := folderAddApp(t)
	found := false
	for _, stop := range a.folderSheet.stops {
		if stop.kind == folderStopAdd && stop.kind.actionWord() == folderAddExistingWord {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Folders place hid the Add existing chats restable")
	}
	if !strings.Contains(foldersPlaceText(a), folderAddExistingWord) {
		t.Fatalf("Folders place hid %q:\n%s", folderAddExistingWord, foldersPlaceText(a))
	}
	found = false
	for _, v := range a.folderPlaceVerbs() {
		if v.key == 'b' && v.word == folderAddExistingWord {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Folders place hid chord b Add existing chats")
	}
}

func TestAddExistingChatsListsInventoryTitlesNotIds(t *testing.T) {
	a, fake := folderAddApp(t)
	standInBilling(t, a)
	if cmd := a.beginFolderAdd(); cmd != nil {
		t.Fatal("beginFolderAdd returned a command")
	}
	if !a.folderSheet.add.open {
		t.Fatal("Add existing chats did not open the picker")
	}
	frame := foldersPlaceText(a)
	for _, want := range []string{
		humanName(folderAddSessions()[0]),
		humanName(folderAddSessions()[1]),
		"filed the August invoice",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("picker hid inventory title %q:\n%s", want, frame)
		}
	}
	for _, id := range []string{"cccc000000000001", "cccc000000000002", "aaaa000000000001"} {
		if strings.Contains(frame, id) {
			t.Fatalf("picker painted remembered id %s:\n%s", id, frame)
		}
	}
	if strings.Contains(frame, humanName(folderAddSessions()[2])) {
		t.Fatalf("picker listed a chat already in Billing:\n%s", frame)
	}
	if len(fake.adds) != 0 {
		t.Fatalf("opening the picker filed %v", fake.adds)
	}
}

func TestAddExistingChatsAddsSeveralWithoutOpeningThem(t *testing.T) {
	a, fake := folderAddApp(t)
	standInBilling(t, a)
	a.beginFolderAdd()
	a.folderAddKey(key(" "))
	a.folderAddKey(key("down"))
	a.folderAddKey(key(" "))
	a.folderAddKey(key("enter"))
	if a.folderSheet.add.open {
		t.Fatal("enter left the picker open")
	}
	if !a.at(pageFolders) {
		t.Fatalf("adding chats left Folders for %q", a.page.word())
	}
	if got := fake.adds; len(got) != 2 ||
		got[0] != [2]string{"col-billing", "cccc000000000001"} ||
		got[1] != [2]string{"col-billing", "cccc000000000002"} {
		t.Fatalf("AddPlacement wrote %v", got)
	}
	if !strings.Contains(a.folderSheet.note, "Added to Billing") {
		t.Fatalf("success note %q, want Added to Billing", a.folderSheet.note)
	}
}

func TestAddExistingChatsFiltersByInventoryTitle(t *testing.T) {
	a, fake := folderAddApp(t)
	standInBilling(t, a)
	a.beginFolderAdd()
	a.folderAddKey(key("c"))
	a.folderAddKey(key("o"))
	a.folderAddKey(key("p"))
	a.folderAddKey(key("p"))
	a.folderAddKey(key("e"))
	a.folderAddKey(key("r"))
	frame := foldersPlaceText(a)
	if !strings.Contains(frame, humanName(folderAddSessions()[0])) {
		t.Fatalf("filter dropped the matching title:\n%s", frame)
	}
	if strings.Contains(frame, humanName(folderAddSessions()[1])) {
		t.Fatalf("filter kept a non-matching title:\n%s", frame)
	}
	a.folderAddKey(key("enter"))
	if len(fake.adds) != 1 || fake.adds[0] != [2]string{"col-billing", "cccc000000000001"} {
		t.Fatalf("filtered enter wrote %v", fake.adds)
	}
}

func TestAddExistingChatsEscAddsNothing(t *testing.T) {
	a, fake := folderAddApp(t)
	standInBilling(t, a)
	a.beginFolderAdd()
	a.folderAddKey(key(" "))
	a.folderAddKey(key("esc"))
	if a.folderSheet.add.open {
		t.Fatal("esc left the picker open")
	}
	if len(fake.adds) != 0 {
		t.Fatalf("esc filed %v", fake.adds)
	}
	if !a.at(pageFolders) {
		t.Fatalf("esc left Folders for %q", a.page.word())
	}
}

func TestAddExistingChatsEnterOnTheRestableOpensThePicker(t *testing.T) {
	a, _ := folderAddApp(t)
	standInBilling(t, a)
	at := -1
	for i, stop := range a.folderSheet.stops {
		if stop.kind == folderStopAdd {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatal("inside Billing hid Add existing chats")
	}
	a.folderSheet.cursor = at
	a.rememberFolderPlace()
	a.enterFolderPlace()
	if !a.folderSheet.add.open {
		t.Fatal("enter on Add existing chats did not open the picker")
	}
}

func TestAddExistingChatsRefusesWithoutAFolder(t *testing.T) {
	a, fake := folderAddApp(t)
	a.folderSheet.cursor = 0
	a.rememberFolderPlace()
	a.beginFolderAdd()
	if a.folderSheet.add.open {
		t.Fatal("picker opened with no folder to add to")
	}
	if a.folderSheet.note != folderAddNoStand {
		t.Fatalf("no-folder note %q", a.folderSheet.note)
	}
	if len(fake.adds) != 0 {
		t.Fatalf("no-folder add wrote %v", fake.adds)
	}
}

func TestAddExistingChatsNilSeamRefuses(t *testing.T) {
	a := foldersPlaceApp(t, nil)
	a.recentSessions = func() []Session { return folderAddSessions() }
	a.beginFolderAdd()
	if a.folderSheet.add.open {
		t.Fatal("nil Folders opened the picker")
	}
	if a.folderSheet.note != folderUnwiredWord {
		t.Fatalf("nil Folders said %q", a.folderSheet.note)
	}
}

func TestAddExistingChatsPickerDoesNotReadTheStoreInView(t *testing.T) {
	a, fake := folderAddApp(t)
	standInBilling(t, a)
	a.beginFolderAdd()
	foldersPlaceText(a)
	reads := fake.reads
	fake.freeze()
	foldersPlaceText(a)
	a.folderAddKey(key("down"))
	foldersPlaceText(a)
	if fake.reads != reads {
		t.Fatalf("picker View or cursor move read Folders (%d -> %d)", reads, fake.reads)
	}
}

func TestFolderAddFunctionsStayUnderTheCeiling(t *testing.T) {
	set := token.NewFileSet()
	source, err := parser.ParseFile(set, "folderadd.go", nil, 0)
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
