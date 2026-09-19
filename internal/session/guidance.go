package session

// Folder instructions for the main turn. They are loaded in software, not by
// ranking, and they checkpoint before membership or work commitment so a
// revision or conflict cannot be applied silently (A7/A9 / J13).

import (
	"context"
	"errors"
	"strings"
)

// ErrGuidanceChanged pauses an affected mutation when folder instructions moved
// or conflict during the turn. Unrelated reads and tools continue.
var ErrGuidanceChanged = errors.New("folder instructions changed or conflict; this write was not applied")

// GuidanceItem is one standing instruction that applies to this chat: a parent
// folder, an ancestor, or Root. Same fields as wsapi.GuidanceItem so wsapi does
// not import session.
type GuidanceItem struct {
	ScopeID, Name, Text, Origin, Actor, SourceRef string
	Revision                                      int
}

// GuidanceRev is one scope's revision in a snapshot.
type GuidanceRev struct {
	ScopeID  string
	Revision int
}

// GuidanceSnapshot is what the checkpoint compares: Root's revision and every
// applicable guidance revision, plus the conflict bit.
type GuidanceSnapshot struct {
	RootRevision int
	Guidance     []GuidanceRev
	Conflict     bool
}

// EffectiveGuidance is the instructions the main turn works under. Items are
// parents + ancestors + Root, deduped by ScopeID, Root once. Conflict means
// incompatible instructions; the turn must not silently pick recency or path.
type EffectiveGuidance struct {
	Items    []GuidanceItem
	Snapshot GuidanceSnapshot
	Conflict bool
}

// GuidanceSource loads applicable folder instructions for one conversation.
// Config.Guidance is nil when folders or wsapi are unavailable.
type GuidanceSource interface {
	Effective(ctx context.Context, conversationID string) (EffectiveGuidance, error)
}

// refreshGuidanceLocked puts folder instructions in front of the model. It is
// called with a.mu held, at the start of every turn, beside standing orders:
// a turn must reason with the instructions that hold now. A source that fails
// is treated as no guidance — the turn still runs.
func (a *Agent) refreshGuidanceLocked(ctx context.Context) {
	loaded := a.loadGuidance(ctx)
	a.guidanceSnap = loaded.Snapshot
	a.guidanceSnap.Conflict = loaded.Conflict
	a.guidanceConflict = loaded.Conflict
	a.guidanceText = renderGuidance(loaded)
}

func (a *Agent) loadGuidance(ctx context.Context) EffectiveGuidance {
	if a == nil || a.config.Guidance == nil {
		return EffectiveGuidance{}
	}
	chat := strings.TrimSpace(a.config.Place.ID())
	if chat == "" {
		return EffectiveGuidance{}
	}
	loaded, err := a.config.Guidance.Effective(ctx, chat)
	if err != nil {
		return EffectiveGuidance{}
	}
	loaded.Snapshot.Conflict = loaded.Conflict
	return loaded
}

func renderGuidance(loaded EffectiveGuidance) string {
	if len(loaded.Items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n<guidance>\n")
	b.WriteString("These are standing folder instructions for this chat. They are not retrieved maybe-relevant text.\n")
	if loaded.Conflict {
		b.WriteString("These instructions conflict; do not pick one silently.\n")
	}
	for _, item := range loaded.Items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = item.ScopeID
			if name == "" {
				name = "Root"
			}
		}
		b.WriteString("- ")
		b.WriteString(name)
		b.WriteString(": ")
		b.WriteString(strings.TrimSpace(item.Text))
		b.WriteByte('\n')
	}
	b.WriteString("</guidance>\n")
	return b.String()
}

// pauseAffectedMutation recomputes guidance and pauses only membership or work
// commitment when the snapshot moved or conflicted. Search, list, and read
// never call this.
func (a *Agent) pauseAffectedMutation(ctx context.Context) error {
	if a == nil || a.config.Guidance == nil {
		return nil
	}
	current := a.loadGuidance(ctx)
	a.mu.Lock()
	held := a.guidanceSnap
	a.mu.Unlock()
	if guidanceSnapshotChanged(held, current.Snapshot) {
		return ErrGuidanceChanged
	}
	return nil
}

func guidanceSnapshotChanged(held, current GuidanceSnapshot) bool {
	if held.RootRevision != current.RootRevision || held.Conflict != current.Conflict {
		return true
	}
	if len(held.Guidance) != len(current.Guidance) {
		return true
	}
	want := map[string]int{}
	for _, row := range held.Guidance {
		want[row.ScopeID] = row.Revision
	}
	for _, row := range current.Guidance {
		revision, ok := want[row.ScopeID]
		if !ok || revision != row.Revision {
			return true
		}
	}
	return false
}
