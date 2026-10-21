package cellstore

import (
	"sync"
	"time"
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
	tail    *time.Timer
	cause   string
	said    []string
	failing bool
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

// TurnStarted is the session's word that a turn began: the turn writes the
// record itself and every call seals it, so no tail seal is owed.
func (w *SealWatch) TurnStarted() {
	w.mu.Lock()
	defer w.mu.Unlock()
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
	w.idle = true
	w.stopTail()
	w.mu.Unlock()
	if w.OnTurnEnd != nil {
		w.OnTurnEnd()
	}
}

// Failing reports whether the most recent seal failed.
func (w *SealWatch) Failing() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.failing
}

// Take hands over the oldest sentence not yet read, or "" when there is none.
func (w *SealWatch) Take() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.said) == 0 {
		return ""
	}
	sentence := w.said[0]
	w.said = w.said[1:]
	return sentence
}
