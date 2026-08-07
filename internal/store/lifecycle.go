package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

type claimPayload struct {
	Owner string `json:"owner"`
	Token uint64 `json:"token"`
}

type completePayload struct {
	Owner   string `json:"owner"`
	Token   uint64 `json:"token"`
	Summary string `json:"summary"`
}

type failPayload struct {
	Owner   string `json:"owner"`
	Token   uint64 `json:"token"`
	Message string `json:"message"`
}

type releasePayload struct {
	Owner     string `json:"owner"`
	Token     uint64 `json:"token"`
	NextToken uint64 `json:"next_token"`
}

// Claim atomically moves a ready pending node to claimed. The UPDATE includes
// both the observed token and pending state: workers racing from separate
// processes may observe the same candidate, but only one can change it.
func (s *Store) Claim(id, owner string) (Claim, bool, error) {
	id = strings.TrimSpace(id)
	owner = strings.TrimSpace(owner)
	if id == "" || owner == "" {
		return Claim{}, false, fmt.Errorf("claim: %w: id and owner are required", ErrInvalid)
	}

	var observed uint64
	if err := s.db.QueryRow(`SELECT claim_token FROM nodes WHERE id = ?`, id).Scan(&observed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Claim{}, false, nil
		}
		return Claim{}, false, fmt.Errorf("claim %q: %w", id, err)
	}
	if observed >= math.MaxInt64 {
		return Claim{}, false, fmt.Errorf("claim %q: token exhausted", id)
	}

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return Claim{}, false, fmt.Errorf("claim %q: %w", id, err)
	}
	defer tx.Rollback()

	result, err := tx.Exec(`
		UPDATE nodes
		SET status = ?, owner = ?, claim_token = claim_token + 1, attempt = attempt + 1
		WHERE id = ?
		  AND status = ?
		  AND folded = 0
		  AND claim_token = ?
		  AND NOT EXISTS (
		      SELECT 1
		      FROM edges AS edge
		      JOIN nodes AS dependency ON dependency.id = edge.from_id
		      WHERE edge.to_id = ?
		        AND edge.kind IN (?, ?)
		        AND dependency.status NOT IN (?, ?, ?)
		  )`,
		Claimed, owner, id, Pending, observed, id,
		FeedsInto, Blocks, Done, Failed, Cancelled)
	if err != nil {
		return Claim{}, false, fmt.Errorf("claim %q: %w", id, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Claim{}, false, fmt.Errorf("claim %q: %w", id, err)
	}
	if changed == 0 {
		return Claim{}, false, nil
	}

	claim := Claim{ID: id, Owner: owner, Token: observed + 1}
	seq, _, err := appendEvent(tx, id, EventNodeClaimed, claimPayload{Owner: owner, Token: claim.Token})
	if err != nil {
		return Claim{}, false, fmt.Errorf("claim %q: %w", id, err)
	}
	if _, err := tx.Exec(`UPDATE nodes SET updated_seq = ? WHERE id = ? AND claim_token = ?`, seq, id, claim.Token); err != nil {
		return Claim{}, false, fmt.Errorf("materialize claim %q: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return Claim{}, false, fmt.Errorf("claim %q: %w", id, err)
	}
	return claim, true, nil
}

// Start moves a claimed node to running.
func (s *Store) Start(claim Claim) error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("start %q: %w", claim.ID, err)
	}
	defer tx.Rollback()

	result, err := tx.Exec(`
		UPDATE nodes SET status = ?
		WHERE id = ? AND owner = ? AND claim_token = ? AND status = ?`,
		Running, claim.ID, claim.Owner, claim.Token, Claimed)
	if err != nil {
		return fmt.Errorf("start %q: %w", claim.ID, err)
	}
	if err := requireChanged(tx, result, claim, Claimed); err != nil {
		return fmt.Errorf("start %q: %w", claim.ID, err)
	}
	seq, at, err := appendEvent(tx, claim.ID, EventNodeStarted, claimPayload{Owner: claim.Owner, Token: claim.Token})
	if err != nil {
		return fmt.Errorf("start %q: %w", claim.ID, err)
	}
	if _, err := tx.Exec(`UPDATE nodes SET started_at = ?, updated_seq = ? WHERE id = ?`, formatTime(at), seq, claim.ID); err != nil {
		return fmt.Errorf("materialize start %q: %w", claim.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("start %q: %w", claim.ID, err)
	}
	return nil
}

// Complete settles a claimed or running node successfully. Composite nodes
// cannot complete while any child remains open.
func (s *Store) Complete(claim Claim, summary string) error {
	summary = bounded(summary, MaxDigestBytes)
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("complete %q: %w", claim.ID, err)
	}
	defer tx.Rollback()

	if err := validateClaim(tx, claim, Claimed, Running); err != nil {
		return fmt.Errorf("complete %q: %w", claim.ID, err)
	}
	var openChild string
	err = tx.QueryRow(`
		SELECT id FROM nodes
		WHERE parent_id = ? AND status NOT IN (?, ?, ?)
		ORDER BY created_seq, id LIMIT 1`, claim.ID, Done, Failed, Cancelled).Scan(&openChild)
	if err == nil {
		return fmt.Errorf("complete %q: %w %q", claim.ID, ErrOpenChild, openChild)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("complete %q: inspect children: %w", claim.ID, err)
	}

	result, err := tx.Exec(`
		UPDATE nodes SET status = ?, summary = ?
		WHERE id = ? AND owner = ? AND claim_token = ? AND status IN (?, ?)`,
		Done, summary, claim.ID, claim.Owner, claim.Token, Claimed, Running)
	if err != nil {
		return fmt.Errorf("complete %q: %w", claim.ID, err)
	}
	if err := requireChanged(tx, result, claim, Claimed, Running); err != nil {
		return fmt.Errorf("complete %q: %w", claim.ID, err)
	}
	payload := completePayload{Owner: claim.Owner, Token: claim.Token, Summary: summary}
	seq, at, err := appendEvent(tx, claim.ID, EventNodeCompleted, payload)
	if err != nil {
		return fmt.Errorf("complete %q: %w", claim.ID, err)
	}
	if _, err := tx.Exec(`UPDATE nodes SET finished_at = ?, updated_seq = ? WHERE id = ?`, formatTime(at), seq, claim.ID); err != nil {
		return fmt.Errorf("materialize completion %q: %w", claim.ID, err)
	}
	if err := refreshGraphFTS(tx, claim.ID); err != nil {
		return fmt.Errorf("index completion %q: %w", claim.ID, err)
	}
	if err := recordSelfReceipt(tx, claim.ID); err != nil {
		return fmt.Errorf("receipt for completion %q: %w", claim.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("complete %q: %w", claim.ID, err)
	}
	return nil
}

// CompleteAndRequestFollowup atomically settles one node and journals the
// ordinary splice that will continue it. Reflex promotion uses this instead of
// two writes so a process exit can leave neither a lost promotion nor a command
// whose partial is still absent from the graph.
func (s *Store) CompleteAndRequestFollowup(claim Claim, summary string, command Command) (Command, error) {
	if command.Kind != CommandSplice || command.Reflex || command.Target != claim.ID ||
		strings.TrimSpace(command.Instruction) == "" {
		return Command{}, fmt.Errorf("complete %q with follow-up: %w: follow-up must be an ordinary splice targeted at the completed node", claim.ID, ErrInvalid)
	}
	summary = bounded(summary, MaxDigestBytes)
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return Command{}, fmt.Errorf("complete %q with follow-up: %w", claim.ID, err)
	}
	defer tx.Rollback()

	if err := validateClaim(tx, claim, Claimed, Running); err != nil {
		return Command{}, fmt.Errorf("complete %q with follow-up: %w", claim.ID, err)
	}
	var openChild string
	err = tx.QueryRow(`
		SELECT id FROM nodes
		WHERE parent_id = ? AND status NOT IN (?, ?, ?)
		ORDER BY created_seq, id LIMIT 1`, claim.ID, Done, Failed, Cancelled).Scan(&openChild)
	if err == nil {
		return Command{}, fmt.Errorf("complete %q with follow-up: %w %q", claim.ID, ErrOpenChild, openChild)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Command{}, fmt.Errorf("complete %q with follow-up: inspect children: %w", claim.ID, err)
	}

	result, err := tx.Exec(`
		UPDATE nodes SET status = ?, summary = ?
		WHERE id = ? AND owner = ? AND claim_token = ? AND status IN (?, ?)`,
		Done, summary, claim.ID, claim.Owner, claim.Token, Claimed, Running)
	if err != nil {
		return Command{}, fmt.Errorf("complete %q with follow-up: %w", claim.ID, err)
	}
	if err := requireChanged(tx, result, claim, Claimed, Running); err != nil {
		return Command{}, fmt.Errorf("complete %q with follow-up: %w", claim.ID, err)
	}
	completion := completePayload{Owner: claim.Owner, Token: claim.Token, Summary: summary}
	completionSeq, completedAt, err := appendEvent(tx, claim.ID, EventNodeCompleted, completion)
	if err != nil {
		return Command{}, fmt.Errorf("complete %q with follow-up: %w", claim.ID, err)
	}
	if _, err := tx.Exec(`UPDATE nodes SET finished_at = ?, updated_seq = ? WHERE id = ?`,
		formatTime(completedAt), completionSeq, claim.ID); err != nil {
		return Command{}, fmt.Errorf("materialize completion %q with follow-up: %w", claim.ID, err)
	}
	if err := refreshGraphFTS(tx, claim.ID); err != nil {
		return Command{}, fmt.Errorf("index completion %q with follow-up: %w", claim.ID, err)
	}
	if err := recordSelfReceipt(tx, claim.ID); err != nil {
		return Command{}, fmt.Errorf("receipt for completion %q with follow-up: %w", claim.ID, err)
	}

	payload := commandPayload{
		SessionID: command.SessionID, Kind: command.Kind, Target: command.Target,
		Instruction: command.Instruction, Attachments: append([]string(nil), command.Attachments...),
	}
	commandSeq, commandAt, err := appendEvent(tx, command.Target, EventCommandRequested, payload)
	if err != nil {
		return Command{}, fmt.Errorf("request follow-up for %q: %w", claim.ID, err)
	}
	if err := applyCommandView(tx, payload, commandSeq, commandAt); err != nil {
		return Command{}, fmt.Errorf("materialize follow-up for %q: %w", claim.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return Command{}, fmt.Errorf("complete %q with follow-up: %w", claim.ID, err)
	}
	command.Seq = commandSeq
	command.Time = commandAt
	command.Status = CommandPending
	command.UpdatedSeq = commandSeq
	return command, nil
}

// Fail settles a claimed or running node unsuccessfully. Failed is terminal,
// so dependents become ready and receive the failure digest instead of being
// stranded.
func (s *Store) Fail(claim Claim, message string) error {
	message = bounded(message, MaxDigestBytes)
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("fail %q: %w", claim.ID, err)
	}
	defer tx.Rollback()

	result, err := tx.Exec(`
		UPDATE nodes SET status = ?, error = ?
		WHERE id = ? AND owner = ? AND claim_token = ? AND status IN (?, ?)`,
		Failed, message, claim.ID, claim.Owner, claim.Token, Claimed, Running)
	if err != nil {
		return fmt.Errorf("fail %q: %w", claim.ID, err)
	}
	if err := requireChanged(tx, result, claim, Claimed, Running); err != nil {
		return fmt.Errorf("fail %q: %w", claim.ID, err)
	}
	payload := failPayload{Owner: claim.Owner, Token: claim.Token, Message: message}
	seq, at, err := appendEvent(tx, claim.ID, EventNodeFailed, payload)
	if err != nil {
		return fmt.Errorf("fail %q: %w", claim.ID, err)
	}
	if _, err := tx.Exec(`UPDATE nodes SET finished_at = ?, updated_seq = ? WHERE id = ?`, formatTime(at), seq, claim.ID); err != nil {
		return fmt.Errorf("materialize failure %q: %w", claim.ID, err)
	}
	if err := recordSelfReceipt(tx, claim.ID); err != nil {
		return fmt.Errorf("receipt for failure %q: %w", claim.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("fail %q: %w", claim.ID, err)
	}
	return nil
}

// Release returns claimed or running work to pending and increments the token
// again. The extra increment is what makes the released Claim stale before a
// replacement worker even arrives.
func (s *Store) Release(claim Claim) error {
	if claim.Token >= math.MaxInt64 {
		return fmt.Errorf("release %q: token exhausted", claim.ID)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("release %q: %w", claim.ID, err)
	}
	defer tx.Rollback()

	result, err := tx.Exec(`
		UPDATE nodes
		SET status = ?, owner = '', claim_token = claim_token + 1, started_at = NULL
		WHERE id = ? AND owner = ? AND claim_token = ? AND status IN (?, ?)`,
		Pending, claim.ID, claim.Owner, claim.Token, Claimed, Running)
	if err != nil {
		return fmt.Errorf("release %q: %w", claim.ID, err)
	}
	if err := requireChanged(tx, result, claim, Claimed, Running); err != nil {
		return fmt.Errorf("release %q: %w", claim.ID, err)
	}
	payload := releasePayload{
		Owner: claim.Owner, Token: claim.Token, NextToken: claim.Token + 1,
	}
	seq, _, err := appendEvent(tx, claim.ID, EventNodeReleased, payload)
	if err != nil {
		return fmt.Errorf("release %q: %w", claim.ID, err)
	}
	if _, err := tx.Exec(`UPDATE nodes SET updated_seq = ? WHERE id = ?`, seq, claim.ID); err != nil {
		return fmt.Errorf("materialize release %q: %w", claim.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("release %q: %w", claim.ID, err)
	}
	return nil
}

// ReleaseOrphans returns every claimed or running node to pending. It exists
// for surface startup: the chat store has exactly one resident writer, so any
// claim found at open belongs to a process that died or was closed mid-run —
// the user watched a leaf sit "running" for 49 minutes with no worker behind
// it. Each release goes through the ordinary CAS path with the recorded
// owner and token, so the journal tells the truth and a genuinely live
// worker (a race at the margin) keeps its claim by failing our stale CAS.
func (s *Store) ReleaseOrphans() ([]string, error) {
	rows, err := s.db.Query(
		`SELECT id, owner, claim_token FROM nodes WHERE status IN (?, ?)`,
		Claimed, Running)
	if err != nil {
		return nil, fmt.Errorf("release orphans: %w", err)
	}
	claims := make([]Claim, 0, 4)
	for rows.Next() {
		var claim Claim
		if err := rows.Scan(&claim.ID, &claim.Owner, &claim.Token); err != nil {
			rows.Close()
			return nil, fmt.Errorf("release orphans: %w", err)
		}
		claims = append(claims, claim)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("release orphans: %w", err)
	}
	released := make([]string, 0, len(claims))
	for _, claim := range claims {
		if err := s.Release(claim); err != nil {
			continue
		}
		released = append(released, claim.ID)
	}
	return released, nil
}

func requireChanged(tx *sql.Tx, result sql.Result, claim Claim, allowed ...Status) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 1 {
		return nil
	}
	if err := validateClaim(tx, claim, allowed...); err != nil {
		return err
	}
	return errors.New("claim transition changed no rows")
}

func validateClaim(tx *sql.Tx, claim Claim, allowed ...Status) error {
	var owner string
	var token uint64
	var status Status
	if err := tx.QueryRow(`SELECT owner, claim_token, status FROM nodes WHERE id = ?`, claim.ID).Scan(&owner, &token, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if owner != claim.Owner || token != claim.Token {
		return ErrClaimLost
	}
	for _, candidate := range allowed {
		if status == candidate {
			return nil
		}
	}
	return fmt.Errorf("node is %s", status)
}

func bounded(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 3 {
		cut := limit
		for cut > 0 && !utf8.ValidString(value[:cut]) {
			cut--
		}
		return value[:cut]
	}
	cut := limit - 3
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	return strings.TrimSpace(value[:cut]) + "..."
}
