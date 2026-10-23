package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	if !spec.VerifyRepair || spec.Item.Stream == nil || spec.Resume != "" {
		return ""
	}
	var candidates []string
	for _, ph := range spec.Item.Stream.Phases {
		if ph.Name != factory.FollowPRStep {
			continue
		}
		if ph.Chat != "" {
			candidates = append(candidates, ph.Chat)
		}
		for i := len(ph.Rounds) - 1; i >= 0 && len(candidates) < 12; i-- {
			if ph.Rounds[i].Chat != "" {
				candidates = append(candidates, ph.Rounds[i].Chat)
			}
		}
	}
	for _, source := range candidates {
		if source == file {
			continue
		}
		notes, _ := (&stageConversation{file: source, dir: dir}).RepairExecutionNotes()
		blocked := false
		for _, n := range notes {
			blocked = blocked || strings.HasPrefix(n, verification.FailurePrefix)
		}
		if blocked {
			continue
		}
		checks, err := verification.Read(source)
		if err != nil || len(checks) == 0 || len(checks) > 512 {
			continue
		}
		at, err := verification.SnapshotAt(dir)
		if err != nil {
			continue
		}
		required, _ := verification.RequiredChecks(checks, at, true)
		if len(required) == 0 {
			continue
		}
		var saved []verification.Check
		for _, c := range required {
			if c.Exit == nil || *c.Exit != 0 || !c.Fresh(at) {
				blocked = true
				break
			}
			saved = append(saved, c)
		}
		if blocked {
			continue
		}
		if err = writeFactoryCheckCheckpoint(file, source, saved); err != nil {
			return ""
		}
		return fmt.Sprintf("\nSaved verification checkpoint: %d measured checks already passed on this exact checkout (HEAD %s). They are available to stage_result. Reuse these results; rerun only checks invalidated by edits or a newly required scope. Review the current feedback and report no_change when the published work already satisfies it.\n", len(saved), at.Head)
	}
	return ""
}
func writeFactoryCheckCheckpoint(file, source string, checks []verification.Check) error {
	target := file + ".checks"
	root, err := os.MkdirTemp(filepath.Dir(file), ".checkpoint-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	for i, c := range checks {
		dir, err := os.MkdirTemp(root, "call-checkpoint-")
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(c.Command)
		if err = os.WriteFile(filepath.Join(dir, "command.json"), raw, 0600); err != nil {
			return err
		}
		path := filepath.Join(dir, "check-saved")
		fields := append([]string{c.Cwd}, c.Context...)
		fields = append(fields, c.Args...)
		if err = os.WriteFile(path, []byte(strings.Join(fields, "\x00")+"\x00"), 0600); err != nil {
			return err
		}
		for suffix, raw := range map[string]string{".exit": "0\n", ".before": c.Before.Head + " " + c.Before.Fingerprint, ".after": c.After.Head + " " + c.After.Fingerprint} {
			if err = os.WriteFile(path+suffix, []byte(raw), 0600); err != nil {
				return err
			}
			stamp := time.Now().Add(time.Duration(i) * time.Nanosecond)
			_ = os.Chtimes(path+suffix, stamp, stamp)
		}
	}
	if err := os.Rename(root, target); err != nil {
		return err
	}
	proof, _ := json.Marshal(map[string]any{"source": source, "checks": len(checks), "head": checks[0].Before.Head, "fingerprint": checks[0].Before.Fingerprint})
	return os.WriteFile(file+".checkpoint.json", proof, 0600)
}
