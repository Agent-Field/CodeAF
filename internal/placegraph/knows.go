package placegraph

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// LineSourceKind preserves how a place learned a line, rather than inferring it
// from the wording when a person edits the text.
type LineSourceKind string

const (
	LineYouWrote   LineSourceKind = "you-wrote"
	LineSaidInChat LineSourceKind = "said-in-chat"
	LineLearned    LineSourceKind = "learned"
	LineFile       LineSourceKind = "file"
)

// LineSource retains the evidence a caller can use to explain a knowledge line.
type LineSource struct {
	Kind    LineSourceKind `json:"kind"`
	ChatID  string         `json:"chatId,omitempty"`
	At      time.Time      `json:"at,omitzero"`
	Answers int            `json:"answers,omitempty"`
	Path    string         `json:"path,omitempty"`
}

// Line is one editable fact. Replacement and use timestamps are stored as facts;
// this storage layer never guesses contradictions or whether a fact is stale.
type Line struct {
	ID               string     `json:"id"`
	PlaceID          string     `json:"placeId"`
	Text             string     `json:"text"`
	Source           LineSource `json:"source"`
	CreatedAt        time.Time  `json:"createdAt,omitzero"`
	EditedAt         time.Time  `json:"editedAt,omitzero"`
	LastUsedAt       time.Time  `json:"lastUsedAt,omitzero"`
	ReplacedBy       string     `json:"replacedBy,omitempty"`
	ReplacedAt       time.Time  `json:"replacedAt,omitzero"`
	AskedStillTrueAt time.Time  `json:"askedStillTrueAt,omitzero"`
}

const (
	ActionLineAdd    Action = "line.add"
	ActionLineUpdate Action = "line.update"
	ActionLineDelete Action = "line.delete"
)

// Knowledge returns independent values in stored order, including replaced lines
// so a surface can explain what changed without inventing historical text.
func (s *Snapshot) Knowledge(placeID string) []Line {
	var out []Line
	for _, l := range s.Lines {
		if l.PlaceID == placeID {
			out = append(out, l)
		}
	}
	return out
}

func (st *State) findLine(id string) *Line {
	for i := range st.Lines {
		if st.Lines[i].ID == id {
			return &st.Lines[i]
		}
	}
	return nil
}

func validateLines(st *State) error {
	seen := map[string]bool{}
	places := map[string]bool{}
	byID := map[string]Line{}
	for _, p := range st.Places {
		places[p.ID] = true
	}
	for _, l := range st.Lines {
		byID[l.ID] = l
	}
	for _, l := range st.Lines {
		if err := validID(l.ID); err != nil {
			return err
		}
		if seen[l.ID] {
			return fmt.Errorf("%w: duplicate line id %s", ErrInvalid, l.ID)
		}
		seen[l.ID] = true
		if !places[l.PlaceID] {
			return fmt.Errorf("%w: line place %s", ErrNotFound, l.PlaceID)
		}
		if strings.TrimSpace(l.Text) == "" || !utf8.ValidString(l.Text) || strings.ContainsRune(l.Text, '\x00') {
			return fmt.Errorf("%w: line text", ErrInvalid)
		}
		if len(l.Text) > MaxInstructions {
			return fmt.Errorf("%w: line text", ErrTooLarge)
		}
		switch l.Source.Kind {
		case LineYouWrote, LineSaidInChat, LineLearned, LineFile:
		default:
			return fmt.Errorf("%w: line source kind", ErrInvalid)
		}
		if l.Source.ChatID != "" {
			if err := validChatID(l.Source.ChatID); err != nil {
				return err
			}
		}
		if l.Source.Answers < 0 || len(l.Source.Path) > MaxSourceRefBytes || !utf8.ValidString(l.Source.Path) || hasControl(l.Source.Path) {
			return fmt.Errorf("%w: line source", ErrInvalid)
		}
		if l.Source.Kind == LineFile && strings.TrimSpace(l.Source.Path) == "" {
			return fmt.Errorf("%w: file line path", ErrInvalid)
		}
		if (l.ReplacedBy == "") != l.ReplacedAt.IsZero() {
			return fmt.Errorf("%w: line replacement timestamp", ErrInvalid)
		}
		if l.ReplacedBy != "" {
			next, ok := byID[l.ReplacedBy]
			if !ok || next.PlaceID != l.PlaceID || next.ID == l.ID {
				return fmt.Errorf("%w: line replacement", ErrInvalid)
			}
		}
	}
	// Completed chains are memoized so a large imported list stays linear.
	completed := map[string]bool{}
	for _, l := range st.Lines {
		path := map[string]bool{}
		id := l.ID
		for id != "" && !completed[id] {
			if path[id] {
				return fmt.Errorf("%w: cyclic line replacement", ErrInvalid)
			}
			path[id] = true
			id = byID[id].ReplacedBy
		}
		for id := range path {
			completed[id] = true
		}
	}
	return nil
}
