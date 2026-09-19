package wscollab

// Scope is who a coordination covers. Selected is a snapshot of marked ids
// and does not grow when a sibling is filed elsewhere. Folder is dynamic:
// a descendant filed later is in (A16). The router does not invent recipients
// from a folder walk — wsapi resolves those — but it will not enlarge a
// selected snapshot it was handed.
type Scope struct {
	Kind     string
	FolderID string
	IDs      []string
}

// Contains reports whether id is already in the snapshot or current members.
func (s Scope) Contains(id string) bool {
	for _, have := range s.IDs {
		if have == id {
			return true
		}
	}
	return false
}

// Add returns a scope that includes id. A selected snapshot is returned
// unchanged. A folder scope grows when the id is new.
func (s Scope) Add(id string) Scope {
	if s.Kind != ScopeFolder || id == "" || s.Contains(id) {
		return s.copy()
	}
	out := s.copy()
	out.IDs = append(out.IDs, id)
	return out
}

func (s Scope) copy() Scope {
	ids := make([]string, len(s.IDs))
	copy(ids, s.IDs)
	return Scope{Kind: s.Kind, FolderID: s.FolderID, IDs: ids}
}
