package store

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
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
	// Reason is why the claim was taken back, in the words a person reads. It
	// is empty for an ordinary cooperative release — a worker handing its own
	// node back says everything by handing it back — and it is REQUIRED of the
	// reaper, because a release nobody asked for is the one event in this
	// journal that an autopsy cannot otherwise explain. Four of these landed on
	// the ink run of 2026-08-29 with no reason recorded anywhere, and reading
	// the store afterwards could not tell a reaped claim from a worker's own
	// hand-back. See [Store.ReleaseSilent].
	Reason string `json:"reason,omitempty"`
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
	if id == RootID {
		// Last line of defense: whatever status the spine is in, it is never
		// claimable work. A pending root (a repair in progress, a historical
		// corruption) must wait for self-healing, not be run as a task.
		return Claim{}, false, nil
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

	tx, err := s.beginWrite()
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
		  AND held = 0
		  AND cancel_requested = 0
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
	tx, err := s.beginWrite()
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
	summary = bounded(summary, MaxSummaryBytes)
	tx, err := s.beginWrite()
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
	summary = bounded(summary, MaxSummaryBytes)
	tx, err := s.beginWrite()
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

	command = command.separated()
	payload := commandPayload{
		SessionID: command.SessionID, Kind: command.Kind, Target: command.Target,
		Instruction: command.Instruction, Context: command.Context,
		Attachments: append([]string(nil), command.Attachments...),
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
	tx, err := s.beginWrite()
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
	return s.release(claim, "")
}

// ReleaseWithReason is Release with the sentence that explains it, journaled on
// the release itself. Everything that takes a claim back from a worker that did
// not offer it uses this one, so the record always says who decided and why.
func (s *Store) ReleaseWithReason(claim Claim, reason string) error {
	return s.release(claim, reason)
}

func (s *Store) release(claim Claim, reason string) error {
	if claim.ID == RootID {
		// The spine root's Running status is structural — it is the permanent
		// trunk every job splices under, not a claim any worker holds.
		// Releasing it once made it pending, a runner claimed it, "completed"
		// it, and every later splice failed on a closed root.
		return fmt.Errorf("release %q: %w: the permanent spine is not a claim", claim.ID, ErrInvalid)
	}
	if claim.Token >= math.MaxInt64 {
		return fmt.Errorf("release %q: token exhausted", claim.ID)
	}
	tx, err := s.beginWrite()
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
		Reason: bounded(strings.TrimSpace(reason), MaxDigestBytes),
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

// ── the claim reaper reads EVIDENCE OF LIFE, never a clock ──────────────────
//
// THE DEFECT THIS ANSWERS, and it is the same defect twice. This sweep used to
// ask one question — has this node been running longer than the window — and a
// node's `started_at` is stamped once, when its claim was granted. So the answer
// was "how long has this worker been ALIVE", and the reaper took the node away
// from every worker that outlived one window's worth of wall clock, whether or
// not anybody was behind it.
//
// On the ink run of 2026-08-29 that arithmetic produced a metronome. `task-2`
// was released and re-claimed at 22m10s, 22m10s, 22m11s and 22m06s — four
// restarts, no fault, no hung call, a leaf making model calls thirty seconds
// before each one. The window was twenty-two minutes because it is derived from
// one leaf attempt's watchdog (see internal/resident's staleClaimAge and
// RaiseStaleAge), and the claim it was measuring legitimately holds TWO attempts:
// the executor's own deadline, the retry that a spent deadline earns, and the
// escalation that may follow. The clock had nothing to say about any of it.
//
// So the question is now the only one a reaper can be right about: HAS THIS NODE
// SHOWN A SIGN OF LIFE. A worker leaves three durable marks as it works — a
// usage row when a model call is billed, a per-turn usage row where the executor
// writes them, a transcript flush when its record fills a batch — and every one
// of them is written by the leaf itself, to this database, as it happens. The
// newest of those, floored at the claim's own start so an earlier attempt's rows
// can never keep a fresh claim alive, is when this node was last known to be
// worked. A claim silent for longer than the window is held by nobody; a claim
// that spoke inside it is held by somebody, however long it has been running.
//
// The old reading is also why the CAS below never protected anyone. "A live
// worker keeps its claim because the token has moved" is true of a worker that
// has FINISHED and false of one that is working: a leaf does not touch its own
// token between turns, so the release always succeeded, and the released node
// was re-claimed inside the same second while its first worker went on writing
// to the same workspace.

// SilentClaim is one claim this sweep found with nobody behind it: which node,
// how long it had been silent, and the sentence that says so. The sentence is
// carried out rather than composed at the caller because the numbers behind it
// are known here and nowhere else.
type SilentClaim struct {
	// ID is the node the claim was on.
	ID string
	// Quiet is how long it had been since the node last showed a sign of life.
	Quiet time.Duration
	// LastSign is when that sign was. It is the claim's own start when the
	// worker never wrote anything at all, which is the honest reading: the
	// claim was granted and nothing has happened since.
	LastSign time.Time
	// Reason is the release sentence, journaled on the release event.
	Reason string
}

// ReleaseSilent returns to pending every claimed or running node that has shown
// no sign of life for the given window, and journals why on each release.
//
// It exists for the live-run case ReleaseOrphans does not reach: a worker that
// dies mid-run — a process killed, a goroutine wedged past every deadline it was
// given — leaves its node running forever, and every downstream node gated on it
// waits behind a claim nobody holds any longer. The resident heartbeats the
// whole time and finds nothing to do, because the ready set never opens: that is
// the live-lock a long-horizon run dies of.
//
// A window of zero or less disarms the sweep entirely, which is what a caller
// that has not decided a window should get.
func (s *Store) ReleaseSilent(quietFor time.Duration) ([]SilentClaim, error) {
	if quietFor <= 0 {
		return nil, nil
	}
	now := time.Now()
	cutoff := formatTime(now.Add(-quietFor))
	// The sign of life is MAX over the claim's start and the three tables a
	// working leaf writes to as it goes. It is computed in the query rather
	// than per candidate because the sweep runs on the dispatch loop several
	// times a second, and three correlated subqueries over indexed node_id
	// columns is one pass instead of one round trip per running node.
	rows, err := s.db.Query(
		`SELECT id, owner, claim_token, cancel_requested, last_sign FROM (
		   SELECT nodes.id AS id, nodes.owner AS owner, nodes.claim_token AS claim_token,
		          nodes.cancel_requested AS cancel_requested,
		          MAX(nodes.started_at,
		              COALESCE((SELECT MAX(usage.ts) FROM usage WHERE usage.node_id = nodes.id), ''),
		              COALESCE((SELECT MAX(usage_turns.ts) FROM usage_turns WHERE usage_turns.node_id = nodes.id), ''),
		              COALESCE((SELECT MAX(transcript.ts) FROM transcript WHERE transcript.node_id = nodes.id), '')
		          ) AS last_sign
		   FROM nodes
		   WHERE nodes.status IN (?, ?) AND nodes.id != ? AND nodes.grp NOT IN (?)
		     AND nodes.started_at IS NOT NULL
		 ) WHERE last_sign < ?`,
		Claimed, Running, RootID, TerritoryGroup, cutoff)
	if err != nil {
		return nil, fmt.Errorf("release silent: %w", err)
	}
	type candidate struct {
		claim    Claim
		cancel   bool
		lastSign time.Time
	}
	candidates := make([]candidate, 0, 4)
	for rows.Next() {
		var item candidate
		var stamp string
		if err := rows.Scan(&item.claim.ID, &item.claim.Owner, &item.claim.Token, &item.cancel, &stamp); err != nil {
			rows.Close()
			return nil, fmt.Errorf("release silent: %w", err)
		}
		// A stamp that will not parse is not a reason to leave a claim standing
		// forever, but it is a reason not to invent a duration for it: the zero
		// time renders as the claim never having been seen alive, which is what
		// a row with no readable timestamp actually establishes.
		item.lastSign, _ = parseTime(stamp)
		candidates = append(candidates, item)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("release silent: %w", err)
	}
	released := make([]SilentClaim, 0, len(candidates))
	for _, item := range candidates {
		quiet := quietFor
		if !item.lastSign.IsZero() {
			quiet = now.Sub(item.lastSign)
		}
		reason := fmt.Sprintf(
			"no sign of life for %s — no model call and no recorded turn since it was picked up",
			quiet.Round(time.Second))
		if err := s.ReleaseWithReason(item.claim, reason); err != nil {
			continue
		}
		if item.cancel {
			_ = s.CancelPending(item.claim.ID, UserCancelReason)
		}
		released = append(released, SilentClaim{
			ID: item.claim.ID, Quiet: quiet, LastSign: item.lastSign, Reason: reason,
		})
	}
	return released, nil
}

// ReleaseOrphans returns every claimed or running node to pending. It exists
// for surface startup: the chat store has exactly one resident writer, so any
// claim found at open belongs to a process that died or was closed mid-run —
// the user watched a leaf sit "running" for 49 minutes with no worker behind
// it. Each release goes through the ordinary CAS path with the recorded
// owner and token, so the journal tells the truth and a genuinely live
// worker (a race at the margin) keeps its claim by failing our stale CAS.
func (s *Store) ReleaseOrphans() ([]string, error) {
	// The spine root is permanently Running by construction and organizational
	// furniture is never worked — neither is an orphan, and releasing the root
	// here once let a runner claim and close the trunk itself.
	rows, err := s.db.Query(
		`SELECT id, owner, claim_token, cancel_requested FROM nodes
		 WHERE status IN (?, ?) AND id != ? AND grp NOT IN (?)`,
		Claimed, Running, RootID, TerritoryGroup)
	if err != nil {
		return nil, fmt.Errorf("release orphans: %w", err)
	}
	type orphan struct {
		claim  Claim
		cancel bool
	}
	claims := make([]orphan, 0, 4)
	for rows.Next() {
		var item orphan
		if err := rows.Scan(&item.claim.ID, &item.claim.Owner, &item.claim.Token, &item.cancel); err != nil {
			rows.Close()
			return nil, fmt.Errorf("release orphans: %w", err)
		}
		claims = append(claims, item)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("release orphans: %w", err)
	}
	released := make([]string, 0, len(claims))
	for _, item := range claims {
		if err := s.Release(item.claim); err != nil {
			continue
		}
		if item.cancel {
			_ = s.CancelPending(item.claim.ID, UserCancelReason)
		}
		released = append(released, item.claim.ID)
	}
	if err := s.finishParkedCancellations(); err != nil {
		return released, err
	}
	return released, nil
}

// finishParkedCancellations completes a cancellation that was asked for and
// then never landed. The runner releases the claim first and cancels second, so
// a store write that fails between the two leaves the node Pending with
// cancel_requested still set — and that row is invisible in both directions:
// Ready excludes it, so nothing will ever claim it, and its parent stays open
// forever waiting for a child no worker will pick up. It is not an orphaned
// claim, which is why the sweep above never repaired it, but it is the same
// kind of wreckage and the same moment is the right one to clear it.
//
// Parked rows are not counted as released work: nobody is resuming them, and
// the surface's "picked up N pieces of work" line would be saying the opposite
// of what happened.
func (s *Store) finishParkedCancellations() error {
	rows, err := s.db.Query(
		`SELECT id, error FROM nodes
		 WHERE status = ? AND cancel_requested = 1 AND id != ?`,
		Pending, RootID)
	if err != nil {
		return fmt.Errorf("finish parked cancellations: %w", err)
	}
	type parked struct{ id, reason string }
	var stuck []parked
	for rows.Next() {
		var item parked
		if err := rows.Scan(&item.id, &item.reason); err != nil {
			rows.Close()
			return fmt.Errorf("finish parked cancellations: %w", err)
		}
		stuck = append(stuck, item)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("finish parked cancellations: %w", err)
	}
	for _, item := range stuck {
		reason := strings.TrimSpace(item.reason)
		if reason == "" {
			reason = UserCancelReason
		}
		// A refusal here is another writer having finished the row first, which
		// is the outcome this wanted anyway.
		_ = s.CancelPending(item.id, reason)
	}
	return nil
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
