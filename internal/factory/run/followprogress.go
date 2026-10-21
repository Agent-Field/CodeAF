package run

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/redact"
)

// Follow PR reports meaningful changes, not tool-by-tool narration or heartbeat
// model calls. Each round can immediately explain its trigger; subsequent model
// updates are at least 30 seconds apart. Service outcomes bypass this throttle.
const followProgressEvery = 30 * time.Second

func followProgressBrief(j Job) string {
	if !factory.IsCIRepairStage(j.Stage) {
		return ""
	}
	return "\nKeep the person informed in brief ordinary assistant messages (one or two sentences): what specific CI failure, review finding or conflict you are investigating; what you found and are changing; when you begin verification; and any meaningful change of approach during a long investigation. Write for the person, not an internal monologue. Give an update as work advances, roughly once a minute during lengthy work; do not repeat unchanged status or narrate tools. These messages are relayed to the job conversation. Distinguish a planned fix, edits in progress, checks actually run, and their observed results. Do not claim the PR was pushed, CI passed remotely or the PR is ready: the service reports those confirmed outcomes. Continue working after updates and finish through stage_result as usual.\n"
}

func followProgressStart(j Job) string {
	if j.Item.FollowUp != nil && j.Item.FollowUp.Kind == "readiness" {
		return "Checking the current PR diff, description and outstanding feedback before marking it ready for review."
	}
	var checks []string
	if p := j.Item.PullRequest; p != nil {
		for _, c := range p.CheckRuns {
			switch c.State {
			case "failure", "timed_out", "cancelled", "action_required", "startup_failure":
				checks = append(checks, c.Name)
			}
		}
		if len(checks) > 0 {
			return "Investigating failing CI checks: " + strings.Join(checks, ", ") + ". Any repair will be checked locally before updating this PR."
		}
		if p.Checks.Failed > 0 {
			return "Investigating the failing CI checks. Any repair will be checked locally before updating this PR."
		}
		if p.Conflict {
			return "Inspecting the merge conflict with the current base. Any repair will be checked before updating this PR."
		}
		if p.ReviewDecision == "changes_requested" || p.Feedback != "" {
			return "Assessing the review feedback against the current code, then verifying any justified repairs before updating this PR."
		}
	}
	return "Inspecting the current PR and the recorded repair request. Any changes will be verified before updating the PR."
}

func followProgressText(text string) string {
	text = strings.TrimSpace(redact.Secrets(text))
	// First paragraph only; full prose remains in the worker's own chat.
	if at := strings.Index(text, "\n\n"); at >= 0 {
		text = text[:at]
	}
	text = strings.Join(strings.Fields(text), " ")
	if strings.HasPrefix(text, "```") || strings.HasPrefix(text, "{") || strings.Contains(text, "<｜DSML｜") {
		return ""
	}
	switch strings.Trim(strings.ToLower(text), ".! ") {
	case "", "done", "ok", "okay":
		return ""
	}
	runes := []rune(text)
	if len(runes) > summaryMost {
		text = strings.TrimSpace(string(runes[:summaryMost-1])) + "…"
	}
	return text
}

func (lp *floorLoop) followProgress(c *loopCtl, st factory.Stage, phase, round int) func(string) {
	if !factory.IsCIRepairStage(st) {
		return nil
	}
	var mu sync.Mutex
	var last string
	var at time.Time
	return func(text string) {
		text = followProgressText(text)
		if text == "" || c.ctx.Err() != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		now := lp.r.now()
		if text == last || !at.IsZero() && now.Sub(at) < followProgressEvery {
			return
		}
		line := "Follow PR: " + text
		err := lp.write(c, func(it *factory.Item) error {
			if it.Manual || it.State != factory.StateRunning || it.Stream == nil || phase < 0 || phase >= len(it.Stream.Phases) {
				return errors.New("the repair is no longer running")
			}
			p := it.Stream.Phases[phase]
			if p.Name != st.Name || p.Round != round || p.State != factory.PhaseRunning {
				return errors.New("the repair round changed")
			}
			loopSay(it, now, "said", line)
			return nil
		})
		if err == nil {
			last, at = text, now
			lp.tell(c.id, line)
		}
	}
}
