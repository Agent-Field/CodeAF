// Package workspace retains logical organization independently of the records
// it groups. A membership neither moves a conversation nor grants control of it.
package workspace

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Kind string

const (
	CollectionKind   Kind = "collection"
	ConversationKind Kind = "conversation"
	TaskKind         Kind = "task"
	ArtifactKind     Kind = "artifact"
	// CrewKind names a crew by its id; ProcessKind names a process a
	// conversation started, by its id, and like a task carries that
	// conversation as its session. Both arrived with schema 2.
	CrewKind    Kind = "crew"
	ProcessKind Kind = "process"
	// StandingKind is an older build's reference to a standing item. Standing
	// items are gone (automations replaced them), so the collections command
	// refuses a new one, but a row that already names one is still a row: it
	// can be found and taken out, and it resolves to nothing. The kind stays in
	// the schema's own list for the same reason: schema 2 grew that list and
	// kept every word schema 1 had, so no migrated row is refused by it.
	StandingKind Kind = "standing"
)

var (
	ErrInvalid  = errors.New("invalid collection or reference")
	ErrNotFound = errors.New("collection not found")
	ErrCycle    = errors.New("collection membership would form a cycle")
	// ErrNameTaken is a name another collection already has, compared without
	// regard to case: two collections a person cannot tell apart by name are
	// one too many.
	ErrNameTaken = errors.New("another collection already has that name")
	// ErrBusy is a lock this store waited busyTimeout for and did not get. It is
	// not corruption and not a refusal: another command is still writing, so
	// this command left the collections database unchanged and can be retried.
	ErrBusy = errors.New("the collections database is busy being written by something else")
)

// Ref preserves the address used by the existing record owner. TASK NUMBERS
// ARE LOCAL TO A CONVERSATION, so their session is part of their identity.
// Artifact IDs are absolute paths, not claims of permanent content identity.
type Ref struct {
	Kind      Kind   `json:"kind"`
	ID        string `json:"id"`
	SessionID string `json:"session_id,omitempty"`
}

type Collection struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Member is a reference as one collection holds it, with when it was added in
// unix milliseconds. Zero is unknown: a row a schema-1 store wrote.
type Member struct {
	Ref
	AddedAt int64 `json:"added_at,omitempty"`
}

// key is a reference's identity, the three columns the uniqueness rule reads.
func (r Ref) key() string { return string(r.Kind) + "\x00" + r.ID + "\x00" + r.SessionID }

// Validate checks the reference shape without asking an execution owner to
// open it. A temporarily unavailable record must remain discoverable here.
func (r Ref) Validate() error {
	if !validText(r.ID, 4096) {
		return fmt.Errorf("%w: record id is empty, too long or contains control characters", ErrInvalid)
	}
	if r.Kind != TaskKind && r.Kind != ProcessKind && r.SessionID != "" {
		return fmt.Errorf("%w: only a task or a process reference has a session id", ErrInvalid)
	}
	switch r.Kind {
	case CollectionKind, ConversationKind, StandingKind, CrewKind:
	case ProcessKind:
		if !validText(r.SessionID, 1024) {
			return fmt.Errorf("%w: a process needs the session id of the conversation that started it", ErrInvalid)
		}
	case TaskKind:
		n, err := strconv.ParseUint(r.ID, 10, 64)
		if err != nil || n == 0 || strconv.FormatUint(n, 10) != r.ID || !validText(r.SessionID, 1024) {
			return fmt.Errorf("%w: a task needs its session id and a positive canonical task number", ErrInvalid)
		}
	case ArtifactKind:
		if !filepath.IsAbs(r.ID) || filepath.Clean(r.ID) != r.ID {
			return fmt.Errorf("%w: an artifact reference needs a clean absolute path", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unknown reference kind %q", ErrInvalid, r.Kind)
	}
	return nil
}

func validText(s string, limit int) bool {
	return s != "" && len(s) <= limit && utf8.ValidString(s) && strings.TrimSpace(s) == s &&
		strings.IndexFunc(s, unicode.IsControl) < 0
}

// ValidateName lets command adapters reject malformed requests before opening
// durable storage. Create and Rename enforce the same rule at the write boundary.
func ValidateName(name string) error {
	if !validText(name, 256) {
		return fmt.Errorf("%w: name must be 1–256 bytes without surrounding whitespace or control characters", ErrInvalid)
	}
	return nil
}
