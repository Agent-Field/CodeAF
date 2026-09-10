package remote

// news.go is THE STATUS ROW, ACROSS A CONNECTION.
//
// THE DEFECT THIS FIXES. Bare `aforge` in a workspace does not run the engine
// in the surface's own process: it dials the machine's engine host
// (internal/enginehost) and the surface talks to a [Agent] over a pipe. So the
// road every live figure on that surface travels — the rate at the right edge
// of the status row, the `via <machine>` rider beside the model on the seam,
// `connecting · 1.2s`, `first word …`, `thinking`, `writing`, and the `served`
// row `/status` prints — ran from internal/provider through internal/session's
// two process-global readers to internal/tui3, ENTIRELY INSIDE THE ENGINE'S
// PROCESS, and stopped there. The surface's code for all four was right the
// whole time. It was reading a desk nobody was writing to.
//
// So two more of the kinds that answer nothing (wire.go's [Frame.Kind]) carry
// them: "phase" for what the request in flight is doing, "lane" for the
// sighting a finished answer leaves behind.
//
// ── THE THREE THINGS THAT ARE NOT OBVIOUS ───────────────────────────────────
//
// ONE. THE READERS ARE GLOBAL AND THE HOST IS NOT. internal/session keeps one
// desk for the whole build, deliberately: a surface that draws a status line
// does not hold the agent that ran the errand behind it (lanenews.go says why).
// One process with one window needs nothing more. A HOST runs many
// conversations down many connections, so a single reader that fanned
// everything to everybody would draw every window's clock on every other
// window. Every piece of news therefore names its own conversation
// (internal/session's newskey.go) and the newsroom below files each connection
// under the same name.
//
// TWO. EVERY ROLE CROSSES AND THE SURFACE DECIDES. A task node's phases are
// drawn where a node is drawn and a conversation's on the status row, and that
// filter is already written on the surface (internal/tui3's phase.go, on
// [lane.Role]). Filtering here would be a second opinion about a question that
// already has one — and it is exactly what the in-process road does, which is
// the point: the same news reaches the same reader either way.
//
// THREE. NOTHING WAITS ON A SLOW PIPE. The fan-out runs on the stream's own
// goroutine, between two deltas — the budget internal/provider's postPhase is
// documented to keep — so a connection whose reader has stopped must not be
// able to hold up the turn it is measuring. Each surface gets an outbox with a
// floor under it and a goroutine of its own; when the outbox is full the frame
// is DROPPED. That is safe in a way a dropped event never would be: a phase is
// a claim about NOW and says itself again every second while it lasts
// (internal/provider's phaseBeat), and a sighting is redrawn by the next
// answer.

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── the engine half ─────────────────────────────────────────────────────────

// newsKeyed is an engine that can name the conversation its news is filed
// under, asserted rather than required of [WrappedAgent] for the reason the
// task lane is: an engine built around something other than an agent has no key
// to give, and a capability that cannot work is absent rather than broken. A
// host that cannot name a conversation simply files nothing under it and the
// surface draws what it has always drawn, which is nothing.
type newsKeyed interface{ NewsKey() string }

// newsKeyOf is the conversation an engine's news arrives under, or "" for one
// that cannot say.
func newsKeyOf(agent WrappedAgent) string {
	door, ok := agent.(newsKeyed)
	if !ok {
		return ""
	}
	return door.NewsKey()
}

// newsBacklog is how many frames one surface's outbox holds before it starts
// dropping.
//
// SIXTY-FOUR IS FOUR SECONDS OF EVERYTHING. A request beats its phase once a
// second and a turn's own stage every five, and a conversation running a wide
// task fan has a handful of those at once; sixty-four is therefore several
// seconds of the busiest case, which is far longer than a pipe on the same
// machine is ever behind. A surface that is further behind than that is a
// surface that has stopped reading, and the right thing to send it when it
// comes back is what is true THEN — not four seconds of clocks it would have
// to draw and discard.
const newsBacklog = 64

// newsFeed is one surface's outbox and the way out of it. It is a pointer held
// in two places — the session's map and the goroutine draining it — so the
// pump can tell "still mine" from "replaced under me", exactly as a task feed
// does (tasklane.go).
type newsFeed struct {
	frames chan Frame
	done   chan struct{}
	once   sync.Once
}

// offer puts one frame in the outbox and NEVER WAITS FOR ROOM. See the third
// law in this file's header: the caller is the stream goroutine of a turn that
// is running, and a status line may not be able to stall one.
func (f *newsFeed) offer(frame Frame) {
	if f == nil {
		return
	}
	select {
	case f.frames <- frame:
	case <-f.done:
	default:
	}
}

// leave ends one outbox. It is idempotent because every road out of a
// connection may reach it.
func (f *newsFeed) leave() {
	if f == nil {
		return
	}
	f.once.Do(func() { close(f.done) })
}

// newsroom is every conversation this process is serving, filed by the name its
// news arrives under.
//
// IT IS PACKAGE-LEVEL BECAUSE THE READERS IT SUBSCRIBES TO ARE. internal/
// session holds one desk per build and hands out one reader slot, so the thing
// that registers for it has to be one thing. The readers are installed when the
// first conversation is filed and taken down when the last leaves, which keeps
// a process that is serving nothing — a test, a build between conversations —
// exactly as it was: internal/provider asks whether anybody is reading phases
// at all to decide whether a stalled pinned lane is asked about or quietly
// borrowed against (its hedge.go), and a host holding no conversation must
// answer that question the same way it always did.
var newsroom struct {
	mu    sync.Mutex
	rooms map[string]map[*Session]struct{}
	keys  map[*Session]string
	// prevPhase and prevLane are the readers that were there before this
	// package took the desk over, so that the last conversation leaving really
	// does put them back.
	prevPhase func(session.PhaseNews)
	prevLane  func(session.LaneNews)
	listening bool
}

// fileNews files this conversation under the name its own engine gives its
// news, and unfiles it when that name is empty.
//
// IT IS CALLED AGAIN AFTER A SWAP, because /new and /resume mint a whole new
// agent on the same connection and the news of the conversation that replaced
// this one arrives under a different name. Filing is idempotent: a session
// already under the right name does nothing at all.
func (sess *Session) fileNews() {
	if sess == nil {
		return
	}
	sess.mu.Lock()
	agent, closed := sess.agent, sess.closed
	sess.mu.Unlock()
	key := ""
	if !closed {
		key = newsKeyOf(agent)
	}
	newsroom.mu.Lock()
	defer newsroom.mu.Unlock()
	if was, filed := newsroom.keys[sess]; filed {
		if was == key {
			return
		}
		unfileLocked(sess, was)
	}
	if key == "" {
		stopListeningLocked()
		return
	}
	if newsroom.rooms == nil {
		newsroom.rooms = map[string]map[*Session]struct{}{}
		newsroom.keys = map[*Session]string{}
	}
	room, open := newsroom.rooms[key]
	if !open {
		room = map[*Session]struct{}{}
		newsroom.rooms[key] = room
	}
	room[sess] = struct{}{}
	newsroom.keys[sess] = key
	startListeningLocked()
}

// dropNews takes this conversation out of the newsroom for good. It runs where
// a conversation ends, and it is idempotent.
func (sess *Session) dropNews() {
	if sess == nil {
		return
	}
	newsroom.mu.Lock()
	defer newsroom.mu.Unlock()
	if was, filed := newsroom.keys[sess]; filed {
		unfileLocked(sess, was)
	}
	stopListeningLocked()
}

// unfileLocked removes one session from one room, and the room with it when it
// empties. The caller holds the newsroom's lock.
func unfileLocked(sess *Session, key string) {
	delete(newsroom.keys, sess)
	room, open := newsroom.rooms[key]
	if !open {
		return
	}
	delete(room, sess)
	if len(room) == 0 {
		delete(newsroom.rooms, key)
	}
}

// startListeningLocked takes the desk, once. The caller holds the newsroom's
// lock, and neither door calls a reader while it registers one, so holding it
// across the registration cannot re-enter.
func startListeningLocked() {
	if newsroom.listening {
		return
	}
	newsroom.listening = true
	newsroom.prevPhase = session.OnPhaseNews(fanPhaseNews)
	newsroom.prevLane = session.OnLaneNews(fanLaneNews)
}

// stopListeningLocked puts it back when the last conversation has gone. See the
// newsroom's own comment for why a host holding nothing must read as a build
// with nobody listening.
func stopListeningLocked() {
	if !newsroom.listening || len(newsroom.keys) > 0 {
		return
	}
	newsroom.listening = false
	session.OnPhaseNews(newsroom.prevPhase)
	session.OnLaneNews(newsroom.prevLane)
	newsroom.prevPhase, newsroom.prevLane = nil, nil
}

// newsroomFor is every conversation filed under one name, as a snapshot taken
// under the lock and read outside it — the shape [Session.announce] uses, for
// its reason: a slow surface may not hold the newsroom while a turn is running.
func newsroomFor(key string) []*Session {
	if key == "" {
		return nil
	}
	newsroom.mu.Lock()
	defer newsroom.mu.Unlock()
	room := newsroom.rooms[key]
	if len(room) == 0 {
		return nil
	}
	filed := make([]*Session, 0, len(room))
	for sess := range room {
		filed = append(filed, sess)
	}
	return filed
}

// fanPhaseNews puts one phase on the connections of the conversation it names.
//
// NEWS THAT CAME OFF A WIRE IS NEVER SENT BACK OUT. A build that is both
// serving and watching — which is every test that drives an engine host inside
// its own process — would otherwise forward what it just received straight back
// out of the door it came in, forever ([provider.PhaseNews.Relayed]).
func fanPhaseNews(news session.PhaseNews) {
	if news.Relayed {
		return
	}
	filed := newsroomFor(news.Session)
	if len(filed) == 0 {
		return
	}
	payload, err := json.Marshal(phaseWireOf(news))
	if err != nil {
		return
	}
	frame := Frame{Kind: "phase", Payload: payload}
	for _, sess := range filed {
		sess.offerNews(frame)
	}
}

// fanLaneNews is its twin for a finished answer's sighting.
func fanLaneNews(news session.LaneNews) {
	if news.Relayed {
		return
	}
	filed := newsroomFor(news.Session)
	if len(filed) == 0 {
		return
	}
	payload, err := json.Marshal(laneWireOf(news))
	if err != nil {
		return
	}
	frame := Frame{Kind: "lane", Payload: payload}
	for _, sess := range filed {
		sess.offerNews(frame)
	}
}

// offerNews hands one frame to every surface in this conversation's room,
// without waiting on any of them.
func (sess *Session) offerNews(frame Frame) {
	sess.mu.Lock()
	feeds := make([]*newsFeed, 0, len(sess.newsfeeds))
	for _, feed := range sess.newsfeeds {
		feeds = append(feeds, feed)
	}
	sess.mu.Unlock()
	for _, feed := range feeds {
		feed.offer(frame)
	}
}

// watchNews opens one surface's outbox. It runs at the END of an arrival, after
// the welcome and the replay behind it are on the wire, so that a phase measured
// while the surface was being welcomed cannot overtake the welcome that tells it
// what conversation it is in.
func (sess *Session) watchNews(s *server) {
	feed := &newsFeed{frames: make(chan Frame, newsBacklog), done: make(chan struct{})}
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		return
	}
	if _, here := sess.surfaces[s]; !here {
		sess.mu.Unlock()
		return
	}
	if sess.newsfeeds == nil {
		sess.newsfeeds = map[*server]*newsFeed{}
	}
	previous := sess.newsfeeds[s]
	sess.newsfeeds[s] = feed
	sess.mu.Unlock()
	previous.leave()
	go sess.pumpNews(s, feed)
}

// dropNewsFeed ends one surface's outbox. It runs where a surface leaves the
// room, beside the rail's own subscription.
func (sess *Session) dropNewsFeed(s *server) {
	sess.mu.Lock()
	feed := sess.newsfeeds[s]
	delete(sess.newsfeeds, s)
	sess.mu.Unlock()
	feed.leave()
}

// pumpNews puts one surface's outbox on its wire.
//
// IT STOPS ON THE FIRST WRITE THAT FAILS, which is the difference between this
// and [Session.pumpTasks]: a task lane must keep draining a channel somebody
// else is writing to, and this channel's only writer already gives up when the
// outbox is full. A dead pipe ends the goroutine and the frames that were in
// the outbox go with it, which is right — every one of them was a claim about a
// moment that has passed.
func (sess *Session) pumpNews(s *server, feed *newsFeed) {
	defer guard.Recover("remote/engine news lane")
	for {
		select {
		case <-feed.done:
			return
		case frame := <-feed.frames:
			sess.mu.Lock()
			stale := sess.closed || sess.newsfeeds[s] != feed
			sess.mu.Unlock()
			if stale {
				return
			}
			if err := s.send(frame); err != nil {
				return
			}
		}
	}
}

// ── the two shapes, both ways ───────────────────────────────────────────────

// phaseWireOf is one phase as it crosses. The moments become elapsed times for
// the reason [PhaseWire] states: the surface ages a phase out against its own
// clock, and two machines need not agree about what time it is.
func phaseWireOf(news session.PhaseNews) PhaseWire {
	at := news.At
	if at.IsZero() {
		at = time.Now()
	}
	wire := PhaseWire{
		Phase:  string(news.Phase),
		Model:  news.Model,
		Role:   string(news.Role),
		Lane:   news.Lane,
		Rate:   news.Rate,
		Detail: news.Detail,
		Then:   news.Then,
	}
	if !news.Since.IsZero() {
		if lasted := at.Sub(news.Since); lasted > 0 {
			wire.SinceMS = lasted.Milliseconds()
		}
	}
	if !news.Deadline.IsZero() {
		if left := news.Deadline.Sub(at); left > 0 {
			wire.DeadlineMS = left.Milliseconds()
		}
	}
	return wire
}

// phaseNewsOf is the other direction, against the moment the frame landed HERE.
// Every clock on this surface is read against this machine's now, so this is the
// only reading either side can safely use.
func phaseNewsOf(wire PhaseWire, at time.Time) session.PhaseNews {
	news := session.PhaseNews{
		Phase:   session.Phase(wire.Phase),
		Model:   wire.Model,
		Role:    lane.Role(wire.Role),
		Lane:    wire.Lane,
		Rate:    wire.Rate,
		Detail:  wire.Detail,
		Then:    wire.Then,
		Since:   at.Add(-time.Duration(wire.SinceMS) * time.Millisecond),
		At:      at,
		Relayed: true,
	}
	if wire.DeadlineMS > 0 {
		news.Deadline = at.Add(time.Duration(wire.DeadlineMS) * time.Millisecond)
	}
	return news
}

// laneWireOf is one finished answer's sighting as it crosses.
func laneWireOf(news session.LaneNews) LaneWire {
	return LaneWire{
		Model:  news.Model,
		Lane:   news.Lane,
		Alt:    news.Alt,
		Winner: news.Winner,
		TTFTMS: news.TTFT.Milliseconds(),
		Rate:   news.Rate,
		Hedged: news.Hedged,
		Trying: news.Trying,
		Reason: news.Reason,
		Failed: news.Failed,
		Role:   string(news.Role),
	}
}

// laneNewsOf is the other direction, stamped with the moment the frame landed
// here for [phaseNewsOf]'s reason.
func laneNewsOf(wire LaneWire, at time.Time) session.LaneNews {
	return session.LaneNews{
		Model:   wire.Model,
		Lane:    wire.Lane,
		Alt:     wire.Alt,
		Winner:  wire.Winner,
		TTFT:    time.Duration(wire.TTFTMS) * time.Millisecond,
		Rate:    wire.Rate,
		Hedged:  wire.Hedged,
		Trying:  wire.Trying,
		Reason:  wire.Reason,
		Failed:  wire.Failed,
		Role:    lane.Role(wire.Role),
		At:      at,
		Relayed: true,
	}
}

// ── the surface half ────────────────────────────────────────────────────────

// phaseFrame is one "phase" frame off the wire, handed to this process's own
// desk so that the surface's registered reader fires exactly as it does for a
// phase measured in this process (internal/session's [session.TellPhase]).
//
// IT IS TAKEN ON THE READER GOROUTINE, which is where a "facts" frame is taken
// and for its reason: the desk must be current before the surface is woken to
// repaint off it, and reading a clock never waits for an update loop to catch
// up.
func (c *Client) phaseFrame(payload json.RawMessage) {
	var wire PhaseWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		return
	}
	session.TellPhase(phaseNewsOf(wire, time.Now()))
}

// laneNewsFrame is its twin for the sighting a finished far answer left behind.
func (c *Client) laneNewsFrame(payload json.RawMessage) {
	var wire LaneWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		return
	}
	session.TellLane(laneNewsOf(wire, time.Now()))
}

// laneOfferDoor is an engine that can answer a standing lane offer
// ([session.Agent.AnswerLaneOffer]). It is asserted and never required, so an
// engine that has no offers to answer makes the capability ABSENT rather than
// present and refusing.
type laneOfferDoor interface {
	AnswerLaneOffer(yes bool) bool
}
