package placegraph

// A PLACE FROM A FOLDER (Places §6d P-6d-2, P-6d-2a, §8e P-8e-7; Shell IX S-IX-9).
//
// Dropping a folder on the rail is the shortest road to a place: the folder IS
// the place's first source and its name IS the place's name. A folder inside a
// git work tree means the repository, not the subfolder, because the person who
// drags `app/src` onto the rail is naming the project around it, and a place
// called "src" would be a name nobody chose.
//
// IT IS IDEMPOTENT. Dropping the same folder twice answers the place the first
// drop made, so a double drop or a second window cannot leave two places that
// both claim one directory.
//
// IT OFFERS AND NEVER FILES. The conversations that already ran inside the folder
// and belong to no place are returned so the surface can say "Move N matching
// tabs" and file them with one ordinary members write; this file moves nothing.

import (
	"path/filepath"
	"sort"
	"strings"
)

// FolderPlace is what FromFolder answers.
type FolderPlace struct {
	Place   Place
	Created bool
	// Receipt is the creation commit, and the zero receipt when the place already
	// existed.
	Receipt Receipt
}

// FromFolder returns the active top-level place whose first source is path's
// folder (or the repository around it), creating it when there is none.
// A refused path answers ErrSourceRefused from NewSource, and a name already
// taken by an unrelated top-level place answers ErrNameTaken.
func (s *Store) FromFolder(path string, pol SourcePolicy) (FolderPlace, error) {
	src, err := droppedFolderSource(path, pol)
	if err != nil {
		return FolderPlace{}, err
	}
	snap, err := s.Snapshot()
	if err != nil {
		return FolderPlace{}, err
	}
	if have, ok := snap.placeForSource(src); ok {
		return FolderPlace{Place: have}, nil
	}
	// The id and stamp are the store's to give, like every other source it holds.
	src.ID = ""
	pl, rc, err := s.CreatePlace(NewPlace{
		Name:    src.Label,
		Tint:    snap.NewTopLevelTint(),
		Context: Context{Sources: []Source{src}},
	})
	if err != nil {
		return FolderPlace{}, err
	}
	return FolderPlace{Place: pl, Created: true, Receipt: rc}, nil
}

// droppedFolderSource validates path as a source and widens a folder inside a work tree
// to the repository root, which NewSource then stores as kind repo.
func droppedFolderSource(path string, pol SourcePolicy) (Source, error) {
	src, err := NewSource(SourceFolder, path, AddedByYou, pol)
	if err != nil {
		return Source{}, err
	}
	if src.Kind == SourceRepo {
		return src, nil
	}
	if root := repoRoot(src.Ref, true); root != "" {
		return NewSource(SourceRepo, root, AddedByYou, pol)
	}
	return src, nil
}

// placeForSource finds the active top-level place whose FIRST source is src's
// path. Only the first counts: a place that merely lists the folder among
// several is about something else.
func (s *Snapshot) placeForSource(src Source) (Place, bool) {
	key := SourceKey(src)
	for _, p := range s.Places {
		if p.Archived || len(p.Parents) > 0 || len(p.Context.Sources) == 0 {
			continue
		}
		if SourceKey(p.Context.Sources[0]) == key {
			return p, true
		}
	}
	return Place{}, false
}

// MatchingUnplacedChats lists the chats whose recorded workspace is the folder
// root or beneath it and which are filed in no place, sorted by id so the offer
// is stable. workspaces maps chat id to workspace (session.SessionWorkspaces).
func (s *Snapshot) MatchingUnplacedChats(root string, workspaces map[string]string) []string {
	root = filepath.Clean(root)
	filed := map[string]bool{}
	for _, m := range s.Memberships {
		filed[m.ChatID] = true
	}
	out := []string{}
	for id, ws := range workspaces {
		if filed[id] || !insideFolder(root, ws) {
			continue
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// insideFolder compares the spelling and, failing that, where symlinks lead,
// since the root is stored resolved and a recorded workspace may not be.
func insideFolder(root, ws string) bool {
	ws = strings.TrimSpace(ws)
	if ws == "" {
		return false
	}
	within := func(p string) bool {
		p = filepath.Clean(p)
		return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
	}
	if within(ws) {
		return true
	}
	real, err := filepath.EvalSymlinks(ws)
	return err == nil && within(real)
}
