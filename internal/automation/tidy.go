package automation

// tidy.go is the one piece of background work the clock does that nobody
// armed: the pass over what codeaf remembers that merges duplicates and retires
// lines a newer one replaced (internal/session's memory_consolidate.go). It
// rides the clock because it wants exactly what the clock already has — one
// process elected among every window, and a machine where nobody is typing.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Idle answers whether the machine has been quiet for at least the given span:
// nothing working right now, and the newest thing anybody said older than it.
// The session supplies it from its reading of every conversation.
type Idle func(quiet time.Duration) bool

// Tidied is what one pass over what is remembered came to: how many lines were
// merged into one clearer line, how many were retired in favour of one that
// replaced them, and what the single call cost.
type Tidied struct {
	Merged     int
	Superseded int
	USD        float64
}

// Changed is how many remembered lines the pass actually moved. Zero is a pass
// that read fifty lines and decided every one of them was already right, which
// is the ordinary answer.
func (t Tidied) Changed() int { return t.Merged + t.Superseded }

// Line is the pass's own account of a tidy, THE EMPTINESS LAW APPLIED: a part
// that is zero is absent rather than printed as a zero, and a pass that changed
// nothing and spent nothing is no line at all.
func (t Tidied) Line() string {
	parts := make([]string, 0, 3)
	if t.Merged > 0 {
		parts = append(parts, strconv.Itoa(t.Merged)+" merged")
	}
	if t.Superseded > 0 {
		parts = append(parts, strconv.Itoa(t.Superseded)+" superseded")
	}
	if t.USD > 0 {
		parts = append(parts, fmt.Sprintf("$%.3f", t.USD))
	}
	if len(parts) == 0 {
		return ""
	}
	return "consolidated · " + strings.Join(parts, " · ")
}

// Tidy is the pass itself. A NIL Tidy is memory off on this machine — a
// capability that cannot work is absent rather than present and refusing — and
// the pass keeps its own gates (how quiet the machine must be, how long since
// the last pass), so the clock may ask it often and it answers cheaply.
type Tidy func(ctx context.Context) (Tidied, error)
