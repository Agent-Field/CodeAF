package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/verification"
)

// Only the known old invalid-Cargo guard may release its saved synthetic
// exhaustion question. A real failure, takeover, budget stop or new decision
// remains at its own recovery door. Publication still goes through the runner.
func factoryVerificationRecovered(it factory.Item, dir string) bool {
	if !factory.ReviewPauseRecoverable(it) || it.FollowUp.RecoveryVersion >= 1 || it.QKind != "out_of_attempts" || it.PullRequest == nil || it.HeadCommit != it.PullRequest.HeadSHA || it.HeadCommit == "" || it.FollowUp.Attempt >= it.FollowUp.Limit {
		return false
	}
	for _, ph := range it.Stream.Phases {
		if ph.Name != factory.FollowPRStep || ph.Result == nil || !strings.HasPrefix(ph.Result.Output, "Verification blocked:") {
			continue
		}
		review := ph.Result.Repair
		if review == nil || review.Readiness == nil || review.Readiness.Head != it.HeadCommit || !review.Readiness.DiffReviewed || !review.Readiness.FeedbackAddressed || !review.Readiness.DescriptionMatches || !review.Readiness.NoUnfinishedWork {
			return false
		}
		checks, err := verification.Read(ph.Chat)
		if err != nil {
			return false
		}
		invalid := false
		for _, c := range checks {
			if c.InvocationProblem() != "" {
				invalid = true
			}
		}
		if !invalid {
			return false
		}
		notes, _ := (&stageConversation{file: ph.Chat, dir: dir}).RepairExecutionNotes()
		for _, n := range notes {
			if strings.HasPrefix(n, verification.FailurePrefix) {
				return false
			}
		}
		return true
	}
	return false
}

// A new conversation receives only measured checks whose contents and head
// still match. The original journal is untouched; prior attempts remain auditable.
// No command, commit, push, or previously approved side effect is replayed.
func inheritFactoryChecks(spec factoryrun.ConversationSpec, file, dir string) string {
	stage := spec.VerificationStage
	if spec.VerifyRepair {
		stage = factory.FollowPRStep
	}
	if (stage != "test" && stage != "proof" && !spec.VerifyRepair) || spec.Item.Stream == nil || spec.Resume != "" {
		return ""
	}
	at, err := verification.SnapshotAt(dir)
	if err != nil || at.Head == "" {
		return ""
	}
	owner, err := verification.OwnerAt(dir)
	if err != nil {
		return ""
	}
	belongs := func(c verification.Check) bool {
		if c.OwnerRoot != "" && c.OwnerRoot != owner {
			return false
		}
		cwd, e := filepath.EvalSymlinks(c.Cwd)
		if e != nil {
			return false
		}
		rel, e := filepath.Rel(owner, cwd)
		return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	var candidates []string
	for _, ph := range spec.Item.Stream.Phases {
		if ph.Name != stage {
			continue
		}
		for _, round := range ph.Rounds {
			if round.Chat != "" && !round.At.IsZero() {
				candidates = append(candidates, round.Chat)
			}
		}
		if ph.Chat != "" && (!ph.At.IsZero() || ((ph.State == factory.PhaseDone || ph.State == factory.PhaseFailed) && ph.Result != nil)) {
			candidates = append(candidates, ph.Chat)
		}
	}
	seenSources := map[string]bool{}
	seenChecks := map[string]bool{}
	var history []verification.Check
	for _, source := range candidates {
		if source == file || seenSources[source] {
			continue
		}
		seenSources[source] = true
		checks, err := verification.Read(source)
		if err != nil {
			continue
		}
		for _, c := range checks {
			identity := c.Source + "\x00" + c.ID
			if seenChecks[identity] {
				continue
			}
			seenChecks[identity] = true
			history = append(history, c)
		}
	}
	required, _ := verification.RequiredChecks(history, at, true)
	// Only finished source conversations reach this point. An interrupted repeat
	// does not refute an earlier pass on exactly the same inputs and context. A
	// later definite failure does, and must be followed by a new passing check.
	qualifying := map[string]verification.Check{}
	for _, c := range history {
		if c.Diagnostic || c.SupersededBy != "" {
			continue
		}
		if c.Problem != "" || (c.Exit != nil && *c.Exit != 0) {
			delete(qualifying, c.Key())
			continue
		}
		if c.Exit != nil && *c.Exit == 0 && c.Fresh(at) && belongs(c) {
			qualifying[c.Key()] = c
		}
	}
	var saved []verification.Check
	passes := 0
	unresolved := 0
	for key, c := range required {
		if c.Exit == nil && c.Problem == "" {
			if pass, ok := qualifying[key]; ok {
				c.SupersededBy = pass.Source + ":" + pass.ID
				c.SupersessionReason = "incomplete repeat from a finished round; exact-input passing evidence retained"
				saved = append(saved, c)
				c = pass
			}
		}
		// A stale historical success is not imported as fresh evidence. Failures
		// and incomplete executions remain obligations alongside independent passes.
		if c.Exit != nil && *c.Exit == 0 && c.Problem == "" {
			if !c.Fresh(at) || !belongs(c) {
				continue
			}
			passes++
		} else {
			unresolved++
		}
		saved = append(saved, c)
	}
	if passes == 0 {
		return ""
	}
	sort.Slice(saved, func(i, j int) bool { return saved[i].Key() < saved[j].Key() })
	if err = writeFactoryCheckCheckpoint(file, "", saved); err != nil {
		return ""
	}
	return fmt.Sprintf("\nSaved verification checkpoint: %d measured checks already passed on this exact checkout (HEAD %s); %d unresolved checks retained. Original source receipts remain authoritative. Reuse qualifying results; rerun unresolved checks and any scope invalidated by edits.\n", passes, at.Head, unresolved)
}

func writeFactoryCheckCheckpoint(file, source string, checks []verification.Check) error {
	if source != "" {
		for i := range checks {
			if checks[i].Source == "" {
				checks[i].Source = source
			}
		}
	}
	return verification.Inherit(file, checks)
}
