package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const standingWatchQuestion = "Should I keep watching this when you're not here? ▸ 1 yes, always · ▸ 2 only while I'm around"

// standingWatchStandDownQuestion is the mirror of the offer, asked in the same
// words from the other side. It reuses the enable/decline option codes exactly:
// the decision is one switch and it should read like one switch, whichever way
// it is currently thrown.
const standingWatchStandDownQuestion = "Nothing stands any more, and I'm still checking every few minutes with no terminal open. Keep watching? ▸ 1 keep watching · ▸ 2 stand down"

// StandingWatchDecision is the journal-derived global policy state.
type StandingWatchDecision string

const (
	StandingWatchUndecided StandingWatchDecision = ""
	StandingWatchOffered   StandingWatchDecision = "offered"
	StandingWatchEnabled   StandingWatchDecision = "enabled"
	StandingWatchDeclined  StandingWatchDecision = "declined"
	// StandingWatchStoodDown is a yes the user has taken back. It is distinct
	// from declined because the host timer exists in this state and has to be
	// removed, and because it can be turned back on — declined never installed
	// anything and its offer was the once-ever gate.
	StandingWatchStoodDown StandingWatchDecision = "stood-down"
)

type standingWatchOfferPayload struct {
	SessionID string `json:"session_id"`
	CharterID string `json:"charter_id"`
}

type standingWatchDecisionPayload struct {
	Reason string `json:"reason"`
}

// StandingWatchPass is the bounded receipt written by `aforge wake`. It is
// journal-native because status needs only the event time.
type StandingWatchPass struct {
	Examined  int `json:"examined"`
	Woken     int `json:"woken"`
	Checked   int `json:"checked"`
	Fired     int `json:"fired"`
	Proposed  int `json:"proposed"`
	No        int `json:"no"`
	Errors    int `json:"errors"`
	Quota     int `json:"quota"`
	Expired   int `json:"expired"`
	RailWaits int `json:"rail_waits"`
}

// OfferStandingWatch atomically records the never-ask-twice gate and surfaces
// exactly one selectable agent question. A restart can therefore land before
// or after the transaction, never between the flag and the question.
func (s *Store) OfferStandingWatch(sessionID, charterID string) (bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	charterID = strings.TrimSpace(charterID)
	if sessionID == "" || charterID == "" {
		return false, fmt.Errorf("offer standing watch: %w: session and charter are required", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return false, fmt.Errorf("offer standing watch: %w", err)
	}
	defer tx.Rollback()

	charter, err := charterInTx(tx, charterID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, fmt.Errorf("offer standing watch: %w: charter %q", ErrNotFound, charterID)
		}
		return false, fmt.Errorf("offer standing watch: %w", err)
	}
	if charter.Status != CharterActive {
		return false, fmt.Errorf("offer standing watch: %w: charter is %s", ErrInvalid, charter.Status)
	}

	var already bool
	if err := tx.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM events WHERE kind IN (?, ?, ?)
	)`, EventStandingWatchOffered, EventStandingWatchEnabled, EventStandingWatchDeclined).Scan(&already); err != nil {
		return false, fmt.Errorf("offer standing watch: %w", err)
	}
	if already {
		return false, nil
	}
	if _, _, err := appendEvent(tx, charterID, EventStandingWatchOffered,
		standingWatchOfferPayload{SessionID: sessionID, CharterID: charterID}); err != nil {
		return false, fmt.Errorf("offer standing watch: %w", err)
	}

	options := []QuestionOption{
		{Label: "yes, always", Value: "standing-watch:enable"},
		{Label: "only while I'm around", Value: "standing-watch:decline"},
	}
	questionPayload := agentQuestionPayload{
		SessionID: sessionID, Text: standingWatchQuestion, OriginCharterID: charterID,
		Urgency: QuestionBlocking, Options: options,
	}
	questionSeq, questionAt, err := appendEvent(tx, charterID, EventAgentQuestionQueued, questionPayload)
	if err != nil {
		return false, fmt.Errorf("offer standing watch: %w", err)
	}
	if err := applyAgentQuestionView(tx, questionPayload, questionSeq, questionAt); err != nil {
		return false, fmt.Errorf("offer standing watch: %w", err)
	}
	messagePayload := messagePayload{
		SessionID: sessionID, Role: RoleAgent, Body: standingWatchQuestion,
		QuestionSeq: questionSeq, Options: options,
	}
	messageSeq, messageAt, err := appendEvent(tx, charterID, EventMessagePosted, messagePayload)
	if err != nil {
		return false, fmt.Errorf("offer standing watch: %w", err)
	}
	if err := applyMessageView(tx, messagePayload, messageSeq, messageAt); err != nil {
		return false, fmt.Errorf("offer standing watch: %w", err)
	}
	surfaced := agentQuestionSurfacedPayload{QuestionSeq: questionSeq, MessageSeq: messageSeq}
	eventSeq, surfacedAt, err := appendEvent(tx, charterID, EventAgentQuestionSurfaced, surfaced)
	if err != nil {
		return false, fmt.Errorf("offer standing watch: %w", err)
	}
	if err := applyAgentQuestionSurfaced(tx, surfaced, eventSeq, surfacedAt); err != nil {
		return false, fmt.Errorf("offer standing watch: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("offer standing watch: %w", err)
	}
	return true, nil
}

// RecordStandingWatchDecision appends one final decision. Replaying a command
// after a crash is idempotent when it agrees with the journal and rejected if
// it conflicts with the decision already made.
func (s *Store) RecordStandingWatchDecision(decision StandingWatchDecision, reason string) error {
	var kind EventKind
	switch decision {
	case StandingWatchEnabled:
		kind = EventStandingWatchEnabled
	case StandingWatchDeclined:
		kind = EventStandingWatchDeclined
	case StandingWatchStoodDown:
		kind = EventStandingWatchStoodDown
	default:
		return fmt.Errorf("record standing watch decision: %w: invalid decision %q", ErrInvalid, decision)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("record standing watch decision: %w: reason is required", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("record standing watch decision: %w", err)
	}
	defer tx.Rollback()
	current, err := standingWatchDecisionTx(tx)
	if err != nil {
		return fmt.Errorf("record standing watch decision: %w", err)
	}
	if current == decision {
		return nil
	}
	// The switch has two ways to be thrown and both are legal. Enabled may be
	// stood down, and a stand-down may be re-enabled — a consent with no
	// reverse gear is the thing this transition exists to end. Everything else
	// stays terminal: declined was answered once and never asked again.
	if !standingWatchTransition(current, decision) {
		if current == StandingWatchOffered {
			return fmt.Errorf("record standing watch decision: %w: %s is not a decision on an offer",
				ErrInvalid, decision)
		}
		if current == StandingWatchUndecided {
			return fmt.Errorf("record standing watch decision: %w: no standing watch offer", ErrInvalid)
		}
		return fmt.Errorf("record standing watch decision: %w: already %s", ErrInvalid, current)
	}
	if _, _, err := appendEvent(tx, RootID, kind, standingWatchDecisionPayload{Reason: reason}); err != nil {
		return fmt.Errorf("record standing watch decision: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record standing watch decision: %w", err)
	}
	return nil
}

// standingWatchTransition states the whole legal graph in one place, so no
// caller has to reason about it and no future state can be added by accident.
func standingWatchTransition(from, to StandingWatchDecision) bool {
	switch from {
	case StandingWatchOffered:
		return to == StandingWatchEnabled || to == StandingWatchDeclined
	case StandingWatchEnabled:
		return to == StandingWatchStoodDown
	case StandingWatchStoodDown:
		return to == StandingWatchEnabled
	default:
		return false
	}
}

// OfferStandingWatchStandDown asks, once per enablement, whether the quiet
// background checks should stop. It mirrors OfferStandingWatch exactly —
// atomic gate plus one selectable question — and reuses the enable/decline
// option codes, so the answer travels the route that already exists.
//
// The gate is "since the last enable" rather than "ever": a user who stands the
// watch down and later turns it back on has a new consent, and a new consent
// can be withdrawn again.
func (s *Store) OfferStandingWatchStandDown(sessionID string) (bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false, fmt.Errorf("offer standing watch stand-down: %w: session is required", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return false, fmt.Errorf("offer standing watch stand-down: %w", err)
	}
	defer tx.Rollback()

	current, err := standingWatchDecisionTx(tx)
	if err != nil {
		return false, fmt.Errorf("offer standing watch stand-down: %w", err)
	}
	if current != StandingWatchEnabled {
		return false, nil
	}
	var enabledSeq int64
	if err := tx.QueryRow(`SELECT seq FROM events WHERE kind = ? ORDER BY seq DESC LIMIT 1`,
		EventStandingWatchEnabled).Scan(&enabledSeq); err != nil {
		return false, fmt.Errorf("offer standing watch stand-down: %w", err)
	}
	var already bool
	if err := tx.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM agent_questions WHERE seq > ? AND text LIKE ?)`,
		enabledSeq, "%"+standingWatchStandDownMarker+"%").Scan(&already); err != nil {
		return false, fmt.Errorf("offer standing watch stand-down: %w", err)
	}
	if already {
		return false, nil
	}

	options := []QuestionOption{
		{Label: "keep watching", Value: "standing-watch:enable"},
		{Label: "stand down", Value: "standing-watch:decline"},
	}
	question := agentQuestionPayload{
		SessionID: sessionID, Text: standingWatchStandDownQuestion,
		Urgency: QuestionNextNaturalMoment, Options: options,
	}
	questionSeq, questionAt, err := appendEvent(tx, RootID, EventAgentQuestionQueued, question)
	if err != nil {
		return false, fmt.Errorf("offer standing watch stand-down: %w", err)
	}
	if err := applyAgentQuestionView(tx, question, questionSeq, questionAt); err != nil {
		return false, fmt.Errorf("offer standing watch stand-down: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("offer standing watch stand-down: %w", err)
	}
	return true, nil
}

// standingWatchStandDownMarker is the substring the once-per-enablement gate
// recognizes its own question by. It is a slice of the question rather than a
// separate token because a marker that can drift out of the sentence it marks
// is a gate that silently stops working.
const standingWatchStandDownMarker = "Keep watching?"

// StandingWatchDecisionState returns the newest journal-derived choice.
func (s *Store) StandingWatchDecisionState() (StandingWatchDecision, error) {
	return standingWatchDecisionQuery(s.db)
}

func standingWatchDecisionTx(tx *sql.Tx) (StandingWatchDecision, error) {
	return standingWatchDecisionQuery(tx)
}

func standingWatchDecisionQuery(query rowQuerier) (StandingWatchDecision, error) {
	var kind EventKind
	err := query.QueryRow(`SELECT kind FROM events WHERE kind IN (?, ?, ?, ?)
		ORDER BY seq DESC LIMIT 1`, EventStandingWatchOffered,
		EventStandingWatchEnabled, EventStandingWatchDeclined, EventStandingWatchStoodDown).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return StandingWatchUndecided, nil
	}
	if err != nil {
		return StandingWatchUndecided, err
	}
	switch kind {
	case EventStandingWatchOffered:
		return StandingWatchOffered, nil
	case EventStandingWatchEnabled:
		return StandingWatchEnabled, nil
	case EventStandingWatchDeclined:
		return StandingWatchDeclined, nil
	case EventStandingWatchStoodDown:
		return StandingWatchStoodDown, nil
	default:
		return StandingWatchUndecided, nil
	}
}

// RecordStandingWatchPass records one completed `aforge wake` pass.
func (s *Store) RecordStandingWatchPass(pass StandingWatchPass) error {
	if !validStandingWatchPass(pass) {
		return fmt.Errorf("record standing watch pass: %w: negative count", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("record standing watch pass: %w", err)
	}
	defer tx.Rollback()
	if _, _, err := appendEvent(tx, RootID, EventStandingWatchPass, pass); err != nil {
		return fmt.Errorf("record standing watch pass: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record standing watch pass: %w", err)
	}
	return nil
}

// LastStandingWake returns the newest headless pass or charter wake/check/fire
// event, whichever is most recent.
func (s *Store) LastStandingWake() (time.Time, bool, error) {
	var timestamp string
	err := s.db.QueryRow(`SELECT ts FROM events WHERE kind IN (?, ?, ?, ?)
		ORDER BY seq DESC LIMIT 1`, EventStandingWatchPass, EventCharterWoken,
		EventSentinelChecked, EventCharterFired).Scan(&timestamp)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("last standing wake: %w", err)
	}
	at, err := parseTime(timestamp)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("last standing wake: parse time: %w", err)
	}
	return at, true, nil
}

func decodeStandingWatchEvent(kind EventKind, payload json.RawMessage) error {
	switch kind {
	case EventStandingWatchOffered:
		var value standingWatchOfferPayload
		if err := json.Unmarshal(payload, &value); err != nil {
			return err
		}
		if strings.TrimSpace(value.SessionID) == "" || strings.TrimSpace(value.CharterID) == "" {
			return fmt.Errorf("invalid standing watch offer")
		}
	case EventStandingWatchEnabled, EventStandingWatchDeclined, EventStandingWatchStoodDown:
		var value standingWatchDecisionPayload
		if err := json.Unmarshal(payload, &value); err != nil {
			return err
		}
		if strings.TrimSpace(value.Reason) == "" {
			return fmt.Errorf("invalid standing watch decision")
		}
	case EventStandingWatchPass:
		var value StandingWatchPass
		if err := json.Unmarshal(payload, &value); err != nil {
			return err
		}
		if !validStandingWatchPass(value) {
			return fmt.Errorf("invalid standing watch pass")
		}
	}
	return nil
}

func validStandingWatchPass(pass StandingWatchPass) bool {
	return pass.Examined >= 0 && pass.Woken >= 0 && pass.Checked >= 0 && pass.Fired >= 0 &&
		pass.Proposed >= 0 && pass.No >= 0 && pass.Errors >= 0 && pass.Quota >= 0 &&
		pass.Expired >= 0 && pass.RailWaits >= 0
}
