package session

import (
	"errors"
	"strings"
)

// The two fallback sentences returned when an answer from another computer
// tries to settle an irreversible question or remember a wider permission. The
// remote server replaces both with host-named sentences from internal/chatlist
// before sending the refusal over the wire.
var (
	// ErrRemoteIrreversible refuses an answer from another computer when the
	// question has irreversible stakes.
	ErrRemoteIrreversible = errors.New("that one has to be answered on the computer the chat is running on")
	// ErrRemoteWidening refuses a permission answer from another computer when
	// its scope reaches beyond a single call.
	ErrRemoteWidening = errors.New("that kind of answer has to be given on the computer the chat is running on")
)

// refuseRemoteAnswer checks whether an answer that arrived from another
// computer is allowed to proceed. It peeks the open-question book with
// [Agent.questionSaid] instead of claiming the entry so a refused answer never
// takes the question off the book, requires no put-back, and leaves no instant
// in which another window could see the question vanish.
func (a *Agent) refuseRemoteAnswer(answer Answer) error {
	if !answer.Remote {
		return nil
	}
	if answer.Revises && a.remoteRevisionIsIrreversible(answer) {
		return ErrRemoteIrreversible
	}
	// When nothing is banked (said == false), stakes are unknown: QuestionConsent,
	// QuestionAsk, QuestionTask, QuestionAutomation, and QuestionConnect fall
	// through to not-irreversible only because an unbanked answer reaches a
	// resolver that drops a late id anyway.
	if q, said := a.questionSaid(answer.Kind, answerToken(answer)); said && q.Stakes == StakesIrreversible {
		return ErrRemoteIrreversible
	}
	if answer.Kind == QuestionConsent {
		if action, ok := AnswerFromKey(QuestionConsent, answer.FirstKey()); ok {
			if ConsentScopeOf(action, answer) != ConsentOnce {
				return ErrRemoteWidening
			}
		}
		if strings.TrimSpace(answer.Comments[AnswerBanked]) != "" {
			return ErrRemoteWidening
		}
	}
	return nil
}

// remoteRevisionIsIrreversible reports whether a revision from another computer
// targets a settled decision whose stakes were irreversible.
func (a *Agent) remoteRevisionIsIrreversible(answer Answer) bool {
	if answer.Kind == QuestionAsk {
		a.mu.Lock()
		open, settled := a.asked.settledLocked(answer.ID)
		var stakes Stakes
		if settled {
			stakes = open.q.Stakes
		}
		a.mu.Unlock()
		if settled && stakes == StakesIrreversible {
			return true
		}
	}
	records := a.Decisions()
	for at := len(records) - 1; at >= 0; at-- {
		record := records[at]
		if record.Kind != answer.Kind {
			continue
		}
		if answer.Ref != "" {
			if record.Ref != answer.Ref {
				continue
			}
		} else if record.ID != answer.ID {
			continue
		}
		return !record.Reversible()
	}
	return false
}
