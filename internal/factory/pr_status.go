package factory

import "strings"

// CLA is an external prerequisite. It remains a merge gate, but asking an
// agent to modify code cannot sign an agreement for the person.
func ExternalPRCheck(c CheckRun) bool {
	return strings.EqualFold(c.Name, "license/cla") && strings.HasPrefix(c.URL, "https://cla-assistant.io/")
}
func PRHasRepairableCheckFailure(pr *PullRequest) bool {
	if pr == nil {
		return false
	}
	external := 0
	for _, c := range pr.CheckRuns {
		switch c.State {
		case "failure", "failed", "error", "timed_out", "cancelled", "action_required", "startup_failure":
			if !ExternalPRCheck(c) {
				return true
			}
			external++
		}
	}
	return pr.Checks.Failed > external
}
func PRWaitingWords(pr *PullRequest) string {
	if pr == nil || !pr.ChecksRead {
		return "Waiting for GitHub check results."
	}
	for _, c := range pr.CheckRuns {
		if ExternalPRCheck(c) && (c.State == "pending" || c.State == "in_progress" || c.State == "failure") {
			return "Contributor agreement pending on GitHub. Watching review feedback and conflicts; code follow-up continues independently."
		}
	}
	if pr.Checks.Pending > 0 {
		return "Waiting for GitHub checks. Watching review feedback and conflicts."
	}
	if len(pr.CheckRuns) == 0 {
		return "No GitHub checks reported. Watching review feedback and conflicts."
	}
	return "Checks passed. Watching review feedback and conflicts; awaiting your merge."
}
