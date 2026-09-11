package session

import "strings"

// A task finishing on a branch is not proof that its changes reached the
// destination the person asked for. Delivery is part of the original frozen
// acceptance; worker text and later model claims cannot relax it.
type deliveryContract struct {
	Kind  string `json:"kind,omitempty"`
	Quote string `json:"quote,omitempty"`
}

func validDelivery(ask string, d deliveryContract) deliveryContract {
	d.Kind, d.Quote = strings.TrimSpace(d.Kind), strings.TrimSpace(d.Quote)
	switch d.Kind {
	case "workspace":
		return deliveryContract{Kind: "workspace"}
	case "branch", "report":
		if d.Quote != "" && strings.Contains(ask, d.Quote) {
			return d
		}
	}
	return deliveryContract{}
}

func routeDelivery(v routeVerdict, ask string) deliveryContract {
	if ask == "" || v.checksRequest != ask {
		return deliveryContract{}
	}
	return validDelivery(ask, v.Delivery)
}

func (d deliveryContract) acceptsRetained() bool {
	return d.Kind == "branch" && d.Quote != ""
}

func (s *Steward) declaredDelivery() deliveryContract {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.delivery
}

// Only this session's own delivery is compared with its workspace. A child
// merges into its parent task, not directly into the person's checkout. A
// retained parent still carries that missing delivery even if its children
// report successful merges. Shared-workspace and report-only tasks have no
// kept changed branch and retain their existing completion behavior.
func (a *Agent) retainedDelivery(n *TaskNode, changed []string, branch, merge string) string {
	if merge != mergeKept || len(changed) == 0 || branch == "" {
		return ""
	}
	if n.parent != a.config.taskID {
		return ""
	}
	root := a.deliverableTree()
	if root != "" {
		// Re-read actual content so an explicit later integration can close the
		// delivery gap without rewriting the task's historical landing receipt.
		args := []string{"diff", "--quiet", "refs/heads/" + branch, "--"}
		for _, path := range changed {
			args = append(args, ":(literal)"+path)
		}
		if _, err := git(root, args...); err == nil {
			return ""
		}
	}
	return branch
}

// Empty receipts stay absent; historical declarations are evidence, not new
// authority when a later session is reopened for a different ask.
func (s *Steward) deliveryReceipt() *deliveryContract {
	d := s.declaredDelivery()
	if d.Kind == "" {
		return nil
	}
	return &d
}

// Missing landing metadata cannot stand in for delivery. Shared-workspace
// changes and an explicitly reconciled branch are separate positive facts.
func (l Landing) needsDelivery() bool {
	return !l.Elsewhere && l.State == TaskDone && len(l.Files) > 0 && !l.Merged && !l.InPlace && !l.Delivered
}

func (n *TaskNode) producedResult() bool {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return strings.TrimSpace(n.resultLocked().text) != ""
}
