package steploop

import (
	"crypto/rand"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

const ReminderQueueCap = 8

var (
	seamMu sync.Mutex
	nowMS  = func() uint64 { return uint64(time.Now().UnixMilli()) }
	idFunc = defaultID

	idMu      sync.Mutex
	idLastMS  uint64
	idCounter uint64
)

// SetNowForTesting swaps Date.now() and returns a restore closure.
func SetNowForTesting(f func() uint64) func() {
	seamMu.Lock()
	prev := nowMS
	nowMS = f
	seamMu.Unlock()
	return func() {
		seamMu.Lock()
		nowMS = prev
		seamMu.Unlock()
	}
}

// SetIDFactoryForTesting swaps MessageID/PartID ascending generation. Prefix
// is "msg" or "prt". It returns a restore closure.
func SetIDFactoryForTesting(f func(prefix string) string) func() {
	seamMu.Lock()
	prev := idFunc
	idFunc = f
	seamMu.Unlock()
	return func() {
		seamMu.Lock()
		idFunc = prev
		seamMu.Unlock()
	}
}

func currentNow() uint64 {
	seamMu.Lock()
	f := nowMS
	seamMu.Unlock()
	return f()
}

func nextID(prefix string) string {
	seamMu.Lock()
	f := idFunc
	seamMu.Unlock()
	return f(prefix)
}

// NewAscendingID lets production adapters mint session-layer records from the
// same ordered sequence as loop-owned messages and parts.
func NewAscendingID(prefix string) string {
	return nextID(prefix)
}

// defaultID is Identifier.create(prefix, "ascending"): six packed time bytes
// followed by randomBase62(14), using one crypto byte modulo 62 per character.
func defaultID(prefix string) string {
	idMu.Lock()
	defer idMu.Unlock()
	ms := currentNow()
	if ms != idLastMS {
		idLastMS = ms
		idCounter = 0
	}
	idCounter++
	packed := ms*0x1000 + idCounter
	random := make([]byte, 14)
	if _, err := rand.Read(random); err != nil {
		panic(err)
	}
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	for index := range random {
		random[index] = alphabet[int(random[index])%len(alphabet)]
	}
	return fmt.Sprintf("%s_%012x%s", prefix, packed&0xffffffffffff, string(random))
}

// ReminderQueue is session.ts:804-846's process-local queue.
type ReminderQueue struct {
	mu          sync.Mutex
	queue       map[string][]string
	lastDrained map[string]string
}

func NewReminderQueue() *ReminderQueue {
	return &ReminderQueue{
		queue:       map[string][]string{},
		lastDrained: map[string]string{},
	}
}

// QueueReminder trims, drops empty/exact duplicates, and drops at cap.
func (q *ReminderQueue) QueueReminder(sessionID, text string) {
	if q == nil {
		return
	}
	trimmed := jscompat.Trim(text)
	if trimmed == "" {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, existing := range q.queue[sessionID] {
		if existing == trimmed {
			return
		}
	}
	if q.lastDrained[sessionID] == trimmed || len(q.queue[sessionID]) >= ReminderQueueCap {
		return
	}
	q.queue[sessionID] = append(q.queue[sessionID], trimmed)
}

// DrainReminders deletes and returns the queue in insertion order, recording
// only the final string for subsequent idempotency.
func (q *ReminderQueue) DrainReminders(sessionID string) []string {
	if q == nil {
		return []string{}
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	pending := q.queue[sessionID]
	if len(pending) == 0 {
		return []string{}
	}
	delete(q.queue, sessionID)
	q.lastDrained[sessionID] = pending[len(pending)-1]
	return append([]string(nil), pending...)
}

// DrainIntoUserMessage is the sole production-style drain point. Existing
// parts include the PlanDB bootstrap reminder, so observer reminders append
// after it and become the most-recent context.
func (q *ReminderQueue) DrainIntoUserMessage(user msgmodel.User, parts msgmodel.Parts) msgmodel.Parts {
	out := append(msgmodel.Parts(nil), parts...)
	synthetic := true
	for _, text := range q.DrainReminders(user.SessionID) {
		out = append(out, msgmodel.TextPart{
			PartBase: msgmodel.PartBase{
				ID:        nextID("prt"),
				SessionID: user.SessionID,
				MessageID: user.ID,
			},
			Text:      text,
			Synthetic: &synthetic,
		})
	}
	return out
}

// OpenFailure is unresolved-failures.ts:27-33.
type OpenFailure struct {
	TaskID             string       `json:"taskID"`
	Title              string       `json:"title"`
	Reason             string       `json:"reason"`
	Bugs               []FailureBug `json:"bugs"`
	RepairHints        []string     `json:"repairHints"`
	BlockedDescendants float64      `json:"blockedDescendants"`
}

type FailureBug struct {
	Severity *string  `json:"severity,omitempty"`
	File     *string  `json:"file,omitempty"`
	Line     *float64 `json:"line,omitempty"`
	Detail   string   `json:"detail"`
}

// RenderRecoveryReminder is unresolved-failures.ts:129-171.
func RenderRecoveryReminder(open []OpenFailure) string {
	if len(open) == 0 {
		return ""
	}
	lines := []string{
		"<system-reminder>",
		"⚠️  RECOVERY REQUIRED — cannot complete the run yet.",
		"",
		fmt.Sprintf("The orchestrator was about to declare completion, but %d cap-exhausted task(s)", len(open)),
		"remain in status=failed with pending descendants. Those descendants are blocked on",
		"the failure and cannot run until you replan.",
		"",
		"Unresolved cap-exhausted failures:",
	}
	for _, failure := range open {
		lines = append(lines, `- `+failure.TaskID+` "`+jsSlice(failure.Title, 0, 90)+`"`)
		lines = append(lines, "    reason: "+jsSlice(failure.Reason, 0, 200))
		lines = append(lines, "    blocking "+jscompat.FormatNumber(failure.BlockedDescendants)+" downstream task(s) in pending/ready/running")
		if len(failure.RepairHints) > 0 {
			lines = append(lines, "    repair hint: "+jsSlice(failure.RepairHints[0], 0, 200))
		}
		if len(failure.Bugs) > 0 {
			bug := failure.Bugs[0]
			where := ""
			if bug.File != nil {
				where = *bug.File
				if bug.Line != nil && jscompat.Truthy(*bug.Line) {
					where += ":" + jscompat.FormatNumber(*bug.Line)
				}
			}
			severity := ""
			if bug.Severity != nil && *bug.Severity != "" {
				severity = "[" + *bug.Severity + "] "
			}
			if where != "" {
				where += " — "
			}
			lines = append(lines, "    bug: "+severity+where+jsSlice(bug.Detail, 0, 180))
		}
	}
	lines = append(lines,
		"",
		"Choose for EACH leaf above — split / pivot / amend / cancel — and dispatch the new plan.",
		"Inspect first if needed:",
		"  `plandb contexts --task <id> --kind blocker`  (reviewer's full bug list + repair_hints)",
		"  `plandb contexts --task <id> --kind review`   (every attempt verbatim)",
		"",
		"Commands:",
		`  plandb task split <id> --into "A, B, C"           (break into smaller leaves)`,
		"  plandb task pivot <id> --file new-plan.yaml         (replace subtree)",
		`  plandb task amend <id> --prepend "NOTE: ..."        (relax acceptance)`,
		`  plandb task cancel <id> --reason "..."              (accept as impossible)`,
		"",
		"This is your final nudge — after you respond, the loop will let you exit even if the",
		"failure is still open. So either replan now, or explicitly cancel.",
		"</system-reminder>",
	)
	return strings.Join(lines, "\n")
}

// jsSlice implements the non-negative slice bounds used by the recovery
// renderer in UTF-16 code units.
func jsSlice(value string, start, end int) string {
	units := utf16.Encode([]rune(value))
	if start > len(units) {
		start = len(units)
	}
	if end > len(units) {
		end = len(units)
	}
	if end < start {
		end = start
	}
	return string(utf16.Decode(units[start:end]))
}

func syntheticTextPart(sessionID, messageID, text string) msgmodel.TextPart {
	synthetic := true
	return msgmodel.TextPart{
		PartBase: msgmodel.PartBase{
			ID:        nextID("prt"),
			SessionID: sessionID,
			MessageID: messageID,
		},
		Text:      text,
		Synthetic: &synthetic,
	}
}
