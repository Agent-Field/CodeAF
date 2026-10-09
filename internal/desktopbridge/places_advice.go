package desktopbridge

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── WHEN THE DESKTOP ASKS WHERE A CHAT BELONGS ──────────────────────────────
//
// internal/placegraph's Recommender decides WHAT to offer: reuse a place before
// making one, rules before a model, one bounded call with labelled candidates,
// and an answer checked against its contract before anything is offered. This
// file decides only WHEN it is asked, and with what evidence, and through which
// door its single model call travels.
//
// THERE ARE EXACTLY THREE TRIGGERS, AND NONE OF THEM IS A READ.
//   - A turn of a conversation this bridge holds SETTLES (the engine said its
//     turn was done). That chat is weighed for an existing place, once — the
//     ledger remembers every chat it has weighed — a moment later, so the title
//     errand that runs beside the end of the turn has landed first.
//   - The Home page ASKS, with POST /places/proposals/organize. That is a
//     person opening a page that shows offers, said once per opening, and never
//     a poll.
//   - The bridge has been IDLE — no turn running in any conversation it holds —
//     for the policy's OrganizeEveryMinutes.
//
// SO A RENDER NEVER COSTS A CALL. GET /places/proposals reads the ledger and
// closes offers the graph has moved past, and nothing else; an SSE reconnect
// replays records the bridge already holds. Neither reaches this file.
//
// ONE JOB AT A TIME, PER BRIDGE. The Recommender's ledger already counts every
// call against a daily budget before the call is made, so two windows cannot
// both spend the last one; what this adds is that one bridge never has two
// questions out at once, a chat queued twice is queued once, and the queue has
// a ceiling. Every ask carries a deadline and dies with the bridge.
//
// THE MODEL IS THE ENGINE'S. No provider client lives here: the question rides
// the open conversation's wire ([remote.MethodPlacesAsk]) to the engine, which
// answers it on the role's own model (Settings → Places organization), journals
// it and bills it like any other background errand. With no conversation open
// that can carry it, the Recommender runs on rules alone — which is not a
// failure, it is the "absent, not broken" law: the rules still offer a chat the
// place whose folder it runs in, and a group of chats that share a folder.

// placeAskDoor is the engine's model door as an open conversation carries it.
// The remote client always has the method; whether the far engine answers it
// is [remote.Welcome.PlaceAsk], which [conversationAsks] checks as well.
type placeAskDoor interface {
	AskPlaces(context.Context, placegraph.ModelRequest) (session.PlacesAnswer, error)
}

const (
	// settleWait is how long after a turn settles the chat is weighed. The title
	// errand runs beside the end of the turn, and a chat weighed before it lands
	// is weighed on its first message alone — once, because the ledger keeps it.
	settleWait = 8 * time.Second
	// askWithin bounds one model ask end to end, the wire included. A filing
	// question is a few hundred words in and one line out.
	askWithin = 60 * time.Second
	// jobWithin bounds one job. Organize may ask about more than one group.
	jobWithin = 3 * time.Minute
	// maxQueuedChats is how many settled chats may wait to be weighed. A chat
	// past it is not lost: it is weighed after its next settled turn.
	maxQueuedChats = 32
	// maxLibrary is how many chats, newest first, the Recommender is shown.
	maxLibrary = 500
	// organizeGap is the least time between two passes the Home page asked
	// for. The ledger already spaces the MODEL calls by the policy's interval;
	// this spaces the rule passes, which read the whole graph.
	organizeGap = time.Minute
	// idleCheck is how often the idle trigger looks at the clock.
	idleCheck = time.Minute
	// firstMessageRunes is how much of a chat's opening message is evidence.
	firstMessageRunes = 600
)

// PlaceAdvice is the bridge's scheduler for place offers. Build it with the
// ledger beside the graph file and attach it with [Bridge.UsePlaceAdvice],
// after [Bridge.UsePlaces].
type PlaceAdvice struct {
	// Ledger is the Recommender's file of offers, considered chats and spent
	// calls (conventionally places-ai.json beside the graph). Required.
	Ledger *placegraph.Ledger
	// Policy reads Settings → Places organization. It is called on every job,
	// so a change applies to the next one. Nil is the defaults.
	Policy func() placegraph.RecommendPolicy
	// Now is the clock. Nil is time.Now.
	Now func() time.Time
	// SettleWait overrides settleWait; zero is the default and a negative
	// value weighs at once (tests).
	SettleWait time.Duration
	// IdleCheck overrides idleCheck; zero is the default (tests shorten it).
	IdleCheck time.Duration
	// SharedWorkspace is the folder EVERY conversation this bridge opens runs
	// in (its --workspace). It is not evidence of what a chat is about — it is
	// where the app was started — so it is left out of a chat's evidence.
	// Passed through, the Recommender's folder rule would offer every chat the
	// same place, without a model, forever after two chats were filed there.
	SharedWorkspace string

	b      *Bridge
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	wake   chan struct{}

	mu        sync.Mutex
	timers    map[string]*time.Timer
	order     []string
	queued    map[string]*conversation
	organize  bool // a pass is queued
	busy      string
	lastPass  time.Time
	lastAsk   *AskRecord
	jobAsk    *AskRecord // the ask made by the job running now, if any
	closed    bool
	startOnce sync.Once
}

// AskRecord is the last model ask this bridge made for places, so a person (or
// a test against a real model) can see which model actually answered.
type AskRecord struct {
	Role  string `json:"role"`
	Model string `json:"model,omitempty"`
	At    string `json:"at"`
	// Error is why the ask failed, or why its answer was not used (an answer
	// that did not fit the contract is refused whole).
	Error string `json:"error,omitempty"`
	// Offered is whether the job the ask was part of left an open offer.
	Offered bool `json:"offered"`
}

// UsePlaceAdvice attaches the scheduler and starts its one worker. Without it
// the proposals routes are absent, never present and failing.
func (b *Bridge) UsePlaceAdvice(a *PlaceAdvice) error {
	if a == nil || a.Ledger == nil {
		return errors.New("place advice needs a ledger")
	}
	b.mu.Lock()
	if b.places == nil {
		b.mu.Unlock()
		return errors.New("place advice needs the places door first")
	}
	a.b = b
	b.advice = a
	b.mu.Unlock()
	a.startOnce.Do(func() {
		a.ctx, a.cancel = context.WithCancel(context.Background())
		a.done, a.wake = make(chan struct{}), make(chan struct{}, 1)
		a.timers, a.queued = map[string]*time.Timer{}, map[string]*conversation{}
		guard.Go("desktop-place-advice", a.run)
	})
	return nil
}

func (a *PlaceAdvice) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *PlaceAdvice) policy() placegraph.RecommendPolicy {
	if a.Policy == nil {
		return placegraph.DefaultRecommendPolicy()
	}
	return a.Policy().Normalized()
}

// close cancels whatever is asking and waits, briefly, for the worker. It
// must be called WITHOUT the bridge's lock: the worker takes it to find a
// conversation to ask through.
func (a *PlaceAdvice) close() {
	a.mu.Lock()
	if a.closed || a.cancel == nil {
		a.mu.Unlock()
		return
	}
	a.closed = true
	for id, t := range a.timers {
		t.Stop()
		delete(a.timers, id)
	}
	a.mu.Unlock()
	a.cancel()
	select {
	case <-a.done:
	case <-time.After(5 * time.Second):
	}
}

// recommender is the Recommender for one job. It is a value built per job
// because the door the model call travels through is chosen per job.
func (a *PlaceAdvice) recommender(ask placegraph.Asker) *placegraph.Recommender {
	a.b.mu.Lock()
	places := a.b.places
	a.b.mu.Unlock()
	return &placegraph.Recommender{Store: places.Store, Ledger: a.Ledger, Policy: a.policy, Ask: ask, Now: a.Now}
}

// ---- the three triggers -----------------------------------------------------

// afterTurn is a conversation's settled turn. The chat is weighed after
// settleWait; a second settle inside the wait moves the wait, it does not
// queue the chat twice.
func (a *PlaceAdvice) afterTurn(s *conversation) {
	chat := ChatIDFromSessionFile(s.conn.Welcome.SessionFile)
	if chat == "" || !a.policy().FilingOffers {
		return
	}
	wait := a.SettleWait
	if wait == 0 {
		wait = settleWait
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	if t := a.timers[chat]; t != nil {
		t.Stop()
	}
	if wait < 0 {
		a.enqueueLocked(chat, s)
		return
	}
	a.timers[chat] = time.AfterFunc(wait, func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		delete(a.timers, chat)
		if !a.closed {
			a.enqueueLocked(chat, s)
		}
	})
}

func (a *PlaceAdvice) enqueueLocked(chat string, s *conversation) {
	if _, ok := a.queued[chat]; ok {
		a.queued[chat] = s
		return
	}
	if len(a.order) >= maxQueuedChats {
		return
	}
	a.queued[chat] = s
	a.order = append(a.order, chat)
	a.nudge()
}

// requestOrganize queues one organizing pass and reports whether it did. A
// pass already queued or running, or one finished within organizeGap, is
// enough.
func (a *PlaceAdvice) requestOrganize() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.organize || a.busy == "organize" {
		return false
	}
	if !a.lastPass.IsZero() && a.now().Sub(a.lastPass) < organizeGap {
		return false
	}
	a.organize = true
	a.nudge()
	return true
}

// idle queues a pass when nothing is running and the policy's interval has
// passed since the last one.
func (a *PlaceAdvice) idle() {
	pol := a.policy()
	if !pol.ClusterOffers && !pol.MergeOffers {
		return
	}
	if a.b.anyRunning() {
		return
	}
	every := time.Duration(pol.OrganizeEveryMinutes) * time.Minute
	a.mu.Lock()
	due := a.lastPass.IsZero() || a.now().Sub(a.lastPass) >= every
	a.mu.Unlock()
	if due {
		a.requestOrganize()
	}
}

func (a *PlaceAdvice) nudge() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// organizing reports whether a pass is queued or running, for the view.
func (a *PlaceAdvice) organizing() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.organize || a.busy == "organize"
}

// ---- the worker -------------------------------------------------------------

func (a *PlaceAdvice) run() {
	defer close(a.done)
	every := a.IdleCheck
	if every <= 0 {
		every = idleCheck
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		if a.ctx.Err() != nil {
			return
		}
		if a.step() {
			continue
		}
		select {
		case <-a.ctx.Done():
			return
		case <-a.wake:
		case <-tick.C:
			a.idle()
		}
	}
}

// step runs the next job, chats first, and reports whether there was one.
func (a *PlaceAdvice) step() bool {
	a.mu.Lock()
	var chat string
	var s *conversation
	switch {
	case len(a.order) > 0:
		chat, a.order = a.order[0], a.order[1:]
		s = a.queued[chat]
		delete(a.queued, chat)
		a.busy = "file"
	case a.organize:
		a.organize, a.busy = false, "organize"
	default:
		a.mu.Unlock()
		return false
	}
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(a.ctx, jobWithin)
	if chat != "" {
		a.fileChat(ctx, chat, s)
	} else {
		a.organizePass(ctx)
	}
	cancel()
	a.mu.Lock()
	if a.busy == "organize" {
		a.lastPass = a.now()
	}
	a.busy = ""
	a.mu.Unlock()
	return true
}

func (a *PlaceAdvice) fileChat(ctx context.Context, chat string, s *conversation) {
	if s == nil || s.closed() {
		return
	}
	evidence := a.evidence(s)
	if evidence.ChatID != chat {
		return
	}
	// The conversation that settled is the one whose engine is asked: it is
	// open, it is this person's, and the call is billed beside that chat.
	rec := a.recommender(a.askVia(s))
	offer, err := rec.FileChat(ctx, evidence, a.library())
	a.settleAsk(offer != nil && offer.Status == placegraph.StatusPending, err)
}

func (a *PlaceAdvice) organizePass(ctx context.Context) {
	rec := a.recommender(a.askVia(a.b.askingConversation()))
	open, err := rec.Organize(ctx, a.library())
	a.settleAsk(len(open) > 0, err)
}

// settleAsk writes what the job made of its ask, when it made one. The
// Recommender's own refusal of an answer (ErrBadAnswer) is said here, because
// the transport succeeded and only the job knows the answer was not used.
func (a *PlaceAdvice) settleAsk(offered bool, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.jobAsk == nil {
		return
	}
	record := *a.jobAsk
	record.Offered = offered
	if record.Error == "" && err != nil {
		record.Error = err.Error()
	}
	a.lastAsk, a.jobAsk = &record, nil
}

// askVia is the Asker that rides s's wire, or nil — rules only — when s is
// nil or its engine does not answer the question.
func (a *PlaceAdvice) askVia(s *conversation) placegraph.Asker {
	door, ok := conversationAsks(s)
	if !ok {
		return nil
	}
	return func(ctx context.Context, req placegraph.ModelRequest) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, askWithin)
		defer cancel()
		answer, err := door.AskPlaces(ctx, req)
		record := &AskRecord{Role: string(req.Role), Model: answer.Model, At: a.now().UTC().Format(time.RFC3339)}
		if err != nil {
			record.Error = err.Error()
		}
		a.mu.Lock()
		a.lastAsk, a.jobAsk = record, record
		a.mu.Unlock()
		return answer.Text, err
	}
}

// conversationAsks reports whether s's engine answers the places question.
func conversationAsks(s *conversation) (placeAskDoor, bool) {
	if s == nil || !s.conn.Welcome.PlaceAsk {
		return nil, false
	}
	door, ok := s.conn.Agent.(placeAskDoor)
	return door, ok
}

func (s *conversation) closed() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// askingConversation is an open conversation whose engine can carry the
// question, preferring one that is not mid-turn; nil when there is none.
func (b *Bridge) askingConversation() *conversation {
	b.mu.Lock()
	defer b.mu.Unlock()
	var busy *conversation
	for _, s := range b.sessions {
		if _, ok := conversationAsks(s); !ok || s.closed() {
			continue
		}
		s.mu.Lock()
		running := s.running
		s.mu.Unlock()
		if !running {
			return s
		}
		busy = s
	}
	return busy
}

// anyRunning reports whether a turn is running in any conversation held here.
func (b *Bridge) anyRunning() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.sessions {
		s.mu.Lock()
		running := s.running
		s.mu.Unlock()
		if running {
			return true
		}
	}
	return false
}

// adviseAfterTurn is a conversation's end-of-stream hook. Only a settled turn
// is weighed.
func (b *Bridge) adviseAfterTurn(s *conversation, settled bool) {
	if !settled {
		return
	}
	b.mu.Lock()
	a := b.advice
	b.mu.Unlock()
	if a != nil {
		a.afterTurn(s)
	}
}

// ---- evidence -----------------------------------------------------------------

// chatEvidence is what an open conversation says about itself: its title, the
// opening message, where it runs, and how many replies it has settled. NONE OF
// IT IS A MODEL'S ANSWER ABOUT PLACES — the title is the conversation's own
// name for itself, and the rest is the transcript.
func chatEvidence(s *conversation, now time.Time) placegraph.ChatEvidence {
	evidence := placegraph.ChatEvidence{
		ChatID:    ChatIDFromSessionFile(s.conn.Welcome.SessionFile),
		Title:     strings.TrimSpace(s.conn.Agent.Title()),
		Workspace: s.conn.Welcome.Workspace,
		UpdatedAt: now.UTC(),
	}
	for _, entry := range s.conn.Agent.Transcript() {
		switch entry.Role {
		case "user":
			if evidence.FirstMessage == "" {
				evidence.FirstMessage = clipRunes(strings.TrimSpace(entry.Text), firstMessageRunes)
			}
		case "assistant":
			if !entry.Interrupted && entry.Tool == "" && strings.TrimSpace(entry.Text) != "" {
				evidence.Replies++
			}
		}
	}
	return evidence
}

// library is every chat the person has, newest first, as the canonical
// session world lists it, with the open conversations' own evidence over the
// top. A chat counts as having replied once its meta carries a finished turn's
// tokens or spend, which the session stamps at the end of every turn.
func (a *PlaceAdvice) library() []placegraph.ChatEvidence {
	a.b.mu.Lock()
	places := a.b.places
	var open []*conversation
	for _, s := range a.b.sessions {
		open = append(open, s)
	}
	a.b.mu.Unlock()
	live := map[string]placegraph.ChatEvidence{}
	for _, s := range open {
		if s.closed() {
			continue
		}
		if e := a.evidence(s); e.ChatID != "" {
			live[e.ChatID] = e
		}
	}
	var out []placegraph.ChatEvidence
	seen := map[string]bool{}
	for _, row := range places.world(false).Sessions() {
		if row.ID == "" || row.Archived || row.DeletionPending || seen[row.ID] || len(out) >= maxLibrary {
			continue
		}
		seen[row.ID] = true
		if e, ok := live[row.ID]; ok {
			if e.Title == "" {
				e.Title = row.Title
			}
			out = append(out, e)
			continue
		}
		replies := 0
		if row.Tokens > 0 || row.Spend > 0 {
			replies = 1
		}
		out = append(out, placegraph.ChatEvidence{ChatID: row.ID, Title: row.Title, Workspace: a.workspace(row.Workspace), Replies: replies, UpdatedAt: row.At})
	}
	for id, e := range live {
		if !seen[id] && len(out) < maxLibrary {
			out = append(out, e)
		}
	}
	return out
}

// evidence is an open conversation's evidence with the shared folder dropped.
func (a *PlaceAdvice) evidence(s *conversation) placegraph.ChatEvidence {
	e := chatEvidence(s, a.now())
	e.Workspace = a.workspace(e.Workspace)
	return e
}

// workspace is folder, or "" when it is the folder every conversation here
// runs in.
func (a *PlaceAdvice) workspace(folder string) string {
	if a.SharedWorkspace != "" && filepath.Clean(folder) == filepath.Clean(a.SharedWorkspace) {
		return ""
	}
	return folder
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
