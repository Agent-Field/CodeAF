package tui3

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── ADD EXISTING CHATS (F06) ────────────────────────────────────────────────
//
// The Folders place's missing door: search and browse saved chats by the names
// the inventory already uses, mark several, and file them in the folder the
// person is standing in. `f` and `/folders add` still file the CURRENT chat;
// this is the many-at-once path that does not ask anyone to remember a 16-hex
// id and does not open or merge those conversations.
//
// THE PICKER IS A FOLDERS-PLACE LAYER, not a second overlay grammar. It owns
// the keyboard the way the name box does ([placeFolders.owns]), draws into the
// place body, and dies when the place closes. Snapshot of the inventory is
// taken when the picker opens, never in View, never on a cursor move.

const folderAddExistingWord = "Add existing chats"

const (
	folderAddHint      = "filter · space mark · enter add · esc cancel"
	folderAddResting   = "filter saved chats"
	folderAddEmptyWord = "no saved chats to add"
	folderAddNoneWord  = "no chat matches"
	folderAddNoStand   = "stand on a folder · then add existing chats"
	folderAddRows      = 10
)

// folderAddPick is the in-place picker. The zero value is closed.
type folderAddPick struct {
	open     bool
	folderID string
	folder   string
	all      []folderAddRow
	lower    []string
	score    []int
	hits     []int
	cursor   int
	top      int
	hover    int
	shown    int
	marked   map[string]bool
	filter   editor
}

// folderAddRow is one saved chat as the picker lists it. Title is what a
// person reads; ref is the membership id AddPlacement wants and is never
// painted.
type folderAddRow struct {
	ref, title, last string
}

func (p *folderAddPick) close() { *p = folderAddPick{} }

func (p folderAddPick) opened() bool { return p.open }

// beginFolderAdd is the one Folders-place door t-ux-add-old fills. Restable
// stop word is [folderAddExistingWord]; chord `b`. Absent this file, that row
// is absent — a capability that cannot work is not a broken button.
func (a *app) beginFolderAdd() tea.Cmd {
	if a.foldersUnavailable() {
		return nil
	}
	id, name := a.folderAddTarget()
	if id == "" {
		a.folderNote(folderAddNoStand)
		return nil
	}
	rows := a.folderAddInventory(id)
	if len(rows) == 0 {
		a.folderNote(folderAddEmptyWord)
		return nil
	}
	a.closeStrip()
	p := &a.folderSheet.add
	*p = folderAddPick{
		open:     true,
		folderID: id,
		folder:   name,
		all:      rows,
		marked:   map[string]bool{},
		hover:    -1,
	}
	p.lower = make([]string, len(rows))
	p.score = make([]int, len(rows))
	for i, row := range rows {
		p.lower[i] = strings.ToLower(strings.TrimSpace(row.title) + " " + strings.TrimSpace(row.last))
	}
	p.rank()
	a.touch()
	return nil
}

func (a *app) folderAddTarget() (id, name string) {
	id = strings.TrimSpace(a.folderPlaceChatFolder())
	if id == "" {
		return "", ""
	}
	name = a.folderAddName(id)
	return id, name
}

func (a *app) folderAddName(id string) string {
	id = strings.TrimSpace(id)
	open := a.folderSheet.reading.open
	if strings.TrimSpace(open.ID) == id && strings.TrimSpace(open.Name) != "" {
		return strings.TrimSpace(open.Name)
	}
	for _, folder := range a.folderSheet.reading.root.Folders {
		if folder.ID == id && strings.TrimSpace(folder.Name) != "" {
			return strings.TrimSpace(folder.Name)
		}
	}
	stop, ok := a.folderPlaceCursor()
	if ok && stop.kind == folderStopFolder && stop.id == id {
		if n := strings.TrimSpace(stop.line.project); n != "" {
			return n
		}
	}
	return id
}

// folderAddInventory is the browse list: resume's saved-chat inventory, plus
// unfiled Root placements the snapshot already holds, titled the way those
// surfaces already title them. Chats already in the destination are omitted
// so adding is not a no-op ritual. The 16-hex id is derived from the session
// folder; a row that cannot produce one is dropped rather than shown as an id.
func (a *app) folderAddInventory(folderID string) []folderAddRow {
	have := a.folderAddHave(folderID)
	seen := map[string]bool{}
	var rows []folderAddRow
	add := func(ref, title, last string) {
		ref = strings.TrimSpace(ref)
		if ref == "" || have[ref] || seen[ref] {
			return
		}
		title = strings.TrimSpace(title)
		if title == "" {
			return
		}
		seen[ref] = true
		rows = append(rows, folderAddRow{ref: ref, title: title, last: strings.TrimSpace(last)})
	}
	if a.recentSessions != nil {
		for _, session := range a.recentSessions() {
			add(conversationRefFromFile(session.File), humanName(session), session.Last)
		}
	}
	for _, place := range a.folderSheet.reading.root.Unfiled {
		if folderCollectionPlacement(place) {
			continue
		}
		add(strings.TrimSpace(place.RefID), strings.TrimSpace(place.Title), "")
	}
	return rows
}

func (a *app) folderAddHave(folderID string) map[string]bool {
	have := map[string]bool{}
	folderID = strings.TrimSpace(folderID)
	if folderID == "" || a.folders == nil {
		return have
	}
	if strings.TrimSpace(a.folderSheet.reading.open.ID) == folderID {
		for _, place := range a.folderSheet.reading.members {
			if folderCollectionPlacement(place) {
				continue
			}
			if ref := strings.TrimSpace(place.RefID); ref != "" {
				have[ref] = true
			}
		}
		return have
	}
	_, members, err := a.folders.FolderSnapshot(a.folderCtx(), folderID)
	if err != nil {
		return have
	}
	for _, place := range members {
		if folderCollectionPlacement(place) {
			continue
		}
		if ref := strings.TrimSpace(place.RefID); ref != "" {
			have[ref] = true
		}
	}
	return have
}

func (p *folderAddPick) rank() {
	words := strings.Fields(strings.ToLower(p.filter.String()))
	p.hits = p.hits[:0]
	for i, text := range p.lower {
		if len(words) == 0 {
			p.hits = append(p.hits, i)
			continue
		}
		total, matched := 0, true
		for _, word := range words {
			score, hit := tokenScore(text, word)
			if !hit {
				matched = false
				break
			}
			total += score
		}
		if !matched {
			continue
		}
		p.score[i] = total
		p.hits = append(p.hits, i)
	}
	if len(words) > 0 {
		sort.SliceStable(p.hits, func(a, b int) bool { return p.score[p.hits[a]] < p.score[p.hits[b]] })
	}
	p.cursor, p.top = 0, 0
}

func (p *folderAddPick) move(delta int) {
	p.cursor = moveCursor(p.cursor, delta, len(p.hits))
	p.follow(folderAddRows)
}

func (p *folderAddPick) follow(height int) {
	p.top = listTop(p.cursor, p.top, len(p.hits), height)
}

func (p *folderAddPick) choice() (folderAddRow, bool) {
	if !p.open || p.cursor < 0 || p.cursor >= len(p.hits) {
		return folderAddRow{}, false
	}
	return p.all[p.hits[p.cursor]], true
}

func (p *folderAddPick) toggle() {
	row, ok := p.choice()
	if !ok || row.ref == "" {
		return
	}
	if p.marked == nil {
		p.marked = map[string]bool{}
	}
	if p.marked[row.ref] {
		delete(p.marked, row.ref)
		return
	}
	p.marked[row.ref] = true
}

func (p *folderAddPick) selected() []folderAddRow {
	if len(p.marked) == 0 {
		if row, ok := p.choice(); ok {
			return []folderAddRow{row}
		}
		return nil
	}
	var out []folderAddRow
	seen := map[string]bool{}
	for _, row := range p.all {
		if !p.marked[row.ref] || seen[row.ref] {
			continue
		}
		seen[row.ref] = true
		out = append(out, row)
	}
	return out
}

func folderAddSpace(msg tea.KeyPressMsg) bool {
	switch msg.String() {
	case " ", "space":
		return true
	}
	return msg.Key().Text == " "
}

func (a *app) folderAddKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &a.folderSheet.add
	switch {
	case msg.String() == "esc":
		p.close()
		a.touch()
		return nil
	case msg.String() == "enter":
		return a.finishFolderAdd()
	case folderAddSpace(msg):
		p.toggle()
		a.touch()
		return nil
	}
	listNavigate(msg, &p.filter, p.move, p.rank, folderAddRows)
	a.touch()
	return nil
}

func (a *app) finishFolderAdd() tea.Cmd {
	p := &a.folderSheet.add
	id := strings.TrimSpace(p.folderID)
	name := p.folder
	rows := p.selected()
	p.close()
	if id == "" || len(rows) == 0 {
		a.touch()
		return nil
	}
	if a.foldersUnavailable() {
		return nil
	}
	failed := false
	for _, row := range rows {
		if err := a.folders.AddPlacement(a.folderCtx(), id, row.ref); err != nil {
			failed = true
		}
	}
	if failed {
		a.folderNote(folderFiledWord)
		a.refreshFolderPlace()
		return nil
	}
	if note := folderAddDoneWord(name); note != "" {
		a.folderNote(note)
	}
	a.refreshFolderPlace()
	return nil
}

func folderAddDoneWord(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return "Added to " + name
}

func (a *app) folderAddActionStops() []folderPlaceStop {
	return []folderPlaceStop{{
		kind: folderStopAdd,
		id:   "add-existing",
		path: folderPlacePath(a.folderSheet.open, ""),
	}}
}

func (a *app) folderAddVerb() verb {
	return verb{key: 'b', word: folderAddExistingWord, do: func() tea.Cmd { return a.beginFolderAdd() }}
}

func (a *app) folderAddBody(width, room int) []placeRow {
	p := &a.folderSheet.add
	lines := a.folderAddLines(width)
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

func (a *app) folderAddLines(width int) []folderPlaceLine {
	p := &a.folderSheet.add
	pal := a.pal
	inner := width - len(placeLead)
	if inner < 1 {
		inner = 1
	}
	head := folderAddExistingWord
	if extra := strings.TrimSpace(p.folder); extra != "" {
		head += rowSep + extra
	}
	out := []folderPlaceLine{{text: placeLead + placeHeading(fit(head, inner), pal), stop: -1}}
	if len(p.hits) == 0 {
		out = append(out, folderPlaceLine{text: placeLead + pal.dim(fit(folderAddNoneWord, inner)), stop: -1})
		return out
	}
	p.follow(folderAddRows)
	for at := p.top; at < len(p.hits); at++ {
		row := p.all[p.hits[at]]
		out = append(out, folderPlaceLine{text: a.folderAddRowText(row, inner, pal), stop: at})
	}
	return out
}

func (a *app) folderAddRowText(row folderAddRow, inner int, pal palette) string {
	mark := a.icon(tokens.GQueued)
	if a.folderSheet.add.marked[row.ref] {
		mark = a.icon(tokens.GSettled)
	}
	title := row.title
	if last := strings.TrimSpace(row.last); last != "" {
		title = title + rowSep + pal.dim(last)
	}
	text := mark + " " + title
	return placeLead + placeSubject(fit(text, inner), false, pal)
}

func (a *app) folderAddHitAt(y int) (int, bool) {
	p := &a.folderSheet.add
	at, ok := placeBodyLine(y, p.top, p.shown)
	if !ok {
		return -1, false
	}
	lines := a.folderAddLines(a.width)
	if at < 0 || at >= len(lines) || lines[at].stop < 0 {
		return -1, false
	}
	return lines[at].stop, true
}

func (a *app) folderAddPress(y int) (tea.Cmd, bool) {
	if at, ok := a.folderAddHitAt(y); ok {
		a.folderSheet.add.cursor = at
		a.folderSheet.add.follow(folderAddRows)
		a.folderSheet.add.toggle()
		a.touch()
	}
	return nil, true
}

func (a *app) folderAddHover(y int) bool {
	next := -1
	if at, ok := a.folderAddHitAt(y); ok {
		next = at
	}
	return placeHoverMoved(&a.folderSheet.add.hover, next, a)
}
