package session

import (
	"strconv"
	"strings"
)

// legacyRoutineCompletion recognizes only standalone runtime receipts whose
// journal already proves nobody typed them. Mixed batches cannot prove that
// they contain no requested watch alert, so they retain their original audience.
func legacyRoutineCompletion(entry sessionEntry) bool {
	if entry.Role != "user" || !entry.Note || len(entry.ToolCalls) != 0 {
		return false
	}
	text := entry.Content
	if strings.HasPrefix(text, "while you worked:") {
		return false
	}
	// The complete reserved job instruction is stronger provenance than a
	// sentence merely saying a program finished. Output remains in its job log.
	if head, tail, ok := strings.Cut(text, "\n"); ok && strings.HasPrefix(tail, jobExitNewsRule) {
		fields := strings.Fields(head)
		if len(fields) >= 4 && fields[0] == "job" && fields[2] == "exited" {
			id, idErr := strconv.ParseUint(fields[1], 10, 64)
			_, codeErr := strconv.Atoi(strings.TrimSuffix(fields[3], ":"))
			if idErr == nil && id > 0 && codeErr == nil && (tail == jobExitNewsRule || strings.HasPrefix(tail, jobExitNewsRule+"\n")) {
				return true
			}
		}
	}
	// Older task receipts without a batch envelope carry the exact delivery
	// lead and the task's canonical head. Ordinary prose or quoted examples do
	// not satisfy both, even when they contain the same task title.
	for _, notice := range []TaskNotice{{State: TaskDone}, {State: TaskFailed}, {State: TaskUnverified}, {State: TaskFailed, Stopped: true}} {
		lead := landingNoteLead(notice)
		if lead == "" || !strings.HasPrefix(text, lead) {
			continue
		}
		head, _, _ := strings.Cut(strings.TrimPrefix(text, lead), "\n")
		fields := strings.Fields(head)
		if len(fields) < 3 || fields[0] != "task" {
			return false
		}
		id, err := strconv.ParseUint(fields[1], 10, 64)
		return err == nil && id > 0 && strings.HasPrefix(head, "task "+fields[1]+" "+ProjectTask(notice.StatusFacts()).Word+": ") && strings.Contains(head, " · transcript ")
	}
	return false
}
