package syncsetup

import (
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// sealedCrew is the crew chat c belongs to on this computer, sealed for its
// directory record, so a paired computer shows a crew made here as one crew,
// read-only (chatlist.Crew says why the record carries it). It is sealed on
// every publish, "in no crew" included, so a chat that left its crew stops
// saying it is in one at its next saved turn. A teams store that cannot be
// read says nothing (""), which keeps whatever the record said last.
func (s *Sync) sealedCrew(c cell.Cell) string {
	f, err := teams.LoadOpen(s.profile())
	if err != nil {
		return ""
	}
	sealed, err := chatlist.SealCrew(directory.MetadataKey(s.Identity.CellKey()), CrewOfChat(f.Open(), c.Root, c.ID))
	if err != nil {
		return ""
	}
	return sealed
}

// CrewOfChat is the crew the chat at root (directory id id) is shown under,
// zero when it is in none. root is the cell's own folder, as the chat list and
// cell.Cell know it. A member names its chat by transcript path on the
// computer that holds it, or as `machine:<cell>` once the chat moved away.
func CrewOfChat(open []teams.Team, root, id string) chatlist.Crew {
	roots := map[string]bool{}
	if root != "" {
		roots[filepath.Clean(root)] = true
		if real, err := filepath.EvalSymlinks(root); err == nil {
			roots[filepath.Clean(real)] = true
		}
	}
	t, manager, ok := teams.ChatCrew(open, func(m teams.Member) bool {
		if remote, ok := teams.RemoteCell(m.Key); ok {
			return id != "" && remote == id
		}
		// An older chat may run from its finished moving copy (cell.Copied)
		// while its membership still names the original's transcript.
		for _, file := range []string{m.File, m.Key} {
			if file == "" {
				continue
			}
			dir := filepath.Clean(session.DirOf(file))
			if roots[dir] || roots[filepath.Clean(cell.CopyRoot(dir))] {
				return true
			}
		}
		return false
	})
	if !ok {
		return chatlist.Crew{}
	}
	out := chatlist.Crew{ID: t.ID, Name: t.Name, Manager: manager}
	if t.FactoryManaged {
		out.Kind = chatlist.CrewKindFactory
	}
	return out
}
