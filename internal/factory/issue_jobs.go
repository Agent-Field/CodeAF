package factory

import "errors"

// IsIssueJob is the factory's work-item boundary. A blank kind is an old local
// issue. PR objects remain attached to issues for publication and follow-up;
// standalone PR/CI jobs cannot enter the active floor.
func IsIssueJob(it Item) bool { return it.Kind == KindIssue || it.Kind == "" }

func RequireIssueJob(it Item) error {
	if !IsIssueJob(it) {
		return errors.New("factory jobs support issues only; open the related issue instead")
	}
	return nil
}

func IssueJobs(items []Item) []Item {
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if IsIssueJob(it) {
			out = append(out, it)
		}
	}
	return out
}
