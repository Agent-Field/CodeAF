package council

// The runner: the turn loop of one discussion between two places.
//
// THIS FILE MAKES NO MODEL CALL OF ITS OWN. Every reply goes through the
// engine's role door via the [Asker] the caller supplies, so the model, the
// budget journal and the provider are the engine's, and the dollars a reply
// cost come back with it. Nothing here opens a second provider client.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// The role's word lives in internal/roles with the others; it is registered
// here because this is the file that makes the call.
func init() {
	roles.Register(roles.RoleCouncil, roles.TierWorker, "what a place says for itself when it talks with another place")
}

// DecidedPrefix opens the reply that ends a discussion. The words after it are
// the outcome and are filed as a knows line in both places.
const DecidedPrefix = "Decided:"

// The speakers a transcript line can carry besides a place name.
const (
	// SpeakerPerson marks a message the person typed into the council's chat.
	SpeakerPerson = "person"
)

// Request is one question for the engine's role door.
type Request struct {
	Role   roles.Role
	System string
	User   string
}

// Answer is the reply text and what it cost, in dollars. A caller that cannot
// price a reply says zero, and the dollar cap then never fires; the turn cap
// still does.
type Answer struct {
	Text    string
	CostUSD float64
}

// Asker asks the engine's role door one question.
type Asker func(ctx context.Context, req Request) (Answer, error)

// Escalation is what leaves a discussion that hit a cap. ToPlace is the nearest
// place both places sit under, or empty when there is none, in which case the
// question is the person's: a needs-you item ("capped councils", I2.11).
type Escalation struct {
	CouncilID string
	ChatID    string
	Topic     string
	Places    [2]string
	ToPlace   string
	// Reason is "turns" or "dollars": which cap was reached first.
	Reason string
	Turns  int
	Spend  float64
}

// Escalation reasons.
const (
	ReasonTurns   = "turns"
	ReasonDollars = "dollars"
)

// Sink is where the runner writes what it cannot keep itself: lines into the
// council's chat, and the escalation. Both are supplied by the desktop bridge.
type Sink interface {
	// Say appends one line to the council's chat. speaker is a place name or
	// [SpeakerPerson].
	Say(chatID, speaker, text string) error
	// Escalate hands a capped discussion on. An error leaves the discussion
	// running so a later Run tries again.
	Escalate(ctx context.Context, e Escalation) error
}

// Runner runs discussions held in one [Store].
type Runner struct {
	store  *Store
	places *placegraph.Store
	ask    Asker
	sink   Sink

	mu   sync.Mutex
	runs map[string]*run
}

// run is the live state of one discussion. It outlives a single Run call so a
// person's message can arrive while no loop is waiting on it.
type run struct {
	paused     bool
	wake       chan struct{}
	transcript []line
	// steer holds the person's messages the next turn has not yet answered.
	steer []string
}

type line struct{ speaker, text string }

// NewRunner joins a store, the place graph its lines are filed in, the role
// door and the chat sink. All four are required.
func NewRunner(store *Store, places *placegraph.Store, ask Asker, sink Sink) (*Runner, error) {
	if store == nil || places == nil || ask == nil || sink == nil {
		return nil, fmt.Errorf("%w: a runner needs a store, places, a role door and a sink", ErrInvalid)
	}
	return &Runner{store: store, places: places, ask: ask, sink: sink, runs: map[string]*run{}}, nil
}

// Start opens a discussion and runs it to its end. The store refuses a pair and
// topic that is open or cooling ([ErrOpen], [ErrCooldown]) before any model is
// asked. Callers that must not block run it on a goroutine.
func (r *Runner) Start(ctx context.Context, placeA, placeB, topic string) (Council, error) {
	c, err := r.store.Begin(placeA, placeB, topic)
	if err != nil {
		return Council{}, err
	}
	return r.Run(ctx, c.ID)
}

// Pause holds a discussion before its next turn. A turn already being answered
// finishes first. It is the moment a person starts typing in the chat.
func (r *Runner) Pause(id string) (Council, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.stateLocked(id)
	st.paused = true
	return r.recordLocked(id, st)
}

// Steer adds the person's message and lets the discussion go on. It does not
// count as a turn (DESIGN-QUESTIONS P-16); the reply it provokes does.
func (r *Runner) Steer(id, text string) (Council, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Council{}, fmt.Errorf("%w: empty message", ErrInvalid)
	}
	c, err := r.store.Get(id)
	if err != nil {
		return Council{}, err
	}
	if c.State.Ended() {
		return Council{}, ErrEnded
	}
	if err := r.sink.Say(c.ChatID, SpeakerPerson, text); err != nil {
		return Council{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.stateLocked(id)
	st.transcript = append(st.transcript, line{SpeakerPerson, text})
	st.steer = append(st.steer, text)
	st.paused = false
	close(st.wake)
	st.wake = make(chan struct{})
	return r.recordLocked(id, st)
}

// Resume lets a paused discussion go on without a message.
func (r *Runner) Resume(id string) (Council, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.stateLocked(id)
	st.paused = false
	close(st.wake)
	st.wake = make(chan struct{})
	return r.recordLocked(id, st)
}

func (r *Runner) stateLocked(id string) *run {
	st, ok := r.runs[id]
	if !ok {
		st = &run{wake: make(chan struct{})}
		r.runs[id] = st
	}
	return st
}

// recordLocked writes the paused or running state and nothing else. It holds
// the runner lock so a loop recording a turn cannot overwrite a pause that
// arrived while that turn was being answered.
func (r *Runner) recordLocked(id string, st *run) (Council, error) {
	c, err := r.store.Get(id)
	if err != nil {
		return Council{}, err
	}
	if c.State.Ended() {
		return c, nil
	}
	return r.store.Record(id, c.Turns, c.Spend, liveState(st), "")
}

func liveState(st *run) State {
	if st.paused {
		return StatePaused
	}
	return StateRunning
}

// Run takes a discussion from where the store has it to its end: decided, or
// escalated at six turns or $0.25. A cancelled context or a failed reply
// returns with the discussion paused, so nothing is lost and Resume goes on.
func (r *Runner) Run(ctx context.Context, id string) (Council, error) {
	for {
		c, err := r.store.Get(id)
		if err != nil {
			return Council{}, err
		}
		if c.State.Ended() {
			return c, nil
		}
		if reason := capReached(c); reason != "" {
			return r.escalate(ctx, c, reason)
		}
		if err := r.waitWhilePaused(ctx, id); err != nil {
			return r.parked(id, err)
		}
		c, err = r.turn(ctx, c)
		if err != nil {
			return r.parked(id, err)
		}
		if c.State.Ended() {
			return c, nil
		}
	}
}

// parked leaves the discussion paused after an interruption and returns the
// error that caused it.
func (r *Runner) parked(id string, cause error) (Council, error) {
	r.mu.Lock()
	st := r.stateLocked(id)
	st.paused = true
	c, err := r.recordLocked(id, st)
	r.mu.Unlock()
	if err != nil {
		return Council{}, errors.Join(cause, err)
	}
	return c, cause
}

func (r *Runner) waitWhilePaused(ctx context.Context, id string) error {
	for {
		r.mu.Lock()
		st := r.stateLocked(id)
		paused, wake := st.paused, st.wake
		r.mu.Unlock()
		if !paused {
			return nil
		}
		select {
		case <-wake:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func capReached(c Council) string {
	switch {
	case c.Turns >= c.Cap:
		return ReasonTurns
	case c.Spend >= c.CapUSD:
		return ReasonDollars
	}
	return ""
}

// turn lets the next place speak. Places[0] opens and the two alternate, by
// turn count, so a resumed discussion picks up with the right voice.
func (r *Runner) turn(ctx context.Context, c Council) (Council, error) {
	snap, err := r.places.Snapshot()
	if err != nil {
		return Council{}, err
	}
	me, other := c.Places[c.Turns%2], c.Places[(c.Turns+1)%2]
	mine, ok := snap.Place(me)
	if !ok {
		return Council{}, fmt.Errorf("%w: %s", placegraph.ErrNotFound, me)
	}
	theirs, ok := snap.Place(other)
	if !ok {
		return Council{}, fmt.Errorf("%w: %s", placegraph.ErrNotFound, other)
	}

	r.mu.Lock()
	st := r.stateLocked(c.ID)
	transcript := append([]line(nil), st.transcript...)
	steer := append([]string(nil), st.steer...)
	r.mu.Unlock()

	ans, err := r.ask(ctx, Request{
		Role:   roles.RoleCouncil,
		System: systemPrompt(mine.Name, theirs.Name, knowsOf(snap, me), c),
		User:   userPrompt(c.Topic, transcript, steer),
	})
	if err != nil {
		return Council{}, err
	}
	reply := strings.TrimSpace(ans.Text)
	if reply == "" {
		return Council{}, fmt.Errorf("%w: %s sent an empty reply", ErrInvalid, mine.Name)
	}
	if err := r.sink.Say(c.ChatID, mine.Name, reply); err != nil {
		return Council{}, err
	}

	r.mu.Lock()
	st.transcript = append(st.transcript, line{mine.Name, reply})
	st.steer = nil
	spend := c.Spend + max(ans.CostUSD, 0)
	// The turn is recorded under the runner lock so a pause that landed while
	// it was being answered keeps the paused state.
	rec, err := r.store.Record(c.ID, c.Turns+1, spend, liveState(st), "")
	r.mu.Unlock()
	if err != nil {
		return Council{}, err
	}
	if outcome, ok := decided(reply); ok {
		return r.decide(rec, outcome)
	}
	return rec, nil
}

// decided reads the outcome from a reply whose first line opens with "Decided:".
func decided(reply string) (string, bool) {
	first, _, _ := strings.Cut(reply, "\n")
	first = strings.TrimSpace(first)
	if len(first) < len(DecidedPrefix) || !strings.EqualFold(first[:len(DecidedPrefix)], DecidedPrefix) {
		return "", false
	}
	outcome := strings.TrimSpace(first[len(DecidedPrefix):])
	return outcome, outcome != ""
}

// decide files the outcome in both places as a knows line the council's chat
// said, then closes the discussion. A failure after the first line removes it
// again, so the two places never disagree about what was decided.
func (r *Runner) decide(c Council, outcome string) (Council, error) {
	var filed []string
	undo := func() {
		for _, id := range filed {
			_, _ = r.places.DeleteLine(id)
		}
	}
	for _, place := range c.Places {
		l, _, err := r.places.AddLine(placegraph.Line{
			PlaceID: place,
			Text:    outcome,
			Source:  placegraph.LineSource{Kind: placegraph.LineSaidInChat, ChatID: c.ChatID, At: r.store.now()},
		})
		if err != nil {
			undo()
			return Council{}, err
		}
		filed = append(filed, l.ID)
	}
	done, err := r.store.Record(c.ID, c.Turns, c.Spend, StateDecided, outcome)
	if err != nil {
		undo()
		return Council{}, err
	}
	return done, nil
}

// escalate hands a capped discussion to the nearest place both sit under, or to
// the person when there is none.
func (r *Runner) escalate(ctx context.Context, c Council, reason string) (Council, error) {
	snap, err := r.places.Snapshot()
	if err != nil {
		return Council{}, err
	}
	if err := r.sink.Escalate(ctx, Escalation{
		CouncilID: c.ID, ChatID: c.ChatID, Topic: c.Topic, Places: c.Places,
		ToPlace: NearestSharedParent(snap, c.Places[0], c.Places[1]),
		Reason:  reason, Turns: c.Turns, Spend: c.Spend,
	}); err != nil {
		return Council{}, err
	}
	return r.store.Record(c.ID, c.Turns, c.Spend, StateEscalated, "")
}

// NearestSharedParent is the closest place that sits above both places, or
// above one of them with the other being that place itself, by the fewest
// steps in total. Archived places are passed over. Ties go to the lower id so
// the answer does not change between runs. "" means they share none.
func NearestSharedParent(snap *placegraph.Snapshot, a, b string) string {
	da, db := ancestorDepths(snap, a), ancestorDepths(snap, b)
	best, bestCost := "", 0
	for id, x := range da {
		y, ok := db[id]
		if !ok {
			continue
		}
		if p, found := snap.Place(id); !found || p.Archived {
			continue
		}
		if cost := x + y; best == "" || cost < bestCost || (cost == bestCost && id < best) {
			best, bestCost = id, cost
		}
	}
	return best
}

// ancestorDepths maps every place above id, and id itself at zero, to its
// distance in parent steps.
func ancestorDepths(snap *placegraph.Snapshot, id string) map[string]int {
	depth := map[string]int{id: 0}
	frontier := []string{id}
	for d := 1; len(frontier) > 0; d++ {
		var next []string
		for _, n := range frontier {
			p, ok := snap.Place(n)
			if !ok {
				continue
			}
			for _, par := range p.Parents {
				if _, seen := depth[par]; !seen {
					depth[par] = d
					next = append(next, par)
				}
			}
		}
		frontier = next
	}
	return depth
}

// maxKnowsLines and maxLineRunes bound what one place hands the model, so a
// place with a long memory cannot spend a discussion's dollars on its prompt.
const (
	maxKnowsLines = 40
	maxLineRunes  = 300
)

// knowsOf is the lines a place still holds, newest first, replaced ones left out.
func knowsOf(snap *placegraph.Snapshot, place string) []string {
	lines := snap.Knowledge(place)
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].CreatedAt.After(lines[j].CreatedAt) })
	var out []string
	for _, l := range lines {
		if l.ReplacedBy != "" {
			continue
		}
		text := strings.Join(strings.Fields(l.Text), " ")
		if r := []rune(text); len(r) > maxLineRunes {
			text = string(r[:maxLineRunes]) + "…"
		}
		out = append(out, text)
		if len(out) == maxKnowsLines {
			break
		}
	}
	return out
}

func systemPrompt(me, other string, knows []string, c Council) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You speak for the place %q in a short discussion with the place %q. ", me, other)
	fmt.Fprintf(&b, "You have at most %d turns between you. Reply in two or three plain sentences, as %s, and use only what %s knows. ", c.Cap, me, me)
	fmt.Fprintf(&b, "When you both agree, begin your reply with %q followed by the outcome on that one line. ", DecidedPrefix)
	b.WriteString("Do not write it before the other place has agreed. If a person writes in the discussion, answer them first.\n")
	if len(knows) > 0 {
		fmt.Fprintf(&b, "\nWhat %s knows:\n", me)
		for _, k := range knows {
			b.WriteString("- " + k + "\n")
		}
	}
	return b.String()
}

func userPrompt(topic string, transcript []line, steer []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Topic: %s\n", topic)
	if len(transcript) > 0 {
		b.WriteString("\nSo far:\n")
		for _, l := range transcript {
			fmt.Fprintf(&b, "%s: %s\n", l.speaker, l.text)
		}
	}
	if len(steer) > 0 {
		b.WriteString("\nThe person has just said, and it comes first:\n")
		for _, s := range steer {
			b.WriteString("- " + s + "\n")
		}
	}
	b.WriteString("\nYour reply:")
	return b.String()
}
