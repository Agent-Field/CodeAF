// Package council is the record of a discussion between two places.
//
// A council is not a new kind of conversation. It is an ordinary session whose
// title is the two place names ("Marketing with Software"), filed in both
// places by the AI. This package remembers that session, the caps on it, and
// the cooldown that stops the same pair from opening the same topic again
// inside an hour. It does not run the turns.
package council

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// The caps are the design's (Decisions, councils): six turns and $0.25, then
// the discussion escalates. They are copied onto every record so a reader of
// the file and a later runner share one figure.
const (
	SchemaVersion = 1
	TurnCap       = 6
	SpendCapUSD   = 0.25
	// Cooldown is how long the same two places must wait before opening the
	// same topic again. It is measured from when the discussion opened.
	Cooldown = time.Hour
	// MaxTopicRunes bounds a topic key. A question is short; this only refuses
	// a runaway caller or a damaged file.
	MaxTopicRunes = 2000
)

// State is where a discussion is. The words are the design's.
type State string

const (
	StateRunning   State = "running"
	StateDecided   State = "decided"
	StateEscalated State = "escalated"
	StatePaused    State = "paused"
)

// Valid reports whether s is one of the four states.
func (s State) Valid() bool {
	switch s {
	case StateRunning, StateDecided, StateEscalated, StatePaused:
		return true
	}
	return false
}

// Ended reports whether the discussion has left the running pair of states.
// A decided or escalated council does not go back.
func (s State) Ended() bool {
	return s == StateDecided || s == StateEscalated
}

// Council is one discussion. Places are the two place ids in the order the
// caller gave them, which is also the order of the chat's title. The cooldown
// index treats the pair as unordered.
type Council struct {
	ID      string    `json:"id"`
	Places  [2]string `json:"places"`
	Topic   string    `json:"topic"`
	ChatID  string    `json:"chatId"`
	Label   string    `json:"label"`
	Turns   int       `json:"turns"`
	Cap     int       `json:"cap"`
	Spend   float64   `json:"spend"`
	CapUSD  float64   `json:"capUSD"`
	State   State     `json:"state"`
	Outcome string    `json:"outcome,omitempty"`
	// OpenedAt starts the cooldown. ClosedAt is set when the discussion ends
	// and is absent while it is running or paused.
	OpenedAt time.Time `json:"openedAt"`
	ClosedAt time.Time `json:"closedAt,omitzero"`
}

// TopicKey is the cooldown's topic: trimmed, internal whitespace collapsed,
// and lowercased. Two questions that differ only by case or spacing are one
// topic. The stored topic is this key.
func TopicKey(topic string) (string, error) {
	if !utf8.ValidString(topic) {
		return "", fmt.Errorf("%w: topic is not text", ErrInvalid)
	}
	for _, r := range topic {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return "", fmt.Errorf("%w: topic has control characters", ErrInvalid)
		}
	}
	key := strings.ToLower(strings.Join(strings.Fields(topic), " "))
	switch {
	case key == "":
		return "", fmt.Errorf("%w: empty topic", ErrInvalid)
	case utf8.RuneCountInString(key) > MaxTopicRunes:
		return "", fmt.Errorf("%w: topic is too long", ErrInvalid)
	}
	return key, nil
}

// Label is the chat's title: the two place names in the order given.
func Label(nameA, nameB string) string {
	return nameA + " with " + nameB
}

// SamePair reports whether the two place ids are the same unordered pair.
func SamePair(places [2]string, a, b string) bool {
	x, y := orderedPair(places[0], places[1])
	u, v := orderedPair(a, b)
	return x == u && y == v
}

func orderedPair(a, b string) (string, string) {
	if a > b {
		return b, a
	}
	return a, b
}

func (c Council) validate() error {
	if err := validCouncilID(c.ID); err != nil {
		return err
	}
	if c.Places[0] == "" || c.Places[1] == "" || c.Places[0] == c.Places[1] {
		return fmt.Errorf("%w: a council needs two places", ErrInvalid)
	}
	key, err := TopicKey(c.Topic)
	if err != nil {
		return err
	}
	if c.Topic != key {
		return fmt.Errorf("%w: topic is not a normalised key", ErrInvalid)
	}
	if !validSessionID(c.ChatID) {
		return fmt.Errorf("%w: bad chat id", ErrInvalid)
	}
	if strings.TrimSpace(c.Label) == "" {
		return fmt.Errorf("%w: empty label", ErrInvalid)
	}
	if c.Cap != TurnCap || c.CapUSD != SpendCapUSD {
		return fmt.Errorf("%w: caps are %d turns and $%.2f", ErrInvalid, TurnCap, SpendCapUSD)
	}
	if c.Turns < 0 || c.Turns > TurnCap {
		return fmt.Errorf("%w: turns", ErrInvalid)
	}
	if math.IsNaN(c.Spend) || math.IsInf(c.Spend, 0) || c.Spend < 0 {
		return fmt.Errorf("%w: spend", ErrInvalid)
	}
	if !c.State.Valid() {
		return fmt.Errorf("%w: state %q", ErrInvalid, c.State)
	}
	if c.OpenedAt.IsZero() {
		return fmt.Errorf("%w: missing openedAt", ErrInvalid)
	}
	if c.State.Ended() != !c.ClosedAt.IsZero() {
		return fmt.Errorf("%w: closedAt does not match the state", ErrInvalid)
	}
	if !c.State.Ended() && c.Outcome != "" {
		return fmt.Errorf("%w: an open discussion has no outcome", ErrInvalid)
	}
	return nil
}

func validCouncilID(id string) error {
	switch {
	case id == "":
		return fmt.Errorf("%w: empty id", ErrInvalid)
	case len(id) > 64 || strings.Contains(id, "..") || strings.ContainsAny(id, `/\`):
		return fmt.Errorf("%w: bad id", ErrInvalid)
	case !utf8.ValidString(id):
		return fmt.Errorf("%w: id is not text", ErrInvalid)
	}
	return nil
}

// validSessionID is the session folder's name: 16 hex characters, the id
// [session.NewSessionID] mints and the id a membership files.
func validSessionID(id string) bool {
	if len(id) != 16 {
		return false
	}
	for _, r := range id {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
