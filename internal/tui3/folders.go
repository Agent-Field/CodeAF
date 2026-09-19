package tui3

import (
	"context"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Folders is the home panel's seam onto logical folder membership. It is a TUI
// interface so this package never imports internal/workspace. The DTOs are
// exported so cmd/codeaf can implement the seam without this package importing
// wsapi, and without wsapi importing tui3. Wiring owns the adapter.
//
// NIL IS UNAVAILABLE, NOT EMPTY. A door that could not open the store leaves
// this nil; the panel heading still exists, mutations refuse with
// [folderUnwiredWord], and the emptiness-law whisper is reserved for a working
// store that happens to hold no folders. A belt that failed every call would
// be a capability advertised as broken.
//
// THE SNAPSHOT IS TAKEN ON THE HOME BEAT and nowhere else. View, the cursor and
// a mere rebuild read [homeView.folders], which is a memo. A call from paint
// would be a store read on a draw, which this surface forbids.
type Folders interface {
	RootSnapshot(ctx context.Context) (FolderRoot, error)
	FolderSnapshot(ctx context.Context, id string) (FolderView, []FolderPlacement, error)
	CreateFolder(ctx context.Context, name string) (FolderView, error)
	RenameFolder(ctx context.Context, id, name string) error
	AddPlacement(ctx context.Context, collectionID, refID string) error
	// AddFolderPlacement nests childFolderID under parentID as a typed
	// collection member. It is a separate verb from AddPlacement so a chat
	// file cannot silently become a folder nest, and so the adapter never
	// falls back to a conversation ref.
	AddFolderPlacement(ctx context.Context, parentID, childFolderID string) error
	RemovePlacement(ctx context.Context, collectionID, refID string) error
	MovePlacement(ctx context.Context, fromID, toID, refID string) error
	WhyHere(ctx context.Context, collectionID, refID string) (FolderWhy, error)
	// InstructFolder writes person-origin standing guidance for one folder.
	// FolderGuidance is that folder's own instructions, loaded on the beat.
	// IndexProgress is software counters for discovery catch-up.
	InstructFolder(ctx context.Context, id, text string) error
	FolderGuidance(ctx context.Context, id string) ([]FolderInstruction, error)
	IndexProgress(ctx context.Context) (FolderIndex, error)
}

// FolderInstruction is one standing guidance line the folder-detail section
// draws. Purpose text on a collection is a description and does not appear
// here; inferred observations are not instructions.
type FolderInstruction struct {
	ScopeID, Text, Origin, Actor, At string
	Revision                         int
}

// FolderIndex is discovery catch-up as software. Passages and Vectors are
// counts, never a percentage. Delayed is the embedder or organizer down;
// Degraded is expansion-only retrieval. Detail is person-facing and empty
// when caught up.
type FolderIndex struct {
	Passages, Vectors int
	Delayed, Degraded bool
	Detail            string
}

// FolderView is one logical folder as the panel draws it. ParentIDs empty means
// the folder stands at Root. MemberCount is unique conversation IDs, not paths.
type FolderView struct {
	ID, Name, Purpose, Lifecycle string
	Revision                     int
	ParentIDs                    []string
	MemberCount                  int
}

// FolderPlacement is one member sitting in a folder. Kind is the membership's
// reference kind as workspace spells it (`collection`, `conversation`, …);
// empty is a conversation, which is every chat row the panel already drew.
// AlsoIn names the other folders that hold the same ref, so a dual placement
// can say "also in Security" without a second store round-trip on the draw.
type FolderPlacement struct {
	CollectionID string
	RefID, Title string
	Kind         string
	AlsoIn       []string
}

// FolderWhy is the latest membership event for an edge, as the `w` verb shows
// it: who put it here and why, with no confidence score.
type FolderWhy struct {
	Origin, Reason, Actor, Evidence, At string
}

// FolderRoot is Root as the beat caches it. Folders may be only the parentless
// collections (wsapi.RootView) or the full graph; parentless ones are Root's
// own rows. A drilled-in folder finds children from ParentIDs when the graph
// is present, and from FolderSnapshot placements whose Kind is collection
// when Root is parentless-only — so the same nested folder can appear through
// both of its parents.
type FolderRoot struct {
	Folders  []FolderView
	Unfiled  []FolderPlacement
	Revision int
}

// homeFoldersReading is the memo [app.readHomeFolders] writes on the beat.
// rows() reads this and never the seam. missing is a nil seam or a failed
// first read: the panel must not draw the empty-workspace whisper over it.
type homeFoldersReading struct {
	root     FolderRoot
	open     FolderView
	members  []FolderPlacement
	index    FolderIndex
	guidance []FolderInstruction
	missing  bool
	// marked is the optional collab selection copied from [app.collabView]
	// after the beat. View reads this and never Collab.Marked.
	marked []CollabMark
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
	folderNestWord        = "nest this folder"
	folderNestHintWord    = "enter a folder to nest it there · esc cancel"
	folderInstructWord    = "instruct this folder"
	folderInstructHead    = "instructions"
	folderInstructWhisper = "standing guidance for chats in this folder"
	folderInstructUsage   = "usage: /folders instruct <name-or-id> <text>"
	folderOrganizerOrigin = "organizer"
	folderDelayedWord     = "discovery delayed"
	folderDegradedWord    = "degraded"
	folderLostWord        = "that chat is no longer in this folder"
	folderUnavailableWord = "unavailable"
	logicalFolderGoneWord = "that folder is no longer here"
	folderFiledWord       = "could not file this chat here"
	// Wave 2 e2e needle names (internal/e2e/folders_needles_test.go) grep
	// these identifiers. Values match the drawing constants above.
	folderInstructSlashWord    = "/folders instruct"
	folderInstructionsHeading  = "instructions"
	folderInstructionsWhisper  = "standing guidance for chats in this folder"
	folderDiscoveryDelayedWord = "discovery delayed"
	folderOrganizerOriginWord  = "organizer"
	// folderCollectionKind is workspace.CollectionKind's bytes, quoted here so
	// this package never imports internal/workspace. A FolderPlacement with
	// this Kind is a nested/shared folder, not a chat.
	folderCollectionKind = "collection"
)

// homeFolderRow is one logical folder on the folders panel. Numbered outside
// the [homeRowKind] iota for the same reason [homeProjectRow] is: that block is
// edited by other lanes.
const homeFolderRow homeRowKind = 246

// homeFolderBack is the sequential way out of a drilled-in folder: enter or
// esc pops one step — the parent that was walked, or Root when the trail is
// empty. It exists so 80-column home never grows a third column of members
// beside the folders, and so a shared child (J02) can be reached through both
// parents and left the same way.
const homeFolderBack homeRowKind = 247

// readHomeFolders is the beat's reading. Nil Folders is unavailable: the memo
// is marked missing so the panel cannot masquerade as an empty working store.
// A RootSnapshot error keeps the last good memo instead of wiping it to empty
// success (n-1x28). The open folder's members are taken in the same pass, so a
// cursor move never has to ask.
func (a *app) readHomeFolders() {
	if a.folders == nil {
		a.home.folders = homeFoldersReading{missing: true}
		return
	}
	ctx := a.folderCtx()
	root, err := a.folders.RootSnapshot(ctx)
	if err != nil {
		if a.home.folders.missing || !a.home.folders.held() {
			a.home.folders.missing = true
		}
		return
	}
	next := homeFoldersReading{root: root}
	open := strings.TrimSpace(a.home.folderOpen)
	a.readFolderDetail(ctx, &next, open)
	if open == "" {
		a.home.folders = next
		return
	}
	folder, members, err := a.folders.FolderSnapshot(ctx, open)
	if err != nil {
		// THE OPEN OBJECT SURVIVES A MISSING FOLDER: we drop the drill-in and
		// say so, rather than leaving the cursor inside a collection the beat
		// can no longer name (J06). The trail dies with it so a later esc
		// cannot reopen a parent walk that no longer stands.
		a.home.folderOpen = ""
		a.home.folderTrail = nil
		a.home.say(logicalFolderGoneWord, "")
		a.home.folders = next
		return
	}
	next.open = folder
	next.members = members
	a.home.folders = next
}

func (r homeFoldersReading) held() bool {
	return len(r.root.Folders) > 0 || len(r.root.Unfiled) > 0 || strings.TrimSpace(r.open.ID) != "" || len(r.members) > 0
}

// foldersUnavailable is every mutation's first check. Nil Options.Folders is a
// visible refusal, never a silent no-op that looks like an empty workspace.
func (a *app) foldersUnavailable() bool {
	if a.folders != nil {
		return false
	}
	a.folderNote(folderUnwiredWord)
	return true
}

func (a *app) folderCtx() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// folderVerbs is `→` on a folders-panel row: new chat here, add the current
// chat, nest this folder, and — on a placement — move, why here, and remove.
// A verb that cannot work is absent, not broken.
func (a *app) folderVerbs(line homeLine) []verb {
	verbs := []verb{
		{key: 'n', word: folderNewChatWord, do: func() tea.Cmd { return a.startInFolder(a.folderIDOf(line)) }},
		{key: 'f', word: folderAddHereWord, do: func() tea.Cmd { return a.addCurrentToFolder(a.folderIDOf(line)) }},
	}
	if line.kind == homeFolderRow {
		verbs = append(verbs, verb{key: 'e', word: folderNestWord, do: func() tea.Cmd { return a.beginFolderNest(line) }})
	}
	if line.kind == homeFolderRow || line.kind == homeFolderBack {
		verbs = append(verbs, verb{key: 'i', word: folderInstructWord, do: func() tea.Cmd { return a.instructThisFolder(line) }})
	}
	if line.kind == homeSession {
		verbs = append(verbs,
			verb{key: 'm', word: folderMoveWord, do: func() tea.Cmd { return a.beginFolderMove(line) }},
			verb{key: 'w', word: folderWhyWord, do: func() tea.Cmd { return a.folderWhyHere(line) }},
			verb{key: 'x', word: folderRemoveWord, do: func() tea.Cmd { return a.removeFolderPlacement(line) }},
		)
	}
	return append(verbs, a.folderCollabVerbs(line)...)
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
	if a.foldersUnavailable() {
		return nil
	}
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
	if a.foldersUnavailable() {
		return nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		a.folderNote(folderNoStandWord)
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
	if a.foldersUnavailable() {
		return nil
	}
	from := a.folderIDOf(line)
	ref := a.placementRefOf(line)
	if from == "" || ref == "" {
		return nil
	}
	a.clearFolderNest()
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

func (a *app) beginFolderNest(line homeLine) tea.Cmd {
	if a.foldersUnavailable() {
		return nil
	}
	if line.kind != homeFolderRow {
		return nil
	}
	child := strings.TrimSpace(line.dir)
	if child == "" {
		return nil
	}
	a.clearFolderMove()
	a.pendingNestChild = child
	a.folderNote(folderNestHintWord)
	return nil
}

func (a *app) clearFolderNest() bool {
	if a.pendingNestChild == "" {
		return false
	}
	a.pendingNestChild = ""
	return true
}

func (a *app) completeFolderNest(parentID string) tea.Cmd {
	parentID = strings.TrimSpace(parentID)
	child := a.pendingNestChild
	a.pendingNestChild = ""
	if a.foldersUnavailable() {
		return nil
	}
	return a.placeFolderIn(parentID, child)
}

func (a *app) placeFolderIn(parentID, childID string) tea.Cmd {
	parentID, childID = strings.TrimSpace(parentID), strings.TrimSpace(childID)
	if parentID == "" || childID == "" || parentID == childID {
		return nil
	}
	if err := a.folders.AddFolderPlacement(a.folderCtx(), parentID, childID); err != nil {
		a.folderNote(err.Error())
		return nil
	}
	a.refreshFolderMemo()
	return nil
}

func (a *app) completeFolderMove(toID string) tea.Cmd {
	toID = strings.TrimSpace(toID)
	from, ref := a.pendingMoveFrom, a.pendingMoveRef
	a.pendingMoveFrom, a.pendingMoveRef = "", ""
	if a.foldersUnavailable() {
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
	if a.foldersUnavailable() {
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

func folderWhyLine(why FolderWhy) string {
	if strings.EqualFold(strings.TrimSpace(why.Origin), folderOrganizerOrigin) {
		return folderOrganizerWhy(why)
	}
	var parts []string
	for _, p := range []string{
		strings.TrimSpace(why.Origin),
		strings.TrimSpace(why.Reason),
		strings.TrimSpace(why.Actor),
		strings.TrimSpace(why.Evidence),
		strings.TrimSpace(why.At),
	} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
}

func (a *app) removeFolderPlacement(line homeLine) tea.Cmd {
	if a.foldersUnavailable() {
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
	if a.pendingNestChild != "" {
		return a.completeFolderNest(id)
	}
	if a.foldersUnavailable() {
		return nil
	}
	cur := strings.TrimSpace(a.home.folderOpen)
	if cur == id {
		a.home.opened, a.home.openedOn = panelFolders, true
		a.refreshFolderMemo()
		a.pointFolderBack()
		return nil
	}
	if cur != "" {
		// PUSH THE PARENT WE ARE LEAVING, not the child we are entering, so
		// esc from Receipts returns to Billing (or Security) rather than Root.
		a.home.folderTrail = append(a.home.folderTrail, cur)
	}
	a.home.folderOpen = id
	// Drilling in is asking to see this folder's members. At 80 columns the
	// resting folders cap folds chats as `N more`, which made live J11 type
	// `x` into the composer instead of removing the organizer placement.
	a.home.opened, a.home.openedOn = panelFolders, true
	a.refreshFolderMemo()
	a.pointFolderBack()
	return nil
}

func (a *app) leaveFolder() bool {
	open := strings.TrimSpace(a.home.folderOpen)
	if open == "" {
		return false
	}
	if n := len(a.home.folderTrail); n > 0 {
		a.home.folderOpen = a.home.folderTrail[n-1]
		a.home.folderTrail = a.home.folderTrail[:n-1]
	} else {
		a.home.folderOpen = ""
		a.home.folderTrail = nil
	}
	a.refreshFolderMemo()
	if a.pointFolderID(open) {
		return true
	}
	if a.home.folderOpen == "" {
		a.focusFoldersPanel()
	} else {
		a.pointFolderBack()
	}
	return true
}

func (a *app) refreshFolderMemo() {
	prev, had := a.home.focusedLine()
	a.readHomeFolders()
	a.readCollab()
	a.home.build()
	a.explainLostFolderRow(prev, had)
	a.touch()
}

// explainLostFolderRow is J06: when the folders-panel placement under the
// cursor is gone, stay in this folder and say so. build() would otherwise
// jump the cursor onto the same chat in another panel.
func (a *app) explainLostFolderRow(prev homeLine, had bool) {
	if !had || !folderPanelSession(prev) {
		return
	}
	if folderLineStillHere(a.home, prev) {
		return
	}
	a.pointFolderBack()
	a.folderNote(folderLostWord)
}

func folderPanelSession(line homeLine) bool {
	return line.kind == homeSession && line.cell != nil && line.cell.panel == panelFolders
}

func folderMemberUnavailable(line homeLine) bool {
	return folderPanelSession(line) && strings.TrimSpace(line.row.Transcript) == ""
}

func folderMemberSame(a, b homeLine) bool {
	if !folderPanelSession(a) || !folderPanelSession(b) {
		return false
	}
	id := strings.TrimSpace(a.row.ID)
	return id != "" && id == strings.TrimSpace(b.row.ID) && a.cellKey() == b.cellKey()
}

func folderLineStillHere(h homeView, want homeLine) bool {
	for _, line := range h.lines {
		if line.sameRow(want) || folderMemberSame(line, want) {
			return true
		}
	}
	return false
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
	a.readCollab()
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
	case "rename":
		from, to, _ := strings.Cut(name, " ")
		return a.renameLogicalFolder(from, to)
	case "nest":
		return a.nestLogicalFolder(name)
	case "new":
		return a.startInFolder(a.folderIDUnderCursor())
	case "instruct":
		return a.instructNamedFolder(name)
	}
	a.folderNote("usage: /folders · /folders create <name> · /folders add <name-or-id> · /folders rename <name-or-id> <new-name> · /folders nest <child> [in <parent>] · /folders instruct <name-or-id> <text> · /folders new")
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
	if a.foldersUnavailable() {
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

func (a *app) renameLogicalFolder(from, to string) tea.Cmd {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == "" || to == "" {
		a.folderNote("usage: /folders rename <name-or-id> <new-name>")
		return nil
	}
	if a.foldersUnavailable() {
		return nil
	}
	folder, ok := a.resolveFolder(from)
	if !ok {
		a.folderNote("no folder called " + from)
		return nil
	}
	if err := a.folders.RenameFolder(a.folderCtx(), folder.ID, to); err != nil {
		a.folderNote("could not rename " + folder.Name)
		return nil
	}
	if a.at(pageHome) {
		a.refreshFolderMemo()
		a.pointFolderID(folder.ID)
	}
	a.folderNote("renamed " + folder.Name + " to " + to)
	return nil
}

func (a *app) nestLogicalFolder(rest string) tea.Cmd {
	childName, parentName := splitFoldersNest(rest)
	if childName == "" {
		a.folderNote("usage: /folders nest <child> [in <parent>]")
		return nil
	}
	if a.foldersUnavailable() {
		return nil
	}
	child, ok := a.resolveFolder(childName)
	if !ok {
		a.folderNote("no folder called " + childName)
		return nil
	}
	parentID := strings.TrimSpace(parentName)
	if parentID == "" {
		parentID = a.folderIDUnderCursor()
	} else if parent, found := a.resolveFolder(parentName); found {
		parentID = parent.ID
	} else {
		a.folderNote("no folder called " + parentName)
		return nil
	}
	if parentID == "" {
		a.folderNote("stand on a folder · then nest " + child.Name + " there")
		return nil
	}
	return a.placeFolderIn(parentID, child.ID)
}

// splitFoldersNest reads `/folders nest Receipts in Billing`. The last ` in `
// is the parent; without it the child nests into the folder under the cursor.
func splitFoldersNest(rest string) (child, parent string) {
	rest = strings.TrimSpace(rest)
	lower := strings.ToLower(rest)
	const sep = " in "
	if i := strings.LastIndex(lower, sep); i >= 0 {
		return strings.TrimSpace(rest[:i]), strings.TrimSpace(rest[i+len(sep):])
	}
	return rest, ""
}

func (a *app) addNamedFolder(name string) tea.Cmd {
	name = strings.TrimSpace(name)
	if name == "" {
		a.folderNote("usage: /folders add <name-or-id>")
		return nil
	}
	if a.foldersUnavailable() {
		return nil
	}
	folder, ok := a.resolveFolder(name)
	if !ok {
		a.folderNote("no folder called " + name)
		return nil
	}
	return a.addCurrentToFolder(folder.ID)
}

func (a *app) resolveFolder(name string) (FolderView, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return FolderView{}, false
	}
	seen := map[string]FolderView{}
	add := func(folder FolderView) {
		id := strings.TrimSpace(folder.ID)
		if id != "" {
			seen[id] = folder
		}
	}
	for _, folder := range a.home.folders.root.Folders {
		add(folder)
	}
	add(a.home.folders.open)
	for _, place := range a.home.folders.members {
		if folderCollectionPlacement(place) {
			add(folderViewFromPlacement(place))
		}
	}
	var named FolderView
	var names int
	for _, folder := range seen {
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
	return FolderView{}, false
}

func (a *app) pointFolderID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for at, line := range a.home.lines {
		if line.kind == homeFolderRow && line.dir == id {
			a.home.cursor = at
			return true
		}
	}
	return false
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

func parentlessFolders(all []FolderView) []FolderView {
	var out []FolderView
	for _, folder := range all {
		if len(folder.ParentIDs) == 0 {
			out = append(out, folder)
		}
	}
	return out
}

func childFolders(all []FolderView, parent string) []FolderView {
	parent = strings.TrimSpace(parent)
	if parent == "" {
		return nil
	}
	var out []FolderView
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

func folderCollectionPlacement(place FolderPlacement) bool {
	return strings.EqualFold(strings.TrimSpace(place.Kind), folderCollectionKind)
}

func folderViewFromPlacement(place FolderPlacement) FolderView {
	name := strings.TrimSpace(place.Title)
	if name == "" {
		name = strings.TrimSpace(place.RefID)
	}
	return FolderView{ID: strings.TrimSpace(place.RefID), Name: name, Lifecycle: "active"}
}
