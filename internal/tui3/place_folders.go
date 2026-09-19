package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// ── THE FOLDERS PLACE ───────────────────────────────────────────────────────
//
// Logical groups of chats as a dedicated registered place on the Home tab bar.
// Folders-entry (CONTRACTS.md) is the freeze: the bar word is `folders`,
// `alt+5` lands here, `/folders` enters this place, `/folder` stays a
// filesystem chooser, and the home `folders` panel remains the enter-from
// heading. Sequential drill-in lives here so an 80-column frame never grows a
// second tree.
//
// THE SNAPSHOT IS TAKEN ON OPEN AND ON THE PLACE BEAT, never in View, never on
// a cursor move. Nil Options.Folders is unavailable: the three visible actions
// refuse with [folderUnwiredWord] rather than drawing a successful empty
// graph. A working store with no collections keeps the emptiness-law whisper
// AND still draws unfiled Root chats — two truths, never "no folders yet".

// foldersPlace is this place's state. The zero value is closed.
//
// cursor is a line of [foldersPlace.stops], which is the layout the last
// paint (and enter, and the strip) walk. selectedID + selectedPath restore
// that row across a beat that rebuilt the graph under us (J43 / P12).
type foldersPlace struct {
	cursor, top, shown, hover int
	open                      string
	trail                     []string
	selectedID, selectedPath  string
	reading                   homeFoldersReading
	organize                  FolderOrganize
	naming                    *editor
	note                      string
	stops                     []folderPlaceStop
}

// folderPlaceStop is one restable row of the Folders place. Kind decides what
// enter does; id + path is the stable selection (object id and the walk that
// reached it).
type folderPlaceStop struct {
	kind folderPlaceKind
	id   string
	path string
	line homeLine
}

type folderPlaceKind uint8

const (
	folderStopNewFolder folderPlaceKind = iota
	folderStopNewChat
	folderStopOrganize
	folderStopCancel
	folderStopBack
	folderStopFolder
	folderStopChat
)

const (
	folderNewFolderWord    = "New folder"
	folderNewChatAction    = "New chat"
	folderOrganizeWord     = "Organize existing chats"
	folderCancelAction     = "cancel"
	folderNameResting      = "a name for the folder"
	folderOrganizeFailWord = "could not organize existing chats"
	folderNameHint         = "type a name · enter create · esc cancel"
	folderPlaceHint        = "enter opens · ↑↓ pick · esc back"
	folderActionHint       = "enter · ↑↓ pick · esc back"
	folderRootPath         = "root"
)

func (k folderPlaceKind) actionWord() string {
	switch k {
	case folderStopNewFolder:
		return folderNewFolderWord
	case folderStopNewChat:
		return folderNewChatAction
	case folderStopOrganize:
		return folderOrganizeWord
	case folderStopCancel:
		return folderCancelAction
	}
	return ""
}

func folderJobLive(state string) bool {
	return state == "queued" || state == "running"
}

func folderJobShown(state string) bool {
	switch state {
	case "queued", "running", "delayed", "done", "cancel":
		return true
	}
	return false
}

func folderPlacePath(open, id string) string {
	base := strings.TrimSpace(open)
	if base == "" {
		base = folderRootPath
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return base
	}
	return base + "/" + id
}

// ── the handle ──────────────────────────────────────────────────────────────

type placeFolders struct{ placeBase }

func init() { registerPlace(placeFolders{}) }

func (placeFolders) id() page     { return pageFolders }
func (placeFolders) word() string { return "folders" }

func (placeFolders) open(a *app) tea.Cmd {
	a.folderSheet.hover = -1
	a.folderSheet.naming = nil
	a.refreshFolderPlace()
	a.closeLists()
	a.dismissWelcome()
	return a.armPlaceClock()
}

func (placeFolders) close(a *app) {
	a.leavePage(pageFolders)
	a.folderSheet = foldersPlace{}
}

func (placeFolders) tick(a *app, _ time.Time) (bool, tea.Cmd) {
	a.refreshFolderPlace()
	return true, nil
}

func (placeFolders) body(a *app, width, room int) []placeRow {
	return a.folderPlaceBody(width, room)
}

func (placeFolders) stops(a *app) []int {
	n := len(a.folderSheet.stops)
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

func (placeFolders) cursorAt(a *app) int { return a.folderSheet.cursor }

func (placeFolders) cursorRow(a *app, rows []placeRow) int {
	want := a.folderSheet.cursor
	for i, row := range rows {
		if at, ok := row.hit.(int); ok && at == want {
			return i
		}
	}
	return -1
}

func (placeFolders) rowID(a *app) string {
	stop, ok := a.folderPlaceCursor()
	if !ok {
		return ""
	}
	return "folders/" + itoa(int(stop.kind)) + "/" + stop.id + "@" + stop.path
}

func (placeFolders) enter(a *app) tea.Cmd { return a.enterFolderPlace() }

func (placeFolders) verbs(a *app) []verb { return a.folderPlaceVerbs() }

func (placeFolders) box(a *app) *editor {
	if a.folderSheet.naming != nil {
		return a.folderSheet.naming
	}
	return placeBase{}.box(a)
}

func (placeFolders) resting(a *app) string {
	if a.folderSheet.naming != nil {
		return folderNameResting
	}
	return ""
}

func (placeFolders) note(a *app, width int) []string {
	text := strings.TrimSpace(a.folderSheet.note)
	if text == "" {
		return nil
	}
	return []string{" " + a.pal.dim(noteFit(text, width-2))}
}

func (placeFolders) hint(a *app) string {
	if a.folderSheet.naming != nil {
		return folderNameHint
	}
	stop, ok := a.folderPlaceCursor()
	if !ok {
		return "esc"
	}
	if stop.kind.actionWord() != "" {
		return folderActionHint
	}
	return folderPlaceHint
}

func (placeFolders) owns(a *app, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if a.folderSheet.naming == nil {
		return nil, false
	}
	return a.folderPlaceNameKey(msg), true
}

func (placeFolders) key(a *app, msg tea.KeyPressMsg) tea.Cmd { return a.folderPlaceKey(msg) }

func (placeFolders) press(a *app, y int) (tea.Cmd, bool) {
	if at, ok := a.folderPlaceHitAt(y); ok {
		a.folderSheet.cursor = at
		a.rememberFolderPlace()
		a.touch()
		return a.enterFolderPlace(), true
	}
	return nil, true
}

func (placeFolders) hover(a *app, y int) bool {
	next := -1
	if at, ok := a.folderPlaceHitAt(y); ok {
		next = at
	}
	return placeHoverMoved(&a.folderSheet.hover, next, a)
}

func (placeFolders) wheel(a *app, delta int) (tea.Cmd, bool) {
	a.moveFolderPlace(delta)
	a.touch()
	return nil, true
}

// ── snapshot ────────────────────────────────────────────────────────────────

// refreshFolderPlace is the beat and the door: world, graph, organize status.
// View reads [foldersPlace.reading] and never the seam.
func (a *app) refreshFolderPlace() {
	a.home.world, a.home.known = a.readWorldKnown()
	a.readFolderPlaceGraph()
	a.readFolderOrganize()
	a.folderSheet.stops = a.folderPlaceLayout()
	a.restoreFolderPlaceCursor()
	a.touch()
}

func (a *app) readFolderPlaceGraph() {
	p := &a.folderSheet
	savedOpen, savedTrail := a.home.folderOpen, append([]string(nil), a.home.folderTrail...)
	a.home.folderOpen, a.home.folderTrail = p.open, append([]string(nil), p.trail...)
	a.readHomeFolders()
	a.readCollab()
	a.readExec()
	p.reading = a.home.folders
	p.open = a.home.folderOpen
	p.trail = append([]string(nil), a.home.folderTrail...)
	a.home.folderOpen, a.home.folderTrail = savedOpen, savedTrail
}

func (a *app) readFolderOrganize() {
	if a.folders == nil {
		return
	}
	view, err := a.folders.OrganizeStatus(a.folderCtx())
	if err != nil {
		return
	}
	a.folderSheet.organize = view
}

// ── layout ──────────────────────────────────────────────────────────────────

func (a *app) folderPlaceLayout() []folderPlaceStop {
	p := &a.folderSheet
	stops := a.folderPlaceActionStops()
	if p.reading.missing {
		return stops
	}
	open := strings.TrimSpace(p.open)
	if open == "" {
		return append(stops, a.folderPlaceRootStops()...)
	}
	return append(stops, a.folderPlaceOpenStops(open)...)
}

func (a *app) folderPlaceActionStops() []folderPlaceStop {
	base := folderPlacePath(a.folderSheet.open, "")
	stops := []folderPlaceStop{
		{kind: folderStopNewFolder, id: "new-folder", path: base},
		{kind: folderStopNewChat, id: "new-chat", path: base},
		{kind: folderStopOrganize, id: "organize", path: base},
	}
	if folderJobLive(a.folderSheet.organize.State) {
		stops = append(stops, folderPlaceStop{kind: folderStopCancel, id: "cancel", path: base})
	}
	return stops
}

func (a *app) folderPlaceRootStops() []folderPlaceStop {
	in := a.folderPlaceGrid()
	var stops []folderPlaceStop
	for _, folder := range parentlessFolders(a.folderSheet.reading.root.Folders) {
		line := folderRowLine(folder)
		stops = append(stops, folderPlaceStop{
			kind: folderStopFolder, id: folder.ID, path: folderPlacePath("", folder.ID), line: line,
		})
	}
	for _, place := range a.folderSheet.reading.root.Unfiled {
		if folderCollectionPlacement(place) {
			continue
		}
		line, ok := folderMemberLine(&in, place, "")
		if !ok {
			continue
		}
		stops = append(stops, folderPlaceStop{
			kind: folderStopChat, id: place.RefID, path: folderPlacePath("", place.RefID), line: line,
		})
	}
	return stops
}

func (a *app) folderPlaceOpenStops(open string) []folderPlaceStop {
	in := a.folderPlaceGrid()
	folder := a.folderSheet.reading.open
	name := folder.Name
	if strings.TrimSpace(name) == "" {
		name = open
	}
	stops := []folderPlaceStop{{
		kind: folderStopBack, id: open, path: folderPlacePath(open, "back"),
		line: folderBackLine(open, name),
	}}
	seen := map[string]bool{}
	for _, child := range childFolders(a.folderSheet.reading.root.Folders, open) {
		seen[child.ID] = true
		stops = append(stops, folderPlaceStop{
			kind: folderStopFolder, id: child.ID, path: folderPlacePath(open, child.ID),
			line: folderRowLine(child),
		})
	}
	for _, place := range a.folderSheet.reading.members {
		if folderCollectionPlacement(place) {
			child := folderViewFromPlacement(place)
			if child.ID == "" || seen[child.ID] {
				continue
			}
			seen[child.ID] = true
			stops = append(stops, folderPlaceStop{
				kind: folderStopFolder, id: child.ID, path: folderPlacePath(open, child.ID),
				line: folderRowLine(child),
			})
			continue
		}
		line, ok := folderMemberLine(&in, place, open)
		if !ok {
			continue
		}
		stops = append(stops, folderPlaceStop{
			kind: folderStopChat, id: place.RefID, path: folderPlacePath(open, place.RefID), line: line,
		})
	}
	return stops
}

func (a *app) folderPlaceGrid() homeGridInput {
	return homeGridInput{
		world:      a.home.world,
		folders:    a.folderSheet.reading,
		folderOpen: a.folderSheet.open,
		now:        a.now(),
	}
}

func (a *app) restoreFolderPlaceCursor() {
	p := &a.folderSheet
	if len(p.stops) == 0 {
		p.cursor = 0
		return
	}
	if p.selectedID != "" {
		for i, stop := range p.stops {
			if stop.id == p.selectedID && stop.path == p.selectedPath {
				p.cursor = i
				return
			}
		}
		for i, stop := range p.stops {
			if stop.id == p.selectedID {
				p.cursor = i
				return
			}
		}
	}
	if p.cursor >= len(p.stops) {
		p.cursor = len(p.stops) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
	a.rememberFolderPlace()
}

func (a *app) rememberFolderPlace() {
	p := &a.folderSheet
	if p.cursor < 0 || p.cursor >= len(p.stops) {
		return
	}
	stop := p.stops[p.cursor]
	p.selectedID, p.selectedPath = stop.id, stop.path
}

func (a *app) folderPlaceCursor() (folderPlaceStop, bool) {
	p := &a.folderSheet
	if p.cursor < 0 || p.cursor >= len(p.stops) {
		return folderPlaceStop{}, false
	}
	return p.stops[p.cursor], true
}

func (a *app) moveFolderPlace(delta int) {
	p := &a.folderSheet
	if len(p.stops) == 0 {
		return
	}
	p.cursor = moveCursor(p.cursor, delta, len(p.stops))
	a.rememberFolderPlace()
}

// ── paint ───────────────────────────────────────────────────────────────────

func (a *app) folderPlaceBody(width, room int) []placeRow {
	p := &a.folderSheet
	lines := a.folderPlaceLines(width)
	cursorLine := folderPlaceLineOf(lines, p.cursor)
	p.top = placeTop(p.top, cursorLine, len(lines), room)
	rows := make([]placeRow, 0, room)
	for i := p.top; i < len(lines) && len(rows) < room; i++ {
		text := lines[i].text
		if lines[i].stop >= 0 && (lines[i].stop == p.cursor || lines[i].stop == p.hover) {
			text = placeBand(text, width, a.pal)
		}
		rows = append(rows, placeRow{text: text, hit: lines[i].stop})
	}
	p.shown = len(rows)
	for len(rows) < room {
		rows = append(rows, placeRow{text: "", hit: -1})
	}
	return rows
}

func folderPlaceLineOf(lines []folderPlaceLine, stop int) int {
	for i, line := range lines {
		if line.stop == stop {
			return i
		}
	}
	return 0
}

func (a *app) folderPlaceHitAt(y int) (int, bool) {
	p := &a.folderSheet
	at, ok := placeBodyLine(y, p.top, p.shown)
	if !ok {
		return -1, false
	}
	lines := a.folderPlaceLines(a.width)
	if at < 0 || at >= len(lines) || lines[at].stop < 0 {
		return -1, false
	}
	return lines[at].stop, true
}

type folderPlaceLine struct {
	text string
	stop int
}

func (a *app) folderPlaceLines(width int) []folderPlaceLine {
	pal := a.pal
	inner := width - len(placeLead)
	if inner < 1 {
		inner = 1
	}
	head := "folders"
	if extra := folderIndexCopy(a.folderSheet.reading.index); extra != "" {
		head += rowSep + extra
	}
	out := []folderPlaceLine{{text: placeLead + placeHeading(fit(head, inner), pal), stop: -1}}
	out = append(out, a.folderPlacePaintStops(width)...)
	out = append(out, a.folderPlacePaintStatus(width)...)
	out = append(out, a.folderPlacePaintWhisper(width)...)
	return out
}

func (a *app) folderPlacePaintStops(width int) []folderPlaceLine {
	pal := a.pal
	inner := width - len(placeLead)
	if inner < 1 {
		inner = 1
	}
	var out []folderPlaceLine
	for i, stop := range a.folderSheet.stops {
		out = append(out, folderPlaceLine{text: a.folderPlaceStopText(stop, inner, pal), stop: i})
	}
	return out
}

func (a *app) folderPlaceStopText(stop folderPlaceStop, inner int, pal palette) string {
	if word := stop.kind.actionWord(); word != "" {
		return placeLead + placeSubject(fit(word, inner), false, pal)
	}
	title := strings.TrimSpace(stop.line.project)
	if stop.line.cell != nil {
		if t := strings.TrimSpace(stop.line.cell.title); t != "" {
			title = t
		}
	}
	if title == "" {
		title = stop.id
	}
	if stop.line.cell != nil && strings.TrimSpace(stop.line.cell.right) != "" {
		return placeLead + spendSides(inner, title, stop.line.cell.right, placeSubjectInk(false, pal), pal.dim)
	}
	if stop.line.cell != nil && strings.TrimSpace(stop.line.cell.note) != "" {
		title = title + rowSep + pal.dim(stop.line.cell.note)
	}
	return placeLead + placeSubject(fit(title, inner), false, pal)
}

func (a *app) folderPlacePaintStatus(width int) []folderPlaceLine {
	state := strings.TrimSpace(a.folderSheet.organize.State)
	if !folderJobShown(state) {
		return nil
	}
	pal := a.pal
	inner := width - len(placeWhisperLead)
	if inner < 1 {
		inner = 1
	}
	line := state
	if d := strings.TrimSpace(a.folderSheet.organize.Detail); d != "" {
		line += rowSep + d
	}
	return []folderPlaceLine{{text: placeWhisperLead + pal.dim(fit(line, inner)), stop: -1}}
}

func (a *app) folderPlacePaintWhisper(width int) []folderPlaceLine {
	p := &a.folderSheet
	if p.reading.missing {
		inner := width - len(placeWhisperLead)
		if inner < 1 {
			inner = 1
		}
		return []folderPlaceLine{{text: placeWhisperLead + a.pal.dim(fit(folderUnwiredWord, inner)), stop: -1}}
	}
	if strings.TrimSpace(p.open) != "" {
		return nil
	}
	if len(parentlessFolders(p.reading.root.Folders)) > 0 {
		return nil
	}
	var out []folderPlaceLine
	for _, words := range homeWhisperLines(folderWhisperWord, width-len(placeWhisperLead)) {
		out = append(out, folderPlaceLine{text: placeWhisperLead + a.pal.dim(words), stop: -1})
	}
	return out
}

// ── keys ────────────────────────────────────────────────────────────────────

func (a *app) folderPlaceKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		return a.escFolderPlace()
	case "up", "ctrl+p":
		a.moveFolderPlace(-1)
		a.touch()
		return nil
	case "down", "ctrl+n":
		a.moveFolderPlace(1)
		a.touch()
		return nil
	case "enter":
		return a.enterFolderPlace()
	}
	if box := a.placeBox(); box != nil {
		listNavigate(msg, box, a.moveFolderPlace, func() {}, 8)
		a.touch()
	}
	return nil
}

func (a *app) escFolderPlace() tea.Cmd {
	p := &a.folderSheet
	if strings.TrimSpace(p.open) != "" {
		a.leaveFolderPlace()
		a.refreshFolderPlace()
		return nil
	}
	a.leavePlace()
	return nil
}

func (a *app) folderPlaceNameKey(msg tea.KeyPressMsg) tea.Cmd {
	box := a.folderSheet.naming
	switch msg.String() {
	case "esc":
		a.folderSheet.naming = nil
		a.touch()
		return nil
	case "enter":
		name := strings.TrimSpace(box.String())
		a.folderSheet.naming = nil
		return a.createLogicalFolder(name)
	}
	listNavigate(msg, box, func(int) {}, func() {}, 1)
	a.touch()
	return nil
}

func (a *app) enterFolderPlace() tea.Cmd {
	stop, ok := a.folderPlaceCursor()
	if !ok {
		return nil
	}
	switch stop.kind {
	case folderStopNewFolder:
		return a.beginFolderPlaceCreate()
	case folderStopNewChat:
		return a.startFolderPlaceChat()
	case folderStopOrganize:
		return a.folderPlaceOrganize()
	case folderStopCancel:
		return a.folderPlaceCancel()
	case folderStopBack:
		a.leaveFolderPlace()
		a.refreshFolderPlace()
		return nil
	case folderStopFolder:
		return a.enterFolderPlaceFolder(stop.id)
	case folderStopChat:
		return a.openFolderPlaceChat(stop.line)
	}
	return nil
}

func (a *app) beginFolderPlaceCreate() tea.Cmd {
	if a.foldersUnavailable() {
		return nil
	}
	box := editor{}
	a.folderSheet.naming = &box
	a.touch()
	return nil
}

func (a *app) startFolderPlaceChat() tea.Cmd {
	if a.foldersUnavailable() {
		return nil
	}
	if !a.canStart() {
		a.folderNote(newUnavailableWord)
		return nil
	}
	id := a.folderPlaceChatFolder()
	a.leavePlace()
	cmd := a.openChatStart()
	if a.startingChat() && id != "" {
		a.pendingFolder = id
	}
	return cmd
}

func (a *app) folderPlaceChatFolder() string {
	stop, ok := a.folderPlaceCursor()
	if ok && stop.kind == folderStopFolder {
		return strings.TrimSpace(stop.id)
	}
	return strings.TrimSpace(a.folderSheet.open)
}

func (a *app) folderPlaceOrganize() tea.Cmd {
	if a.foldersUnavailable() {
		return nil
	}
	view, err := a.folders.OrganizeExisting(a.folderCtx())
	if err != nil {
		a.folderNote(folderOrganizeFailWord)
		return nil
	}
	a.folderSheet.organize = view
	a.refreshFolderPlace()
	return nil
}

func (a *app) folderPlaceCancel() tea.Cmd {
	if a.foldersUnavailable() {
		return nil
	}
	if err := a.folders.CancelOrganize(a.folderCtx()); err != nil {
		a.folderNote(folderOrganizeFailWord)
		return nil
	}
	a.folderSheet.organize.State = "cancel"
	a.refreshFolderPlace()
	return nil
}

func (a *app) enterFolderPlaceFolder(id string) tea.Cmd {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	p := &a.folderSheet
	if cur := strings.TrimSpace(p.open); cur != "" {
		p.trail = append(p.trail, cur)
	}
	p.open = id
	a.refreshFolderPlace()
	return nil
}

func (a *app) leaveFolderPlace() {
	p := &a.folderSheet
	n := len(p.trail)
	if n == 0 {
		p.open = ""
		return
	}
	p.open = p.trail[n-1]
	p.trail = p.trail[:n-1]
}

func (a *app) openFolderPlaceChat(line homeLine) tea.Cmd {
	a.leavePlace()
	return a.homeOpenLine(line)
}

func (a *app) folderPlaceVerbs() []verb {
	verbs := []verb{
		{key: 'c', word: folderNewFolderWord, do: func() tea.Cmd { return a.beginFolderPlaceCreate() }},
		{key: 'n', word: folderNewChatAction, do: func() tea.Cmd { return a.startFolderPlaceChat() }},
		{key: 'o', word: folderOrganizeWord, do: func() tea.Cmd { return a.folderPlaceOrganize() }},
	}
	if folderJobLive(a.folderSheet.organize.State) {
		verbs = append(verbs, verb{key: 'x', word: folderCancelAction, do: func() tea.Cmd { return a.folderPlaceCancel() }})
	}
	stop, ok := a.folderPlaceCursor()
	if !ok {
		return verbs
	}
	if stop.kind == folderStopFolder || stop.kind == folderStopChat || stop.kind == folderStopBack {
		return mergeFolderPlaceVerbs(verbs, a.folderVerbs(stop.line))
	}
	return verbs
}

func mergeFolderPlaceVerbs(base, extra []verb) []verb {
	seen := map[rune]bool{}
	for _, item := range base {
		seen[item.key] = true
	}
	for _, item := range extra {
		if seen[item.key] {
			continue
		}
		seen[item.key] = true
		base = append(base, item)
	}
	return base
}

func (a *app) folderPlaceFolderID() string {
	stop, ok := a.folderPlaceCursor()
	if !ok {
		return strings.TrimSpace(a.folderSheet.open)
	}
	switch stop.kind {
	case folderStopFolder:
		return strings.TrimSpace(stop.id)
	case folderStopBack, folderStopChat, folderStopNewChat, folderStopNewFolder, folderStopOrganize, folderStopCancel:
		return strings.TrimSpace(a.folderSheet.open)
	}
	return strings.TrimSpace(a.folderSheet.open)
}

func (a *app) pointFolderPlaceID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for at, stop := range a.folderSheet.stops {
		if stop.kind == folderStopFolder && stop.id == id {
			a.folderSheet.cursor = at
			a.rememberFolderPlace()
			return true
		}
	}
	return false
}

func (a *app) folderPlaceFrame(width, height int) ([]string, []int, int, int) {
	lines, hits, caretX, caretY := a.placeDraw(placeFolders{}, width, height)
	return lines, placeLineHits(hits), caretX, caretY
}
