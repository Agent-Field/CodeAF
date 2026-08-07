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
	receipt := fmt.Sprintf("paused — %d %s held", held, plural(held, "node", "nodes"))
	if running > 0 {
		receipt += fmt.Sprintf(", %d running %s will hold after the current turn",
			running, plural(running, "leaf", "leaves"))
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
		receipt: fmt.Sprintf("resumed — %d %s returned to the claim queue", resumed, plural(resumed, "node", "nodes")),
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
		receipt: "reprioritized — " + surgeryLabel(node) + " will be claimed before its pending siblings",
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
	provenance := store.Provenance{
		Origin: store.OriginUser, SessionID: command.SessionID, Intent: intent,
		RetryOf: predecessor.ID, Attachments: append([]string(nil), predecessor.Provenance.Attachments...),
	}
	if err := r.store.Splice(parent, subtree, provenance); err != nil {
		root, found, readErr := r.store.Node(remap[command.Target])
		if readErr != nil || !found || root.Provenance.RetryOf != predecessor.ID {
			return commandOutcome{}, err
		}
	}
	return commandOutcome{
		status:  store.CommandApplied,
		result:  fmt.Sprintf("respliced %d retry nodes after %s", len(subtree.Nodes), predecessor.ID),
		receipt: "restarted — fresh work linked to " + surgeryLabel(predecessor),
	}, nil
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
