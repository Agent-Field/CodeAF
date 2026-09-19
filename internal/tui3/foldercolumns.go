package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Miller columns over the logical Folders graph. Finder is a layout
// reference only: the columns walk membership, never the filesystem.
//
// Wide terminals pin a details pane on the right. Depth that does not fit
// windows older ancestor columns and keeps a breadcrumb. An 80-column
// frame stays one navigation column. THE SNAPSHOT IS TAKEN ON THE BEAT,
// never in View, never on a cursor move.

const (
	folderColumnsWideMin = 100
	folderColumnWidth    = 22
	folderDetailsMin     = 28
	folderRootTitle      = "Root"
)

type folderColumnView struct {
	title    string
	parentID string
	rows     []folderColumnRow
}

type folderColumnRow struct {
	kind     folderPlaceKind
	id, path string
	title    string
	onPath   bool
}

func folderColumnsWide(width int) bool {
	return width >= folderColumnsWideMin
}

func folderColumnBudget(width int) (navCols, colW, detailW int) {
	inner := width - len(placeLead)
	if inner < folderColumnWidth+folderDetailsMin {
		return 1, folderColumnWidth, folderDetailsMin
	}
	detailW = folderDetailsMin
	remain := inner - detailW
	navCols = remain / (folderColumnWidth + 1)
	if navCols < 1 {
		navCols = 1
	}
	colW = folderColumnWidth
	detailW = inner - navCols*(colW+1)
	if detailW < folderDetailsMin {
		detailW = folderDetailsMin
	}
	return navCols, colW, detailW
}

func (a *app) rebuildFolderColumns() {
	p := &a.folderSheet
	p.cols = a.folderColumnViews()
	p.windowFrom = folderWindowFrom(len(p.cols), a.width)
}

func (a *app) folderColumnViews() []folderColumnView {
	p := &a.folderSheet
	cols := []folderColumnView{{title: folderRootTitle, rows: a.folderColumnRootRows()}}
	for _, id := range folderWalkIDs(p.trail, p.open) {
		cols = append(cols, folderColumnView{
			title:    a.folderColumnTitle(id),
			parentID: id,
			rows:     a.folderColumnChildRows(id),
		})
	}
	if extra := a.folderColumnPreview(); extra.title != "" {
		cols = append(cols, extra)
	}
	return cols
}

func folderWalkIDs(trail []string, open string) []string {
	var ids []string
	for _, id := range trail {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if open = strings.TrimSpace(open); open != "" {
		ids = append(ids, open)
	}
	return ids
}

func (a *app) folderColumnTitle(id string) string {
	if folder, ok := a.folderInReadings(id); ok && strings.TrimSpace(folder.Name) != "" {
		return folder.Name
	}
	if id == "" {
		return folderRootTitle
	}
	return id
}

func (a *app) folderColumnRootRows() []folderColumnRow {
	var rows []folderColumnRow
	for _, folder := range parentlessFolders(a.folderSheet.reading.root.Folders) {
		rows = append(rows, a.folderColumnFolderRow("", folder))
	}
	in := a.folderPlaceGrid()
	for _, place := range a.folderSheet.reading.root.Unfiled {
		if folderCollectionPlacement(place) {
			continue
		}
		if row, ok := a.folderColumnChatRow("", place, &in); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

func (a *app) folderColumnChildRows(parent string) []folderColumnRow {
	var rows []folderColumnRow
	seen := map[string]bool{}
	for _, child := range childFolders(a.folderSheet.reading.root.Folders, parent) {
		seen[child.ID] = true
		rows = append(rows, a.folderColumnFolderRow(parent, child))
	}
	for _, place := range a.folderMembersOf(parent) {
		if folderCollectionPlacement(place) {
			child := folderViewFromPlacement(place)
			if child.ID == "" || seen[child.ID] {
				continue
			}
			seen[child.ID] = true
			rows = append(rows, a.folderColumnFolderRow(parent, child))
			continue
		}
		in := a.folderPlaceGrid()
		if row, ok := a.folderColumnChatRow(parent, place, &in); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

func (a *app) folderMembersOf(id string) []FolderPlacement {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	if id == strings.TrimSpace(a.folderSheet.open) {
		return a.folderSheet.reading.members
	}
	if id == strings.TrimSpace(a.folderSheet.previewID) {
		return a.folderSheet.preview
	}
	return nil
}

func (a *app) folderColumnFolderRow(parent string, folder FolderView) folderColumnRow {
	path := folderPlacePath(parent, folder.ID)
	return folderColumnRow{
		kind: folderStopFolder, id: folder.ID, path: path,
		title: folder.Name, onPath: a.folderPathHas(folder.ID),
	}
}

func (a *app) folderColumnChatRow(parent string, place FolderPlacement, in *homeGridInput) (folderColumnRow, bool) {
	line, ok := folderMemberLine(in, place, parent)
	if !ok {
		return folderColumnRow{}, false
	}
	title := strings.TrimSpace(place.Title)
	if title == "" && line.cell != nil {
		title = strings.TrimSpace(line.cell.title)
	}
	if title == "" {
		title = place.RefID
	}
	return folderColumnRow{
		kind: folderStopChat, id: place.RefID, path: folderPlacePath(parent, place.RefID),
		title: title,
	}, true
}

func (a *app) folderPathHas(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	if id == strings.TrimSpace(a.folderSheet.open) || id == strings.TrimSpace(a.folderSheet.selectedID) {
		return true
	}
	for _, step := range a.folderSheet.trail {
		if step == id {
			return true
		}
	}
	return false
}

func (a *app) folderColumnPreview() folderColumnView {
	stop, ok := a.folderPlaceCursor()
	if !ok || stop.kind != folderStopFolder {
		return folderColumnView{}
	}
	if stop.id == "" || stop.id == strings.TrimSpace(a.folderSheet.open) {
		return folderColumnView{}
	}
	rows := a.folderColumnChildRows(stop.id)
	if len(rows) == 0 {
		for _, child := range childFolders(a.folderSheet.reading.root.Folders, stop.id) {
			rows = append(rows, a.folderColumnFolderRow(stop.id, child))
		}
	}
	if len(rows) == 0 {
		return folderColumnView{}
	}
	return folderColumnView{title: a.folderColumnTitle(stop.id), parentID: stop.id, rows: rows}
}

func folderWindowFrom(n, width int) int {
	nav, _, _ := folderColumnBudget(width)
	if n <= nav {
		return 0
	}
	return n - nav
}

func (a *app) folderColumnCrumb() string {
	p := &a.folderSheet
	if p.windowFrom <= 0 {
		return ""
	}
	var names []string
	names = append(names, folderRootTitle)
	for _, id := range folderWalkIDs(p.trail, p.open) {
		names = append(names, a.folderColumnTitle(id))
	}
	return strings.Join(names, " · ")
}

func (a *app) folderColumnsLines(width int) []folderPlaceLine {
	nav, colW, detailW := folderColumnBudget(width)
	p := &a.folderSheet
	from := p.windowFrom
	if from < 0 {
		from = 0
	}
	visible := p.cols
	if from < len(visible) {
		visible = visible[from:]
	}
	if len(visible) > nav {
		visible = visible[:nav]
	}
	out := a.folderColumnsHead(width)
	out = append(out, a.folderPlacePaintStops(width)...)
	out = append(out, a.folderColumnsGrid(visible, colW, detailW)...)
	out = append(out, a.folderPlacePaintStatus(width)...)
	out = append(out, a.folderPlacePaintChange(width)...)
	out = append(out, a.folderPlacePaintWhisper(width)...)
	return out
}

func (a *app) folderColumnsHead(width int) []folderPlaceLine {
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
	if crumb := a.folderColumnCrumb(); crumb != "" {
		out = append(out, folderPlaceLine{text: placeLead + pal.dim(fit(crumb, inner)), stop: -1})
	}
	return out
}

func (a *app) folderColumnsGrid(cols []folderColumnView, colW, detailW int) []folderPlaceLine {
	pal := a.pal
	heads := make([]string, 0, len(cols)+1)
	for _, col := range cols {
		heads = append(heads, fit(col.title, colW))
	}
	heads = append(heads, fit("details", detailW))
	out := []folderPlaceLine{{text: placeLead + pal.dim(strings.Join(heads, " ")), stop: -1}}
	rows := folderColumnsRowCount(cols, a.folderSheet.detail)
	for i := 0; i < rows; i++ {
		cells := make([]string, 0, len(cols)+1)
		hit := -1
		for _, col := range cols {
			cell, at := a.folderColumnCell(col, i, colW)
			cells = append(cells, cell)
			if at >= 0 {
				hit = at
			}
		}
		detail := ""
		if i < len(a.folderSheet.detail.lines) {
			detail = fit(a.folderSheet.detail.lines[i], detailW)
		} else {
			detail = fit("", detailW)
		}
		cells = append(cells, pal.dim(detail))
		out = append(out, folderPlaceLine{text: placeLead + strings.Join(cells, " "), stop: hit})
	}
	return out
}

func folderColumnsRowCount(cols []folderColumnView, detail folderDetailView) int {
	n := len(detail.lines)
	for _, col := range cols {
		if len(col.rows) > n {
			n = len(col.rows)
		}
	}
	return n
}

func (a *app) folderColumnCell(col folderColumnView, at, width int) (string, int) {
	if at < 0 || at >= len(col.rows) {
		return fit("", width), -1
	}
	row := col.rows[at]
	title := row.title
	if title == "" {
		title = row.id
	}
	ink := a.pal.dim
	if row.onPath {
		ink = a.pal.ink
	}
	return ink(fit(title, width)), a.folderStopIndex(row.id, row.path)
}

func (a *app) folderStopIndex(id, path string) int {
	for i, stop := range a.folderSheet.stops {
		if stop.id == id && stop.path == path {
			return i
		}
	}
	return -1
}

func (a *app) folderPlacePaintActionStops(width int) []folderPlaceLine {
	pal := a.pal
	inner := width - len(placeLead)
	if inner < 1 {
		inner = 1
	}
	var out []folderPlaceLine
	for i, stop := range a.folderSheet.stops {
		if stop.kind.actionWord() == "" {
			continue
		}
		out = append(out, folderPlaceLine{text: a.folderPlaceStopText(stop, inner, pal), stop: i})
	}
	return out
}

func (a *app) folderColumnsKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if a.bar.on {
		return nil, false
	}
	switch msg.String() {
	case "right":
		return a.folderPlaceDrill(), true
	case "left":
		return a.folderPlaceRetreat(), true
	case "shift+right":
		if a.openStrip() {
			a.touch()
		}
		return nil, true
	}
	return nil, false
}

func (a *app) folderPlaceDrill() tea.Cmd {
	if a.folderSheet.detailFocus {
		return nil
	}
	stop, ok := a.folderPlaceCursor()
	if !ok {
		return nil
	}
	switch stop.kind {
	case folderStopFolder:
		if strings.TrimSpace(a.folderSheet.open) == stop.id {
			a.folderSheet.detailFocus = true
			a.touch()
			return nil
		}
		return a.enterFolderPlaceFolder(stop.id)
	case folderStopChat:
		a.folderSheet.detailFocus = true
		a.touch()
		return nil
	}
	return nil
}

func (a *app) folderPlaceRetreat() tea.Cmd {
	if a.folderSheet.detailFocus {
		a.folderSheet.detailFocus = false
		a.touch()
		return nil
	}
	if strings.TrimSpace(a.folderSheet.open) != "" {
		a.leaveFolderPlace()
		a.refreshFolderPlace()
	}
	return nil
}
