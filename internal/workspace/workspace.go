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
	StandingKind     Kind = "standing"
	ArtifactKind     Kind = "artifact"
)

var (
	ErrInvalid  = errors.New("invalid collection or reference")
	ErrNotFound = errors.New("collection not found")
	ErrCycle    = errors.New("collection membership would form a cycle")
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

// Validate checks the reference shape without asking an execution owner to
// open it. A temporarily unavailable record must remain discoverable here.
func (r Ref) Validate() error {
	if !validText(r.ID, 4096) {
		return fmt.Errorf("%w: record id is empty, too long or contains control characters", ErrInvalid)
	}
	if r.Kind != TaskKind && r.SessionID != "" {
		return fmt.Errorf("%w: only a task reference has a session id", ErrInvalid)
	}
	switch r.Kind {
	case CollectionKind, ConversationKind, StandingKind:
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
