package resident

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// AskQuestion gives resident components one policy-aware entry point. Every
// urgency is queued first; only blocking questions cross into the thread in
// the same call.
func (r *Reconciler) AskQuestion(question store.AgentQuestion) (store.AgentQuestion, error) {
	if r == nil || r.store == nil {
		return store.AgentQuestion{}, errors.New("resident ask question: nil store")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.askQuestionLocked(question)
}

func (r *Reconciler) askQuestionLocked(question store.AgentQuestion) (store.AgentQuestion, error) {
	if question.DefaultAnswer != "" && !question.ExpiresAt.IsZero() {
		now := r.now()
		if base := question.ExpiresAt.Sub(now); base > 0 {
			question.ExpiresAt = now.Add(r.store.SilenceConsentWait(base))
		}
	}
	queued, err := r.store.AskQuestion(question)
	if err != nil {
		return store.AgentQuestion{}, err
	}
	if queued.Urgency == store.QuestionBlocking {
		if _, err := r.store.SurfaceQuestionForSession(queued.Seq, question.SessionID); err != nil {
			return store.AgentQuestion{}, err
		}
		queued.Status = store.QuestionAsked
	}
	return queued, nil
}

// AttachSession is the resident's session-attach natural moment. It surfaces
// at most one queued next-natural-moment question and never surfaces a
// whenever question.
func (r *Reconciler) AttachSession(sessionID string) error {
	if r == nil || r.store == nil {
		return errors.New("resident attach session: nil store")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.expireQuestionsLocked(); err != nil {
		return fmt.Errorf("resident attach session: expire questions: %w", err)
	}
	if err := r.surfaceBlockingQuestionsLocked(sessionID); err != nil {
		return fmt.Errorf("resident attach session: surface blocking questions: %w", err)
	}
	if err := r.surfaceNaturalQuestionLocked(sessionID); err != nil {
		return fmt.Errorf("resident attach session: surface queued question: %w", err)
	}
	return nil
}

func (r *Reconciler) surfaceNaturalQuestionLocked(sessionID string) error {
	questions, err := r.store.PendingQuestions(sessionID, 50)
	if err != nil {
		return err
	}
	for _, question := range questions {
		if question.Urgency != store.QuestionNextNaturalMoment {
			continue
		}
		_, err := r.store.SurfaceQuestionForSession(question.Seq, sessionID)
		return err // hard cap: exactly one candidate per natural moment
	}
	return nil
}

func (r *Reconciler) surfaceBlockingQuestionsLocked(sessionID string) error {
	questions, err := r.store.PendingQuestions(sessionID, 50)
	if err != nil {
		return err
	}
	for _, question := range questions {
		if question.Urgency != store.QuestionBlocking {
			continue
		}
		if _, err := r.store.SurfaceQuestionForSession(question.Seq, sessionID); err != nil {
			return err
		}
	}
	return nil
}

func (r *Reconciler) expireQuestionsLocked() error {
	questions, err := r.store.UnresolvedQuestions(200)
	if err != nil {
		return err
	}
	now := r.now()
	for _, question := range questions {
		reason, expired, err := r.questionExpiry(question, now)
		if err != nil {
			return err
		}
		if !expired {
			continue
		}
		if err := r.store.ResolveQuestion(question.Seq, store.QuestionExpired, reason); err != nil &&
			!errors.Is(err, store.ErrInvalid) {
			return err
		}
	}
	return nil
}

func (r *Reconciler) questionExpiry(question store.AgentQuestion, now time.Time) (string, bool, error) {
	if !question.ExpiresAt.IsZero() && !now.Before(question.ExpiresAt) {
		return "relevance window elapsed", true, nil
	}
	if question.OriginNodeID != "" {
		node, found, err := r.store.Node(question.OriginNodeID)
		if err != nil {
			return "", false, err
		}
		if !found {
			return "originating job no longer exists", true, nil
		}
		switch node.Status {
		case store.Done, store.Failed, store.Cancelled:
			return fmt.Sprintf("originating job settled as %s", node.Status), true, nil
		}
	}
	if question.OriginCharterID != "" {
		charter, found, err := r.store.Charter(question.OriginCharterID)
		if err != nil {
			return "", false, err
		}
		if !found {
			return "originating charter no longer exists", true, nil
		}
		if charter.Status == store.CharterRetired {
			return "originating charter retired", true, nil
		}
	}
	return "", false, nil
}

func questionCharterOrigin(options []store.QuestionOption) string {
	for _, option := range options {
		parts := strings.Split(option.Value, ":")
		if len(parts) >= 3 && parts[0] == "charter" {
			return strings.TrimSpace(parts[2])
		}
	}
	return ""
}
