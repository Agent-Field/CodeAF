package resident

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The narrator is how parallel work stays legible without becoming noise.
// Progress is milestone-driven, not event-driven: completions accumulate and
// are spoken as one casual line after a debounce, silence longer than a
// heartbeat earns a "still going", failures interrupt immediately (via the
// announcement path), and the deliverable's own landing supersedes anything
// still unspoken. Small single-node jobs stay quiet — their answer arrives
// faster than narration would.

const (
	// narrateDebounce is the minimum quiet period between progress lines for
	// one job. Everything that lands inside it is folded into the next line.
	narrateDebounce = 25 * time.Second
	// narrateHeartbeat is how long a job may run silently before the thread
	// says it is still alive.
	narrateHeartbeat = 2 * time.Minute
	// narratePreviousKept bounds how much of its own prior speech the
	// narrator sees, enough to not repeat itself.
	narratePreviousKept = 4
)

// Narration is everything the narrator may speak from, for one job.
type Narration struct {
	// Goal is the user's verbatim intent for this subtree.
	Goal string
	// Finished are labels of parts landed since the last update, each with a
	// one-line result.
	Finished []string
	// Running are labels of parts in flight, each with elapsed time.
	Running []string
	// Queued is how many parts are admitted but not yet started.
	Queued int
	// Previous are the narrator's own recent lines for this job.
	Previous []string
}

// NarrateFunc turns one Narration into a single casual line for the thread.
// Returning an empty line skips the update without error.
type NarrateFunc func(ctx context.Context, narration Narration) (string, error)

type subtreeProgress struct {
	sessionID string
	finished  []string
	previous  []string
	lastPost  time.Time
}

// WithNarrator installs progress narration and returns the reconciler for
// chaining. A nil narrator (the default) keeps progress out of the thread
// entirely — the graph lens still shows it live.
func (r *Reconciler) WithNarrator(narrate NarrateFunc) *Reconciler {
	r.narrate = narrate
	return r
}

// observeProgress accumulates one settled or started descendant into its
// job's progress state. Top-level nodes are not narrated here: their landing
// is the final answer and their failure already interrupts.
func (r *Reconciler) observeProgress(event store.Event, node store.Node, root store.Node) {
	if r.narrate == nil {
		return
	}
	state := r.ensureProgress(root)
	switch event.Kind {
	case store.EventNodeCompleted:
		label := clipLabel(firstLine(node.Brief), 60)
		if result := firstLine(node.Summary); result != "" {
			label += " — " + clipLabel(result, 90)
		}
		state.finished = append(state.finished, label)
	case store.EventNodeStarted:
		// Existence of state is what arms the heartbeat; the receipt already
		// told the user work was queued, so starting is not itself news.
	}
}

func (r *Reconciler) ensureProgress(root store.Node) *subtreeProgress {
	if r.progress == nil {
		r.progress = make(map[string]*subtreeProgress)
	}
	state, ok := r.progress[root.ID]
	if !ok {
		state = &subtreeProgress{sessionID: root.Provenance.SessionID, lastPost: time.Now()}
		r.progress[root.ID] = state
	}
	return state
}

// speakProgress applies the firing rules and posts at most one line per job.
func (r *Reconciler) speakProgress(ctx context.Context) error {
	if r.narrate == nil || len(r.progress) == 0 {
		return nil
	}
	nodes, err := r.store.ActiveNodes()
	if err != nil {
		return err
	}
	byID := make(map[string]store.Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}

	for rootID, state := range r.progress {
		if err := ctx.Err(); err != nil {
			return err
		}
		root, ok := byID[rootID]
		if !ok || terminalStatus(root.Status) {
			// The final answer (or failure interrupt) owns the ending; any
			// unspoken progress would arrive after the result and read as
			// noise.
			delete(r.progress, rootID)
			continue
		}

		var running []string
		queued := 0
		for _, node := range nodes {
			if node.ID == rootID || !descendsFrom(byID, node, rootID) {
				continue
			}
			switch node.Status {
			case store.Running, store.Claimed:
				label := clipLabel(firstLine(node.Brief), 60)
				// Whole minutes, and nothing at all under one. A second-resolution
				// clock made this label a different string on every heartbeat, so
				// the running block — and everything the model had already been
				// told below it — was re-billed each time for a number the prompt
				// itself says to mention only when it is notable. Under a minute
				// is never notable; after that the minute is the unit a person
				// would say out loud.
				if elapsed := time.Since(node.StartedAt); !node.StartedAt.IsZero() && elapsed >= time.Minute {
					label += fmt.Sprintf(" (%dm in)", int(elapsed.Minutes()))
				}
				running = append(running, label)
			case store.Pending:
				queued++
			}
		}

		quiet := time.Since(state.lastPost)
		fire := (len(state.finished) > 0 && quiet >= narrateDebounce) ||
			(len(running) > 0 && quiet >= narrateHeartbeat)
		if !fire {
			continue
		}

		line, err := r.narrate(ctx, Narration{
			Goal:     root.Provenance.Intent,
			Finished: state.finished,
			Running:  running,
			Queued:   queued,
			Previous: state.previous,
		})
		// A narrator error or empty line skips this update; lastPost still
		// advances so a persistent failure cannot hammer the model.
		state.lastPost = time.Now()
		state.finished = nil
		if err != nil || strings.TrimSpace(line) == "" {
			continue
		}
		line = strings.TrimSpace(line)
		if _, err := r.store.PostMessage(store.Message{
			SessionID: state.sessionID,
			Role:      store.RoleAgent,
			Body:      boundMessage(line),
			NodeID:    rootID,
		}); err != nil {
			return err
		}
		state.previous = append(state.previous, line)
		if len(state.previous) > narratePreviousKept {
			state.previous = state.previous[len(state.previous)-narratePreviousKept:]
		}
	}
	return nil
}

func terminalStatus(status store.Status) bool {
	return status == store.Done || status == store.Failed || status == store.Cancelled
}

// descendsFrom reports whether node sits anywhere under rootID.
func descendsFrom(byID map[string]store.Node, node store.Node, rootID string) bool {
	for node.Parent != "" && node.Parent != store.RootID {
		if node.Parent == rootID {
			return true
		}
		parent, ok := byID[node.Parent]
		if !ok {
			return false
		}
		node = parent
	}
	return false
}

// topLevelAncestor resolves the top-level job a node belongs to. A node
// parented directly on the spine root is its own job.
func topLevelAncestor(byID map[string]store.Node, node store.Node) (store.Node, bool) {
	for {
		if node.Parent == store.RootID {
			return node, true
		}
		parent, ok := byID[node.Parent]
		if !ok {
			return store.Node{}, false
		}
		node = parent
	}
}

// recordForNarration routes one journal event into its job's progress state.
// Top-level nodes are excluded: their landing is the final answer and their
// failure already interrupts the thread.
func (r *Reconciler) recordForNarration(byID map[string]store.Node, event store.Event) {
	if r.narrate == nil || byID == nil {
		return
	}
	node, ok := byID[event.NodeID]
	if !ok {
		return
	}
	if node.Parent == store.RootID || node.Parent == "" {
		return
	}
	root, ok := topLevelAncestor(byID, node)
	if !ok || root.Provenance.SessionID == "" {
		return
	}
	state := r.ensureProgress(root)
	if event.Kind == store.EventNodeFailed {
		// The failure interrupt already spoke; count it as speech so the
		// next progress line does not arrive on its heels.
		state.lastPost = time.Now()
		return
	}
	r.observeProgress(event, node, root)
}
