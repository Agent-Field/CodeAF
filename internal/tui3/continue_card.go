package tui3

import (
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/session"
)

// continueCard keeps the selected chat's facts separate from the window behind Home.
type continueCard struct {
	draft      composerState
	project    string
	returnFile string
}

func (a *app) continueCardFor(row chatlist.Row, fromWindow bool) *continueCard {
	card := &continueCard{}
	if a.file != "" && filepath.Base(session.DirOf(a.file)) == row.Cell {
		card.draft = a.mainComposer()
		card.project = a.hostedPath(a.placeWord(tildePath(a.workspace, a.tilde)))
		if fromWindow {
			card.returnFile = a.file
		}
	}
	if line, ok := a.homeLineOf(row.Cell); ok {
		card.project = a.hostedPath(tildePath(line.row.Workspace, a.tilde))
		if line.row.Origin != "" {
			card.project = filepath.Base(line.row.Origin) + session.MovedCopyWord
		}
		return card
	}
	for _, project := range a.home.world.Projects {
		for _, s := range project.Sessions {
			if homeSessionID(s) == row.Cell {
				card.project = a.hostedPath(tildePath(s.Workspace, a.tilde))
				if s.Origin != "" {
					card.project = filepath.Base(s.Origin) + session.MovedCopyWord
				}
				return card
			}
		}
	}
	return card
}

// questionPreviewRows shows only the composer explicitly attached to a move card.
func (a *app) questionPreviewRows(q questionShown, width int) []string {
	if q.continuation == nil {
		rows, _, _ := a.inputBlock(width)
		return rows
	}
	box := q.continuation.draft.box
	if box.empty() {
		return nil
	}
	rows, _, _ := draftBlockWithTags(&box, a.pal, width, homeDraftRows, "", "", nil, a.pal.ink)
	return rows
}
