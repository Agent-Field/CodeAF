package tui3

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/session"
)

// foldersPanel is `folders`: logical groups of chats, not filesystem
// directories. `/folder` `/place` `/dir` stay filesystem. This is an eighth
// home panel, not an eighth tab-bar place.
//
// EMPTY IT KEEPS ITS HEADING AND ONE DIM TEACHING LINE — never "no folders
// yet". With rows it is Root: parentless collections, then unfiled chats.
// Enter drills in sequentially (back, children, members) so an 80-column
// frame never grows a third column of the graph.
type foldersPanel struct{ homePanelBase }

func (foldersPanel) rows(in *homeGridInput) homePanelRows {
	open := strings.TrimSpace(in.folderOpen)
	if open != "" {
		return homePanelCut(in, panelFolders, folderOpenLines(in, open))
	}
	return homePanelCut(in, panelFolders, folderRootLines(in))
}

func folderRootLines(in *homeGridInput) []homeLine {
	var lines []homeLine
	for _, folder := range parentlessFolders(in.folders.root.Folders) {
		lines = append(lines, folderRowLine(folder))
	}
	for _, place := range in.folders.root.Unfiled {
		if line, ok := folderMemberLine(in, place, ""); ok {
			lines = append(lines, line)
		}
	}
	return lines
}

func folderOpenLines(in *homeGridInput, open string) []homeLine {
	folder := in.folders.open
	if strings.TrimSpace(folder.ID) == "" {
		for _, candidate := range in.folders.root.Folders {
			if candidate.ID == open {
				folder = candidate
				break
			}
		}
	}
	name := folder.Name
	if strings.TrimSpace(name) == "" {
		name = open
	}
	lines := []homeLine{folderBackLine(open, name)}
	for _, child := range childFolders(in.folders.root.Folders, open) {
		lines = append(lines, folderRowLine(child))
	}
	for _, place := range in.folders.members {
		if line, ok := folderMemberLine(in, place, open); ok {
			lines = append(lines, line)
		}
	}
	return lines
}

func folderBackLine(id, name string) homeLine {
	cell := &homeCell{panel: panelFolders, title: folderBackWord, note: name}
	return homeLine{kind: homeFolderBack, dir: id, project: name, cell: cell}
}

func folderRowLine(folder folderView) homeLine {
	cell := &homeCell{panel: panelFolders, title: folder.Name}
	if folder.MemberCount > 0 {
		cell.right = fmt.Sprintf("%d %s", folder.MemberCount, switcherPlural(folder.MemberCount, "chat", "chats"))
	}
	return homeLine{kind: homeFolderRow, dir: folder.ID, project: folder.Name, cell: cell}
}

func folderMemberLine(in *homeGridInput, place folderPlacement, collectionID string) (homeLine, bool) {
	id := strings.TrimSpace(place.RefID)
	if id == "" {
		return homeLine{}, false
	}
	if collectionID == "" {
		collectionID = strings.TrimSpace(place.CollectionID)
	}
	row, ok := folderSessionOf(in, id)
	title := strings.TrimSpace(place.Title)
	if title == "" {
		title = strings.TrimSpace(row.Title)
	}
	if title == "" {
		title = id
	}
	if !ok {
		row = session.SessionRow{ID: id, Title: title}
	}
	cell := &homeCell{panel: panelFolders, title: title, note: folderAlsoIn(place.AlsoIn), key: collectionID}
	return homeLine{kind: homeSession, row: row, dir: row.ProjectDir, project: row.Project, cell: cell}, true
}

func folderSessionOf(in *homeGridInput, id string) (session.SessionRow, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return session.SessionRow{}, false
	}
	for _, row := range in.rows {
		if row.session.ID == id {
			return row.session, true
		}
	}
	for _, project := range in.world.Projects {
		for _, row := range project.Sessions {
			if row.ID == id {
				return row, true
			}
		}
	}
	return session.SessionRow{}, false
}
