package cellstore

import (
	"os"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// TailQuiet is how long the record must be still, after a turn ended, before
// the writes that came after the turn-end seal are sealed. Titles, summaries and
// memories are written seconds after the answer, in bursts; one seal after the
// burst covers all of it and costs one upload.
const TailQuiet = 2 * time.Second

// SealWatch remembers whether the last seal held, so a seat that stops sealing
// is a state a person can see for as long as it lasts and not one sentence
// said once per launch.
//
// IT IS THE ONE HOLDER OF THAT FACT. The recorder reports every seal's outcome
// to [SealWatch.Report]; a surface asks [SealWatch.Failing] to draw its status
// segment and drains [SealWatch.Take] for the sentences worth saying. Three
// changes are worth a sentence and nothing else is: sealing starting to fail,
// its cause changing while it is still failing, and sealing recovering. The
// same failure repeating is the segment's business, never another line.
//
// The zero value is ready to use, and safe for concurrent use. A door with no
// surface to draw a segment on sets Say, and the sentences go there as they
// happen instead of waiting to be taken.
type SealWatch struct {
	turn cell.Cell

	// Say, when set, receives each sentence as it happens.
	Say func(string)
	// OnTurnEnd, when set, hears each time the agent finishes a turn. The door
	// sets it to the sync side's idle call.
	OnTurnEnd func()
	// OnTail, when set, is called once the record has been quiet for Quiet after
	// writes that came behind the turn-end seal. The door seals the tree and asks
	// the sync side to upload, exactly as it does at the end of a turn, so there
	// is one way to make a record durable and this is only another moment to use
	// it.
	OnTail func()
	// Quiet is TailQuiet unless a test sets it shorter.
	Quiet   time.Duration
	mu      sync.Mutex
	idle    bool
	started bool
	tail    *time.Timer
	cause   string
	said    []string
	failing bool
	// requiredEnd follows conversation writes, so later bookkeeping cannot hide recovery.
	requiredEnd int64
}

// Report takes the outcome of one seal: nil when it held, the error when it did
// not. It matches [Options.Report], so a watch's method is the recorder's hook.
func (w *SealWatch) Report(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err == nil {
		w.recover()
		return
	}
	w.requireTranscript()
	w.fail(err.Error())
}

// Note says a sentence that is not about a seal failing, such as a file the
// guard kept out of the saved history. It never changes whether sealing is
// failing, and it travels the same road as the watch's own sentences.
func (w *SealWatch) Note(sentence string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.say(sentence)
}

func (w *SealWatch) fail(cause string) {
	switch {
	case !w.failing:
		w.say("calls are not being sealed, so rewind cannot reach them: " + cause)
	case cause != w.cause:
		w.say("sealing still fails, for a different reason: " + cause)
	}
	w.failing, w.cause = true, cause
}

func (w *SealWatch) recover() {
	if w.turn.Root != "" && !w.ownTurnSaved() {
		return
	}
	if w.failing {
		w.say("sealing works again, and the calls made meanwhile are sealed now")
	}
	w.failing, w.cause = false, ""
}

func (w *SealWatch) say(sentence string) {
	if w.Say != nil {
		w.Say(sentence)
		return
	}
	w.said = append(w.said, sentence)
}

// Waiting arms a newly constructed chat that is waiting for its first input.
// It schedules nothing until a write arrives. Once a turn has started or ended,
// calling it again cannot change that turn's state.
func (w *SealWatch) Waiting() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.started {
		w.idle = true
	}
}

// TurnStarted is the session's word that a turn began: the turn writes the
// record itself and every call seals it, so no tail seal is owed.
func (w *SealWatch) TurnStarted() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.started = true
	w.idle = false
	w.stopTail()
}

// Wrote is the session's word that something the chat owns was written: a line
// of its transcript or a row of its memories. Written while the agent waits for
// the person, it is behind the last seal and no turn is coming to seal it, so a
// trailing seal is owed once the writes stop. Each write moves it back, so a
// burst of them is one seal.
func (w *SealWatch) Wrote() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.idle || w.OnTail == nil {
		return
	}
	w.stopTail()
	w.tail = time.AfterFunc(w.quiet(), w.sealTail)
}

func (w *SealWatch) quiet() time.Duration {
	if w.Quiet > 0 {
		return w.Quiet
	}
	return TailQuiet
}

func (w *SealWatch) stopTail() {
	if w.tail != nil {
		w.tail.Stop()
		w.tail = nil
	}
}

func (w *SealWatch) sealTail() {
	w.mu.Lock()
	owed := w.idle && w.tail != nil
	w.tail = nil
	w.mu.Unlock()
	if owed {
		w.OnTail()
	}
}

// TurnEnded is the session's word that the agent finished a turn. It only
// forwards, so a watch without a listener costs nothing.
func (w *SealWatch) TurnEnded() {
	w.mu.Lock()
	w.started = true
	w.idle = true
	w.requireTranscript()
	w.stopTail()
	w.mu.Unlock()
	if w.OnTurnEnd != nil {
		w.OnTurnEnd()
	}
}

// Failing reports whether this chat still has a saving failure.
func (w *SealWatch) Failing() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failing && w.turn.Root != "" {
		w.recover()
	}
	return w.failing
}

// Take hands over the oldest sentence not yet read, or "" when there is none.
func (w *SealWatch) Take() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failing && w.turn.Root != "" {
		w.recover()
	}
	if len(w.said) == 0 {
		return ""
	}
	sentence := w.said[0]
	w.said = w.said[1:]
	return sentence
}

// TrackTurn binds recovery notices to this chat's saved transcript.
func (w *SealWatch) TrackTurn(root string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.turn = cell.Cell{Root: root}
	w.requiredEnd = 0
	w.requireTranscript()
}

func (w *SealWatch) requireTranscript() {
	if w.turn.Root != "" {
		if info, err := os.Stat(rel(w.turn, cell.TranscriptPath)); err == nil {
			w.requiredEnd = max(w.requiredEnd, info.Size())
		}
	}
}

// A successful background seal cannot speak for the chat's unsaved answer.
// Synced turns also retain their failure until the publish boundary covers it.
func (w *SealWatch) ownTurnSaved() bool {
	if w.started && !w.idle {
		return false
	}
	captures.Lock()
	state := captures.cells[w.turn.Root]
	if state != nil {
		select {
		case <-state.done:
			if state.err != nil {
				captures.Unlock()
				return false
			}
		default:
			captures.Unlock()
			return false
		}
	}
	captures.Unlock()
	head, err := Head(w.turn)
	if err != nil || head == nil {
		return false
	}
	info, err := os.Stat(rel(w.turn, cell.TranscriptPath))
	if err != nil {
		return false
	}
	requiredEnd := w.requiredEnd
	if !w.idle {
		requiredEnd = max(requiredEnd, info.Size())
	}
	if info.Size() < requiredEnd || head.Receipt.Transcript.End < requiredEnd {
		return false
	}
	if head.Turn.Fence != 0 || requiresPublication(w.turn, head.Receipt.Transcript.End) {
		unpublished, _, err := Unpublished(w.turn, "")
		if err != nil || unpublished != "" {
			return false
		}
	}
	return true
}
