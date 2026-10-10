package placegraph

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

// AddLine uses the graph's atomic write and receipt stack, so remembering a fact
// has the same cross-window conflict protection as filing a chat.
func (s *Store) AddLine(line Line) (Line, Receipt, error) {
	var added Line
	rc, err := s.mutate(func(st *State, now time.Time) (*change, error) {
		p, err := st.needPlace(line.PlaceID)
		if err != nil {
			return nil, err
		}
		if p.Archived {
			return nil, ErrArchived
		}
		if line.ID == "" {
			line.ID = s.opts.NewID("ln_")
		}
		if st.findLine(line.ID) != nil {
			return nil, fmt.Errorf("%w: duplicate line id", ErrInvalid)
		}
		if line.CreatedAt.IsZero() {
			line.CreatedAt = now
		}
		st.Lines = append(st.Lines, line)
		added = line
		return &change{ActionLineAdd, line.ID}, nil
	})
	if err != nil {
		return Line{}, Receipt{}, err
	}
	return added, rc, nil
}

// UpdateLine keeps identity and creation time stable while allowing callers to
// record real use, replacement and still-true questions alongside edited text.
func (s *Store) UpdateLine(line Line) (Receipt, error) {
	return s.mutate(func(st *State, _ time.Time) (*change, error) {
		old := st.findLine(line.ID)
		if old == nil {
			return nil, ErrNotFound
		}
		if line.PlaceID != old.PlaceID || !line.CreatedAt.Equal(old.CreatedAt) {
			return nil, fmt.Errorf("%w: line identity", ErrInvalid)
		}
		if *old == line {
			return nil, nil
		}
		*old = line
		return &change{ActionLineUpdate, line.ID}, nil
	})
}

// DeleteLine clears references to the removed replacement so Undo can restore
// the whole relationship and no surviving line points to a missing fact.
func (s *Store) DeleteLine(id string) (Receipt, error) {
	return s.mutate(func(st *State, _ time.Time) (*change, error) {
		if st.findLine(id) == nil {
			return nil, ErrNotFound
		}
		kept := st.Lines[:0:0]
		for _, l := range st.Lines {
			if l.ID == id {
				continue
			}
			if l.ReplacedBy == id {
				l.ReplacedBy = ""
				l.ReplacedAt = time.Time{}
			}
			kept = append(kept, l)
		}
		st.Lines = kept
		return &change{ActionLineDelete, id}, nil
	})
}

// migrateKnowledge runs under the file lock. Its durable marker prevents old
// instructions restored by a rollback build from being imported a second time.
func (s *Store) migrateKnowledge(st *State) (bool, error) {
	if st.KnowsMigrated {
		return false, nil
	}
	now := s.opts.Now().UTC()
	changed := false
	for i := range st.Places {
		p := &st.Places[i]
		// Blank lines delimit paragraphs; line wraps within one paragraph stay intact.
		var paragraph []string
		flush := func() {
			text := strings.TrimSpace(strings.Join(paragraph, "\n"))
			if text != "" {
				st.Lines = append(st.Lines, Line{ID: s.opts.NewID("ln_"), PlaceID: p.ID, Text: text, Source: LineSource{Kind: LineYouWrote}, CreatedAt: now})
				changed = true
			}
			paragraph = nil
		}
		for _, row := range strings.Split(strings.ReplaceAll(p.Context.Instructions, "\r\n", "\n"), "\n") {
			if strings.TrimSpace(row) == "" {
				flush()
			} else {
				paragraph = append(paragraph, row)
			}
		}
		flush()
		if p.Context.Instructions != "" {
			changed = true
			p.Context.Instructions = ""
		}
		for _, src := range p.Context.Sources {
			if !textKnowledgeSource(src) {
				continue
			}
			text := src.Label
			if strings.TrimSpace(text) == "" {
				text = src.Ref
			}
			st.Lines = append(st.Lines, Line{ID: s.opts.NewID("ln_"), PlaceID: p.ID, Text: text, Source: LineSource{Kind: LineFile, Path: src.Ref, At: src.At}, CreatedAt: now})
			changed = true
		}
	}
	st.KnowsMigrated = true
	if changed {
		st.Revision++
	}
	if err := validateLines(st); err != nil {
		return false, err
	}
	return true, nil
}

// Migration keeps references rather than opening arbitrary files or fetching
// URLs. Only explicit text suffixes qualify; unknown media stay in Sources.
func textKnowledgeSource(src Source) bool {
	ref := src.Ref
	switch src.Kind {
	case SourceFile:
	case SourceURL:
		u, err := url.Parse(ref)
		if err != nil {
			return false
		}
		ref = u.Path
	default:
		return false
	}
	switch strings.ToLower(filepath.Ext(ref)) {
	case ".txt", ".md", ".markdown", ".rst", ".csv", ".json", ".yaml", ".yml", ".toml", ".xml", ".html", ".htm", ".log":
		return true
	}
	return false
}
