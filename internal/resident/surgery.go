package resident

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func (r *Reconciler) amend(command store.Command) (commandOutcome, error) {
	node, found, err := r.store.Node(command.Target)
	if err != nil {
		return commandOutcome{}, err
	}
	if !found {
		return commandOutcome{}, fmt.Errorf("target %q no longer exists", command.Target)
	}
	active, err := r.store.AttachAmendment(command.Target, command.SessionID, command.Instruction)
	if err != nil {
		return commandOutcome{}, err
	}
	receipt := "amended — " + surgeryLabel(node) + " now also carries: " + clipLabel(firstLine(command.Instruction), 120)
	if active {
		receipt += " (applies at the next turn)"
	}
	return commandOutcome{status: store.CommandApplied, result: "amendment attached", receipt: receipt}, nil
}

func (r *Reconciler) pause(command store.Command) (commandOutcome, error) {
	nodes, err := r.store.Nodes()
	if err != nil {
		return commandOutcome{}, err
	}
	targets, ok := descendants(nodes, command.Target)
	if !ok {
		return commandOutcome{}, fmt.Errorf("target %q no longer exists", command.Target)
	}
	held, running := 0, 0
	for _, id := range targets {
		node, found, err := r.store.Node(id)
		if err != nil {
			return commandOutcome{}, err
		}
		if !found || terminal(node.Status) || node.Held {
			continue
		}
		if err := r.store.SetNodeHold(id, true, command.Instruction); err != nil {
			return commandOutcome{}, err
		}
		held++
		if node.Status == store.Claimed || node.Status == store.Running {
			running++
		}
	}
	receipt := fmt.Sprintf("paused — %d %s held", held, plural(held, "step", "steps"))
	if running > 0 {
		receipt += fmt.Sprintf(", %d running %s will hold after the current turn",
			running, plural(running, "step", "steps"))
	}
	return commandOutcome{
		status: store.CommandApplied, result: fmt.Sprintf("held %d nodes", held), receipt: receipt,
	}, nil
}

func (r *Reconciler) resume(command store.Command) (commandOutcome, error) {
	nodes, err := r.store.Nodes()
	if err != nil {
		return commandOutcome{}, err
	}
	targets, ok := descendants(nodes, command.Target)
	if !ok {
		return commandOutcome{}, fmt.Errorf("target %q no longer exists", command.Target)
	}
	resumed := 0
	for _, id := range targets {
		node, found, err := r.store.Node(id)
		if err != nil {
			return commandOutcome{}, err
		}
		if !found || terminal(node.Status) || !node.Held {
			continue
		}
		if err := r.store.SetNodeHold(id, false, command.Instruction); err != nil {
			return commandOutcome{}, err
		}
		resumed++
	}
	return commandOutcome{
		status: store.CommandApplied, result: fmt.Sprintf("resumed %d nodes", resumed),
		receipt: fmt.Sprintf("resumed — %d %s back in the queue", resumed, plural(resumed, "step", "steps")),
	}, nil
}

func (r *Reconciler) reprioritize(command store.Command) (commandOutcome, error) {
	node, found, err := r.store.Node(command.Target)
	if err != nil {
		return commandOutcome{}, err
	}
	if !found {
		return commandOutcome{}, fmt.Errorf("target %q no longer exists", command.Target)
	}
	priority, err := r.store.NextSiblingPriority(command.Target)
	if err != nil {
		return commandOutcome{}, err
	}
	if err := r.store.SetNodePriority(command.Target, priority, command.Instruction); err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{
		status: store.CommandApplied, result: fmt.Sprintf("priority set to %d", priority),
		receipt: "moved up — " + surgeryLabel(node) + " goes next, ahead of the rest of what is queued",
	}, nil
}

func (r *Reconciler) restart(command store.Command) (commandOutcome, error) {
	snapshot, err := r.store.Snapshot()
	if err != nil {
		return commandOutcome{}, err
	}
	byID := make(map[string]store.Node, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		byID[node.ID] = node
	}
	predecessor, ok := byID[command.Target]
	if !ok {
		return commandOutcome{}, fmt.Errorf("target %q no longer exists", command.Target)
	}
	ids, ok := descendants(snapshot.Nodes, command.Target)
	if !ok {
		return commandOutcome{}, fmt.Errorf("target %q no longer exists", command.Target)
	}
	selected := make(map[string]bool, len(ids))
	remap := make(map[string]string, len(ids))
	for index, id := range ids {
		selected[id] = true
		remap[id] = fmt.Sprintf("retry-%d-%d", command.Seq, index+1)
	}
	needs := make(map[string][]store.Need)
	for _, edge := range snapshot.Edges {
		if !selected[edge.To] {
			continue
		}
		from := edge.From
		if selected[from] {
			from = remap[from]
		}
		needs[edge.To] = append(needs[edge.To], store.Need{NodeID: from, Kind: edge.Kind})
	}
	// Every retried node feeds from the attempt it replaces. Without this a
	// restart was a clean slate in the worst sense: a fresh worker in a fresh
	// directory with no way to read the partial its predecessor had already
	// written and paid for, retyping from the brief while the half-finished
	// file sat on disk beside it. The dependency digest is the channel that
	// already exists for exactly this — a settled node's words and its files
	// handed to whoever comes next.
	//
	// Only a settled predecessor may be wired. A node still pending would never
	// settle, and a hard dependency on it is a retry that can never be claimed.
	for _, id := range ids {
		if node := byID[id]; terminal(node.Status) {
			needs[id] = append(needs[id], store.Need{NodeID: id, Kind: store.FeedsInto})
		}
	}
	subtree := store.Subtree{Nodes: make([]store.NodeSpec, 0, len(ids))}
	for _, id := range ids {
		node := byID[id]
		parent := ""
		if id != command.Target {
			parent = remap[node.Parent]
		}
		subtree.Nodes = append(subtree.Nodes, store.NodeSpec{
			ID: remap[id], Parent: parent, Brief: node.Brief, Title: node.Title,
			Group: node.Group, Stage: node.Stage, Needs: needs[id],
		})
	}
	parent := predecessor.Parent
	if parent == "" || parent == command.Target {
		parent = store.RootID
	}
	if candidate, exists := byID[parent]; !exists || terminal(candidate.Status) || candidate.Folded {
		parent = store.RootID
	}
	intent := strings.TrimSpace(predecessor.Provenance.Intent)
	if intent == "" {
		intent = predecessor.Brief
	}
	// A retry inherits what its predecessor was, not only what it was asked.
	// The model the user pinned, the workflow it was compiled from, the belief
	// it was a trial of, the service it was meant to stand up: all of it used
	// to be dropped here, so a user who pinned a strong model, watched it fail
	// and said "try again" got the cheap one. Attachments were already carried;
	// these are the rest of the same idea.
	provenance := store.Provenance{
		Origin: store.OriginUser, SessionID: command.SessionID, Intent: intent,
		RetryOf:       predecessor.ID,
		WorkModel:     predecessor.Provenance.WorkModel,
		Craft:         predecessor.Provenance.Craft,
		TrialOf:       predecessor.Provenance.TrialOf,
		ServiceIntent: predecessor.Provenance.ServiceIntent,
		Attachments:   append([]string(nil), predecessor.Provenance.Attachments...),
	}
	// "Rerun that with the better model" used to become a restart on whatever
	// the default happened to be: the model words were recognized, journaled on
	// the command, and then dropped on the floor here. The head states the
	// request in its receipt; this states the outcome, which is the half only
	// the catalog can answer.
	model, note := r.restartWorkModel(command.Instruction)
	provenance.WorkModel = model
	if model == "" {
		provenance.WorkModel = strings.TrimSpace(predecessor.Provenance.WorkModel)
	}
	if err := r.store.Splice(parent, subtree, provenance); err != nil {
		root, found, readErr := r.store.Node(remap[command.Target])
		if readErr != nil || !found || root.Provenance.RetryOf != predecessor.ID {
			return commandOutcome{}, err
		}
	}
	receipt := "restarted — fresh work linked to " + surgeryLabel(predecessor)
	if note != "" {
		receipt += " · " + note
	}
	return commandOutcome{
		status:  store.CommandApplied,
		result:  fmt.Sprintf("respliced %d retry nodes after %s", len(subtree.Nodes), predecessor.ID),
		receipt: receipt,
	}, nil
}

// RestartModelMarker is the head's reading of the model words on a restart,
// written out here for the same reason CorrectionMarker is: it is a wire form
// between two halves of the system and the dependency between them runs one
// way. RestartBoostModel is what the marker carries when the ask named the
// boost slot rather than a model.
const (
	RestartModelMarker = "Run this restart on:"
	RestartBoostModel  = "the boost model"
)

// ModelResolveFunc answers whether the catalog has the model a restart named.
// It is a seam rather than a lookup because the catalog is the surface's, and
// the resident holds no opinion about which provider exists this week. Without
// it every restart runs on the default and says so.
type ModelResolveFunc func(names []string, boost bool) (string, bool)

// WithModelResolver installs the surface's catalog-backed reading of the model
// words a restart carries.
func (r *Reconciler) WithModelResolver(resolve ModelResolveFunc) *Reconciler {
	r.resolveModel = resolve
	return r
}

// restartWorkModel reads the model a restart asked for and says what became of
// the request. An unresolvable name is worth one calm clause rather than
// silence: the user asked for something specific and got something else, and a
// restart that quietly runs on the default is the failure this whole path was
// added to end.
func (r *Reconciler) restartWorkModel(instruction string) (string, string) {
	index := strings.LastIndex(instruction, RestartModelMarker)
	if index < 0 {
		return "", ""
	}
	choice := strings.TrimSpace(firstLine(instruction[index+len(RestartModelMarker):]))
	if choice == "" {
		return "", ""
	}
	boost := choice == RestartBoostModel
	var names []string
	if !boost {
		names = []string{choice}
	}
	if r.resolveModel == nil {
		return "", "on the usual model — I can't reach the catalog from here"
	}
	model, ok := r.resolveModel(names, boost)
	if !ok || strings.TrimSpace(model) == "" {
		return "", "on the usual model — I don't have " + clipLabel(choice, 60)
	}
	return strings.TrimSpace(model), "on " + strings.TrimSpace(model)
}

func surgeryLabel(node store.Node) string {
	if title := strings.TrimSpace(node.Title); title != "" {
		return firstLine(title)
	}
	if brief := firstLine(node.Brief); brief != "" {
		return brief
	}
	return node.ID
}
