// This file ports checkpoint mechanics from swe-pro/src/cli/cmd/resume.ts:1-344.
package codeaf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/auditorgate"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/contextpolicy"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/fixgenerator"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/ledgers"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/scheduler"
)

const resumeCheckpointRelative = ".codeaf/resume-checkpoint.json"

var resumableTerminalStatuses = map[string]bool{
	"fail": true, "crashed": true, "escalated": true,
	"budget-exhausted": true, "budget-exhausted-retries": true,
}

type resumeCheckpointFile struct {
	Goal         string   `json:"goal"`
	SessionID    string   `json:"sessionID,omitempty"`
	FinalStatus  string   `json:"finalStatus"`
	Cycle        float64  `json:"cycle"`
	Reason       string   `json:"reason,omitempty"`
	WallStartTS  *float64 `json:"wallStartTs,omitempty"`
	CostSpentUSD *float64 `json:"costSpentUsd,omitempty"`
	TS           float64  `json:"ts"`
}

type resumeCheckpoint struct {
	HasCheckpoint    bool
	OpenBlockers     []ledgers.OpenBlocker
	PendingTaskCount int
	VerdictStatus    string
	FailureSignals   []string
	LastCycle        float64
	TerminalStatus   string
}

type staleClaims struct {
	Claimed []string
	Running []string
}

func writeTerminalCheckpoint(workspace string, row resumeCheckpointFile) {
	if row.Goal == "" {
		return
	}
	row.TS = float64(time.Now().UnixMilli())
	directory := filepath.Join(workspace, ".codeaf")
	if os.MkdirAll(directory, 0o755) != nil {
		return
	}
	finalPath := filepath.Join(workspace, resumeCheckpointRelative)
	tmp, err := os.CreateTemp(directory, "resume-checkpoint.json.tmp-*")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	if encoder.Encode(row) != nil || tmp.Sync() != nil || tmp.Close() != nil {
		_ = tmp.Close()
		return
	}
	_ = os.Rename(tmpPath, finalPath)
}

func readResumeCheckpoint(workspace string) *resumeCheckpointFile {
	raw, err := os.ReadFile(filepath.Join(workspace, resumeCheckpointRelative))
	if err != nil {
		return nil
	}
	var row resumeCheckpointFile
	if json.Unmarshal(raw, &row) != nil || row.Goal == "" {
		return nil
	}
	return &row
}

func rehydrateCheckpoint(workspace string) resumeCheckpoint {
	result := resumeCheckpoint{
		OpenBlockers: ledgers.LoadOpenBlockers(workspace),
	}
	verdict, _ := auditorgate.ReadVerdictFile(workspace)
	if verdict != nil {
		result.VerdictStatus = string(verdict.Verdict)
		for _, blocker := range verdict.Blockers {
			if blocker.Detail != "" {
				result.FailureSignals = append(result.FailureSignals, blocker.Detail)
			}
		}
	}
	for _, record := range ledgers.ReadCycleRecords(workspace) {
		if record.Cycle > result.LastCycle {
			result.LastCycle = record.Cycle
		}
	}
	if persisted := fixgenerator.ReadAuditCycles(workspace); persisted > result.LastCycle {
		result.LastCycle = persisted
	}
	if file := readResumeCheckpoint(workspace); file != nil {
		result.TerminalStatus = file.FinalStatus
		if file.Cycle > result.LastCycle {
			result.LastCycle = file.Cycle
		}
	}
	for _, task := range plandb.GetPlanDB().ListTasks(nil) {
		if task.Status == plandb.StatusReady || task.Status == plandb.StatusClaimed {
			result.PendingTaskCount++
		}
	}
	result.HasCheckpoint = len(result.OpenBlockers) > 0 ||
		result.PendingTaskCount > 0 || result.VerdictStatus == "fail" ||
		resumableTerminalStatuses[result.TerminalStatus]
	return result
}

func releaseStaleClaims() staleClaims {
	return releaseStaleClaimsExcept(nil, nil)
}

// releaseStaleClaimsExcept is the in-run form of the resume requeue. eligible
// limits reclamation to the graph being drained, while live protects rows that
// still have a scheduler dispatch behind their claim.
func releaseStaleClaimsExcept(
	eligible map[string]struct{}, live map[string]struct{},
) staleClaims {
	var result staleClaims
	for _, task := range plandb.GetPlanDB().ListTasks(nil) {
		if task.Status != plandb.StatusClaimed && task.Status != plandb.StatusRunning {
			continue
		}
		if eligible != nil {
			if _, ok := eligible[task.ID]; !ok {
				continue
			}
		}
		if _, ok := live[task.ID]; ok {
			continue
		}
		// Re-check under the scheduler registry lock. The earlier snapshot is
		// only an optimization; a dispatch may have started since it was taken.
		if scheduler.ReleaseTaskIfNotInFlight(task.ID) == nil {
			continue
		}
		if task.Status == plandb.StatusClaimed {
			result.Claimed = append(result.Claimed, task.ID)
		} else {
			result.Running = append(result.Running, task.ID)
		}
	}
	return result
}

func buildResumeSeed(
	goal string,
	checkpoint resumeCheckpoint,
	ledger []ledgers.AttemptRecord,
	stale staleClaims,
) string {
	brief := contextpolicy.BuildDistilledBrief(contextpolicy.BuildDistilledBriefInput{
		TaskDescription: goal, FailureSignals: checkpoint.FailureSignals,
		AttemptedApproaches: []string{}, Ledger: ledger,
		OpenBlockers: checkpoint.OpenBlockers,
	})
	lines := []string{
		"# Resumed run (fresh context)",
		"",
		"A prior session on this task hit a budget/wall limit and checkpointed. Its",
		"work product, task graph, and audit state are already on disk. Do NOT start",
		"over — CONTINUE from where it stopped:",
	}
	if checkpoint.PendingTaskCount > 0 {
		lines = append(lines,
			"- "+itoa(checkpoint.PendingTaskCount)+
				" fix-task(s) are still pending in PlanDB; execute them.",
		)
	}
	if len(checkpoint.OpenBlockers) > 0 {
		lines = append(lines,
			"- "+itoa(len(checkpoint.OpenBlockers))+
				" blocker(s) remain open; resolve each before claiming done.",
		)
	}
	staleIDs := append(append([]string{}, stale.Claimed...), stale.Running...)
	if len(staleIDs) > 0 {
		sample := staleIDs
		if len(sample) > 10 {
			sample = sample[:10]
		}
		lines = append(lines,
			"- Released "+itoa(len(staleIDs))+
				" stale task(s) from the prior process ("+
				strings.Join(sample, ", ")+"); re-execute them.",
		)
	}
	lines = append(lines, "", brief)
	return strings.Join(lines, "\n")
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var buffer [32]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[index:])
}
