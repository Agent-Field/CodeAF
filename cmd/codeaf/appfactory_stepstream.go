package main

// A STEP'S TURNS REACH THE APP. A factory step runs on its own session agent
// ([stageConversation]), whose events are read where it was started, so no
// turn the app started carries them and the app's hold on that conversation
// only reads its journal. Each event is told here, and for a conversation the
// app holds it is said as the manager's are: words as deltas, and at each
// boundary the journal's new entries, read back so an id is the one
// `agents.entries` answers. A CONVERSATION THE APP DOES NOT HOLD COSTS A LOOKUP
// OF ITS ID AND READS NOTHING; it is read whole when the app opens it.
//
// An event only notes what happened. The saying runs on a timer, so a read of
// the journal never holds up the step that told it, and a held chat costs at
// most one read per appStepDeltaEvery, however many events came. The one read
// an event does itself is the first of a hold's, which fixes what the app
// already knows.
//
// The page reads a running step's newest entries as well, so what is lost here
// (an event before the hold, a read that failed, a boundary told before its
// line is on disk) is caught up there, under the same ids, with no entry drawn
// twice.
//
// A step's chat is not told turn.started, turn.done, turn.tool, thinking or
// retry notices. Whether a step is working comes from the job, and a retry
// notice would stay up with no turn end to clear it.

import (
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

const (
	// appStepDeltaEvery is the shortest time between two sayings of a held step
	// chat, however fast its tokens come.
	appStepDeltaEvery = 200 * time.Millisecond
	// appStepLook is how many of a conversation's newest entries one reading covers.
	appStepLook = 64
	// appStepTurnBase keeps a step turn's number apart from the turns the app numbers
	// itself. A person's message to a step is one of those, under the same conversation.
	appStepTurnBase = 1_000_000
)

// stageNews hears every event of a step's conversation, with its journal.
var stageNews atomic.Pointer[func(string, session.Event)]

func tellStageNews(file string, event session.Event) {
	if hear := stageNews.Load(); hear != nil {
		(*hear)(file, event)
	}
}

// appStepStream is what has been said of one held step conversation, on one hold.
type appStepStream struct {
	id   string
	live *appLive

	mu         sync.Mutex
	seeded     bool
	boundaries int // told since the last reading that covered them
	placed     bool
	withheld   bool // the message's words so far could still be the withdrawal
	turn       uint64
	msg        int
	words      strings.Builder
	timer      *time.Timer
	saying     bool
	said       map[string]string
}

// appStepPlacing is where a conversation's next words go.
type appStepPlacing struct {
	n, msg int
}

// listenStages starts hearing the steps' events, and answers the function that stops.
func (b *appBackend) listenStages() func() {
	hear := b.stageEvent
	stageNews.Store(&hear)
	return func() { stageNews.CompareAndSwap(&hear, nil) }
}

func appStepToldKind(kind session.EventKind) bool {
	switch kind {
	case session.EventTextDelta, session.EventAssistantDone, session.EventRetrying, session.EventToolBegin,
		session.EventToolEnd, session.EventToolFailed, session.EventToolFinished, session.EventTurnDone, session.EventError:
		return true
	}
	return false
}

// appStepFollowsItsLine says an event is told after the journal line it reports,
// so that line is news even to a hold's first reading.
func appStepFollowsItsLine(kind session.EventKind) bool {
	switch kind {
	case session.EventTextDelta, session.EventRetrying, session.EventToolBegin:
		return false
	}
	return true
}

// stageEvent notes one step event, when the app holds its conversation.
func (b *appBackend) stageEvent(file string, event session.Event) {
	if !appStepToldKind(event.Kind) {
		return
	}
	stream := b.stepStreamOf(appAgentID(file))
	if stream == nil {
		return
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if !stream.seeded {
		if page, _, err := b.Entries(stream.id, "", appStepLook); err == nil {
			stream.seed(page, appStepFollowsItsLine(event.Kind))
		}
	}
	switch event.Kind {
	case session.EventTextDelta:
		stream.words.WriteString(event.Text)
	case session.EventRetrying:
		stream.endMessage()
		return
	default:
		stream.flush()
		stream.endMessage()
		stream.boundaries++
	}
	b.stepArm(stream)
}

// stepStreamOf answers the stream of a held conversation, nil when it is not
// held. A conversation let go and held again has a new outbox, so a new stream.
func (b *appBackend) stepStreamOf(id string) (stream *appStepStream) {
	b.locked(func() {
		a := b.agents[id]
		if a == nil || a.live == nil {
			delete(b.stepStreams, id)
			return
		}
		stream = b.stepStreams[id]
		if stream != nil && stream.live == a.live {
			return
		}
		stream = &appStepStream{id: id, live: a.live, withheld: true, said: map[string]string{}}
		if b.stepStreams == nil {
			b.stepStreams = map[string]*appStepStream{}
		}
		b.stepStreams[id] = stream
	})
	return stream
}

// stepArm asks for a saying. The caller holds the stream's lock.
func (b *appBackend) stepArm(s *appStepStream) {
	if s.timer == nil && !s.saying {
		s.timer = time.AfterFunc(appStepDeltaEvery, func() { b.stepSay(s) })
	}
}

// stepSay says what the events noted. The journal is read with the lock let go,
// so the step can go on telling events. A boundary told during the read makes
// that reading stale for placing words, so another saying follows it. After a
// failed read nothing follows until the next event: the page's reading catches up.
func (b *appBackend) stepSay(s *appStepStream) {
	s.mu.Lock()
	s.timer, s.saying = nil, true
	seen := s.boundaries
	read := seen > 0 || !s.seeded || (!s.placed && s.speakable())
	s.mu.Unlock()

	var page []appwire.Entry
	var place appStepPlacing
	var err error
	if read {
		page, place, err = b.stepRead(s.id)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.saying = false
	if read && err != nil {
		return
	}
	if !s.seeded && read {
		s.seed(page, false)
	}
	if seen > 0 {
		s.sayEntries(page, place.n)
	}
	if s.boundaries != seen {
		b.stepArm(s)
		return
	}
	s.boundaries = 0
	if read && !s.placed && s.speakable() {
		s.turn, s.msg, s.placed = b.stepMark(s.id, place.n), place.msg, true
	}
	s.flush()
}

// stepRead reads the newest entries, and looks further back for the next words'
// place only when those are all tool rows.
func (b *appBackend) stepRead(id string) ([]appwire.Entry, appStepPlacing, error) {
	page, more, err := b.Entries(id, "", appStepLook)
	if err != nil {
		return nil, appStepPlacing{}, err
	}
	place, found := appStepPlace(page)
	for earlier := page; !found && more && len(earlier) > 0; {
		earlier, more, err = b.Entries(id, earlier[0].ID, appStepLook)
		if err != nil {
			return nil, appStepPlacing{}, err
		}
		place, found = appStepPlace(earlier)
	}
	return page, place, nil
}

// speakable says the words held can be said: they cannot be the withdrawal any
// more, which is never drawn ([session.NoChangeReply]). The caller holds the lock.
func (s *appStepStream) speakable() bool {
	if s.words.Len() == 0 {
		return false
	}
	return !s.withheld || !strings.HasPrefix(session.NoChangeReply, strings.TrimLeft(s.words.String(), " \t\r\n"))
}

// flush says the words held, when the message has its place and they can be
// said. It reads nothing. The caller holds the lock.
func (s *appStepStream) flush() {
	if !s.placed || !s.speakable() {
		return
	}
	s.live.box.Delta(s.id, s.turn, s.msg, appwire.DeltaText, s.words.String())
	s.words.Reset()
	s.withheld = false
}

// endMessage forgets the message being streamed. Its words not said are not
// lost: the entry that ends it carries them. The caller holds the lock.
func (s *appStepStream) endMessage() {
	s.words.Reset()
	s.placed, s.withheld = false, true
}

// seed takes a hold's first reading as what the app knows, since it read those
// entries when it opened the conversation. The newest stays news when the event
// that prompted the reading reports it. The caller holds the lock.
func (s *appStepStream) seed(page []appwire.Entry, newestIsNews bool) {
	s.seeded = true
	for _, e := range page {
		s.said[e.ID] = appStepFingerprint(e)
	}
	if newestIsNews && len(page) > 0 {
		delete(s.said, page[len(page)-1].ID)
	}
}

// sayEntries says what a reading adds to what was said. The caller holds the lock.
func (s *appStepStream) sayEntries(page []appwire.Entry, n int) {
	keep := make(map[string]string, len(page))
	for _, e := range page {
		fingerprint := appStepFingerprint(e)
		keep[e.ID] = fingerprint
		if s.said[e.ID] == fingerprint {
			continue
		}
		if e.Turn == 0 {
			e.Turn = appStepTurn(n)
		}
		s.live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: e})
	}
	s.said = keep
}

// appStepFingerprint says whether an entry changed. Its turn does not count: it
// appears when the words after it are placed, and that is no news of the entry.
func appStepFingerprint(e appwire.Entry) string {
	e.Turn = 0
	data, err := json.Marshal(e)
	if err != nil {
		return e.ID
	}
	return string(data)
}

// appStepPlace is where the next words of a conversation go, by the listing's
// own counts: the person's message they follow, -1 before any, and how many
// messages along from it. found is false when the page holds neither a person's
// message nor an answer, as a page of tool rows does.
func appStepPlace(page []appwire.Entry) (place appStepPlacing, found bool) {
	for i := len(page) - 1; i >= 0; i-- {
		e := page[i]
		n, ok := appEntryMessage(e.ID)
		switch {
		case !ok:
		case e.Kind == appwire.EntryUser:
			return appStepPlacing{n: n}, true
		case e.Msg != nil && (e.Kind == appwire.EntryAnswer || e.Kind == appwire.EntryUpdate):
			return appStepPlacing{n: n, msg: *e.Msg + 1}, true
		}
	}
	return appStepPlacing{n: -1}, false
}

// appStepTurn numbers the turn after the person's message n.
func appStepTurn(n int) uint64 { return uint64(appStepTurnBase + n + 1) }

// stepMark answers the turn message n's words are said under, and gives the
// message that turn, as a turn the app started has, so a later listing closes
// those words whichever way the page reads it: told at a boundary, or caught up
// by its own reading. A message the app already numbered keeps its number.
func (b *appBackend) stepMark(id string, n int) (turn uint64) {
	turn = appStepTurn(n)
	if n < 0 {
		return turn
	}
	b.locked(func() {
		a := b.agents[id]
		if a == nil {
			return
		}
		if mark, ok := a.marks[n]; ok {
			turn = mark.turn
			return
		}
		if a.marks == nil {
			a.marks = map[int]appTurnMark{}
		}
		a.marks[n] = appTurnMark{turn: turn}
	})
	return turn
}
