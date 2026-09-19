package tui3

import (
	"context"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Folders is the home panel's seam onto logical folder membership. It is a TUI
// interface so this package never imports internal/workspace, and so a nil
// value is a panel that still draws its whisper rather than a belt that fails
// every time it is asked. Wiring may wrap *wsapi.Service; the methods here are
// the snapshot and mutate set the panel actually calls.
//
// THE SNAPSHOT IS TAKEN ON THE HOME BEAT and nowhere else. View, the cursor and
// a mere rebuild read [homeView.folders], which is a memo. A call from paint
// would be a store read on a draw, which this surface forbids.
type Folders interface {
	RootSnapshot(ctx context.Context) (folderRoot, error)
	FolderSnapshot(ctx context.Context, id string) (folderView, []folderPlacement, error)
	CreateFolder(ctx context.Context, name string) (folderView, error)
	AddPlacement(ctx context.Context, collectionID, refID string) error
	RemovePlacement(ctx context.Context, collectionID, refID string) error
	MovePlacement(ctx context.Context, fromID, toID, refID string) error
	WhyHere(ctx context.Context, collectionID, refID string) (folderWhy, error)
}

// folderView is one logical folder as the panel draws it. ParentIDs empty means
// the folder stands at Root. MemberCount is unique conversation IDs, not paths.
type folderView struct {
	ID, Name, Purpose, Lifecycle string
	Revision                     int
	ParentIDs                    []string
	MemberCount                  int
}

// folderPlacement is one conversation sitting in a folder. AlsoIn names the
// other folders that hold the same chat, so a dual placement can say
// "also in Security" without a second store round-trip on the draw.
type folderPlacement struct {
	CollectionID string
	RefID, Title string
	AlsoIn       []string
}

// folderWhy is the latest membership event for an edge, as the `w` verb shows
// it: who put it here and why, with no confidence score.
type folderWhy struct {
	Origin, Reason, Actor, Evidence, At string
}

// folderRoot is Root as the beat caches it. Folders is every collection the
// service knows — parentless ones are Root's own rows, and ParentIDs is how a
// drilled-in folder finds its children — because FolderSnapshot returns
// placements, not nested folders.
type folderRoot struct {
	Folders  []folderView
	Unfiled  []folderPlacement
	Revision int
}

// homeFoldersReading is the memo [app.readHomeFolders] writes on the beat.
// rows() reads this and never the seam.
type homeFoldersReading struct {
	root    folderRoot
	open    folderView
	members []folderPlacement
}

// The verb strip on a folders row, quoted in the contract and the manual as
// these exact phrases.
const (
	folderNewChatWord     = "new chat here"
	folderAddHereWord     = "add current chat"
	folderMoveWord        = "move this placement"
	folderWhyWord         = "why here"
	folderRemoveWord      = "remove this placement"
	folderAlsoInWord      = "also in "
	folderWhisperWord     = "logical groups of chats · /folders create Billing"
	folderBackWord        = "back"
	folderUnwiredWord     = "folders are not wired here"
	folderNoStandWord     = "stand on a folder · then n starts a chat there"
	folderMoveHintWord    = "enter a folder to move it there · esc cancel"
	folderLostWord        = "that chat is no longer in this folder"
	logicalFolderGoneWord = "that folder is no longer here"
	folderFiledWord       = "could not file this chat here"
)

// homeFolderRow is one logical folder on the folders panel. Numbered outside
// the [homeRowKind] iota for the same reason [homeProjectRow] is: that block is
// edited by other lanes.
const homeFolderRow homeRowKind = 246

// homeFolderBack is the sequential way out of a drilled-in folder: enter or
// esc returns to Root (or to the parent list). It exists so 80-column home
// never grows a third column of members beside the folders.
const homeFolderBack homeRowKind = 247

// readHomeFolders is the beat's reading. Nil Folders leaves the memo empty so
// the panel still draws its whisper. The open folder's members are taken in
// the same pass, so a cursor move never has to ask.
func (a *app) readHomeFolders() {
	a.home.folders = homeFoldersReading{}
	if a.folders == nil {
		return
	}
	ctx := a.folderCtx()
	root, err := a.folders.RootSnapshot(ctx)
	if err != nil {
		return
	}
	a.home.folders.root = root
	open := strings.TrimSpace(a.home.folderOpen)
	if open == "" {
		return
	}
	folder, members, err := a.folders.FolderSnapshot(ctx, open)
	if err != nil {
		// THE OPEN OBJECT SURVIVES A MISSING FOLDER: we drop the drill-in and
		// say so, rather than leaving the cursor inside a collection the beat
		// can no longer name (J06).
		a.home.folderOpen = ""
		a.home.say(logicalFolderGoneWord, "")
		return
	}
	a.home.folders.open = folder
	a.home.folders.members = members
}

func (a *app) folderCtx() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// folderVerbs is `→` on a folders-panel row: new chat here, add the current
// chat, and — on a placement — move, why here, and remove. A verb that cannot
// work is absent, not broken.
func (a *app) folderVerbs(line homeLine) []verb {
	verbs := []verb{
		{key: 'n', word: folderNewChatWord, do: func() tea.Cmd { return a.startInFolder(a.folderIDOf(line)) }},
		{key: 'f', word: folderAddHereWord, do: func() tea.Cmd { return a.addCurrentToFolder(a.folderIDOf(line)) }},
	}
	if line.kind == homeSession {
		verbs = append(verbs,
			verb{key: 'm', word: folderMoveWord, do: func() tea.Cmd { return a.beginFolderMove(line) }},
			verb{key: 'w', word: folderWhyWord, do: func() tea.Cmd { return a.folderWhyHere(line) }},
			verb{key: 'x', word: folderRemoveWord, do: func() tea.Cmd { return a.removeFolderPlacement(line) }},
		)
	}
	return verbs
}

func (a *app) folderIDOf(line homeLine) string {
	switch line.kind {
	case homeFolderRow:
		return strings.TrimSpace(line.dir)
	case homeFolderBack:
		return strings.TrimSpace(a.home.folderOpen)
	case homeSession:
		if line.cell != nil {
			if id := strings.TrimSpace(line.cell.key); id != "" {
				return id
			}
		}
		return strings.TrimSpace(a.home.folderOpen)
	}
	return strings.TrimSpace(a.home.folderOpen)
}

func (a *app) placementRefOf(line homeLine) string {
	if line.kind != homeSession {
		return ""
	}
	if id := strings.TrimSpace(line.row.ID); id != "" {
		return id
	}
	return a.conversationRef()
}

// startInFolder is `n` / `/folders new`: the existing start page, with
// pendingFolder set so the first message files the new conversation here.
// Nothing is minted until that message. Esc clears the pending id and creates
// no transcript.
func (a *app) startInFolder(id string) tea.Cmd {
	id = strings.TrimSpace(id)
	if id == "" {
		a.folderNote(folderNoStandWord)
		return nil
	}
	if !a.canStart() {
		a.folderNote(newUnavailableWord)
		return nil
	}
	a.closeHome()
	cmd := a.openChatStart()
	if a.startingChat() {
		a.pendingFolder = id
	}
	return cmd
}

// conversationRef is the 16-hex session id membership talks in — the folder
// name of the journal, never a UI path.
func (a *app) conversationRef() string {
	id := strings.TrimSpace(filepath.Base(homeSessionDirOf(a.file)))
	if id == "" || id == "." || id == string(filepath.Separator) {
		return ""
	}
	return id
}

// filePendingFolder is first-message order: the transcript has just been
// minted ([app.renewRefusing]), then we file. A failure leaves the chat
// unfiled under Root; retry is `/folders add` and is idempotent.
func (a *app) filePendingFolder() {
	id := strings.TrimSpace(a.pendingFolder)
	a.pendingFolder = ""
	if id == "" || a.folders == nil {
		return
	}
	ref := a.conversationRef()
	if ref == "" {
		return
	}
	if err := a.folders.AddPlacement(a.folderCtx(), id, ref); err != nil {
		a.note(folderFiledWord)
	}
}

// reapPendingFolder is esc's half of pending membership: once the start page
// is no longer up and no first message minted a transcript, the pending id is
// dropped and nothing was created. It runs at the end of Update so chatstart.go
// does not have to know about folders.
func (a *app) reapPendingFolder() {
	if a.pendingFolder != "" && !a.startingChat() {
		a.pendingFolder = ""
	}
}

func (a *app) addCurrentToFolder(id string) tea.Cmd {
	id = strings.TrimSpace(id)
	if id == "" {
		a.folderNote(folderNoStandWord)
		return nil
	}
	if a.folders == nil {
		a.folderNote(folderUnwiredWord)
		return nil
	}
	ref := a.conversationRef()
	if ref == "" {
		a.folderNote("no current chat to file")
		return nil
	}
	if err := a.folders.AddPlacement(a.folderCtx(), id, ref); err != nil {
		a.folderNote(folderFiledWord)
		return nil
	}
	a.refreshFolderMemo()
	return nil
}

func (a *app) beginFolderMove(line homeLine) tea.Cmd {
	from := a.folderIDOf(line)
	ref := a.placementRefOf(line)
	if from == "" || ref == "" {
		return nil
	}
	a.pendingMoveFrom, a.pendingMoveRef = from, ref
	a.folderNote(folderMoveHintWord)
	return nil
}

func (a *app) clearFolderMove() bool {
	if a.pendingMoveFrom == "" && a.pendingMoveRef == "" {
		return false
	}
	a.pendingMoveFrom, a.pendingMoveRef = "", ""
	return true
}

func (a *app) completeFolderMove(toID string) tea.Cmd {
	toID = strings.TrimSpace(toID)
	from, ref := a.pendingMoveFrom, a.pendingMoveRef
	a.pendingMoveFrom, a.pendingMoveRef = "", ""
	if a.folders == nil {
		a.folderNote(folderUnwiredWord)
		return nil
	}
	if toID == "" || from == "" || ref == "" || toID == from {
		return nil
	}
	if err := a.folders.MovePlacement(a.folderCtx(), from, toID, ref); err != nil {
		a.folderNote("could not move that chat here")
		return nil
	}
	a.refreshFolderMemo()
	return nil
}

func (a *app) folderWhyHere(line homeLine) tea.Cmd {
	if a.folders == nil {
		a.folderNote(folderUnwiredWord)
		return nil
	}
	id, ref := a.folderIDOf(line), a.placementRefOf(line)
	if id == "" || ref == "" {
		return nil
	}
	why, err := a.folders.WhyHere(a.folderCtx(), id, ref)
	if err != nil {
		a.folderNote("could not say why this chat is here")
		return nil
	}
	a.folderNote(folderWhyLine(why))
	return nil
}

func folderWhyLine(why folderWhy) string {
	var parts []string
	for _, p := range []string{strings.TrimSpace(why.Origin), strings.TrimSpace(why.Reason), strings.TrimSpace(why.Actor)} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
}

func (a *app) removeFolderPlacement(line homeLine) tea.Cmd {
	if a.folders == nil {
		a.folderNote(folderUnwiredWord)
		return nil
	}
	id, ref := a.folderIDOf(line), a.placementRefOf(line)
	if id == "" || ref == "" {
		return nil
	}
	if err := a.folders.RemovePlacement(a.folderCtx(), id, ref); err != nil {
		a.folderNote("could not remove that placement")
		return nil
	}
	a.refreshFolderMemo()
	return nil
}

func (a *app) enterFolder(id string) tea.Cmd {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	if a.pendingMoveRef != "" {
		return a.completeFolderMove(id)
	}
	a.home.folderOpen = id
	a.refreshFolderMemo()
	a.pointFolderBack()
	return nil
}

func (a *app) leaveFolder() bool {
	if strings.TrimSpace(a.home.folderOpen) == "" {
		return false
	}
	a.home.folderOpen = ""
	a.refreshFolderMemo()
	a.focusFoldersPanel()
	return true
}

func (a *app) refreshFolderMemo() {
	a.readHomeFolders()
	a.home.build()
	a.touch()
}

func (a *app) pointFolderBack() {
	for at, line := range a.home.lines {
		if line.kind == homeFolderBack && line.stop() {
			a.home.cursor = at
			return
		}
	}
}

func (a *app) focusFoldersPanel() {
	for at, line := range a.home.lines {
		if panel, ok := line.panelOf(); ok && panel == panelFolders && line.stop() {
			a.home.cursor = at
			return
		}
	}
}

func (a *app) showFoldersPanel() tea.Cmd {
	var cmd tea.Cmd
	if !a.at(pageHome) {
		cmd = a.showPage(pageHome)
	}
	a.readHomeFolders()
	a.home.build()
	a.focusFoldersPanel()
	a.touch()
	return cmd
}

func (a *app) runFoldersCommand(rest string) tea.Cmd {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return a.showFoldersPanel()
	}
	action, name, _ := strings.Cut(rest, " ")
	name = strings.TrimSpace(name)
	switch strings.ToLower(action) {
	case "create":
		return a.createLogicalFolder(name)
	case "add":
		return a.addNamedFolder(name)
	case "new":
		return a.startInFolder(a.folderIDUnderCursor())
	}
	a.folderNote("usage: /folders · /folders create <name> · /folders add <name-or-id> · /folders new")
	return nil
}

func (a *app) folderIDUnderCursor() string {
	if !a.at(pageHome) {
		return strings.TrimSpace(a.home.folderOpen)
	}
	line, ok := a.home.focusedLine()
	if !ok || (line.cell != nil && line.cell.panel != panelFolders && line.kind != homeFolderRow) {
		if open := strings.TrimSpace(a.home.folderOpen); open != "" {
			return open
		}
		return ""
	}
	return a.folderIDOf(line)
}

func (a *app) createLogicalFolder(name string) tea.Cmd {
	name = strings.TrimSpace(name)
	if name == "" {
		a.folderNote("usage: /folders create <name>")
		return nil
	}
	if a.folders == nil {
		a.folderNote(folderUnwiredWord)
		return nil
	}
	folder, err := a.folders.CreateFolder(a.folderCtx(), name)
	if err != nil {
		a.folderNote("could not create " + name)
		return nil
	}
	if a.at(pageHome) {
		a.refreshFolderMemo()
		a.pointFolderID(folder.ID)
	}
	a.folderNote("created " + folder.Name)
	return nil
}

func (a *app) addNamedFolder(name string) tea.Cmd {
	name = strings.TrimSpace(name)
	if name == "" {
		a.folderNote("usage: /folders add <name-or-id>")
		return nil
	}
	if a.folders == nil {
		a.folderNote(folderUnwiredWord)
		return nil
	}
	folder, ok := a.resolveFolder(name)
	if !ok {
		a.folderNote("no folder called " + name)
		return nil
	}
	return a.addCurrentToFolder(folder.ID)
}

func (a *app) resolveFolder(name string) (folderView, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return folderView{}, false
	}
	var named folderView
	var names int
	for _, folder := range a.home.folders.root.Folders {
		if folder.ID == name {
			return folder, true
		}
		if strings.EqualFold(folder.Name, name) {
			named = folder
			names++
		}
	}
	if names == 1 {
		return named, true
	}
	return folderView{}, false
}

func (a *app) pointFolderID(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	for at, line := range a.home.lines {
		if line.kind == homeFolderRow && line.dir == id {
			a.home.cursor = at
			return
		}
	}
}

func (a *app) folderNote(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if a.at(pageHome) {
		a.home.say(text, "")
		a.touch()
		return
	}
	a.note(text)
}

func folderAlsoIn(names []string) string {
	var kept []string
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			kept = append(kept, name)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return folderAlsoInWord + strings.Join(kept, ", ")
}

func parentlessFolders(all []folderView) []folderView {
	var out []folderView
	for _, folder := range all {
		if len(folder.ParentIDs) == 0 {
			out = append(out, folder)
		}
	}
	return out
}

func childFolders(all []folderView, parent string) []folderView {
	parent = strings.TrimSpace(parent)
	if parent == "" {
		return nil
	}
	var out []folderView
	for _, folder := range all {
		for _, id := range folder.ParentIDs {
			if id == parent {
				out = append(out, folder)
				break
			}
		}
	}
	return out
}
