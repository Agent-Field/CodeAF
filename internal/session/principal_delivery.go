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
		if _, err := git(root, args...); err == nil && anythingToCompare(root, branch, changed) {
			return ""
		}
	}
	return branch
}

// Git diff --quiet cannot tell identical content from pathspecs that matched
// nothing. THE CHANGED LIST IS A CLAIM, NOT A RECEIPT: TaskNode.finish records
// it without reconciliation, and taskTree.comeHome discards the paths that
// commitTaskWork actually saved. Work reported as still on a branch costs a
// person one look; work reported as delivered when it is not costs them the
// work, so an unreadable or empty comparison stays retained.
//
// The two commands read the two sides the diff itself compares — what the kept
// branch carries, and what the workspace tracks — and both take the pathspecs
// the diff took, so all three readings agree on what a path means.
func anythingToCompare(root, branch string, changed []string) bool {
	branchArgs := []string{"ls-tree", "-r", "-z", "--name-only", "refs/heads/" + branch, "--"}
	workspaceArgs := []string{"ls-files", "-z", "--"}
	wanted := make(map[string]bool, len(changed))
	for _, path := range changed {
		pathspec := ":(literal)" + path
		branchArgs = append(branchArgs, pathspec)
		workspaceArgs = append(workspaceArgs, pathspec)
		wanted[path] = true
	}
	branchNames, branchErr := git(root, branchArgs...)
	if namesOne(wanted, branchNames, branchErr) {
		return true
	}
	workspaceNames, workspaceErr := git(root, workspaceArgs...)
	return namesOne(wanted, workspaceNames, workspaceErr)
}

// namesOne reports whether one of git's NUL-separated listings actually holds a
// path that was asked about.
//
// IT MATCHES A NAME AND NOT MERE OUTPUT, because [git] hands back what the
// command wrote to BOTH streams: a warning on stderr would otherwise read as a
// file that exists, which is the exact reading this guard was written to refuse.
// A command that failed at all answers no — a repository that cannot be read is
// not a repository that has been shown to hold the work.
func namesOne(wanted map[string]bool, out string, err error) bool {
	if err != nil {
		return false
	}
	for _, name := range gitNULPaths(out) {
		if wanted[name] {
			return true
		}
	}
	return false
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
