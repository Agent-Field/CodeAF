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
	folder, ok := f.viewLocked(id)
	if !ok {
		return FolderView{}, nil, errors.New("unknown folder")
	}
	return folder, append([]FolderPlacement(nil), f.members[id]...), nil
}

// viewLocked finds a folder that RootSnapshot may have omitted: wsapi lists
// only parentless collections at Root, and a nested/shared child lives on
// FolderSnapshot placements whose Kind is collection.
func (f *fakeFolders) viewLocked(id string) (FolderView, bool) {
	for _, folder := range f.root.Folders {
		if folder.ID == id {
			return folder, true
		}
	}
	for _, places := range f.members {
		for _, place := range places {
			if folderCollectionPlacement(place) && place.RefID == id {
				return folderViewFromPlacement(place), true
			}
		}
	}
	return FolderView{}, false
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

// twoParentReceipts is J02: RootSnapshot lists only parentless folders, and
// Receipts is a Kind=collection member of both Billing and Security. The same
// chat sits inside Receipts so both paths show the same contents.
func twoParentReceipts() *fakeFolders {
	billing := FolderView{ID: "col-billing", Name: "Billing", Lifecycle: "active"}
	security := FolderView{ID: "col-security", Name: "Security", Lifecycle: "active"}
	underBilling := FolderPlacement{
		CollectionID: "col-billing", RefID: "col-receipts", Title: "Receipts",
		Kind: folderCollectionKind, AlsoIn: []string{"Security"},
	}
	underSecurity := FolderPlacement{
		CollectionID: "col-security", RefID: "col-receipts", Title: "Receipts",
		Kind: folderCollectionKind, AlsoIn: []string{"Billing"},
	}
	inside := FolderPlacement{
		CollectionID: "col-receipts", RefID: "aaaa000000000002", Title: "Emailed receipt links",
	}
	return &fakeFolders{
		root: FolderRoot{Folders: []FolderView{billing, security}},
		members: map[string][]FolderPlacement{
			"col-billing":  {underBilling},
			"col-security": {underSecurity},
			"col-receipts": {inside},
		},
	}
}

func TestSharedChildFolderShowsUnderBothParents(t *testing.T) {
	fake := twoParentReceipts()
	a := newLiveLab(t).open()
	a.folders = fake
	a.readHomeFolders()
	a.home.build()
	frame := homeText(a)
	if strings.Contains(frame, "Receipts") {
		t.Fatalf("Root listed a nested shared folder:\n%s", frame)
	}
	if strings.Contains(frame, "Emailed receipt links") {
		t.Fatalf("Root listed a nested folder's chats:\n%s", frame)
	}
	for _, parent := range []string{"col-billing", "col-security"} {
		a.enterFolder(parent)
		frame = homeText(a)
		if !strings.Contains(frame, "Receipts") {
			t.Fatalf("%s did not show the shared child:\n%s", parent, frame)
		}
		if folderRowKind(a, "col-receipts") != homeFolderRow {
			t.Fatalf("%s drew Receipts as %v, want a folder row", parent, folderRowKind(a, "col-receipts"))
		}
		if sessionRowOnFolders(a, "col-receipts") {
			t.Fatalf("%s dropped Kind and drew Receipts as a chat", parent)
		}
		a.enterFolder("col-receipts")
		if a.home.folderOpen != "col-receipts" {
			t.Fatalf("enter Receipts via %s left folderOpen %q", parent, a.home.folderOpen)
		}
		frame = homeText(a)
		if !strings.Contains(frame, "Emailed receipt links") {
			t.Fatalf("%s path did not show Receipts' contents:\n%s", parent, frame)
		}
		a.leaveFolder()
		if a.home.folderOpen != parent {
			t.Fatalf("esc from Receipts via %s jumped to %q", parent, a.home.folderOpen)
		}
		a.leaveFolder()
	}
}

func TestRenameFolderShowsUnderBothParents(t *testing.T) {
	fake := twoParentReceipts()
	a := newLiveLab(t).open()
	a.folders = fake
	a.readHomeFolders()
	a.home.build()
	homeText(a)
	a.enterFolder("col-billing")
	if cmd := a.runFoldersCommand("rename Receipts Invoices"); cmd != nil {
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
	a.enterFolder("col-receipts")
	a.home.say("", "")
	frame = homeText(a)
	if !strings.Contains(frame, "Invoices") {
		t.Fatalf("opened Billing path still used the old name:\n%s", frame)
	}
	if !strings.Contains(frame, "Emailed receipt links") {
		t.Fatalf("renamed folder lost its contents:\n%s", frame)
	}
	a.leaveFolder()
	a.leaveFolder()
	a.enterFolder("col-security")
	a.home.say("", "")
	frame = homeText(a)
	if !strings.Contains(frame, "Invoices") || strings.Contains(frame, "Receipts") {
		t.Fatalf("Security did not show the rename:\n%s", frame)
	}
	a.enterFolder("col-receipts")
	a.home.say("", "")
	frame = homeText(a)
	if !strings.Contains(frame, "Invoices") || !strings.Contains(frame, "Emailed receipt links") {
		t.Fatalf("Security path did not show the renamed folder's contents:\n%s", frame)
	}
}

func TestDroppedCollectionKindIsDrawnAsAChat(t *testing.T) {
	fake := twoParentReceipts()
	places := fake.members["col-billing"]
	places[0].Kind = ""
	fake.members["col-billing"] = places
	a := newLiveLab(t).open()
	a.folders = fake
	a.readHomeFolders()
	a.home.build()
	a.enterFolder("col-billing")
	homeText(a)
	if folderRowKind(a, "col-receipts") == homeFolderRow {
		t.Fatal("a Kind-less placement was still a folder row")
	}
	if !sessionRowOnFolders(a, "col-receipts") {
		t.Fatal("dropping Kind did not draw the nested folder as a chat")
	}
}

func folderRowKind(a *app, id string) homeRowKind {
	for _, line := range a.home.lines {
		if line.kind == homeFolderRow && line.dir == id {
			return line.kind
		}
	}
	return 0
}

func sessionRowOnFolders(a *app, refID string) bool {
	for _, line := range a.home.lines {
		if line.kind == homeSession && line.cell != nil && line.cell.panel == panelFolders && line.row.ID == refID {
			return true
		}
	}
	return false
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

func TestDropHomeKeepsTheFolderPath(t *testing.T) {
	fake := twoParentReceipts()
	a := newLiveLab(t).open()
	a.folders = fake
	a.readHomeFolders()
	a.home.build()
	homeText(a)
	a.enterFolder("col-billing")
	a.enterFolder("col-receipts")
	if a.home.folderOpen != "col-receipts" {
		t.Fatalf("setup left folderOpen %q", a.home.folderOpen)
	}
	trail := append([]string(nil), a.home.folderTrail...)
	a.dropHome()
	if a.home.folderOpen != "col-receipts" {
		t.Fatalf("dropHome forgot folderOpen: %q", a.home.folderOpen)
	}
	if len(a.home.folderTrail) != len(trail) {
		t.Fatalf("dropHome forgot folderTrail: %v", a.home.folderTrail)
	}
	a.raiseHome()
	if a.home.folderOpen != "col-receipts" {
		t.Fatalf("returning to home left folderOpen %q, want Receipts", a.home.folderOpen)
	}
	frame := homeText(a)
	if !strings.Contains(frame, "Emailed receipt links") {
		t.Fatalf("returning to home did not restore Receipts:\n%s", frame)
	}
}

func TestGonePlacementSaysFolderLostWord(t *testing.T) {
	fake := billingSecurityFolders()
	a := newLiveLab(t).open()
	a.folders = fake
	a.home.folderOpen = "col-billing"
	a.readHomeFolders()
	a.home.build()
	homeText(a)
	var member homeLine
	for _, line := range a.home.lines {
		if line.kind == homeSession && line.cell != nil && line.cell.panel == panelFolders {
			member = line
			break
		}
	}
	if member.kind == 0 {
		t.Fatal("no member row")
	}
	if !a.home.pointSame(member) {
		t.Fatal("could not stand on the member")
	}
	fake.members["col-billing"] = nil
	a.refreshFolderMemo()
	if a.home.msg != folderLostWord {
		t.Fatalf("gone placement said %q, want %q", a.home.msg, folderLostWord)
	}
	if a.home.folderOpen != "col-billing" {
		t.Fatalf("gone placement left the folder: %q", a.home.folderOpen)
	}
}

func TestGoneWorldRowIsLabelledUnavailable(t *testing.T) {
	fake := billingSecurityFolders()
	fake.members["col-billing"] = []FolderPlacement{{
		CollectionID: "col-billing",
		RefID:        "dead000000000001",
		Title:        "Deleted chat",
	}}
	a := newLiveLab(t).open()
	a.folders = fake
	a.home.folderOpen = "col-billing"
	a.readHomeFolders()
	a.home.build()
	frame := homeText(a)
	if !strings.Contains(frame, folderUnavailableWord) {
		t.Fatalf("gone world row was not labelled unavailable:\n%s", frame)
	}
	if !strings.Contains(frame, "Deleted chat") {
		t.Fatalf("unavailable row looked empty:\n%s", frame)
	}
	var member homeLine
	for _, line := range a.home.lines {
		if line.kind == homeSession && line.cell != nil && line.cell.panel == panelFolders && line.row.ID == "dead000000000001" {
			member = line
			break
		}
	}
	if member.kind == 0 {
		t.Fatal("unavailable member was not drawn")
	}
	if !folderMemberUnavailable(member) {
		t.Fatal("gone world row was drawn as an ordinary chat")
	}
}

func TestFolderWhyLineKeepsEvidenceAndAt(t *testing.T) {
	why := FolderWhy{
		Origin:   "person",
		Reason:   "filed from home",
		Actor:    "me",
		Evidence: "receipt thread",
		At:       "2026-09-18T12:00:00Z",
	}
	got := folderWhyLine(why)
	for _, want := range []string{"person", "filed from home", "me", "receipt thread", "2026-09-18T12:00:00Z"} {
		if !strings.Contains(got, want) {
			t.Fatalf("why line %q dropped %q", got, want)
		}
	}
	if got := folderWhyLine(FolderWhy{Origin: "person"}); got != "person" {
		t.Fatalf("empty why fields drew %q", got)
	}
	fake := billingSecurityFolders()
	fake.whys = map[string]FolderWhy{"col-billing/aaaa000000000001": why}
	a := newLiveLab(t).open()
	a.folders = fake
	member := homeLine{kind: homeSession, row: session.SessionRow{ID: "aaaa000000000001"}, cell: &homeCell{panel: panelFolders, key: "col-billing"}}
	a.folderWhyHere(member)
	for _, want := range []string{"person", "filed from home", "me", "receipt thread", "2026-09-18T12:00:00Z"} {
		if !strings.Contains(a.home.msg, want) {
			t.Fatalf("w said %q, dropped %q", a.home.msg, want)
		}
	}
}
