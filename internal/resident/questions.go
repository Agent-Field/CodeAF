package resident

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// compileAskWindow is how long a clarifying question about a request stays
// about that request. Long enough to survive a night away from the machine —
// the answer to "which airport?" is still the answer in the morning — and short
// enough that it is not still sitting there a week later, when the trip has
// been booked some other way and the question is an accusation.
const compileAskWindow = 20 * time.Hour

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
	return r.rehomeOrphanedBlockingLocked()
}

// rehomeOrphanedBlockingLocked rescues a blocking question whose conversation
// is gone. Every launch mints a fresh session id, so "book the flight" → "which
// airport?" → quit → next morning left the question filed under a session
// nobody would ever attach to again: it was already Asked, so the pending sweep
// skipped it; it can never expire on origin, having neither node nor charter;
// and the answer lookup is session-filtered, so even typing the answer could
// not reach it. The request was neither done nor refused nor mentioned again.
//
// The one live surface adopts it. A question that already belongs to the
// attached session is left exactly where it is, which is what stops this from
// repeating.
func (r *Reconciler) rehomeOrphanedBlockingLocked() error {
	seen, found, err := r.store.LastSeen()
	if err != nil || !found || seen.State != store.SeenAttached {
		return err
	}
	live := strings.TrimSpace(seen.SessionID)
	if live == "" {
		return nil
	}
	questions, err := r.store.UnresolvedQuestions(200)
	if err != nil {
		return err
	}
	for _, question := range questions {
		if question.Status != store.QuestionAsked || question.Urgency != store.QuestionBlocking {
			continue
		}
		original := strings.TrimSpace(question.SessionID)
		if original == "" || original == live {
			continue
		}
		// A second window that is still being used is not an orphan. If anything
		// at all has been said in the original session since the question was
		// asked, the user can see it where it is, and moving it would take a
		// live conversation's question away from it.
		since, err := r.store.Messages(original, question.AskedMessageSeq, 1)
		if err != nil {
			return err
		}
		if len(since) > 0 {
			continue
		}
		if _, err := r.store.ResurfaceQuestion(question.Seq, live); err != nil &&
			!errors.Is(err, store.ErrInvalid) {
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
		if err := r.sayTheRequestLapsed(question, reason); err != nil {
			return err
		}
	}
	return nil
}

// sayTheRequestLapsed is the loud half of expiry, and it is narrow twice over.
//
// Blocking only: a blocking question is one the system said it could not go on
// without, so the whole request rides on it, and retiring that quietly is how a
// flight nobody booked becomes a thing nobody ever mentioned again. Every other
// urgency stays silent — a taste annotation retiring itself is not news, and
// announcing it would turn the quietest loop in the resident into noise.
//
// And its own window only: a question retired because its job settled or its
// charter was pulled already has a visible reason in the thread, and telling
// the user that the request "never went ahead" when the job it belonged to
// finished would be a lie. The window is the case with no other trace.
func (r *Reconciler) sayTheRequestLapsed(question store.AgentQuestion, reason string) error {
	if question.Urgency != store.QuestionBlocking || reason != expiredOnItsWindow {
		return nil
	}
	sessionID := strings.TrimSpace(question.SessionID)
	if sessionID == "" {
		return nil
	}
	body := "That question lapsed unanswered (" + reason + "), so the request behind it never went ahead: " +
		clipLabel(firstLine(question.Text), 160) + " — ask again whenever you want it."
	_, err := r.store.PostMessage(store.Message{
		SessionID: sessionID,
		Role:      store.RoleSystem,
		Body:      boundMessage(body),
		NodeID:    question.OriginNodeID,
	})
	return err
}

// expiredOnItsWindow is the reason only a relevance window produces. It is a
// constant rather than a bool returned alongside because questionExpiry already
// speaks in reasons, and one vocabulary is easier to keep true than two.
const expiredOnItsWindow = "relevance window elapsed"

func (r *Reconciler) questionExpiry(question store.AgentQuestion, now time.Time) (string, bool, error) {
	if !question.ExpiresAt.IsZero() && !now.Before(question.ExpiresAt) {
		return expiredOnItsWindow, true, nil
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
