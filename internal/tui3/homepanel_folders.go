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
// yet". That whisper is a working store with nothing in it. A nil seam is
// unavailable: the heading stays and the panel names the refusal, rather
// than looking like an empty workspace. With rows it is Root: parentless
// collections, then unfiled chats. Enter drills in sequentially (back,
// children, members) so an 80-column frame never grows a third column of
// the graph.
type foldersPanel struct{ homePanelBase }

func (foldersPanel) rows(in *homeGridInput) homePanelRows {
	if in.folders.missing {
		// NIL IS A WHISPER, NOT A ROW. A fake homeFolderRow used to stand in
		// the field, steal squeeze room from scheduled / since-you-left, and
		// give an empty home a cursor stop that was not a project.
		return homePanelRows{emptyWord: folderUnwiredWord}
	}
	open := strings.TrimSpace(in.folderOpen)
	var out homePanelRows
	if open != "" {
		out = homePanelCut(in, panelFolders, folderOpenLines(in, open))
	} else {
		out = homePanelCut(in, panelFolders, folderRootLines(in))
	}
	// INDEXING LIVES ON THE HEADING so a squeezed 80-col panel still says
	// delayed/degraded without stealing the emptiness whisper or the back row.
	out.said = folderIndexCopy(in.folders.index)
	return out
}

func folderRootLines(in *homeGridInput) []homeLine {
	var lines []homeLine
	for _, folder := range parentlessFolders(in.folders.root.Folders) {
		lines = append(lines, folderRowLine(folder))
	}
	for _, place := range in.folders.root.Unfiled {
		if folderCollectionPlacement(place) {
			continue
		}
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
	lines = append(lines, folderInstructLines(in.folders.guidance)...)
	seen := map[string]bool{}
	for _, child := range childFolders(in.folders.root.Folders, open) {
		seen[child.ID] = true
		lines = append(lines, folderRowLine(child))
	}
	for _, place := range in.folders.members {
		if folderCollectionPlacement(place) {
			child := folderViewFromPlacement(place)
			if child.ID == "" || seen[child.ID] {
				continue
			}
			seen[child.ID] = true
			lines = append(lines, folderRowLine(child))
			continue
		}
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

func folderRowLine(folder FolderView) homeLine {
	cell := &homeCell{panel: panelFolders, title: folder.Name}
	if folder.MemberCount > 0 {
		cell.right = fmt.Sprintf("%d %s", folder.MemberCount, switcherPlural(folder.MemberCount, "chat", "chats"))
	}
	return homeLine{kind: homeFolderRow, dir: folder.ID, project: folder.Name, cell: cell}
}

func folderMemberLine(in *homeGridInput, place FolderPlacement, collectionID string) (homeLine, bool) {
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
	note := folderAlsoIn(place.AlsoIn)
	if !ok {
		// A GONE WORLD ROW IS UNAVAILABLE, NOT AN ORDINARY CHAT. Synthesizing
		// a live SessionRow from title-or-id used to draw J08's missing
		// conversation as a normal session with nothing to tell it apart.
		row = session.SessionRow{ID: id, Title: title}
		if note != "" {
			note = folderUnavailableWord + " · " + note
		} else {
			note = folderUnavailableWord
		}
	}
	if collabChatMarked(in, id) {
		if note != "" {
			note = collabMarkedWord + " · " + note
		} else {
			note = collabMarkedWord
		}
	}
	cell := &homeCell{panel: panelFolders, title: title, note: note, key: collectionID}
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
