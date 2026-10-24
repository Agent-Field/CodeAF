package session

import (
	"github.com/Agent-Field/agentfield/sdk/go/ai"
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
	// Old crew receipts were authored single-line records. A person's quoted text has no Note mark.
	if !strings.ContainsAny(text, "\r\n") {
		head, body, ok := strings.Cut(text, " crew · ")
		fields := strings.Fields(head)
		if ok && len(fields) == 2 && fields[0] == "task" && strings.Contains(body, " · worker ") && strings.Contains(body, " · checker ") {
			id, err := strconv.ParseUint(fields[1], 10, 64)
			if err == nil && id > 0 {
				return true
			}
		}
	}
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

// Receipt provenance is carried separately so inspecting it cannot consume the answer's own tags.
func completionTaskTags(journal *sessionFile, message ai.Message, routine bool, presentation *presentationIndex) []TaskReplyTag {
	if routine {
		if mark := presentation.of(message); mark != nil && len(mark.CompletionTasks) > 0 {
			return append([]TaskReplyTag(nil), mark.CompletionTasks...)
		}
	}
	if !routine || journal == nil {
		return nil
	}
	return journal.taskReplyTags(message)
}

// Older mixed batches cannot prove that a requested alert was absent, so their complete content stays visible.
func legacyAttentionReceipt(entry sessionEntry) bool {
	if entry.Role != "user" || !entry.Note || len(entry.ToolCalls) != 0 || legacyRoutineCompletion(entry) {
		return false
	}
	text := strings.TrimSpace(entry.Content)
	if strings.HasPrefix(text, "while you worked:") {
		return true
	}
	return LegacyWatchAlert(text)
}

// LegacyWatchAlert recognizes only the reserved terminal watch records, not periodic watch ticks.
func LegacyWatchAlert(text string) bool {
	head, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	if !strings.HasPrefix(head, "watch ") {
		return false
	}
	_, summary, ok := strings.Cut(head, ": ")
	return ok && (summary == "until matched" || strings.HasPrefix(summary, "quiet for ") || strings.HasPrefix(summary, "stopped: the command failed "))
}
