package session

import (
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/decide"
)

// The decision gate runs when a question is raised, before that question
// reaches the presence desk. A place that has earned the right answers it
// once. A place still learning attaches its proposal and lets the person
// confirm. Anything the place must not touch — irreversible work, "always
// ask me", a policy that forbids it — goes to the person unchanged.
//
// THE GATE IS FAIL-CLOSED. A missing graph, a score that cannot be read, a
// key the question did not offer, or an answer the lane refuses: the person
// is asked, and nothing is recorded as decided. A question already withdrawn
// is never decided. Calling the gate again on the same question repeats the
// first result and does not answer it a second time.

const (
	// DecideForPerson means the question reaches the person.
	DecideForPerson = "person"
	// DecidePropose means a place in learning attached its pick.
	DecidePropose = "proposal"
	// DecideTake means a place answered the question itself.
	DecideTake = "decided"
)

// DecisionReceipt is the record an automatic answer leaves. Text is the
// sentence a person reads: "Allowed automatically by Marketing · reason · Why?".
// The aside that draws it in the transcript is a later lane; this is the
// record that lane is given.
type DecisionReceipt struct {
	PlaceID    string
	Place      string
	Because    string
	Percent    int
	Text       string
	DecisionID string
	At         time.Time
}

// DecideResult is what the gate did with one question. Reach is false only
// when the place answered it, which is the signal to keep it off the desk.
type DecideResult struct {
	Outcome   string
	Reach     bool
	PlaceID   string
	Escalated bool
	Receipt   DecisionReceipt
}

// DecideGate is the hook one session consults. Graph is the place graph the
// chat is filed in. OpenStore is that place's decision ledger. Score is the
// confidence of this question in that place, and the key the place would
// pick. Nil Score, store or graph leaves the question for the person.
type DecideGate struct {
	Graph     decide.EscalationGraph
	ChatID    string
	OpenStore func(placeID string) (*decide.Store, error)
	// Score returns the measurement and the proposed answer key. An error
	// asks the person.
	Score func(placeID string, q Question) (decide.Result, string, error)
	// SubjectClass is the learning-ring class joined to the ask kind
	// (shell-read, git). Nil means the ask kind stands alone.
	SubjectClass func(Question) string
	// PolicyForbids reports that this question must not be answered except
	// by a person, beyond the kinds the gate already refuses. Nil adds nothing.
	PolicyForbids func(Question) bool
	Now           func() time.Time

	mu       sync.Mutex
	seen     map[string]gateMemo
	receipts []DecisionReceipt
	// resolves counts answers actually given, so a replay can be told from
	// a second decision.
	resolves int
}

type gateMemo struct {
	outcome   string
	placeID   string
	escalated bool
	pick      *Pick
	proposal  *QuestionProposal
	receipt   DecisionReceipt
}

// SetDecideGate installs the hook. Nil leaves every question for the person.
func (a *Agent) SetDecideGate(g *DecideGate) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.decideGate = g
	a.mu.Unlock()
}

// Receipts returns the automatic answers this gate has emitted, oldest first.
func (g *DecideGate) Receipts() []DecisionReceipt {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]DecisionReceipt, len(g.receipts))
	copy(out, g.receipts)
	return out
}

// decideBeforePresence runs the gate. True means the question was answered
// and must not be shown as open.
func (a *Agent) decideBeforePresence(q *Question) bool {
	if a == nil || q == nil {
		return false
	}
	a.mu.Lock()
	g := a.decideGate
	a.mu.Unlock()
	if g == nil {
		return false
	}
	return !g.apply(a, q).Reach
}

func (g *DecideGate) apply(a *Agent, q *Question) DecideResult {
	g.mu.Lock()
	defer g.mu.Unlock()
	if a == nil || q == nil || !g.ready() {
		return DecideResult{Outcome: DecideForPerson, Reach: true}
	}
	token := questionToken(q.Kind, q.Token())
	// A QUESTION ALREADY WITHDRAWN IS NEVER DECIDED. A replay of one this
	// gate already answered keeps that first answer and does not take a new
	// one; a question withdrawn before the gate saw it is left for the caller.
	if !q.Open() {
		if memo, ok := g.seen[token]; ok && memo.outcome == DecideTake {
			return g.replay(q, memo)
		}
		return DecideResult{Outcome: DecideForPerson, Reach: true}
	}
	if memo, ok := g.seen[token]; ok {
		return g.replay(q, memo)
	}
	if q.Stakes == StakesIrreversible || g.anyRootAlwaysAsks() || g.forbids(*q) {
		return g.person(q, false)
	}
	owner, err := g.owner()
	if err != nil || owner == "" {
		return DecideResult{Outcome: DecideForPerson, Reach: true}
	}
	st, result, key, err := g.measure(owner, *q)
	if err != nil {
		return DecideResult{Outcome: DecideForPerson, Reach: true}
	}
	if result.Percent < 0 || result.Percent > 100 {
		return g.person(q, false)
	}
	// Per-kind "always ask" is the same stop as the place setting: the person
	// is asked, and the question is not offered as a proposal on the way.
	if st.Mode == decide.ModeAsk {
		return g.person(q, false)
	}
	if st.Mode == decide.ModeLearning || st.Mode == "" {
		return g.propose(q, owner, st, result, key)
	}
	settings, _ := g.Graph.EffectiveDecide(owner)
	if st.Mode == decide.ModeDeciding && result.Decides(settings.Threshold) {
		if !canTake(*q, key) {
			return g.person(q, false)
		}
		return g.take(a, q, owner, result, key, false)
	}
	return g.escalate(a, q)
}

func (g *DecideGate) ready() bool {
	return g.Graph != nil && g.OpenStore != nil && g.Score != nil && strings.TrimSpace(g.ChatID) != ""
}

func (g *DecideGate) forbids(q Question) bool {
	if g.PolicyForbids != nil && g.PolicyForbids(q) {
		return true
	}
	// THESE KINDS ARE A PERSON'S BY THEIR OWN LAW. A judgement, a confirmation
	// and a landing say nothing but a person answers them. A clarification's
	// answer is words the asker did not have, so there is no key to take.
	switch q.Ask {
	case AskJudgement, AskConfirmation, AskLanding, AskClarification:
		return true
	default:
		return false
	}
}

func (g *DecideGate) anyRootAlwaysAsks() bool {
	for _, m := range g.Graph.PlacesOf(g.ChatID) {
		settings, _ := g.Graph.EffectiveDecide(m.PlaceID)
		if settings.AlwaysAsk {
			return true
		}
	}
	return false
}

// owner is the place the walk would start at: the chat's place, or the
// nearest common ancestor when the chat is filed in several. The probe never
// authorises a decision; it only records the first place the walk considers.
func (g *DecideGate) owner() (string, error) {
	var owner string
	_, err := decide.Escalate(g.Graph, g.ChatID, func(id string) (decide.Assessment, error) {
		if owner == "" {
			owner = id
		}
		return decide.Assessment{Mode: decide.ModeLearning}, nil
	})
	return owner, err
}

func (g *DecideGate) measure(placeID string, q Question) (decide.KindState, decide.Result, string, error) {
	store, err := g.OpenStore(placeID)
	if err != nil || store == nil {
		if err == nil {
			err = decide.ErrInvalid
		}
		return decide.KindState{}, decide.Result{}, "", err
	}
	class := ""
	if g.SubjectClass != nil {
		class = g.SubjectClass(q)
	}
	st, err := store.Mode(decide.KindKey(string(q.Ask), class))
	if err != nil {
		return decide.KindState{}, decide.Result{}, "", err
	}
	if st.Mode == "" {
		st.Mode = decide.ModeLearning
	}
	result, key, err := g.Score(placeID, q)
	if err != nil {
		return decide.KindState{}, decide.Result{}, "", err
	}
	return st, result, strings.TrimSpace(key), nil
}

func (g *DecideGate) propose(q *Question, placeID string, st decide.KindState, result decide.Result, key string) DecideResult {
	if !canTake(*q, key) {
		return g.person(q, false)
	}
	pick := &Pick{
		Key:     key,
		Reason:  strings.TrimSpace(result.Because),
		Percent: result.Percent,
		Basis:   append([]string(nil), result.Basis...),
	}
	proposal := &QuestionProposal{
		Place:  g.placeName(placeID),
		Agreed: decide.Agreements(st),
		Of:     decide.RingSize,
	}
	q.Pick = clonePick(pick)
	q.Proposal = cloneProposal(proposal)
	g.remember(q, gateMemo{
		outcome:  DecidePropose,
		placeID:  placeID,
		pick:     clonePick(pick),
		proposal: cloneProposal(proposal),
	})
	return DecideResult{Outcome: DecidePropose, Reach: true, PlaceID: placeID}
}

func (g *DecideGate) escalate(a *Agent, q *Question) DecideResult {
	placeID, err := decide.Escalate(g.Graph, g.ChatID, func(id string) (decide.Assessment, error) {
		st, result, _, measureErr := g.measure(id, *q)
		if measureErr != nil {
			return decide.Assessment{}, measureErr
		}
		return decide.Assessment{Mode: st.Mode, Percent: result.Percent}, nil
	})
	if err != nil {
		// A failed lookup must not authorise a decision, and it is not
		// remembered: the same question can be tried again once the read works.
		return DecideResult{Outcome: DecideForPerson, Reach: true}
	}
	if placeID == "" {
		return g.person(q, true)
	}
	st, result, key, err := g.measure(placeID, *q)
	if err != nil {
		return DecideResult{Outcome: DecideForPerson, Reach: true}
	}
	settings, _ := g.Graph.EffectiveDecide(placeID)
	if st.Mode == decide.ModeDeciding && result.Decides(settings.Threshold) && canTake(*q, key) {
		return g.take(a, q, placeID, result, key, true)
	}
	return g.person(q, true)
}

func (g *DecideGate) take(a *Agent, q *Question, placeID string, result decide.Result, key string, escalated bool) DecideResult {
	name := g.placeName(placeID)
	because := strings.TrimSpace(result.Because)
	at := g.now()
	answer := Answer{
		At:        at,
		Kind:      q.Kind,
		ID:        q.ID,
		Ref:       q.Ref,
		Key:       key,
		Picked:    []string{key},
		Ask:       q.Ask,
		DecidedBy: DecidedByDial,
		Scope:     ScopeOnce,
		Why:       because,
		From:      decisionReceiptText(q.Ask, name, because),
	}
	if err := a.ResolveQuestion(answer); err != nil {
		return DecideResult{Outcome: DecideForPerson, Reach: true}
	}
	g.resolves++
	a.recordDecision(decisionRecordOf(*q, answer))
	decisionID := placeID + ":" + questionToken(q.Kind, q.Token())
	if store, err := g.OpenStore(placeID); err == nil && store != nil {
		// The answer is already given. A ledger that refuses the row must
		// not make the gate answer the question again on replay.
		_ = store.Append(g.decision(q, placeID, decisionID, result, key, at))
	}
	receipt := DecisionReceipt{
		PlaceID:    placeID,
		Place:      name,
		Because:    because,
		Percent:    result.Percent,
		Text:       answer.From,
		DecisionID: decisionID,
		At:         at,
	}
	g.receipts = append(g.receipts, receipt)
	a.queueDecisionReceipt(g.asideReceipt(q, placeID, decisionID, name, result, key, at))
	a.emitQuestion(EventQuestionAnswered, *q, &answer)
	g.remember(q, gateMemo{
		outcome:   DecideTake,
		placeID:   placeID,
		escalated: escalated,
		receipt:   receipt,
	})
	return DecideResult{
		Outcome:   DecideTake,
		Reach:     false,
		PlaceID:   placeID,
		Escalated: escalated,
		Receipt:   receipt,
	}
}

func (g *DecideGate) decision(q *Question, placeID, id string, result decide.Result, key string, at time.Time) decide.Decision {
	class := ""
	if g.SubjectClass != nil {
		class = g.SubjectClass(*q)
	}
	action := key
	if option, ok := q.Option(key); ok && strings.TrimSpace(option.Label) != "" {
		action = strings.TrimSpace(option.Label)
	}
	return decide.Decision{
		ID:      id,
		PlaceID: placeID,
		QuestionRef: decide.QuestionRef{
			Session: g.ChatID,
			Kind:    string(q.Kind),
			ID:      q.Token(),
		},
		AskKind:    string(q.Ask),
		Subject:    class,
		Action:     action,
		By:         placeID,
		Because:    strings.TrimSpace(result.Because),
		Percent:    result.Percent,
		Stakes:     string(q.Stakes),
		Reversible: q.Stakes != StakesIrreversible,
		At:         at,
		Undo:       decide.Undo{Token: id},
	}
}

// asideReceipt is the transcript's receipt for a decision just taken, built from
// the same ledger row the store keeps so the two cannot disagree.
func (g *DecideGate) asideReceipt(q *Question, placeID, id, place string, result decide.Result, key string, at time.Time) decide.Receipt {
	return decide.ReceiptOf(g.decision(q, placeID, id, result, key, at), place)
}

func (g *DecideGate) person(q *Question, escalated bool) DecideResult {
	g.remember(q, gateMemo{outcome: DecideForPerson, escalated: escalated})
	return DecideResult{Outcome: DecideForPerson, Reach: true, Escalated: escalated}
}

func (g *DecideGate) remember(q *Question, memo gateMemo) {
	if g.seen == nil {
		g.seen = map[string]gateMemo{}
	}
	g.seen[questionToken(q.Kind, q.Token())] = memo
}

func (g *DecideGate) replay(q *Question, memo gateMemo) DecideResult {
	switch memo.outcome {
	case DecidePropose:
		q.Pick = clonePick(memo.pick)
		q.Proposal = cloneProposal(memo.proposal)
		return DecideResult{Outcome: DecidePropose, Reach: true, PlaceID: memo.placeID}
	case DecideTake:
		return DecideResult{
			Outcome:   DecideTake,
			Reach:     false,
			PlaceID:   memo.placeID,
			Escalated: memo.escalated,
			Receipt:   memo.receipt,
		}
	default:
		return DecideResult{Outcome: DecideForPerson, Reach: true, Escalated: memo.escalated}
	}
}

func (g *DecideGate) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

func (g *DecideGate) placeName(id string) string {
	p, ok := g.Graph.Place(id)
	if !ok || strings.TrimSpace(p.Name) == "" {
		return id
	}
	return strings.TrimSpace(p.Name)
}

func canTake(q Question, key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	_, ok := q.Option(key)
	return ok
}

func decisionReceiptText(ask AskKind, placeName, because string) string {
	return decide.Sentence(string(ask), placeName, because)
}

func clonePick(p *Pick) *Pick {
	if p == nil {
		return nil
	}
	out := *p
	if p.Basis != nil {
		out.Basis = append([]string(nil), p.Basis...)
	}
	return &out
}

func cloneProposal(p *QuestionProposal) *QuestionProposal {
	if p == nil {
		return nil
	}
	out := *p
	return &out
}
