package session

import (
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// The two ways a question ends, as a person-facing receipt names them.
const (
	QuestionDecided   = "decided"
	QuestionWithdrawn = "withdrawn"
)

// questionOutcomesKept is how many withdrawals this session remembers. A receipt
// is for the minutes after a question went away; a long tail would be a log.
const questionOutcomesKept = 64

// questionOutcomesDefault is how many outcomes a caller that named no limit gets.
const questionOutcomesDefault = 20

// QuestionOutcome is how one question ended, in the words a person would read on
// a receipt: it was decided, or it stopped being asked.
type QuestionOutcome struct {
	Kind  QuestionKind
	Token string
	Head  string
	// Outcome is [QuestionDecided] or [QuestionWithdrawn].
	Outcome string
	// Words is the person-facing sentence: what was picked, or why the question
	// is no longer needed. It never names machinery.
	Words string
	// By is who decided, or who withdrew it, in the vocabulary of the record the
	// outcome was read from.
	By string
	At time.Time
	// CallID is the tool call the question was about, copied from the question's
	// subject. The decision record and the replayed history both carry it, so a
	// window reopened later can put the receipt back under the call that asked.
	// A question about no call falls back to the `ask` call that raised it, and
	// is empty only for a question nothing in the history asked.
	CallID string
}

// questionWithdrawals is the session's short memory of withdrawn questions.
// Decisions have a durable record ([DecisionRecord]); a withdrawal has only the
// event that announced it, so this is where a window opened later finds it.
type questionWithdrawals struct {
	mu   sync.Mutex
	kept []QuestionOutcome
}

func (w *questionWithdrawals) remember(outcome QuestionOutcome) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.kept = append(w.kept, outcome)
	if over := len(w.kept) - questionOutcomesKept; over > 0 {
		w.kept = append([]QuestionOutcome(nil), w.kept[over:]...)
	}
}

func (w *questionWithdrawals) list() []QuestionOutcome {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]QuestionOutcome(nil), w.kept...)
}

// anchorCall is the call a receipt hangs under. The call a question is about
// wins, because that is the row the question points at; a plain question has no
// such row, so the call that asked it stands in. Both are rows the replayed
// history also draws, which is what makes the place the same after a reload.
func anchorCall(subject SubjectRef, askedIn string) string {
	if subject.CallID != "" {
		return subject.CallID
	}
	return askedIn
}

// withdrawnOutcome is the receipt for a question that stopped being asked. The
// reason is the asker's own clause ("the turn moved on without it"), joined to
// the plain statement that nothing is owed any more.
func withdrawnOutcome(q Question) QuestionOutcome {
	words := "No longer needed"
	if reason := strings.TrimSpace(q.Withdrawn.Reason); reason != "" {
		words += " — " + reason
	}
	return QuestionOutcome{
		Kind: q.Kind, Token: q.Token(), Head: q.Head,
		Outcome: QuestionWithdrawn, Words: words,
		By: string(q.Withdrawn.By), At: q.Withdrawn.At, CallID: anchorCall(q.Subject, q.AskedIn),
	}
}

// decidedOutcome is the receipt for an answered question, read from the durable
// record so a window opened tomorrow draws the same words as the one that asked.
func decidedOutcome(record DecisionRecord) QuestionOutcome {
	token := strings.TrimSpace(record.Ref)
	if token == "" {
		token = Question{ID: record.ID}.Token()
	}
	return QuestionOutcome{
		Kind: record.Kind, Token: token, Head: record.Head,
		Outcome: QuestionDecided, Words: sentenceCase(record.Words()),
		By: string(record.By), At: record.At, CallID: anchorCall(record.Subject, record.AskedIn),
	}
}

func sentenceCase(text string) string {
	first, size := utf8.DecodeRuneInString(text)
	if first == utf8.RuneError {
		return text
	}
	return string(unicode.ToUpper(first)) + text[size:]
}

// RecentQuestionOutcomes is the last few questions that ended, newest first:
// decisions from the session's record and withdrawals from its short memory. It
// is what lets a window say "Allow once" or "No longer needed" under a question
// that is no longer on the list, instead of the question vanishing.
//
// A limit of zero or less asks for the default. A session with no folder has no
// decision record ([Agent.Decisions]) and answers withdrawals alone; withdrawals
// are kept for this process only, so they are not on a page reopened after a
// restart.
func (a *Agent) RecentQuestionOutcomes(limit int) []QuestionOutcome {
	if limit <= 0 {
		limit = questionOutcomesDefault
	}
	var outcomes []QuestionOutcome
	for _, record := range a.Decisions() {
		outcomes = append(outcomes, decidedOutcome(record))
	}
	outcomes = append(outcomes, a.withdrawals.list()...)
	sort.SliceStable(outcomes, func(i, j int) bool { return outcomes[i].At.After(outcomes[j].At) })
	if len(outcomes) > limit {
		outcomes = outcomes[:limit]
	}
	return outcomes
}
