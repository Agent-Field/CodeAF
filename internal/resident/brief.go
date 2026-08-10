package resident

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
)

// DefaultBriefAfter is long enough that an ordinary lunch break stays quiet.
const DefaultBriefAfter = 4 * time.Hour

// BriefEvent is one journal fact the composer may turn into a slim fold row.
// Text is already a truthful fallback; the model's job is voice and compression.
type BriefEvent struct {
	Seq  int64               `json:"seq"`
	Time time.Time           `json:"time"`
	Kind store.BriefItemKind `json:"kind"`
	Text string              `json:"text"`
	Ref  string              `json:"ref,omitempty"`
}

// BriefActivity is the bounded, factual input to one arrival composition.
type BriefActivity struct {
	Since         time.Time    `json:"since"`
	Events        []BriefEvent `json:"events"`
	Done          int          `json:"done"`
	Failed        int          `json:"failed"`
	Cancelled     int          `json:"cancelled"`
	Questions     int          `json:"questions"`
	CharterFired  int          `json:"charter_fired"`
	FactsLearned  int          `json:"facts_learned"`
	SkillsLearned int          `json:"skills_learned"`
	CraftsForged  int          `json:"crafts_forged"`
	CostUSD       float64      `json:"cost_usd"`
	// Waiting counts the standing rows: what is stopped on the user right now,
	// as opposed to what happened while they were gone.
	Waiting int `json:"waiting"`
}

// BriefDraftItem is the resident voice for one input event, joined by Seq.
type BriefDraftItem struct {
	Seq  int64  `json:"seq"`
	Body string `json:"body"`
}

// BriefDraft is one model response: the closed sentence and expanded rows.
type BriefDraft struct {
	Headline string           `json:"headline"`
	Items    []BriefDraftItem `json:"items"`
}

// BriefComposeFunc makes one arrival message from journal-derived facts.
type BriefComposeFunc func(ctx context.Context, activity BriefActivity) (BriefDraft, error)

// WithBriefComposer installs the resident-only model seam used at session open.
func (r *Reconciler) WithBriefComposer(compose BriefComposeFunc) *Reconciler {
	r.composeBrief = compose
	return r
}

// SessionOpened journals an attach edge, then posts at most one folded arrival
// message for qualifying activity since the previous seen watermark.
func (r *Reconciler) SessionOpened(ctx context.Context, sessionID, surface string, after time.Duration) error {
	deliver, err := r.SessionOpening(sessionID, surface, after)
	if err != nil {
		return err
	}
	if deliver == nil {
		return nil
	}
	return deliver(ctx)
}

// SessionOpening is SessionOpened split at its one slow seam, for a surface
// that must not make the user watch it.
//
// The half that runs here is the half whose ordering matters: reading the
// previous seen watermark and journalling the attach edge, both cheap. The
// half it hands back is the expensive one — a journal gather, a model
// round-trip, and the post — and it is already bounded by the window this call
// fixed, so running it later cannot widen or move what the brief covers. The
// thread is the delivery channel either way; a brief that arrives a moment
// after the surface does arrives in exactly the same place.
//
// Nil means there is nothing to say and nothing to wait for.
func (r *Reconciler) SessionOpening(sessionID, surface string, after time.Duration) (func(context.Context) error, error) {
	if r.store == nil {
		return nil, fmt.Errorf("open resident session: nil store")
	}
	if after < 0 {
		return nil, fmt.Errorf("open resident session: %w: negative brief threshold", store.ErrInvalid)
	}
	previous, found, err := r.store.LastSeen()
	if err != nil {
		return nil, fmt.Errorf("open resident session: %w", err)
	}
	attached, err := r.store.TouchSeen(surface, sessionID, store.SeenAttached)
	if err != nil {
		return nil, fmt.Errorf("open resident session: %w", err)
	}
	if !found || attached.Time.Sub(previous.Time) < after || r.composeBrief == nil {
		return nil, nil
	}
	throughSeq := r.arrivalBoundary(sessionID, previous.Seq, attached.Seq-1)
	return func(ctx context.Context) error {
		activity, err := r.briefActivity(previous, throughSeq)
		if err != nil {
			return fmt.Errorf("open resident session: gather brief: %w", err)
		}
		if len(activity.Events) == 0 {
			return nil
		}
		draft, err := r.composeBrief(ctx, activity)
		if err != nil {
			return fmt.Errorf("open resident session: compose brief: %w", err)
		}
		message := materializeBrief(previous.Seq, throughSeq, activity, draft)
		message.SessionID = sessionID
		if _, err := thread.Post(r.store, message); err != nil {
			return fmt.Errorf("open resident session: post brief: %w", err)
		}
		return nil
	}, nil
}

// SessionClosed journals the user's attention leaving this surface.
func (r *Reconciler) SessionClosed(sessionID, surface string) error {
	if r.store == nil {
		return fmt.Errorf("close resident session: nil store")
	}
	_, err := r.store.TouchSeen(surface, sessionID, store.SeenDetached)
	if err != nil {
		return fmt.Errorf("close resident session: %w", err)
	}
	return nil
}

// arrivalBoundary closes the away-window where the user's arrival began.
//
// AttachSession expires, rescues and surfaces questions before SessionOpening
// journals the attach edge, so every one of those messages sits below that edge
// and inside the window — and the brief then reported "2 questions" for
// questions the act of opening the app had raised a millisecond earlier. The
// mark AttachSession takes is the honest end of "while you were away". Falling
// back to the attach edge keeps every embedding path that never calls
// AttachSession behaving exactly as before.
func (r *Reconciler) arrivalBoundary(sessionID string, sinceSeq, attachSeq int64) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.arrivalSession != strings.TrimSpace(sessionID) {
		return attachSeq
	}
	if r.arrivalSeq < sinceSeq || r.arrivalSeq >= attachSeq {
		return attachSeq
	}
	return r.arrivalSeq
}

func (r *Reconciler) briefActivity(previous store.Seen, throughSeq int64) (BriefActivity, error) {
	activity := BriefActivity{Since: previous.Time}
	// The window is known before the read, so it belongs in the query rather
	// than in a break at the top of the loop: the journal is append-only and
	// the tail past the watermark grows forever.
	events, err := r.store.EventsThrough(previous.Seq, throughSeq)
	if err != nil {
		return activity, err
	}
	// Only nodes are read from here, never edges, and only to recognise a job
	// root. Folding collapses a landed job onto its own root, which stays in
	// the active view as that fold's outermost representative — so the compact
	// view answers this question with the same rows as the full one, without
	// deserializing folded history or joining the whole edge table.
	nodes, err := r.store.ActiveNodes()
	if err != nil {
		return activity, err
	}
	byID := make(map[string]store.Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	newSkills := make(map[int64]bool)
	var spentSeq int64
	for _, event := range events {
		switch event.Kind {
		case store.EventNodeCompleted, store.EventNodeFailed, store.EventNodeCancelled:
			node, ok := byID[event.NodeID]
			if !ok || !briefJobRoot(node, byID) {
				continue
			}
			label := briefNodeLabel(node)
			if event.Kind == store.EventNodeCompleted {
				activity.Done++
				detail := firstLine(node.Summary)
				if detail == "" {
					detail = label + " finished."
				}
				activity.Events = append(activity.Events, BriefEvent{
					Seq: event.Seq, Time: event.Time, Kind: store.BriefDone,
					Text: label + " — " + detail, Ref: node.ID,
				})
				continue
			}
			if event.Kind == store.EventNodeCancelled {
				var payload struct {
					Reason string `json:"reason"`
				}
				_ = json.Unmarshal(event.Payload, &payload)
				detail := firstLine(payload.Reason)
				if detail == "" {
					detail = "cancelled"
				}
				activity.Cancelled++
				activity.Events = append(activity.Events, BriefEvent{
					Seq: event.Seq, Time: event.Time, Kind: store.BriefCancelled,
					Text: label + " — " + detail, Ref: node.ID,
				})
				continue
			}
			activity.Failed++
			detail := firstLine(node.Error)
			if detail == "" {
				detail = "stopped without a recorded reason"
			}
			activity.Events = append(activity.Events, BriefEvent{
				Seq: event.Seq, Time: event.Time, Kind: store.BriefFailure,
				Text: label + " — " + detail, Ref: node.ID,
			})

		case store.EventCharterFired:
			charter, ok, readErr := r.store.Charter(event.NodeID)
			if readErr != nil {
				return activity, readErr
			}
			text := "Standing charter " + event.NodeID + " fired."
			if ok {
				text = firstLine(charter.Invariant)
				if text == "" {
					text = firstLine(charter.Action.Template)
				}
				text = "Charter fired — " + text
			}
			activity.CharterFired++
			activity.Events = append(activity.Events, BriefEvent{
				Seq: event.Seq, Time: event.Time, Kind: store.BriefCharter,
				Text: text, Ref: event.NodeID,
			})

		case store.EventFactLearned:
			fact, ok, readErr := r.store.Fact(event.Seq)
			if readErr != nil {
				return activity, readErr
			}
			if !ok {
				continue
			}
			kind := store.BriefFact
			if fact.Kind == store.FactSkill {
				kind = store.BriefSkill
				activity.SkillsLearned++
				newSkills[fact.Seq] = true
			} else {
				activity.FactsLearned++
			}
			activity.Events = append(activity.Events, BriefEvent{
				Seq: event.Seq, Time: event.Time, Kind: kind,
				Text: fact.Body, Ref: "#" + strconv.FormatInt(fact.Seq, 10),
			})

		case store.EventFactActivated:
			var payload struct {
				FactSeq int64 `json:"fact_seq"`
			}
			if json.Unmarshal(event.Payload, &payload) != nil || payload.FactSeq <= 0 || newSkills[payload.FactSeq] {
				continue
			}
			fact, ok, readErr := r.store.Fact(payload.FactSeq)
			if readErr != nil {
				return activity, readErr
			}
			if !ok {
				continue
			}
			activity.SkillsLearned++
			activity.Events = append(activity.Events, BriefEvent{
				Seq: event.Seq, Time: event.Time, Kind: store.BriefSkill,
				Text: "Skill ready — " + fact.Body, Ref: "#" + strconv.FormatInt(fact.Seq, 10),
			})

		case store.EventMessagePosted:
			var payload struct {
				Role    store.Role             `json:"role"`
				Body    string                 `json:"body"`
				Options []store.QuestionOption `json:"options"`
				Brief   *store.Brief           `json:"brief"`
			}
			if json.Unmarshal(event.Payload, &payload) != nil || payload.Brief != nil ||
				payload.Role != store.RoleAgent || !briefQuestion(payload.Body, payload.Options) {
				continue
			}
			activity.Questions++
			activity.Events = append(activity.Events, BriefEvent{
				Seq: event.Seq, Time: event.Time, Kind: store.BriefQuestion,
				Text: firstLine(payload.Body),
			})

		case store.EventUsageRecorded:
			var usage store.NodeUsage
			if json.Unmarshal(event.Payload, &usage) != nil || usage.Cost <= 0 {
				continue
			}
			activity.CostUSD += usage.Cost
			spentSeq = event.Seq
		}
	}
	// Know-how forged while the user was away is the one brief row the journal
	// cannot supply: a craft version lives in the craft repository, and its
	// history is that repository's own.
	for _, forged := range r.craftForgedSince(previous.Time) {
		activity.CraftsForged++
		activity.Events = append(activity.Events, forged)
	}
	standing, err := r.briefStanding(byID, throughSeq)
	if err != nil {
		return activity, err
	}
	activity.Waiting = len(standing)
	activity.Events = append(activity.Events, standing...)
	if activity.CostUSD > 0 {
		activity.Events = append(activity.Events, BriefEvent{
			Seq: spentSeq, Kind: store.BriefSpend,
			Text: fmt.Sprintf("$%.2f spent while work continued.", activity.CostUSD),
		})
	}
	return activity, nil
}

const (
	// briefStallAfter is how long a top-level job may show no landing before
	// the brief calls it stalled. It is deliberately longer than any leaf
	// deadline: a job whose leaves are each landing inside fifteen minutes is
	// working, however slowly, and an hour with nothing settled is the first
	// point at which "it is still going" and "it is stuck" stop being the same
	// sentence to the person reading.
	briefStallAfter = time.Hour
	// briefStandingRows caps the standing half. The brief's whole discipline is
	// that it is a closed sentence with detail behind it, and a returning user
	// who is blocking six things needs to be told that, not handed six rows —
	// the headline carries the count.
	briefStandingRows = 3
	// briefStandingLabelBytes bounds one row's quotation of a job or question.
	briefStandingLabelBytes = 90
)

// briefStanding is the half of the brief that is not an event: what is waiting
// on the user right now. It is computed from current state at compose time
// rather than from the window's journal because the interesting thing about an
// unanswered question is exactly that nothing has happened to it.
//
// Two shapes, in the order they cost the user. Work stopped on a question they
// have not answered — with how long, because the cost of the delay is the whole
// point and the age is already stored. Then work that is open and has not
// landed anything in a long time, which is the other way a job goes quiet
// without ever failing.
func (r *Reconciler) briefStanding(byID map[string]store.Node, throughSeq int64) ([]BriefEvent, error) {
	now := r.now()
	rows := make([]BriefEvent, 0, briefStandingRows)
	blocked := make(map[string]bool)

	questions, err := r.store.UnresolvedQuestions(200)
	if err != nil {
		return nil, err
	}
	for _, question := range questions {
		if len(rows) >= briefStandingRows {
			break
		}
		// Only a question the user has actually seen can be said to be waiting
		// on them, and only one raised before this arrival: a question surfaced
		// by the arrival itself is being read right now.
		if question.Status != store.QuestionAsked || question.Seq > throughSeq {
			continue
		}
		age := store.AgeLabel(question.CreatedAt, now)
		if age == "" {
			continue
		}
		node, held := byID[strings.TrimSpace(question.OriginNodeID)]
		if held && (node.Status == store.Done || node.Status == store.Failed || node.Status == store.Cancelled) {
			held = false
		}
		text := clipLabel(firstLine(question.Text), briefStandingLabelBytes) +
			" — waiting on you " + age + "."
		ref := ""
		if held {
			blocked[node.ID] = true
			ref = node.ID
			text = clipLabel(briefNodeLabel(node), briefStandingLabelBytes) +
				" has been stopped " + age + ", waiting on one answer."
		}
		rows = append(rows, BriefEvent{
			Seq: question.Seq, Time: question.CreatedAt, Kind: store.BriefWaiting,
			Text: text, Ref: ref,
		})
	}

	// A job's own progress is the newest landing anywhere under it, so the
	// stall test walks the active view once rather than querying per job.
	newestLanding := make(map[string]time.Time)
	for _, node := range byID {
		root := briefRootOf(node, byID)
		if root == "" || node.FinishedAt.IsZero() {
			continue
		}
		if node.FinishedAt.After(newestLanding[root]) {
			newestLanding[root] = node.FinishedAt
		}
	}
	for _, node := range byID {
		if len(rows) >= briefStandingRows {
			break
		}
		if !briefJobRoot(node, byID) || blocked[node.ID] {
			continue
		}
		switch node.Status {
		case store.Pending, store.Claimed, store.Running:
		default:
			continue
		}
		// Nothing has landed yet is the sharpest version of stalled, so the
		// job's own start stands in for a landing it never had.
		since := newestLanding[node.ID]
		if since.IsZero() {
			since = node.StartedAt
		}
		if since.IsZero() || now.Sub(since) < briefStallAfter {
			continue
		}
		rows = append(rows, BriefEvent{
			Seq: node.CreatedSeq, Time: since, Kind: store.BriefWaiting,
			Text: clipLabel(briefNodeLabel(node), briefStandingLabelBytes) +
				" is still open and nothing has landed on it " +
				store.AgeLabel(since, now) + ".",
			Ref: node.ID,
		})
	}
	return rows, nil
}

// briefRootOf names the top-level job one node belongs to, or empty for the
// organizational scaffolding that is nobody's job.
func briefRootOf(node store.Node, byID map[string]store.Node) string {
	for hops := 0; hops < maxSessionWalk; hops++ {
		if briefJobRoot(node, byID) {
			return node.ID
		}
		parent, ok := byID[node.Parent]
		if !ok {
			return ""
		}
		node = parent
	}
	return ""
}

func briefJobRoot(node store.Node, byID map[string]store.Node) bool {
	if node.ID == store.RootID || node.Group == store.TerritoryGroup {
		return false
	}
	if node.Parent == store.RootID {
		return true
	}
	parent, ok := byID[node.Parent]
	return ok && parent.Group == store.TerritoryGroup
}

func briefNodeLabel(node store.Node) string {
	for _, candidate := range []string{node.Title, node.Provenance.Intent, node.Brief, node.ID} {
		if candidate = firstLine(candidate); candidate != "" {
			return candidate
		}
	}
	return "Work"
}

func briefQuestion(body string, options []store.QuestionOption) bool {
	body = strings.TrimSpace(body)
	return len(options) > 0 || strings.HasSuffix(body, "?") ||
		strings.HasPrefix(body, store.DailyRailQuestionPrefix)
}

func materializeBrief(sinceSeq, throughSeq int64, activity BriefActivity, draft BriefDraft) store.Message {
	composed := make(map[int64]string, len(draft.Items))
	for _, item := range draft.Items {
		if item.Seq > 0 && strings.TrimSpace(item.Body) != "" {
			composed[item.Seq] = oneLine(item.Body)
		}
	}
	items := make([]store.BriefItem, 0, len(activity.Events))
	for _, event := range activity.Events {
		body := composed[event.Seq]
		if body == "" {
			body = oneLine(event.Text)
		}
		items = append(items, store.BriefItem{Kind: event.Kind, Body: body, Ref: event.Ref})
	}
	headline := briefHeadline(draft.Headline)
	if headline == "" {
		headline = defaultBriefHeadline(activity)
	}
	const lead = "While you were away"
	if strings.HasPrefix(strings.ToLower(headline), strings.ToLower(lead)) {
		rest := strings.TrimSpace(headline[len(lead):])
		rest = strings.TrimLeft(rest, ":—–- ")
		headline = lead + ":"
		if rest != "" {
			headline += " " + rest
		}
	} else {
		headline = lead + ": " + strings.TrimSpace(headline)
	}
	return store.Message{
		Role: store.RoleAgent, Body: boundMessage(headline),
		Brief: &store.Brief{
			SinceSeq: sinceSeq, ThroughSeq: throughSeq,
			Done: activity.Done, Failed: activity.Failed, Cancelled: activity.Cancelled, Questions: activity.Questions,
			CharterFired: activity.CharterFired, FactsLearned: activity.FactsLearned,
			SkillsLearned: activity.SkillsLearned, Waiting: activity.Waiting,
			CostUSD: activity.CostUSD, Items: items,
		},
	}
}

func defaultBriefHeadline(activity BriefActivity) string {
	parts := make([]string, 0, 6)
	if activity.Done > 0 {
		parts = append(parts, fmt.Sprintf("%d %s done", activity.Done, plural(activity.Done, "thing", "things")))
	}
	if activity.Failed > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", activity.Failed, plural(activity.Failed, "failure", "failures")))
	}
	if activity.Cancelled > 0 {
		parts = append(parts, fmt.Sprintf("%d cancelled", activity.Cancelled))
	}
	if activity.Questions > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", activity.Questions, plural(activity.Questions, "question", "questions")))
	}
	if learned := activity.FactsLearned + activity.SkillsLearned; learned > 0 {
		parts = append(parts, fmt.Sprintf("%d learned", learned))
	}
	if activity.CraftsForged > 0 {
		parts = append(parts, fmt.Sprintf("%d %s forged", activity.CraftsForged,
			plural(activity.CraftsForged, "craft", "crafts")))
	}
	if activity.CharterFired > 0 {
		parts = append(parts, fmt.Sprintf("%d %s fired", activity.CharterFired,
			plural(activity.CharterFired, "charter", "charters")))
	}
	if activity.CostUSD > 0 {
		parts = append(parts, fmt.Sprintf("$%.2f", activity.CostUSD))
	}
	// Last in the sentence and first in importance: everything before this is
	// finished business, and this is the only clause the reader has to act on.
	if activity.Waiting > 0 {
		parts = append(parts, fmt.Sprintf("%d waiting on you", activity.Waiting))
	}
	return "While you were away: " + strings.Join(parts, ", ")
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func briefHeadline(value string) string {
	value = oneLine(value)
	for index, r := range value {
		if r != '.' && r != '?' && r != '!' {
			continue
		}
		next := index + 1
		if next == len(value) || (next < len(value) && value[next] == ' ') {
			return strings.TrimSpace(value[:next])
		}
	}
	return value
}
