package placegraph

import (
	"fmt"
	"strings"
	"time"
)

// WriteKnowledge edits or adds the person's words and their explicit replacement
// in one receipt. An edit has its own date so creation and provenance survive,
// while the conflict rule still treats the edit as newer information.
func (s *Store) WriteKnowledge(placeID, id, text, supersedes string) (Line, *AskOnce, Receipt, error) {
	var line Line
	var ask *AskOnce
	rc, err := s.mutate(func(st *State, now time.Time) (*change, error) {
		p, err := st.needPlace(placeID)
		if err != nil {
			return nil, err
		}
		if p.Archived {
			return nil, ErrArchived
		}
		text = strings.TrimSpace(text)
		if text == "" {
			return nil, fmt.Errorf("%w: line text", ErrInvalid)
		}
		action := ActionLineAdd
		if id == "" {
			line = Line{ID: s.opts.NewID("ln_"), PlaceID: placeID, Text: text, Source: LineSource{Kind: LineYouWrote}, CreatedAt: now}
		} else {
			old := liveLine(st, placeID, id)
			if old == nil {
				return nil, ErrNotFound
			}
			line = *old
			if line.Text == text && supersedes == "" {
				return nil, nil
			}
			line.Text, line.EditedAt = text, now
			action = ActionLineUpdate
		}
		if supersedes != "" {
			older := liveLine(st, placeID, supersedes)
			if older == nil || supersedes == line.ID {
				return nil, fmt.Errorf("%w: replacement line", ErrInvalid)
			}
			struck, conflict := Supersede(*older, line, now)
			ask = conflict
			if conflict == nil {
				*older = struck
			}
		}
		if id == "" {
			st.Lines = append(st.Lines, line)
		} else {
			*st.findLine(id) = line
		}
		return &change{action, line.ID}, nil
	})
	return line, ask, rc, err
}

// ConfirmKnowledge records a real confirmation under the same file lock as
// edits, so a stale window cannot overwrite the latest words while saying yes.
func (s *Store) ConfirmKnowledge(placeID, id string) (Line, Receipt, error) {
	var line Line
	rc, err := s.mutate(func(st *State, now time.Time) (*change, error) {
		p, err := st.needPlace(placeID)
		if err != nil {
			return nil, err
		}
		if p.Archived {
			return nil, ErrArchived
		}
		old := liveLine(st, placeID, id)
		if old == nil {
			return nil, ErrNotFound
		}
		old.LastUsedAt, old.AskedStillTrueAt = now, time.Time{}
		line = *old
		return &change{ActionLineUpdate, id}, nil
	})
	return line, rc, err
}
