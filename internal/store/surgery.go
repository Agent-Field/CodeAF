package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	// SurgerySpendGateUSD is recorded spend above which cancel/restart needs
	// explicit consent. It lives here rather than with the conversational head
	// because the same loss threshold now guards a second path: revision that
	// would throw away a leaf already running.
	SurgerySpendGateUSD = 0.25
	// SurgeryRuntimeGate is live runtime above which cancellation needs consent.
	SurgeryRuntimeGate = 5 * time.Minute
	// SurgeryCascadeGateNodes gates every operation that affects a larger tree.
	SurgeryCascadeGateNodes = 3
)

// NodeControl is the scheduler-visible part of conversational surgery.
// Cancel wins over Hold when both are present: a worker must relinquish its
// claim permanently rather than merely parking work the user withdrew.
type NodeControl struct {
	CancelRequested bool
	Held            bool
}

type nodeControlPayload struct {
	Reason string `json:"reason,omitempty"`
}

type nodePriorityPayload struct {
	Priority int    `json:"priority"`
	Reason   string `json:"reason,omitempty"`
}

// SurgeryImpact is the loss/cascade estimate used by conversational gates.
type SurgeryImpact struct {
	Nodes      int
	OpenNodes  int
	Running    int
	Cost       float64
	RunningFor time.Duration
}

// SurgeryTarget is one BM25-ranked graph referent with a user-facing age.
type SurgeryTarget struct {
	Node  Node
	Age   string
	Score float64
}

// RequestNodeCancel records cooperative cancellation for a claim owner to
// observe between model turns. Pending nodes use CancelPending directly.
func (s *Store) RequestNodeCancel(id, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "cancelled by user"
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("request node cancel: %w", err)
	}
	defer tx.Rollback()
	var status Status
	var requested bool
	if err := tx.QueryRow(`SELECT status, cancel_requested FROM nodes WHERE id = ? AND folded = 0`, id).
		Scan(&status, &requested); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("request node cancel: %w: %q", ErrNotFound, id)
		}
		return fmt.Errorf("request node cancel: %w", err)
	}
	if status != Claimed && status != Running {
		return fmt.Errorf("request node cancel: %w: %q is %s", ErrInvalid, id, status)
	}
	if requested {
		return nil
	}
	seq, _, err := appendEvent(tx, id, EventNodeCancelRequested, nodeControlPayload{Reason: reason})
	if err != nil {
		return fmt.Errorf("request node cancel: %w", err)
	}
	if err := applyNodeCancelRequestedView(tx, id, seq); err != nil {
		return fmt.Errorf("request node cancel: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("request node cancel: %w", err)
	}
	return nil
}

// SetNodeHold journals a scheduler hold or its release. Running claim owners
// observe the flag at their next executor boundary and Release back to pending.
func (s *Store) SetNodeHold(id string, held bool, reason string) error {
	reason = strings.TrimSpace(reason)
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("set node hold: %w", err)
	}
	defer tx.Rollback()
	var status Status
	var current bool
	if err := tx.QueryRow(`SELECT status, held FROM nodes WHERE id = ? AND folded = 0`, id).
		Scan(&status, &current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("set node hold: %w: %q", ErrNotFound, id)
		}
		return fmt.Errorf("set node hold: %w", err)
	}
	if terminal(status) {
		return fmt.Errorf("set node hold: %w: %q is %s", ErrInvalid, id, status)
	}
	if current == held {
		return nil
	}
	kind := EventNodeHeld
	if !held {
		kind = EventNodeResumed
	}
	seq, _, err := appendEvent(tx, id, kind, nodeControlPayload{Reason: reason})
	if err != nil {
		return fmt.Errorf("set node hold: %w", err)
	}
	if err := applyNodeHoldView(tx, id, held, seq); err != nil {
		return fmt.Errorf("set node hold: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("set node hold: %w", err)
	}
	return nil
}

// SetNodePriority changes claim order without altering dependencies.
func (s *Store) SetNodePriority(id string, priority int, reason string) error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("set node priority: %w", err)
	}
	defer tx.Rollback()
	if err := requirePending(tx, id, "set node priority"); err != nil {
		return err
	}
	var current int
	if err := tx.QueryRow(`SELECT priority FROM nodes WHERE id = ?`, id).Scan(&current); err != nil {
		return fmt.Errorf("set node priority: %w", err)
	}
	if current == priority {
		return nil
	}
	payload := nodePriorityPayload{Priority: priority, Reason: strings.TrimSpace(reason)}
	seq, _, err := appendEvent(tx, id, EventNodePriorityChanged, payload)
	if err != nil {
		return fmt.Errorf("set node priority: %w", err)
	}
	if err := applyNodePriorityView(tx, id, priority, seq); err != nil {
		return fmt.Errorf("set node priority: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("set node priority: %w", err)
	}
	return nil
}

// NextSiblingPriority returns a value that moves id ahead of every pending
// sibling while retaining deterministic order among all other nodes.
func (s *Store) NextSiblingPriority(id string) (int, error) {
	var parent sql.NullString
	var status Status
	if err := s.db.QueryRow(`SELECT parent_id, status FROM nodes WHERE id = ?`, id).Scan(&parent, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("next sibling priority: %w", err)
	}
	if status != Pending {
		return 0, fmt.Errorf("next sibling priority: %w: %q is %s", ErrInvalid, id, status)
	}
	var maximum int
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(priority), 0) FROM nodes WHERE parent_id IS ? AND status = ?`,
		parent, Pending).Scan(&maximum); err != nil {
		return 0, fmt.Errorf("next sibling priority: %w", err)
	}
	if maximum == int(^uint(0)>>1) {
		return 0, fmt.Errorf("next sibling priority: %w: priority exhausted", ErrInvalid)
	}
	return maximum + 1, nil
}

// AttachAmendment appends rather than rewrites the user's contract. For an
// active claim it also journals an anchored user message; the chat executor's
// existing steering mailbox delivers that exact text before its next turn.
func (s *Store) AttachAmendment(id, sessionID, instruction string) (bool, error) {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return false, fmt.Errorf("attach amendment: %w: empty instruction", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return false, fmt.Errorf("attach amendment: %w", err)
	}
	defer tx.Rollback()
	var brief string
	var status Status
	if err := tx.QueryRow(`SELECT brief, status FROM nodes WHERE id = ? AND folded = 0`, id).Scan(&brief, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, fmt.Errorf("attach amendment: %w: %q", ErrNotFound, id)
		}
		return false, fmt.Errorf("attach amendment: %w", err)
	}
	if status != Pending && status != Claimed && status != Running {
		return false, fmt.Errorf("attach amendment: %w: %q is %s", ErrInvalid, id, status)
	}
	brief = appendBoundedAmendment(brief, instruction)
	seq, _, err := appendEvent(tx, id, EventNodeAmended, nodeAmendedPayload{Brief: brief})
	if err != nil {
		return false, fmt.Errorf("attach amendment: %w", err)
	}
	if err := applyNodeAmendedView(tx, id, brief, "", seq); err != nil {
		return false, fmt.Errorf("attach amendment: %w", err)
	}
	active := status == Claimed || status == Running
	if active {
		payload := messagePayload{SessionID: sessionID, Role: RoleUser, Body: instruction, NodeID: id}
		messageSeq, at, err := appendEvent(tx, id, EventMessagePosted, payload)
		if err != nil {
			return false, fmt.Errorf("attach amendment steering: %w", err)
		}
		if err := applyMessageView(tx, payload, messageSeq, at); err != nil {
			return false, fmt.Errorf("attach amendment steering: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("attach amendment: %w", err)
	}
	return active, nil
}

func appendBoundedAmendment(brief, instruction string) string {
	suffix := "\n\nAmendment: " + instruction
	if len(suffix) >= MaxDigestBytes {
		return bounded("Amendment: "+instruction, MaxDigestBytes)
	}
	brief = strings.TrimSpace(brief)
	if len(brief)+len(suffix) > MaxDigestBytes {
		brief = bounded(brief, MaxDigestBytes-len(suffix))
	}
	return brief + suffix
}

// Control returns the direct durable control flags for one node.
func (s *Store) Control(id string) (NodeControl, error) {
	var control NodeControl
	if err := s.db.QueryRow(`SELECT cancel_requested, held FROM nodes WHERE id = ?`, id).
		Scan(&control.CancelRequested, &control.Held); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NodeControl{}, ErrNotFound
		}
		return NodeControl{}, fmt.Errorf("read node control: %w", err)
	}
	return control, nil
}

// Impact totals the target subtree's durable spend and live runtime.
func (s *Store) Impact(id string, now time.Time) (SurgeryImpact, error) {
	if now.IsZero() {
		now = time.Now()
	}
	rows, err := s.db.Query(`
		WITH RECURSIVE descendants(id) AS (
			SELECT id FROM nodes WHERE id = ?
			UNION ALL
			SELECT child.id FROM descendants JOIN nodes AS child ON child.parent_id = descendants.id
		)
		SELECT node.status, node.started_at, COALESCE(SUM(usage.cost), 0)
		FROM descendants
		JOIN nodes AS node ON node.id = descendants.id
		LEFT JOIN usage ON usage.node_id = node.id
		GROUP BY node.id, node.status, node.started_at`, id)
	if err != nil {
		return SurgeryImpact{}, fmt.Errorf("surgery impact: %w", err)
	}
	defer rows.Close()
	var impact SurgeryImpact
	for rows.Next() {
		var status Status
		var started sql.NullString
		var cost float64
		if err := rows.Scan(&status, &started, &cost); err != nil {
			return SurgeryImpact{}, fmt.Errorf("surgery impact: %w", err)
		}
		impact.Nodes++
		impact.Cost += cost
		if !terminal(status) {
			impact.OpenNodes++
		}
		if status == Running || status == Claimed {
			impact.Running++
			if started.Valid {
				at, err := parseTime(started.String)
				if err != nil {
					return SurgeryImpact{}, fmt.Errorf("surgery impact: %w", err)
				}
				if elapsed := now.Sub(at); elapsed > impact.RunningFor {
					impact.RunningFor = elapsed
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return SurgeryImpact{}, fmt.Errorf("surgery impact: %w", err)
	}
	if impact.Nodes == 0 {
		return SurgeryImpact{}, fmt.Errorf("surgery impact: %w: %q", ErrNotFound, id)
	}
	return impact, nil
}

// SearchSurgeryTargets mirrors charter reference resolution with a small
// in-memory BM25 index over the current snapshot. Title and brief dominate;
// verbatim intent and landed summary still make ordinary user wording work.
//
// A statusless search — the read, as opposed to the verb — also reaches fold
// roots, and that is a correction. Every folded node was excluded here, which is
// right for a fold's members and wrong for its root: folding is what happens to
// a job once it is thoroughly over, and a job that is over is exactly the one a
// person asks about the next day. The transcript was a settled, distilled and
// folded architecture review, asked about in a fresh session, answered with
// "nothing in the current graph or notebook matches" — honestly, because the
// search could not see it. A fold root durably carries the digest and the
// pointers to what it wrote; its members are represented by it and stay hidden,
// because the root speaks for them.
//
// The eligibility is deliberately tied to the absence of a status filter rather
// than tested at each call site. Every caller that intends to act supplies the
// statuses its verb may legally touch, and no verb may touch settled work; every
// caller that intends to read supplies none. So the same argument that already
// separates reading from acting decides this, and no folded node can be reached
// by a verb through a route that did not exist before.
// The same argument decides the corpus. A statusless read searches the
// addressable graph — which includes the jobs a territory packed away — and a
// verb searches the compact one it always did. Filing a settled job under a
// territory was never meant to decide whether it can be spoken about, and it
// was deciding exactly that: the read that opens the head's toolbelt starts
// here, so a January job being tidied in February made every later question
// about it fall through to a router with no January in its context at all.
func (s *Store) SearchSurgeryTargets(reference string, includeLeaves bool, allowed ...Status) ([]SurgeryTarget, error) {
	allowedSet := make(map[Status]bool, len(allowed))
	for _, status := range allowed {
		allowedSet[status] = true
	}
	read := func() ([]Node, error) {
		if len(allowedSet) == 0 {
			return s.AddressableNodes()
		}
		return s.ActiveNodes()
	}
	nodes, err := read()
	if err != nil {
		return nil, err
	}
	byID := make(map[string]Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	readOnly := len(allowedSet) == 0
	eligible := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		if node.ID == RootID || node.Group == TerritoryGroup || node.Group == "charter" ||
			(len(allowedSet) > 0 && !allowedSet[node.Status]) {
			continue
		}
		if node.Folded && !(readOnly && node.FoldRoot) {
			continue
		}
		if !includeLeaves && !isSurgeryJobRoot(node, byID) {
			continue
		}
		eligible = append(eligible, node)
	}
	terms := surgeryTerms(reference)
	type document struct {
		node   Node
		terms  map[string]float64
		length float64
		score  float64
	}
	documents := make([]document, 0, len(eligible))
	df := make(map[string]int)
	totalLength := 0.0
	for _, node := range eligible {
		weighted := make(map[string]float64)
		addWeightedTerms(weighted, node.Title, 6)
		addWeightedTerms(weighted, node.Brief, 3)
		addWeightedTerms(weighted, node.ID, 2)
		addWeightedTerms(weighted, node.Summary, 2)
		addWeightedTerms(weighted, node.Provenance.Intent, 1)
		length := 0.0
		for term, count := range weighted {
			length += count
			if containsTerm(terms, term) {
				df[term]++
			}
		}
		documents = append(documents, document{node: node, terms: weighted, length: length})
		totalLength += length
	}
	if len(documents) == 0 {
		return nil, nil
	}
	averageLength := totalLength / float64(len(documents))
	if averageLength == 0 {
		averageLength = 1
	}
	maxScore := 0.0
	for index := range documents {
		if len(terms) == 0 {
			documents[index].score = 1
			continue
		}
		for _, term := range terms {
			tf := documents[index].terms[term]
			if tf == 0 {
				continue
			}
			idf := math.Log(1 + (float64(len(documents)-df[term])+0.5)/(float64(df[term])+0.5))
			normalizer := tf + 1.2*(0.25+0.75*documents[index].length/averageLength)
			documents[index].score += idf * tf * 2.2 / normalizer
		}
		lowerReference := strings.ToLower(strings.TrimSpace(reference))
		if lowerReference != "" && strings.Contains(strings.ToLower(documents[index].node.Title), lowerReference) {
			documents[index].score += 4
		}
		if documents[index].score > maxScore {
			maxScore = documents[index].score
		}
	}
	sort.SliceStable(documents, func(i, j int) bool {
		if documents[i].score != documents[j].score {
			return documents[i].score > documents[j].score
		}
		return documents[i].node.UpdatedSeq > documents[j].node.UpdatedSeq
	})
	results := make([]SurgeryTarget, 0, len(documents))
	for _, document := range documents {
		if len(terms) > 0 && (document.score == 0 || document.score < maxScore*0.45) {
			continue
		}
		var timestamp string
		at := time.Time{}
		if err := s.db.QueryRow(`SELECT ts FROM events WHERE seq = ?`, document.node.CreatedSeq).Scan(&timestamp); err == nil {
			at, _ = parseTime(timestamp)
		}
		results = append(results, SurgeryTarget{
			Node: document.node, Age: AgeLabel(at, time.Now()), Score: document.score,
		})
		if len(results) == 8 {
			break
		}
	}
	return results, nil
}

// RedirectOptionValue encodes one answer to a redirection askback. Both ends
// of the option are somewhere else — the head asks which job the user meant,
// the reconciler asks whether a running leaf may be thrown away — so the shape
// belongs with the durable option rather than with either speaker.
func RedirectOptionValue(action, target, message string) string {
	return strings.Join([]string{"redirect", action, target,
		base64.RawURLEncoding.EncodeToString([]byte(message))}, ":")
}

// DecodeRedirectOption reverses RedirectOptionValue. A value from any other
// family decodes as not-ok rather than as an error.
func DecodeRedirectOption(value string) (action, target, message string, ok bool) {
	parts := strings.SplitN(value, ":", 4)
	if len(parts) != 4 || parts[0] != "redirect" {
		return "", "", "", false
	}
	switch parts[1] {
	// "correct" is the settled twin of "apply": the same question — which work
	// did you mean — answered about a deliverable rather than a running plan.
	case "apply", "new", "correct", "cancel", "keep":
	default:
		return "", "", "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return "", "", "", false
	}
	return parts[1], parts[2], string(decoded), true
}

func isSurgeryJobRoot(node Node, byID map[string]Node) bool {
	if node.Parent == RootID {
		return true
	}
	parent, ok := byID[node.Parent]
	return ok && parent.Group == TerritoryGroup
}

func surgeryTerms(value string) []string {
	seen := make(map[string]bool)
	var terms []string
	for _, term := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		if len(term) < 2 || seen[term] {
			continue
		}
		seen[term] = true
		terms = append(terms, term)
	}
	return terms
}

func addWeightedTerms(target map[string]float64, value string, weight float64) {
	for _, term := range surgeryTerms(value) {
		target[term] += weight
	}
}

func containsTerm(terms []string, candidate string) bool {
	for _, term := range terms {
		if term == candidate {
			return true
		}
	}
	return false
}

func applyNodeCancelRequestedView(tx *sql.Tx, id string, seq int64) error {
	return replayUpdate(tx, id, `UPDATE nodes SET cancel_requested = 1, updated_seq = ? WHERE id = ?`, seq, id)
}

func applyNodeHoldView(tx *sql.Tx, id string, held bool, seq int64) error {
	return replayUpdate(tx, id, `UPDATE nodes SET held = ?, updated_seq = ? WHERE id = ?`, held, seq, id)
}

func applyNodePriorityView(tx *sql.Tx, id string, priority int, seq int64) error {
	return replayUpdate(tx, id, `UPDATE nodes SET priority = ?, updated_seq = ? WHERE id = ?`, priority, seq, id)
}

func decodeNodeControlPayload(raw json.RawMessage) error {
	var payload nodeControlPayload
	return json.Unmarshal(raw, &payload)
}
