package tui3

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
	"github.com/Agent-Field/aforge-v2/internal/workspaceview"
)

// ── THE FOLDERS PLACE ───────────────────────────────────────────────────────
//
// The person's logical folders: what they filed chats, work and files in, and
// what they placed in a folder so that its rules reach it. It is browsing and
// nothing else — a folder here is a place to find things and come back to, not
// an agent, not a directory on disk, and not a setting on a conversation. The
// `/folder` picker chooses a DIRECTORY for a conversation to work in; this place
// never attaches, moves, files, places or starts anything.
//
// ── EVERY READING IS THE ENGINE'S ───────────────────────────────────────────
//
// A folder's records live on the machine whose conversations this surface lists,
// so every fact on this page arrives through [CollectionSeam] — the engine's own
// reading on every launch that goes through one (internal/remote's
// collections.go), and this process's own disk only where this process IS the
// engine. A surface without the seam says so in one line and never opens a
// database of its own in its place.
//
// ── THREE READS, AND WHAT KEEPS EACH HONEST ─────────────────────────────────
//
//   - THE PAGE is read on the way in, on a walk into or out of a folder, and on
//     the place clock's beat. An answer carries the generation it was asked
//     under and the folder it was asked for, so an answer for a folder the person
//     has already walked out of is dropped rather than drawn over the one they
//     are in.
//   - THE SELECTED ROW, read closely, waits for a QUIET INTERVAL after the cursor
//     stops, so holding `↓` down a long folder costs one read and not forty. Its
//     answer is dropped unless the cursor is still on the row it was asked about.
//   - A FILE'S OPENING rides with the selected row for an Artifact, under the
//     same generation, and is read by the machine that holds the file.
//
// ── COMING BACK IS WHERE YOU WERE ───────────────────────────────────────────
//
// Every other place forgets its cursor when it closes. This one keeps the path
// walked and the row chosen in every folder on it, because the one thing a
// folder is for is being returned to: open the shared chat from Product, go back
// to the folders, and the cursor is on the same chat in the same folder.

// CollectionSeam is the three readings of the person's folders. Every field is
// optional and a nil one is that reading absent here, which the place says.
type CollectionSeam struct {
	// Page is one folder's contents, or the top level for an empty id.
	Page func(ctx context.Context, id string, limit int) (workspaceview.FolderPage, error)
	// Item is one row read closely: where else it is filed, where it is placed,
	// the rules and shared context that reach it, and ongoing work itself.
	Item func(ctx context.Context, ref workspace.Ref) (workspaceview.FolderItem, error)
	// File is the opening of a file a folder refers to, read where it is kept.
	File func(ctx context.Context, path string) (workspaceview.ArtifactPreview, error)
}

// foldersReadTimeout bounds one reading. A folder is a handful of database
// queries and one walk of the machine; a reading that has not come back in this
// long is a connection in trouble, and the page says so rather than waiting.
const foldersReadTimeout = 15 * time.Second

// foldersQuiet is how long the cursor must rest before the row under it is read
// closely.
const foldersQuiet = 120 * time.Millisecond

// foldersPlace is the whole place.
type foldersPlace struct {
	// path is the folders walked into, outermost first; empty is the top level.
	path []workspace.Collection
	// chosen is the row the cursor was on in each folder of the walk, by folder
	// id ("" for the top level). IT SURVIVES THE PLACE CLOSING.
	chosen map[string]workspace.Ref

	page    workspaceview.FolderPage
	pageFor string
	landed  bool
	pageErr string
	gen     int

	cursor, top, shown, hover int
	// owner is which row each body line belongs to, -1 for none.
	owner []int

	item    *workspaceview.FolderItem
	itemRef workspace.Ref
	itemErr string
	itemGen int
	file    *workspaceview.ArtifactPreview
	fileErr string

	// world is the machine's conversations as the surface already reads them,
	// for the doors onto a chat or a piece of work.
	world session.World

	// detail is the selected row drawn across the whole body, which is how a
	// narrow window reads what a wide one draws beside the list.
	detail    bool
	detailTop int
}

type foldersPageMsg struct {
	gen  int
	id   string
	page workspaceview.FolderPage
	err  error
}

type foldersItemTickMsg struct{ gen int }

type foldersItemMsg struct {
	gen  int
	ref  workspace.Ref
	item workspaceview.FolderItem
	err  error
}

type foldersFileMsg struct {
	gen     int
	path    string
	preview workspaceview.ArtifactPreview
	err     error
}

// The sentences this place says. Each is spelled once.
const (
	foldersAbsentWord = "Folders cannot be read here: this conversation's machine did not offer them."
	foldersTopEmpty   = "No folders yet. A folder holds chats, work and files you want to come back to; " +
		"make one with aforge collections create <name>, or ask in a chat to file something."
	foldersEmptyWord   = "Nothing is filed or placed in this folder."
	foldersGoneWord    = "that folder is not here any more"
	foldersNoChatWord  = "that chat is not on this machine"
	foldersNoWorkWord  = "that work is not in this machine's record"
	foldersFailedWord  = "could not read this folder"
	foldersBinaryWord  = "not a text file, so nothing is shown"
	foldersPreviewFail = "could not read the file: "
	// foldersMoreWord closes a folder holding more rows than one page carries.
	foldersMoreWord = "more are in this folder than one page shows"
)

// The verbs on a row's strip and the words of its foot.
const (
	foldersOpenWord    = "open"
	foldersInWord      = "go in"
	foldersDetailWord  = "details"
	foldersBackWord    = "← back out"
	foldersPreviewWord = "preview"
)

func (p *foldersPlace) current() string {
	if len(p.path) == 0 {
		return ""
	}
	return p.path[len(p.path)-1].ID
}

func (p *foldersPlace) selected() (workspaceview.FolderRow, bool) {
	if p.pageFor != p.current() || p.cursor < 0 || p.cursor >= len(p.page.Rows) {
		return workspaceview.FolderRow{}, false
	}
	return p.page.Rows[p.cursor], true
}

// remember writes down the row the cursor is on in the folder it is in.
func (p *foldersPlace) remember() {
	row, ok := p.selected()
	if !ok {
		return
	}
	if p.chosen == nil {
		p.chosen = map[string]workspace.Ref{}
	}
	p.chosen[p.current()] = row.Ref
}

// ── the readings ────────────────────────────────────────────────────────────

func (p *foldersPlace) open(a *app) tea.Cmd {
	p.world = a.readWorld()
	p.hover = -1
	p.detail, p.detailTop = false, 0
	return tea.Batch(a.armPlaceClock(), a.foldersReadPage())
}

// close drops the answers in flight and keeps the walk: the path, the row chosen
// in each folder, and the last page, so coming back draws where you were while
// the fresh reading is on its way.
func (p *foldersPlace) close(a *app) {
	a.leavePage(pageFolders)
	p.gen++
	p.itemGen++
	p.detail, p.hover = false, -1
}

// foldersReadPage asks for the folder the walk is standing in.
func (a *app) foldersReadPage() tea.Cmd {
	p := &a.browse
	id := p.current()
	p.gen++
	if p.pageFor != id {
		p.page, p.pageFor, p.landed, p.pageErr = workspaceview.FolderPage{}, id, false, ""
		p.item, p.itemRef, p.itemErr, p.file, p.fileErr = nil, workspace.Ref{}, "", nil, ""
		p.top = 0
	}
	read := a.collections.Page
	if read == nil {
		p.landed = true
		return nil
	}
	gen := p.gen
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), foldersReadTimeout)
		defer cancel()
		page, err := read(ctx, id, workspaceview.FolderLimit)
		return foldersPageMsg{gen: gen, id: id, page: page, err: err}
	}
}

// foldersPageLanded takes a folder's page, puts the cursor back on the row it
// was on in this folder, and reads that row closely.
func (a *app) foldersPageLanded(msg foldersPageMsg) tea.Cmd {
	p := &a.browse
	if !a.at(pageFolders) || msg.gen != p.gen || msg.id != p.current() {
		return nil
	}
	p.landed = true
	a.touch()
	if msg.err != nil {
		if len(p.path) > 0 && strings.Contains(msg.err.Error(), workspace.ErrNotFound.Error()) {
			// THE FOLDER WENT AWAY WHILE THE WALK STOOD IN IT. The walk steps back
			// out to where it can stand and says why, rather than drawing an empty
			// folder that is not there.
			p.path = p.path[:len(p.path)-1]
			a.pageMsg = foldersGoneWord
			return a.foldersReadPage()
		}
		p.pageErr = msg.err.Error()
		return nil
	}
	p.page, p.pageErr = msg.page, ""
	p.cursor = p.landOn(p.chosen[msg.id])
	p.remember()
	return a.foldersAskItem(true)
}

// landOn is the index of the row naming want, or the cursor kept in range when
// that row is no longer in the folder.
func (p *foldersPlace) landOn(want workspace.Ref) int {
	if want.Kind != "" {
		for i, row := range p.page.Rows {
			if row.Ref == want {
				return i
			}
		}
	}
	return max(0, min(p.cursor, len(p.page.Rows)-1))
}

// foldersAskItem reads the selected row closely: now, or after the quiet
// interval when the cursor has just moved.
func (a *app) foldersAskItem(now bool) tea.Cmd {
	p := &a.browse
	row, ok := p.selected()
	if !ok {
		p.item, p.itemRef, p.itemErr, p.file, p.fileErr = nil, workspace.Ref{}, "", nil, ""
		return nil
	}
	if p.itemRef != row.Ref {
		// A DIFFERENT ROW DRAWS NOTHING OF THE LAST ONE. The page's own facts
		// about the row are drawn at once; what the close reading adds is drawn
		// when it arrives, and never borrowed from the row the cursor left.
		p.item, p.itemRef, p.itemErr, p.file, p.fileErr = nil, row.Ref, "", nil, ""
		p.detailTop = 0
	}
	p.itemGen++
	if now {
		return a.foldersReadItem(p.itemGen, row.Ref)
	}
	if a.foldersArm != nil {
		return a.foldersArm(p.itemGen)
	}
	gen := p.itemGen
	return tea.Tick(foldersQuiet, func(time.Time) tea.Msg { return foldersItemTickMsg{gen: gen} })
}

func (a *app) foldersItemTick(msg foldersItemTickMsg) tea.Cmd {
	p := &a.browse
	if !a.at(pageFolders) || msg.gen != p.itemGen {
		return nil
	}
	row, ok := p.selected()
	if !ok {
		return nil
	}
	return a.foldersReadItem(msg.gen, row.Ref)
}

// foldersReadItem is the close reading of one row, and a file's opening beside
// it for an Artifact.
func (a *app) foldersReadItem(gen int, ref workspace.Ref) tea.Cmd {
	var cmds []tea.Cmd
	if read := a.collections.Item; read != nil {
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), foldersReadTimeout)
			defer cancel()
			item, err := read(ctx, ref)
			return foldersItemMsg{gen: gen, ref: ref, item: item, err: err}
		})
	}
	if read := a.collections.File; read != nil && ref.Kind == workspace.ArtifactKind {
		path := ref.ID
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), foldersReadTimeout)
			defer cancel()
			preview, err := read(ctx, path)
			return foldersFileMsg{gen: gen, path: path, preview: preview, err: err}
		})
	}
	return tea.Batch(cmds...)
}

func (a *app) foldersItemLanded(msg foldersItemMsg) {
	p := &a.browse
	row, ok := p.selected()
	if !a.at(pageFolders) || msg.gen != p.itemGen || !ok || row.Ref != msg.ref {
		return
	}
	if msg.err != nil {
		p.itemErr = msg.err.Error()
	} else {
		item := msg.item
		p.item, p.itemErr = &item, ""
	}
	a.touch()
}

func (a *app) foldersFileLanded(msg foldersFileMsg) {
	p := &a.browse
	row, ok := p.selected()
	if !a.at(pageFolders) || msg.gen != p.itemGen || !ok || row.Ref.ID != msg.path {
		return
	}
	if msg.err != nil {
		p.file, p.fileErr = nil, msg.err.Error()
	} else {
		preview := msg.preview
		p.file, p.fileErr = &preview, ""
	}
	a.touch()
}

// ── moving and opening ──────────────────────────────────────────────────────

func (p *foldersPlace) move(a *app, delta int) tea.Cmd {
	if len(p.page.Rows) == 0 {
		return nil
	}
	next := moveCursor(p.cursor, delta, len(p.page.Rows))
	a.touch()
	if next == p.cursor {
		return nil
	}
	p.cursor = next
	p.remember()
	return a.foldersAskItem(false)
}

// up walks out of the folder the path is standing in, back to the row it was
// entered from.
func (p *foldersPlace) up(a *app) tea.Cmd {
	if len(p.path) == 0 {
		return nil
	}
	p.path = p.path[:len(p.path)-1]
	p.detail = false
	a.touch()
	return a.foldersReadPage()
}

// enter opens the row under the cursor through the owner that keeps it: a
// folder is walked into, a chat is opened where it already lives (the one
// conversation, never a copy), a finite piece of work opens its record, ongoing
// work opens on the standing place, and a file is previewed here.
//
// NOTHING ABOUT THE WALK TRAVELS WITH THE DOOR. A chat opened from Marketing is
// the same conversation opened from Product, with the same folders and the same
// rules it had; which folder a person came through is where they will come back
// to, not a fact about the conversation.
func (p *foldersPlace) enter(a *app) tea.Cmd {
	row, ok := p.selected()
	if !ok {
		return nil
	}
	p.remember()
	if !row.Available {
		a.pageMsg = drawableLine(row.Unavailable)
		a.touch()
		return nil
	}
	switch row.Ref.Kind {
	case workspace.CollectionKind:
		p.path = append(p.path, workspace.Collection{ID: row.Ref.ID, Name: collectionRowTitle(row)})
		p.cursor, p.top, p.detail = 0, 0, false
		a.touch()
		return a.foldersReadPage()
	case workspace.ConversationKind:
		p.world = a.readWorld()
		chat, found := foldersSession(p.world, row.Ref.ID)
		if !found {
			a.pageMsg = foldersNoChatWord
			a.touch()
			return nil
		}
		return a.openConversationRow(chat)
	case workspace.TaskKind:
		p.world = a.readWorld()
		if entry, found := foldersTask(p.world, row.Ref); found {
			return a.openTaskRecord(&entry)
		}
		a.pageMsg = foldersNoWorkWord
		a.touch()
		return nil
	case workspace.StandingKind:
		return a.openStandingAt(row.Ref.ID)
	case workspace.ArtifactKind:
		p.detail, p.detailTop = true, 0
		a.touch()
		if p.file == nil && p.fileErr == "" {
			return a.foldersAskItem(true)
		}
	}
	return nil
}

// foldersSession finds one conversation in the world by its id.
func foldersSession(world session.World, id string) (session.SessionRow, bool) {
	for _, project := range world.Projects {
		for _, row := range project.Sessions {
			if row.ID == id {
				return row, true
			}
		}
	}
	return session.SessionRow{}, false
}

// foldersTask finds one row of a conversation's record by the pair that names it.
func foldersTask(world session.World, ref workspace.Ref) (session.TaskIndexEntry, bool) {
	chat, ok := foldersSession(world, ref.SessionID)
	if !ok {
		return session.TaskIndexEntry{}, false
	}
	for _, entry := range chat.Tasks.Rows {
		if strings.TrimSpace(entry.ID) == ref.ID {
			return entry, true
		}
	}
	return session.TaskIndexEntry{}, false
}

// ── the keys ────────────────────────────────────────────────────────────────

func (a *app) foldersKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &a.browse
	box := a.placeBox()
	empty := box == nil || box.empty()
	key := msg.String()
	if p.detail {
		switch key {
		case "esc":
			p.detail = false
			a.touch()
			return nil
		case "left", "backspace":
			if empty {
				p.detail = false
				a.touch()
				return nil
			}
		case "up", "ctrl+p":
			p.detailTop = max(0, p.detailTop-1)
			a.touch()
			return nil
		case "down", "ctrl+n":
			p.detailTop++
			a.touch()
			return nil
		case "pgup":
			p.detailTop = max(0, p.detailTop-10)
			a.touch()
			return nil
		case "pgdown":
			p.detailTop += 10
			a.touch()
			return nil
		}
	}
	switch key {
	case "esc":
		if !empty {
			box.reset()
			a.touch()
			return nil
		}
		a.leavePlace()
		return nil
	case "up", "ctrl+p":
		return p.move(a, -1)
	case "down", "ctrl+n":
		return p.move(a, 1)
	case "pgup":
		return p.move(a, -max(1, p.shown))
	case "pgdown":
		return p.move(a, max(1, p.shown))
	case "enter":
		if !empty {
			// WORDS IN THE BOX ARE A CONVERSATION, as on every place, and it starts
			// where any new conversation starts: in no folder. Being on this page is
			// not a choice of where the chat belongs.
			return a.placeTalk()
		}
		return p.enter(a)
	case "left", "backspace":
		if empty && len(p.path) > 0 {
			return p.up(a)
		}
	}
	if box != nil {
		listNavigate(msg, box, func(int) {}, func() {}, max(1, p.shown))
		a.touch()
	}
	return nil
}

// ── the place ───────────────────────────────────────────────────────────────

// placeFolders is this place's handle on the registry (pages.go's [place]).
type placeFolders struct{ placeBase }

func init() { registerPlace(placeFolders{}) }

func (placeFolders) id() page     { return pageFolders }
func (placeFolders) word() string { return "folders" }

func (placeFolders) open(a *app) tea.Cmd { return a.browse.open(a) }
func (placeFolders) close(a *app)        { a.browse.close(a) }

// tick is the beat: the page is read again, so a chat filed in the next terminal
// is on this page within a beat, and the cursor stays on its row.
func (placeFolders) tick(a *app, now time.Time) bool {
	a.browse.world = a.readWorld()
	a.placeLater = tea.Batch(a.placeLater, a.foldersReadPage())
	return true
}

func (placeFolders) body(a *app, width, room int) []placeRow {
	return a.browse.body(a, width, room)
}

func (p *foldersPlace) body(a *app, width, room int) []placeRow {
	rows := make([]placeRow, 0, room)
	p.owner = p.owner[:0]
	add := func(text string, at int) {
		if len(rows) < room {
			rows = append(rows, placeRow{text: text, hit: at})
			p.owner = append(p.owner, at)
		}
	}
	defer func() {
		for len(rows) < room {
			rows = append(rows, placeRow{text: "", hit: -1})
			p.owner = append(p.owner, -1)
		}
	}()
	if a.collections.Page == nil {
		for _, line := range placeTeachProse(foldersAbsentWord, width, a.pal) {
			add(line, -1)
		}
		p.shown = 0
		return rows
	}
	add(foldersCrumb(p.path, width, a.pal), -1)
	add("", -1)
	listRoom := room - 2
	if p.detail {
		lines := collectionInspectLines(p.inspect(a, width-2), width-2, a.pal)
		p.detailTop = max(0, min(p.detailTop, len(lines)-listRoom))
		for i := p.detailTop; i < len(lines); i++ {
			add(" "+lines[i], -1)
		}
		p.shown = 0
		return rows
	}
	listWidth, inspectWidth := foldersSplit(width)
	var list []string
	var owners []int
	switch {
	case p.pageErr != "" && len(p.page.Rows) == 0:
		list = append(list, " "+a.pal.warn(fit(foldersFailedWord+" · "+drawableLine(p.pageErr), listWidth-1)))
		owners = append(owners, -1)
	case p.landed && len(p.page.Rows) == 0:
		teach := foldersEmptyWord
		if len(p.path) == 0 {
			teach = foldersTopEmpty
		}
		for _, line := range placeTeachProse(teach, listWidth, a.pal) {
			list, owners = append(list, line), append(owners, -1)
		}
	}
	shown := listRoom - len(list)
	if p.page.More {
		shown--
	}
	p.top = placeTop(p.top, p.cursor, len(p.page.Rows), shown)
	p.shown = 0
	for i := p.top; i < len(p.page.Rows) && p.shown < shown; i++ {
		row := p.page.Rows[i]
		line := collectionRowLine(row, a.icon(collectionGlyph(row)), listWidth, a.pal)
		switch {
		case i == p.cursor:
			line = a.pal.selected(line, listWidth)
		case i == p.hover:
			line = a.pal.cursor(line, listWidth)
		}
		list, owners = append(list, line), append(owners, i)
		p.shown++
	}
	if p.page.More && len(p.page.Rows) > 0 {
		list, owners = append(list, " "+a.pal.dim(fit(foldersMoreWord, listWidth-1))), append(owners, -1)
	}
	if p.pageErr != "" && len(p.page.Rows) > 0 {
		// A REFRESH THAT FAILED KEEPS THE ROWS IT HAD and says it could not read
		// them again, rather than emptying a folder that was there a beat ago.
		list, owners = append(list, " "+a.pal.warn(fit(foldersFailedWord+" · "+drawableLine(p.pageErr), listWidth-1))), append(owners, -1)
	}
	var inspect []string
	if inspectWidth > 0 {
		if _, ok := p.selected(); ok {
			inspect = collectionInspectLines(p.inspect(a, inspectWidth), inspectWidth, a.pal)
		}
	}
	for i := 0; i < listRoom; i++ {
		left, at := "", -1
		if i < len(list) {
			left, at = list[i], owners[i]
		}
		if inspectWidth == 0 {
			add(left, at)
			continue
		}
		right := ""
		if i < len(inspect) {
			right = inspect[i]
		}
		add(padTo(left, listWidth)+strings.Repeat(" ", homeGutter)+right, at)
	}
	return rows
}

// inspect gathers what the inspector draws for the selected row at a width.
func (p *foldersPlace) inspect(a *app, width int) collectionInspect {
	row, _ := p.selected()
	in := collectionInspect{row: row, item: p.item, itemErr: p.itemErr, path: func(path string) string {
		return a.hostedPath(tildePath(path, a.tilde))
	}}
	if len(p.path) > 0 {
		in.here = p.path[len(p.path)-1].Name
	}
	switch row.Ref.Kind {
	case workspace.ConversationKind:
		if chat, ok := foldersSession(p.world, row.Ref.ID); ok {
			in.chat = &chat
		}
	case workspace.TaskKind:
		if owner, ok := foldersSession(p.world, row.Ref.SessionID); ok {
			in.owner = &owner
		}
	case workspace.ArtifactKind:
		switch {
		case p.fileErr != "":
			in.previewNote = foldersPreviewFail + drawableLine(p.fileErr)
		case p.file != nil:
			in.size, in.modified = p.file.Size, p.file.Modified
			if p.file.Binary {
				in.previewNote = foldersBinaryWord
				break
			}
			lines := strings.Split(p.file.Text, "\n")
			for i := range lines {
				lines[i] = drawableLine(lines[i])
			}
			in.preview = a.codeRows(strings.Join(lines, "\n"), row.Ref.ID, width)
		}
	}
	return in
}

func (placeFolders) stops(a *app) []int {
	stops := make([]int, len(a.browse.page.Rows))
	for i := range stops {
		stops[i] = i
	}
	return stops
}

func (placeFolders) cursorAt(a *app) int { return a.browse.cursor }
func (placeFolders) cursorRow(a *app, rows []placeRow) int {
	if a.browse.detail {
		return -1
	}
	return placeRowAtLine(rows, a.browse.cursor)
}

func (placeFolders) rowID(a *app) string {
	row, ok := a.browse.selected()
	if !ok {
		return ""
	}
	return "folders:" + a.browse.current() + ":" + string(row.Ref.Kind) + ":" + row.Ref.ID + ":" + row.Ref.SessionID
}

func (placeFolders) enter(a *app) tea.Cmd { return a.browse.enter(a) }

// verbs is the row's strip: open it, and read it across the whole page — which
// is how a window too narrow for the inspector column reads what a wide one
// draws beside the list.
func (placeFolders) verbs(a *app) []verb {
	p := &a.browse
	row, ok := p.selected()
	if !ok || p.detail {
		return nil
	}
	open := foldersOpenWord
	switch row.Ref.Kind {
	case workspace.CollectionKind:
		open = foldersInWord
	case workspace.ArtifactKind:
		open = foldersPreviewWord
	}
	return []verb{
		{key: 'o', word: open, do: func() tea.Cmd { return p.enter(a) }},
		{key: 'd', word: foldersDetailWord, do: func() tea.Cmd {
			p.detail, p.detailTop = true, 0
			a.touch()
			return nil
		}},
	}
}

func (placeFolders) press(a *app, y int) bool {
	p := &a.browse
	line := y - placeHeadRows
	if line < 0 || line >= len(p.owner) || p.owner[line] < 0 {
		return true
	}
	if at := p.owner[line]; at != p.cursor {
		p.cursor = at
		p.remember()
		a.placeLater = tea.Batch(a.placeLater, a.foldersAskItem(false))
	}
	a.touch()
	return true
}

func (placeFolders) hover(a *app, y int) bool {
	p := &a.browse
	next := -1
	if line := y - placeHeadRows; line >= 0 && line < len(p.owner) {
		next = p.owner[line]
	}
	return placeHoverMoved(&p.hover, next, a)
}

func (placeFolders) wheel(a *app, delta int) bool {
	p := &a.browse
	if p.detail {
		p.detailTop = max(0, p.detailTop+delta)
		a.touch()
		return true
	}
	a.placeLater = tea.Batch(a.placeLater, p.move(a, delta))
	return true
}

func (placeFolders) key(a *app, msg tea.KeyPressMsg) tea.Cmd { return a.foldersKey(msg) }

// hint is what the row under the cursor can be asked for, and how to leave.
// NOTHING IS NAMED THAT IS NOT BOUND: a folder with no rows names no row key.
func (placeFolders) hint(a *app) string {
	p := &a.browse
	if p.detail {
		return "↑↓ scroll · esc back to the list"
	}
	back := ""
	if len(p.path) > 0 {
		back = " · " + foldersBackWord
	}
	row, ok := p.selected()
	if !ok {
		if back != "" {
			return foldersBackWord + " · esc"
		}
		return "esc"
	}
	open := "enter " + foldersOpenWord
	switch row.Ref.Kind {
	case workspace.CollectionKind:
		open = "enter " + foldersInWord
	case workspace.ArtifactKind:
		open = "enter " + foldersPreviewWord
	}
	// THE ORDER IS WHAT A NARROW FOOT KEEPS: [hintFit] drops the clause nearest
	// the way out first, so `↑↓ pick` goes before the verbs do — and on a window
	// with no inspector the verbs are the only road to a row's details.
	return open + " · " + homeStripWord(foldersOpenWord, foldersDetailWord) + back + " · ↑↓ pick · esc"
}
