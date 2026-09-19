package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Pinned details for the selected Folders-place node. Built on the beat
// from cached snapshots; View never asks the store. An async result that
// does not match the current selectedID + selectedPath + generation is
// dropped (J49). Empty blocks are omitted — never a sentence saying the
// pane is empty.

type folderDetailView struct {
	id, path string
	gen      uint64
	kind     folderPlaceKind
	lines    []string
	actions  []folderPlaceStop
}

type folderDetailReq struct {
	selectedID, selectedPath string
	gen                      uint64
}

func (a *app) rebuildFolderDetails() {
	p := &a.folderSheet
	p.detailGen++
	stop, ok := a.folderPlaceCursor()
	id, path := "", ""
	if ok {
		id, path = stop.id, stop.path
	}
	req := folderDetailReq{selectedID: id, selectedPath: path, gen: p.detailGen}
	a.applyFolderDetail(req, a.buildFolderDetail(stop, ok, req.gen))
}

func (a *app) applyFolderDetail(req folderDetailReq, view folderDetailView) bool {
	p := &a.folderSheet
	if req.gen != p.detailGen {
		return false
	}
	if req.selectedID != p.selectedID || req.selectedPath != p.selectedPath {
		return false
	}
	p.detail = view
	return true
}

func (a *app) buildFolderDetail(stop folderPlaceStop, ok bool, gen uint64) folderDetailView {
	view := folderDetailView{id: stop.id, path: stop.path, gen: gen}
	if !ok {
		return view
	}
	view.kind = stop.kind
	switch stop.kind {
	case folderStopFolder, folderStopBack:
		a.fillFolderDetail(&view, stop)
	case folderStopChat:
		a.fillChatDetail(&view, stop)
	default:
		if open := strings.TrimSpace(a.folderSheet.open); open != "" {
			a.fillFolderDetail(&view, folderPlaceStop{kind: folderStopFolder, id: open, path: folderPlacePath(open, "")})
		}
	}
	return view
}

func (a *app) fillFolderDetail(view *folderDetailView, stop folderPlaceStop) {
	folder := a.folderDetailFolder(stop.id)
	a.detailAdd(view, strings.TrimSpace(folder.Name))
	a.detailAdd(view, strings.TrimSpace(folder.Purpose))
	a.appendFolderActivity(view, stop.id)
	a.appendFolderChildren(view, stop.id)
	a.appendFolderChats(view, stop.id)
	a.appendCoordinating(view, stop.id)
	a.appendFolderInstructions(view, folder)
	a.appendChangeAndWhy(view, stop)
	view.actions = a.folderDetailFolderActions(stop)
}

func (a *app) fillChatDetail(view *folderDetailView, stop folderPlaceStop) {
	in := a.folderPlaceGrid()
	place := a.folderDetailPlacement(stop)
	line, _ := folderMemberLine(&in, place, a.folderSheet.open)
	title := strings.TrimSpace(place.Title)
	if title == "" && line.cell != nil {
		title = strings.TrimSpace(line.cell.title)
	}
	if title == "" {
		title = stop.id
	}
	a.detailAdd(view, title)
	if row, found := folderSessionOf(&in, stop.id); found {
		if extra := strings.TrimSpace(row.Title); extra != "" && extra != title {
			a.detailAdd(view, extra)
		}
	}
	a.appendChatWork(view, stop.id)
	if also := folderAlsoIn(place.AlsoIn); also != "" {
		a.detailAdd(view, also)
	}
	a.appendChangeAndWhy(view, stop)
	a.detailAdd(view, folderOpenChatWord)
	view.actions = a.folderDetailChatActions(stop)
}

func (a *app) folderDetailFolder(id string) FolderView {
	if strings.TrimSpace(id) == strings.TrimSpace(a.folderSheet.reading.open.ID) {
		return a.folderSheet.reading.open
	}
	if folder, ok := a.folderInReadings(id); ok {
		return folder
	}
	return FolderView{ID: id}
}

func (a *app) folderDetailPlacement(stop folderPlaceStop) FolderPlacement {
	for _, place := range a.folderSheet.reading.members {
		if place.RefID == stop.id {
			return place
		}
	}
	for _, place := range a.folderSheet.reading.root.Unfiled {
		if place.RefID == stop.id {
			return place
		}
	}
	for _, place := range a.folderSheet.preview {
		if place.RefID == stop.id {
			return place
		}
	}
	return FolderPlacement{RefID: stop.id, Title: stop.line.project}
}

func (a *app) appendFolderActivity(view *folderDetailView, id string) {
	for _, work := range a.folderSheet.reading.works {
		if text := folderExecWorkText(work); text != "" && a.folderWorkTouches(work, id) {
			a.detailAdd(view, text)
		}
	}
	if state := strings.TrimSpace(a.folderSheet.organize.State); folderJobShown(state) {
		line := state
		if d := strings.TrimSpace(a.folderSheet.organize.Detail); d != "" {
			line += rowSep + d
		}
		a.detailAdd(view, line)
	}
}

func (a *app) folderWorkTouches(work ExecWork, folderID string) bool {
	if strings.Contains(work.Road, folderID) || strings.Contains(work.SourceRef, folderID) {
		return true
	}
	for _, place := range a.folderMembersOf(folderID) {
		if execWorkBelongs(work, place.RefID) {
			return true
		}
	}
	return false
}

func (a *app) appendFolderChildren(view *folderDetailView, id string) {
	for _, child := range childFolders(a.folderSheet.reading.root.Folders, id) {
		a.detailAdd(view, child.Name)
	}
	seen := map[string]bool{}
	for _, child := range childFolders(a.folderSheet.reading.root.Folders, id) {
		seen[child.ID] = true
	}
	for _, place := range a.folderMembersOf(id) {
		if !folderCollectionPlacement(place) || seen[place.RefID] {
			continue
		}
		seen[place.RefID] = true
		name := strings.TrimSpace(place.Title)
		if name == "" {
			name = place.RefID
		}
		a.detailAdd(view, name)
	}
}

func (a *app) appendFolderChats(view *folderDetailView, id string) {
	for _, place := range a.folderMembersOf(id) {
		if folderCollectionPlacement(place) {
			continue
		}
		title := strings.TrimSpace(place.Title)
		if title == "" {
			title = place.RefID
		}
		if also := folderAlsoIn(place.AlsoIn); also != "" {
			title += rowSep + also
		}
		a.detailAdd(view, title)
	}
}

func (a *app) appendCoordinating(view *folderDetailView, _ string) {
	seen := map[string]bool{}
	for _, mark := range a.folderSheet.reading.marked {
		title := strings.TrimSpace(mark.Title)
		if title == "" {
			title = mark.RefID
		}
		if title == "" || seen[title] {
			continue
		}
		seen[title] = true
		a.detailAdd(view, title)
	}
	for _, part := range a.collabView.participants {
		title := strings.TrimSpace(part.SourceTitle)
		if title == "" || seen[title] {
			continue
		}
		seen[title] = true
		a.detailAdd(view, title)
	}
}

func (a *app) appendFolderInstructions(view *folderDetailView, folder FolderView) {
	own := folderInstructBody(a.folderSheet.reading.guidance)
	if strings.TrimSpace(folder.ID) != strings.TrimSpace(a.folderSheet.reading.open.ID) {
		own = ""
	}
	if own != "" {
		a.detailAdd(view, folderInstructHead)
		a.detailAdd(view, own)
	}
	for _, parent := range folder.ParentIDs {
		if body := a.folderInheritedInstructions(parent); body != "" {
			a.detailAdd(view, body)
		}
	}
}

func (a *app) folderInheritedInstructions(id string) string {
	if strings.TrimSpace(id) == strings.TrimSpace(a.folderSheet.reading.open.ID) {
		return folderInstructBody(a.folderSheet.reading.guidance)
	}
	return ""
}

func (a *app) appendChatWork(view *folderDetailView, ref string) {
	for _, work := range a.folderSheet.reading.works {
		if !execWorkBelongs(work, ref) {
			continue
		}
		if text := folderExecWorkText(work); text != "" {
			a.detailAdd(view, text)
		}
	}
}

func (a *app) appendChangeAndWhy(view *folderDetailView, stop folderPlaceStop) {
	if line := folderChangeLine(a.folderSheet.change); line != "" {
		a.detailAdd(view, line)
	}
	if why := strings.TrimSpace(a.folderSheet.whyLine); why != "" && a.folderSheet.whyRef == stop.id {
		a.detailAdd(view, why)
	}
}

func (a *app) detailAdd(view *folderDetailView, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	view.lines = append(view.lines, text)
}

func (a *app) folderDetailFolderActions(stop folderPlaceStop) []folderPlaceStop {
	base := folderPlacePath(a.folderSheet.open, stop.id)
	actions := []folderPlaceStop{{kind: folderStopManage, id: stop.id, path: base + "/manage"}}
	if a.collab != nil && len(a.folderSheet.reading.marked) > 0 {
		actions = append(actions, folderPlaceStop{kind: folderStopCoordinate, id: stop.id, path: base + "/coord"})
	}
	if folderChangeLine(a.folderSheet.change) != "" {
		actions = append(actions, folderPlaceStop{kind: folderStopUndo, id: stop.id, path: base + "/undo"})
		actions = append(actions, folderPlaceStop{kind: folderStopWhy, id: stop.id, path: base + "/why"})
	}
	return actions
}

func (a *app) folderDetailChatActions(stop folderPlaceStop) []folderPlaceStop {
	base := folderPlacePath(a.folderSheet.open, stop.id)
	actions := []folderPlaceStop{
		{kind: folderStopOpenChat, id: stop.id, path: base + "/open", line: stop.line},
		{kind: folderStopOrganizeThis, id: stop.id, path: base + "/this"},
	}
	if folderChangeLine(a.folderSheet.change) != "" {
		actions = append(actions, folderPlaceStop{kind: folderStopUndo, id: stop.id, path: base + "/undo"})
	}
	actions = append(actions, folderPlaceStop{kind: folderStopWhy, id: stop.id, path: base + "/why", line: stop.line})
	if a.collab != nil && len(a.folderSheet.reading.marked) > 0 {
		actions = append(actions, folderPlaceStop{kind: folderStopCoordinate, id: stop.id, path: base + "/coord"})
	}
	return actions
}

func (a *app) folderPlacePaintChange(width int) []folderPlaceLine {
	line := folderChangeLine(a.folderSheet.change)
	if line == "" {
		return nil
	}
	inner := width - len(placeWhisperLead)
	if inner < 1 {
		inner = 1
	}
	return []folderPlaceLine{{text: placeWhisperLead + a.pal.dim(fit(line, inner)), stop: -1}}
}

func (a *app) folderPlaceDetailStops() []folderPlaceStop {
	if a.folderSheet.reading.missing {
		return nil
	}
	return append([]folderPlaceStop(nil), a.folderSheet.detail.actions...)
}

func (a *app) manageThisFolder() tea.Cmd {
	if a.foldersUnavailable() {
		return nil
	}
	id := a.folderPlaceFolderID()
	if id == "" {
		a.folderNote(folderNoStandWord)
		return nil
	}
	if line, ok := a.folderManagingChat(id); ok {
		return a.openFolderPlaceChat(line)
	}
	return a.startInFolder(id)
}

func (a *app) folderManagingChat(folderID string) (homeLine, bool) {
	ref := a.conversationRef()
	if ref == "" {
		return homeLine{}, false
	}
	in := a.folderPlaceGrid()
	for _, place := range a.folderMembersOf(folderID) {
		if folderCollectionPlacement(place) || place.RefID != ref {
			continue
		}
		line, ok := folderMemberLine(&in, place, folderID)
		if ok {
			return line, true
		}
	}
	return homeLine{}, false
}

func (a *app) enterFolderDetailStop(stop folderPlaceStop) tea.Cmd {
	switch stop.kind {
	case folderStopManage:
		return a.manageThisFolder()
	case folderStopOrganizeThis:
		return a.organizeThisChat()
	case folderStopOpenChat:
		return a.openFolderPlaceChat(stop.line)
	case folderStopCoordinate:
		return a.coordinateMarked()
	case folderStopUndo:
		return a.undoFolderChange()
	case folderStopWhy:
		if stop.line.kind == homeSession {
			return a.folderWhyHere(stop.line)
		}
		return a.folderWhyChange()
	}
	return nil
}

func (a *app) folderWhyChange() tea.Cmd {
	if a.foldersUnavailable() {
		return nil
	}
	change := a.folderSheet.change
	if change.CollectionID == "" || change.RefID == "" {
		return nil
	}
	why, err := a.folders.WhyHere(a.folderCtx(), change.CollectionID, change.RefID)
	if err != nil {
		a.folderNote("could not say why this chat is here")
		return nil
	}
	a.folderSheet.whyLine = folderWhyLine(why)
	a.folderSheet.whyRef = change.RefID
	a.folderNote(a.folderSheet.whyLine)
	return nil
}

func (a *app) readFolderPlacePreview() {
	p := &a.folderSheet
	stop, ok := a.folderPlaceCursor()
	if !ok || stop.kind != folderStopFolder || a.folders == nil {
		p.previewID, p.preview = "", nil
		return
	}
	if stop.id == strings.TrimSpace(p.open) {
		p.previewID, p.preview = stop.id, p.reading.members
		return
	}
	_, members, err := a.folders.FolderSnapshot(a.folderCtx(), stop.id)
	if err != nil {
		p.previewID, p.preview = stop.id, nil
		return
	}
	p.previewID, p.preview = stop.id, members
}

func (a *app) folderNarrowDetailLines(width int) []folderPlaceLine {
	inner := width - len(placeLead)
	if inner < 1 {
		inner = 1
	}
	out := a.folderColumnsHead(width)
	for _, line := range a.folderSheet.detail.lines {
		out = append(out, folderPlaceLine{text: placeLead + a.pal.dim(fit(line, inner)), stop: -1})
	}
	for i, stop := range a.folderSheet.stops {
		if stop.kind.actionWord() == "" {
			continue
		}
		out = append(out, folderPlaceLine{text: a.folderPlaceStopText(stop, inner, a.pal), stop: i})
	}
	return out
}
