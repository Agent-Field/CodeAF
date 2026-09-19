// Package wsdiscover is the rebuildable discovery index.
//
// Journals stay authoritative. This store holds derived passages, a lexical
// FTS index, and vectors keyed by embedding model, version, and dimension.
// Cursor identity is session id + source generation + ordinal + content hash —
// never a byte offset — so a rewind or rewrite can invalidate the derived rows
// without mistaking a reused file position for the same words (A22).
package wsdiscover

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/home"
)

// DefaultPath is the one discovery file the product opens. Tests pass their
// own path so they never write the process home.
func DefaultPath() string { return home.Join("v3", "discovery.db") }

const (
	SourcePresent     = "present"
	SourceDeleted     = "deleted"
	SourceUnavailable = "unavailable"

	delayedDetail = "discovery delayed"
)

var (
	ErrInvalid     = errors.New("invalid discovery record")
	ErrNotFound    = errors.New("discovery source not found")
	ErrForeign     = errors.New("this database is not a discovery store")
	ErrUnavailable = errors.New("discovery source is unavailable")
)

// Cursor is the persistent ingest identity. A byte offset is not a field and
// is not stored; the four values below are the whole key.
type Cursor struct {
	SessionID   string
	Generation  int // source generation; rewind/rewrite bumps this
	Ordinal     int64
	ContentHash string // hash of the indexed bytes
}

// Passage is one derived slice of a journal. Vector is empty until embedded.
// Model/Version/Dimension identify the embedding generation so a mismatch
// cannot be compared as if it were the same space.
type Passage struct {
	SessionID   string
	SourceRef   string
	ContentHash string
	Speaker     string
	Text        string
	Generation  int
	Ordinal     int64
	Model       string
	Version     string
	Dimension   int
	Vector      []float32
}

// Record is one journal entry offered for ingest. The store hashes Text to
// form the cursor; the caller names the session, generation, and ordinal.
type Record struct {
	SessionID  string
	SourceRef  string
	Speaker    string
	Text       string
	Generation int
	Ordinal    int64
}

// Embedder turns passage text into vectors. Tests inject a fake. Production
// binds the shipped provider adapter or reports unavailable — never a silent
// zero-vector success, and never a default stub on Open.
type Embedder interface {
	Embed(ctx context.Context, texts []string) (vectors [][]float32, model, version string, dim int, err error)
	Available(ctx context.Context) (model string, ok bool, err error)
}

// IndexProgress is software counters, not a fake percentage. Detail is empty
// when the index is caught up; Delayed uses the person-facing delayed copy.
type IndexProgress struct {
	Passages, Vectors int
	Cursor            Cursor
	Delayed, Degraded bool
	Detail            string
}

func contentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func (r Record) validate() error {
	if strings.TrimSpace(r.SessionID) == "" {
		return fmt.Errorf("%w: session id is empty", ErrInvalid)
	}
	if r.Generation < 1 {
		return fmt.Errorf("%w: generation must be a positive source generation", ErrInvalid)
	}
	if r.Ordinal < 1 {
		return fmt.Errorf("%w: ordinal must be a positive message ordinal, not a byte offset", ErrInvalid)
	}
	if strings.TrimSpace(r.Text) == "" {
		return fmt.Errorf("%w: passage text is empty", ErrInvalid)
	}
	return nil
}

func (r Record) passage() Passage {
	return Passage{
		SessionID:   r.SessionID,
		SourceRef:   r.SourceRef,
		ContentHash: contentHash(r.Text),
		Speaker:     r.Speaker,
		Text:        r.Text,
		Generation:  r.Generation,
		Ordinal:     r.Ordinal,
	}
}

func (c Cursor) Empty() bool {
	return c.SessionID == "" && c.Generation == 0 && c.Ordinal == 0 && c.ContentHash == ""
}
