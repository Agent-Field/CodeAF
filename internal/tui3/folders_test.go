package tui3

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// fakeFolders is an in-memory Folders seam. freeze panics on any call so a
// test can prove View and a cursor move never read the store.
type fakeFolders struct {
	mu      sync.Mutex
	frozen  bool
	root    FolderRoot
	members map[string][]FolderPlacement
	whys    map[string]FolderWhy
	err     error
	creates int
	adds    [][2]string
	removes [][2]string
	moves   [][3]string
	renames [][2]string
	reads   int
}

func (f *fakeFolders) freeze() { f.mu.Lock(); f.frozen = true; f.mu.Unlock() }

func (f *fakeFolders) touch(op string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.frozen {
		panic("Folders." + op + " called after freeze (View or cursor must not read the store)")
	}
	if strings.Contains(op, "Snapshot") {
		f.reads++
	}
}

func (f *fakeFolders) RootSnapshot(context.Context) (FolderRoot, error) {
	f.touch("RootSnapshot")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return FolderRoot{}, f.err
	}
	return f.root, nil
}

func (f *fakeFolders) FolderSnapshot(_ context.Context, id string) (FolderView, []FolderPlacement, error) {
	f.touch("FolderSnapshot")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return FolderView{}, nil, f.err
	}
	for _, folder := range f.root.Folders {
		if folder.ID == id {
			return folder, append([]FolderPlacement(nil), f.members[id]...), nil
		}
	}
	return FolderView{}, nil, nil
}

func (f *fakeFolders) CreateFolder(_ context.Context, name string) (FolderView, error) {
	f.touch("CreateFolder")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates++
	folder := FolderView{ID: "col-" + name, Name: name, Lifecycle: "active"}
	f.root.Folders = append(f.root.Folders, folder)
	return folder, nil
}

func (f *fakeFolders) AddPlacement(_ context.Context, collectionID, refID string) error {
	f.touch("AddPlacement")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.adds = append(f.adds, [2]string{collectionID, refID})
	return nil
}

func (f *fakeFolders) RemovePlacement(_ context.Context, collectionID, refID string) error {
	f.touch("RemovePlacement")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removes = append(f.removes, [2]string{collectionID, refID})
	return nil
}

func (f *fakeFolders) MovePlacement(_ context.Context, fromID, toID, refID string) error {
	f.touch("MovePlacement")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moves = append(f.moves, [3]string{fromID, toID, refID})
	return nil
}

func (f *fakeFolders) RenameFolder(_ context.Context, id, name string) error {
	f.touch("RenameFolder")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renames = append(f.renames, [2]string{id, name})
	for i, folder := range f.root.Folders {
		if folder.ID == id {
			f.root.Folders[i].Name = name
		}
	}
	for collectionID, places := range f.members {
		for i, place := range places {
			if folderCollectionPlacement(place) && place.RefID == id {
				places[i].Title = name
			}
		}
		f.members[collectionID] = places
	}
	return nil
}

func (f *fakeFolders) WhyHere(_ context.Context, collectionID, refID string) (FolderWhy, error) {
	f.touch("WhyHere")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.whys != nil {
		if why, ok := f.whys[collectionID+"/"+refID]; ok {
			return why, nil
		}
	}
	return FolderWhy{Origin: "person", Reason: "filed from home"}, nil
}

func billingSecurityFolders() *fakeFolders {
	billing := FolderView{ID: "col-billing", Name: "Billing", Lifecycle: "active", MemberCount: 1}
	receipts := FolderView{ID: "col-receipts", Name: "Receipts", Lifecycle: "active", ParentIDs: []string{"col-billing", "col-security"}}
	security := FolderView{ID: "col-security", Name: "Security", Lifecycle: "active", MemberCount: 1}
	place := FolderPlacement{
		CollectionID: "col-billing",
		RefID:        "aaaa000000000001",
		Title:        "Porting the Resume Picker",
		AlsoIn:       []string{"Security"},
	}
	return &fakeFolders{
		root: FolderRoot{Folders: []FolderView{billing, receipts, security}},
		members: map[string][]FolderPlacement{
			"col-billing":  {place},
			"col-security": {{CollectionID: "col-security", RefID: "aaaa000000000001", Title: "Porting the Resume Picker", AlsoIn: []string{"Billing"}}},
		},
	}
}

func TestFoldersIsNotAnAliasOfFolder(t *testing.T) {
	if err := checkCommands(commands); err != nil {
		t.Fatal(err)
	}
	for _, c := range commands {
		if c.name == "folder" {
			for _, word := range c.alias {
				if word == "folders" {
					t.Fatal("/folders is an alias of /folder")
				}
			}
		}
		if c.name == "folders" {
			for _, word := range c.alias {
				if word == "folder" || word == "place" || word == "dir" {
					t.Fatalf("/folders aliases filesystem /%s", word)
				}
			}
		}
	}
	if err := checkCommands([]command{
		{name: "folder", alias: []string{"folders"}},
	}); err == nil {
		t.Fatal("checkCommands allowed /folders as an alias of /folder")
	}
	if got := homeFate("folders", ""); got != fatePlace {
		t.Fatalf("bare /folders fate %q, want %q", got, fatePlace)
	}
	if got := homeFate("folder", ""); got != fateTargetFolder {
		t.Fatalf("/folder fate moved to %q", got)
	}
}

func TestPendingFolderEscCreatesNothing(t *testing.T) {
	lab := newLiveLab(t)
	started := 0
	a := lab.app(lab.mine)
	a.start = func(workspace string) (Conversation, error) {
		started++
		return Conversation{
			Agent:       &fakeAgent{model: "m"},
			SessionFile: lab.mine,
			Workspace:   workspace,
		}, nil
	}
	a.width, a.height = 120, 45
	a.folders = billingSecurityFolders()
	a.openHome()
	homeText(a)
	before := a.file
	a.pendingFolder = "col-billing"
	a.welcome.open, a.welcome.start = true, true
	if !a.startingChat() {
		t.Fatal("start page was not up")
	}
	a.cancelChatStart()
	a.reapPendingFolder()
	if a.pendingFolder != "" {
		t.Fatalf("esc left pendingFolder %q", a.pendingFolder)
	}
	if a.startingChat() {
		t.Fatal("esc left the start page up")
	}
	if a.file != before {
		t.Fatalf("esc replaced the transcript: %q -> %q", before, a.file)
	}
	if started != 0 {
		t.Fatalf("esc minted %d conversations", started)
	}
	fake := a.folders.(*fakeFolders)
	if len(fake.adds) != 0 {
		t.Fatalf("esc filed a placement: %v", fake.adds)
	}
}

func TestFirstMessageFilesThePendingFolder(t *testing.T) {
	lab := newLiveLab(t)
	a := lab.app(lab.mine)
	a.folders = billingSecurityFolders()
	a.pendingFolder = "col-billing"
	a.welcome.open, a.welcome.start = true, true
	a.file = lab.mine
	a.filePendingFolder()
	fake := a.folders.(*fakeFolders)
	if len(fake.adds) != 1 || fake.adds[0] != [2]string{"col-billing", "aaaa000000000001"} {
		t.Fatalf("first message filed %v, want billing/aaaa000000000001", fake.adds)
	}
	if a.pendingFolder != "" {
		t.Fatalf("pendingFolder survived the first message: %q", a.pendingFolder)
	}
}

func TestComposerAndSelectionSurviveAMembershipChange(t *testing.T) {
	lab := newLiveLab(t)
	fake := billingSecurityFolders()
	a := lab.open()
	a.folders = fake
	a.readHomeFolders()
	a.home.build()
	homeText(a)
	a.pointFolderID("col-billing")
	want, ok := a.home.focusedLine()
	if !ok || want.kind != homeFolderRow || want.dir != "col-billing" {
		t.Fatalf("cursor was not on Billing: %+v", want)
	}
	a.home.box.setText("keep this sentence")
	fake.root.Folders = append(fake.root.Folders, FolderView{ID: "col-ops", Name: "Ops", Lifecycle: "active"})
	a.readHomeFolders()
	a.home.build()
	homeText(a)
	if a.home.box.String() != "keep this sentence" {
		t.Fatalf("membership change wiped the composer: %q", a.home.box.String())
	}
	a.home.box.setText("")
	a.home.build()
	homeText(a)
	a.pointFolderID("col-billing")
	got, ok := a.home.focusedLine()
	if !ok || got.kind != homeFolderRow || got.dir != "col-billing" {
		t.Fatalf("membership change dropped Billing: %+v", got)
	}
}

func TestNilFoldersIsUnavailableNotEmpty(t *testing.T) {
	a := newLiveLab(t).open()
	a.folders = nil
	a.readHomeFolders()
	a.home.build()
	frame := homeText(a)
	if !strings.Contains(frame, "folders") {
		t.Fatalf("nil Folders dropped the heading:\n%s", frame)
	}
	if strings.Contains(frame, folderWhisperWord) {
		t.Fatalf("nil Folders masqueraded as an empty working store:\n%s", frame)
	}
	if !strings.Contains(frame, folderUnwiredWord) {
		t.Fatalf("nil Folders hid the refusal:\n%s", frame)
	}

	member := homeLine{kind: homeSession, row: session.SessionRow{ID: "aaaa000000000001"}, cell: &homeCell{panel: panelFolders, key: "col-billing"}}
	for _, step := range []struct {
		name string
		run  func() tea.Cmd
	}{
		{"n", func() tea.Cmd { return a.startInFolder("col-billing") }},
		{"f", func() tea.Cmd { return a.addCurrentToFolder("col-billing") }},
		{"m", func() tea.Cmd { return a.beginFolderMove(member) }},
		{"w", func() tea.Cmd { return a.folderWhyHere(member) }},
		{"x", func() tea.Cmd { return a.removeFolderPlacement(member) }},
		{"create", func() tea.Cmd { return a.createLogicalFolder("Billing") }},
		{"add", func() tea.Cmd { return a.addNamedFolder("Billing") }},
		{"rename", func() tea.Cmd { return a.renameLogicalFolder("Billing", "Invoices") }},
	} {
		a.home.say("", "")
		if cmd := step.run(); cmd != nil {
			t.Fatalf("%s returned a command on a nil store", step.name)
		}
		if a.home.msg != folderUnwiredWord {
			t.Fatalf("%s said %q, want %q", step.name, a.home.msg, folderUnwiredWord)
		}
	}
	if a.pendingFolder != "" || a.pendingMoveFrom != "" {
		t.Fatalf("nil mutation left pending state folder=%q move=%q", a.pendingFolder, a.pendingMoveFrom)
	}
}

func TestEmptyWorkingFoldersDrawsTheWhisper(t *testing.T) {
	a := newLiveLab(t).open()
	a.folders = &fakeFolders{}
	a.readHomeFolders()
	a.home.build()
	frame := homeText(a)
	if !strings.Contains(frame, folderWhisperWord) {
		t.Fatalf("empty working store dropped the whisper:\n%s", frame)
	}
	if strings.Contains(frame, folderUnwiredWord) {
		t.Fatalf("empty working store said it was unwired:\n%s", frame)
	}
	if strings.Contains(frame, "no folders yet") {
		t.Fatalf("empty working store broke the emptiness law:\n%s", frame)
	}
}

func TestFolderReadErrorKeepsTheLastGoodSnapshot(t *testing.T) {
	fake := billingSecurityFolders()
	a := newLiveLab(t).open()
	a.folders = fake
	a.readHomeFolders()
	a.home.build()
	homeText(a)
	a.pointFolderID("col-billing")
	fake.err = errors.New("store down")
	a.readHomeFolders()
	a.home.build()
	frame := homeText(a)
	if !strings.Contains(frame, "Billing") {
		t.Fatalf("a failed refresh wiped the last good snapshot:\n%s", frame)
	}
	if strings.Contains(frame, folderWhisperWord) {
		t.Fatalf("a failed refresh masqueraded as empty:\n%s", frame)
	}
	got, ok := a.home.focusedLine()
	if !ok || got.kind != homeFolderRow || got.dir != "col-billing" {
		t.Fatalf("a failed refresh moved the cursor to %+v", got)
	}
}

func TestSharedChildFolderShowsUnderBothParents(t *testing.T) {
	billing := FolderView{ID: "col-billing", Name: "Billing", Lifecycle: "active"}
	security := FolderView{ID: "col-security", Name: "Security", Lifecycle: "active"}
	receipts := FolderPlacement{RefID: "col-receipts", Title: "Receipts", Kind: folderCollectionKind}
	fake := &fakeFolders{
		root: FolderRoot{Folders: []FolderView{billing, security}},
		members: map[string][]FolderPlacement{
			"col-billing":  {receipts},
			"col-security": {receipts},
		},
	}
	a := newLiveLab(t).open()
	a.folders = fake
	a.readHomeFolders()
	a.home.build()
	frame := homeText(a)
	if strings.Contains(frame, "Receipts") {
		t.Fatalf("Root listed a nested shared folder:\n%s", frame)
	}
	a.enterFolder("col-billing")
	frame = homeText(a)
	if !strings.Contains(frame, "Receipts") {
		t.Fatalf("Billing did not show the shared child:\n%s", frame)
	}
	a.leaveFolder()
	a.enterFolder("col-security")
	frame = homeText(a)
	if !strings.Contains(frame, "Receipts") {
		t.Fatalf("Security did not show the shared child:\n%s", frame)
	}
}

func TestRenameFolderShowsUnderBothParents(t *testing.T) {
	billing := FolderView{ID: "col-billing", Name: "Billing", Lifecycle: "active"}
	security := FolderView{ID: "col-security", Name: "Security", Lifecycle: "active"}
	receipts := FolderPlacement{RefID: "col-receipts", Title: "Receipts", Kind: folderCollectionKind}
	fake := &fakeFolders{
		root: FolderRoot{Folders: []FolderView{billing, security}},
		members: map[string][]FolderPlacement{
			"col-billing":  {receipts},
			"col-security": {receipts},
		},
	}
	a := newLiveLab(t).open()
	a.folders = fake
	a.readHomeFolders()
	a.home.build()
	homeText(a)
	a.enterFolder("col-billing")
	if cmd := a.renameLogicalFolder("Receipts", "Invoices"); cmd != nil {
		t.Fatal("rename returned a command")
	}
	if len(fake.renames) != 1 || fake.renames[0] != [2]string{"col-receipts", "Invoices"} {
		t.Fatalf("rename called %v", fake.renames)
	}
	a.home.say("", "")
	frame := homeText(a)
	if !strings.Contains(frame, "Invoices") || strings.Contains(frame, "Receipts") {
		t.Fatalf("Billing still showed the old name:\n%s", frame)
	}
	a.leaveFolder()
	a.enterFolder("col-security")
	a.home.say("", "")
	frame = homeText(a)
	if !strings.Contains(frame, "Invoices") || strings.Contains(frame, "Receipts") {
		t.Fatalf("Security did not show the rename:\n%s", frame)
	}
}

func TestFolderSessionLookupFindsTheWorldRow(t *testing.T) {
	a := newLiveLab(t).open()
	in := a.home.gridInput()
	row, ok := folderSessionOf(&in, "aaaa000000000001")
	if !ok || row.ID != "aaaa000000000001" {
		t.Fatalf("did not find the live session: ok=%v row=%+v", ok, row)
	}
	if _, ok := folderSessionOf(&in, ""); ok {
		t.Fatal("empty id looked up a session")
	}
}

func TestAlsoInClause(t *testing.T) {
	if got := folderAlsoIn(nil); got != "" {
		t.Fatalf("empty AlsoIn drew %q", got)
	}
	if got := folderAlsoIn([]string{"Security"}); got != "also in Security" {
		t.Fatalf("dual placement said %q", got)
	}
}
