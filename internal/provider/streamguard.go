package provider

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// THE GUARD OVER ONE MODEL STREAM.
//
// Two things go wrong with a request that the retry loop in retry.go cannot see,
// because both of them happen AFTER the response headers have arrived and a
// perfectly healthy-looking stream is open:
//
//   - SILENCE. The endpoint accepts the request and then says nothing, or says
//     so little that the answer will never arrive. The body watchdog in
//     transport.go counts ANY BYTE as progress, keepalive comments included, so
//     an endpoint that trickles colons down the wire forever is invisible to it.
//     What this counts instead is the model WRITING: a token of answer, a token
//     of reasoning, a fragment of a tool call. Nothing else moves the clock.
//
//   - DEGENERATION. The model loses the thread and writes soup — a run of one
//     letter, a paragraph repeated until the token budget is gone, words with
//     three alphabets inside them. It is a real thing that happened to this
//     session on 2026-08-20 against a 150k-token context, twice in one
//     conversation, and the second time was worse than the first BECAUSE THE
//     FIRST ONE WAS STILL IN THE CONTEXT. Junk in the transcript breeds junk.
//
// Both answers are the same shape: cut the request, say so, ask again. The
// asking again is [Agent.completeWithRetry]'s in internal/session, which already
// throws away the dead attempt's partial text, its warm tool batch and its
// half-arrived calls — this layer only detects, cuts, and names what happened.
//
// WHAT THIS LAYER NEVER DOES is decide the person's words. A cut leaves through
// [StreamCut], which internal/session turns into the sentence a person reads,
// for the reason patience.go gives about its own two seams: the words live where
// the words live.

const (
	// firstDeltaBound is how long a request may be open with the model having
	// written NOTHING — no answer token, no reasoning token, no tool-call
	// fragment.
	//
	// It is generous on purpose. A reasoning model at a long context legitimately
	// thinks for a minute before its first token, and cutting a call that was
	// about to answer is worse than waiting: the retry pays the whole prompt
	// again. Ninety seconds is past every healthy first token this adapter has
	// measured (velocity.go's ledger) and short of the two-minute header
	// deadline, so a stall is named here rather than surfacing as a torn
	// connection.
	firstDeltaBound = 90 * time.Second
	// midStreamGapBound is how long an ESTABLISHED stream may go quiet. It is
	// half the first bound because the question is different: a model that has
	// started writing has finished deciding, and forty-five seconds between two
	// tokens of one sentence is a connection that is not coming back.
	midStreamGapBound = 45 * time.Second
)

// stallFirstBound and stallGapBound are what the watchdog actually reads. The
// constants above are the figures — one source of truth for the manual page and
// for the sentence a cut is named with — and these exist only so a test can
// prove the machinery in milliseconds rather than in minutes.
var (
	stallFirstBound = firstDeltaBound
	stallGapBound   = midStreamGapBound
)

// CutReason says which of the two things went wrong, and it is the only thing
// this package decides about a cut. The sentence is composed upstream.
type CutReason int

const (
	// CutSilent is a request that produced nothing within firstDeltaBound.
	CutSilent CutReason = iota
	// CutStalled is a request that started writing and then went quiet for
	// midStreamGapBound.
	CutStalled
	// CutBabble is a reply that stopped being language: a repetition loop, or
	// text switching alphabet inside its own words.
	CutBabble
)

// StreamCut is the error a guarded stream fails with. It is a distinct type
// rather than a message because the decision upstream — retry, and how many
// times — is made on the reason, and a decision made by matching substrings of
// an error string is a decision that breaks the next time somebody rewords it.
type StreamCut struct {
	Reason CutReason
	// Waited is how long the stream was quiet, on the two silence reasons, and
	// zero on CutBabble. It is the constant that fired rather than a measurement:
	// the timer is what decided, so the timer's own bound is the honest figure.
	Waited time.Duration
}

func (c *StreamCut) Error() string {
	if c == nil {
		return ""
	}
	switch c.Reason {
	case CutSilent:
		return fmt.Sprintf("nothing came back from the model in %s", roundSeconds(c.Waited))
	case CutStalled:
		return fmt.Sprintf("the model stopped mid-reply and went quiet for %s", roundSeconds(c.Waited))
	default:
		return "the reply stopped being language and was cut"
	}
}

// CutFrom reports whether an error is a guarded stream's cut, and which kind.
func CutFrom(err error) (*StreamCut, bool) {
	for err != nil {
		if cut, ok := err.(*StreamCut); ok {
			return cut, true
		}
		unwrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return nil, false
		}
		err = unwrapped.Unwrap()
	}
	return nil, false
}

// roundSeconds spells a bound the way a person says it. The constants above are
// whole seconds, so this is exact rather than approximate.
func roundSeconds(d time.Duration) string {
	return d.Round(time.Second).String()
}

// ── the off switch ──────────────────────────────────────────────────────────

type babbleGuardKey struct{}

// WithoutBabbleGuard takes the degeneration guard off every call made under
// ctx. The silence watchdog has no switch and is not affected: a request that
// produced nothing in ninety seconds has failed by any reading, and there is no
// preference under which sitting on it is the answer.
//
// It rides the context for the reason patience.go's seams do — the adapter is
// shared by every agent in the process, so a field on the client would make a
// task node's setting the conversation's.
func WithoutBabbleGuard(ctx context.Context) context.Context {
	return context.WithValue(ctx, babbleGuardKey{}, false)
}

// babbleGuardOn reports whether this call watches for degeneration. Default on:
// an unstamped context is every call that existed before this did.
func babbleGuardOn(ctx context.Context) bool {
	if ctx == nil {
		return true
	}
	on, stamped := ctx.Value(babbleGuardKey{}).(bool)
	return !stamped || on
}

// ── the silence watchdog ────────────────────────────────────────────────────

// stallWatch cuts a request that is not being written to.
//
// One timer, reset by the model writing anything. It fires at most once — the
// cancel it pulls is the attempt's, and pulling it twice would be a second cut
// of a stream that is already dead.
type stallWatch struct {
	mu      sync.Mutex
	timer   *time.Timer
	cancel  context.CancelFunc
	spoken  bool
	tripped *StreamCut
}

func newStallWatch(cancel context.CancelFunc) *stallWatch {
	watch := &stallWatch{cancel: cancel}
	watch.timer = time.AfterFunc(stallFirstBound, func() { watch.fire() })
	return watch
}

// progress says the model wrote something. It restarts the clock at the
// mid-stream bound, because from the first token onwards the question is about
// gaps rather than about the wait to be served.
func (w *stallWatch) progress() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.tripped != nil {
		return
	}
	w.spoken = true
	w.timer.Reset(stallGapBound)
}

func (w *stallWatch) fire() {
	w.mu.Lock()
	if w.tripped != nil {
		w.mu.Unlock()
		return
	}
	if w.spoken {
		w.tripped = &StreamCut{Reason: CutStalled, Waited: stallGapBound}
	} else {
		w.tripped = &StreamCut{Reason: CutSilent, Waited: stallFirstBound}
	}
	cancel := w.cancel
	w.mu.Unlock()
	cancel()
}

// cut is the trip, or nil. It is read after the stream has died, to tell a
// watchdog's cancellation apart from the person's own interrupt.
func (w *stallWatch) cut() *StreamCut {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.tripped
}

func (w *stallWatch) stop() {
	w.mu.Lock()
	timer := w.timer
	w.mu.Unlock()
	timer.Stop()
}

// ── the degeneration guard ──────────────────────────────────────────────────

const (
	// babbleWindow is the tail of assistant text the compression test reads, in
	// bytes. Four kilobytes is long enough to hold several repetitions of a
	// sentence-length cycle and short enough that the whole window is about the
	// same thought.
	babbleWindow = 4 << 10
	// babbleFloor is the compressed-to-raw ratio below which the window is a
	// loop of SOME cycle length — one letter, one line, one paragraph; the test
	// does not care which, which is the whole reason it is a compressor and not
	// a pattern.
	//
	// Measured against the two real degenerations of 2026-08-20 and a corpus of
	// legitimate replies (streamguard_test.go): the '    0\n' loop bottoms out
	// at 0.015, while a markdown table reaches 0.22, ASCII art 0.086, and prose
	// 0.24. Three and a half percent sits in the empty middle.
	babbleFloor = 0.035
	// babbleEvery is how much new text must arrive before the window is read
	// again. The test runs at DELTA CADENCE and not per byte: a compressor over
	// four kilobytes costs tens of microseconds, which is nothing once every
	// half-kilobyte and a real tax once per token.
	babbleEvery = 512
	// churnWindow is the tail the script test reads, in RUNES.
	churnWindow = 600
	// churnBound is mid-word alphabet switches per hundred runes, above which
	// the text is not being written in any language. Legitimate multilingual
	// writing switches at word boundaries; every innocent in the corpus scores
	// exactly zero, and the mixed-script degeneration scores 5.5.
	churnBound = 3.0
	// churnScripts is how many alphabets must be PRESENT before the churn figure
	// is believed at all. Two is a bilingual answer, which is ordinary.
	churnScripts = 3
	// churnRunes is how many runes an alphabet needs in the window to count as
	// present, so one loanword, one quoted name or one emoji is never a third
	// alphabet.
	churnRunes = 5
	// cleanCap bounds the tail this keeps. It is twice the compression window so
	// that the window is always full of real text after a fenced block passes.
	cleanCap = 2 * babbleWindow
)

// babbleWatch reads the assistant's TEXT as it streams and says when it has
// stopped being language.
//
// IT NEVER SEES A TOOL RESULT. A tool that prints a million zeros is doing its
// job, and a guard that read results would cut the turn that asked for them —
// so this is fed from the content deltas of the model's own reply and from
// nothing else.
//
// FENCED CODE IS EXCLUDED for the same reason: a zero matrix, a test log and a
// generated table are all legitimate replies that compress to nothing, and all
// three arrive inside ``` fences. A fence is honoured only when it is
// well-formed — an opener whose info string looks like a language tag, a closer
// that is bare — because the 2026-08-20 degeneration emitted
// "```ongoingSpark........................" mid-soup, and a scanner that
// believed that would have been blinded by the very text it was watching for.
type babbleWatch struct {
	// clean is the tail of text OUTSIDE fenced code, oldest bytes dropped.
	clean []byte
	// pending is the line being written, which is not yet known to be a fence.
	pending []byte
	inFence bool
	// since counts bytes of clean text added since the last test.
	since int
}

func (b *babbleWatch) write(delta string) bool {
	for len(delta) > 0 {
		at := strings.IndexByte(delta, '\n')
		if at < 0 {
			b.grow(delta)
			break
		}
		b.grow(delta[:at+1])
		b.endLine()
		delta = delta[at+1:]
	}
	// A line long enough to be the whole window cannot be a fence marker, and
	// holding it would let a model that never presses return write past the
	// guard entirely.
	if len(b.pending) > babbleWindow {
		b.endLine()
	}
	if b.since < babbleEvery {
		return false
	}
	b.since = 0
	return b.tripped()
}

// grow adds one piece of the line being written, and counts it towards the next
// test.
//
// THE COUNTER IS BYTES OF WATCHED TEXT and not bytes of settled line, because a
// model in a repetition loop may never press return: the first version counted
// only completed lines, and an excerpt of the real 2026-08-20 soup — fifteen
// hundred runes with not one newline in them — streamed past the guard because
// the test was never due. Text inside a fence is not watched and is not counted,
// so a ten-megabyte code block costs one test rather than twenty thousand.
func (b *babbleWatch) grow(piece string) {
	b.pending = append(b.pending, piece...)
	if !b.inFence {
		b.since += len(piece)
	}
}

// endLine settles the pending line: it either toggles the fence, or joins the
// clean tail.
func (b *babbleWatch) endLine() {
	line := b.pending
	b.pending = nil
	if kind, ok := fenceLine(line); ok {
		switch {
		case b.inFence && kind != fenceOpen:
			b.inFence = false
			return
		case !b.inFence && kind != fenceClose:
			b.inFence = true
			return
		}
	}
	if b.inFence {
		return
	}
	b.clean = append(b.clean, line...)
	if len(b.clean) > cleanCap {
		b.clean = append(b.clean[:0], b.clean[len(b.clean)-cleanCap:]...)
	}
}

// window is the text the two tests read: the clean tail plus whatever line is
// still being written, when that line is not inside a fence.
func (b *babbleWatch) window() []byte {
	if b.inFence || len(b.pending) == 0 {
		return b.clean
	}
	return append(append(make([]byte, 0, len(b.clean)+len(b.pending)), b.clean...), b.pending...)
}

func (b *babbleWatch) tripped() bool {
	window := b.window()
	return loopedTail(window) || churnedTail(window)
}

type fenceKind int

const (
	fenceOpen fenceKind = iota
	fenceClose
	fenceEither
)

// fenceLine reports whether one line is a markdown code fence, and which end.
//
// A CLOSER IS BARE and an OPENER MAY CARRY A LANGUAGE TAG, and the tag has to
// look like one: up to twenty characters of the alphabet a language name is
// spelled in, and nothing else. Everything looser was tried against the real
// degenerate text and let it hide.
func fenceLine(line []byte) (fenceKind, bool) {
	trimmed := bytes.TrimSpace(line)
	if !bytes.HasPrefix(trimmed, []byte("```")) {
		return fenceEither, false
	}
	info := trimmed[3:]
	if len(info) == 0 {
		return fenceEither, true
	}
	if len(info) > 20 {
		return fenceEither, false
	}
	for _, r := range string(info) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '+', r == '#', r == '-', r == '_':
		default:
			return fenceEither, false
		}
	}
	return fenceOpen, true
}

// loopedTail is the COMPRESSION COLLAPSE test: a window that zlib squeezes below
// babbleFloor is a repetition of some cycle, and the cycle's length does not
// matter to the answer.
//
// The window must be FULL before this may say anything. A short reply that
// happens to be one repeated line is somebody answering "no, no, no" and is not
// a model that has come off the rails.
func loopedTail(window []byte) bool {
	if len(window) < babbleWindow {
		return false
	}
	tail := window[len(window)-babbleWindow:]
	var counter countingWriter
	writer := zlib.NewWriter(&counter)
	if _, err := writer.Write(tail); err != nil {
		return false
	}
	if err := writer.Close(); err != nil {
		return false
	}
	return float64(counter.n)/float64(len(tail)) < babbleFloor
}

// countingWriter is how many bytes the compressor produced. The compressed
// bytes themselves are never wanted, so they are never kept.
type countingWriter struct{ n int }

func (c *countingWriter) Write(p []byte) (int, error) {
	c.n += len(p)
	return len(p), nil
}

var _ io.Writer = (*countingWriter)(nil)

// churnedTail is the SCRIPT CHURN test: it counts alphabet switches BETWEEN TWO
// ADJACENT LETTERS, which is the difference between a multilingual answer and
// soup.
//
// A person writing about two languages puts a space, a comma or a quote between
// them — "the Russian is компьютер" switches alphabet at a boundary, and that
// switch is not counted here at all. A model that has lost the thread writes
// "стаthisada", where the switch is inside the word, and that is the only kind
// this counts.
//
// Latin, Han, Kana and Hangul are mutually TOLERANT, because CJK writing sets
// Latin words against native script with no separator as a matter of course:
// "このAPIはHTTPリクエストを受け取り" is ordinary Japanese and scored 27 switches
// per hundred runes before the tolerance existed.
func churnedTail(window []byte) bool {
	runes := []rune(string(window))
	if len(runes) < churnWindow {
		return false
	}
	runes = runes[len(runes)-churnWindow:]
	counts := make(map[scriptClass]int, 8)
	switches := 0
	previous := scriptNeutral
	for _, r := range runes {
		class := scriptOf(r)
		if class == scriptNeutral {
			previous = scriptNeutral
			continue
		}
		counts[class]++
		if previous != scriptNeutral && previous != class && !(tolerant(previous) && tolerant(class)) {
			switches++
		}
		previous = class
	}
	present := 0
	for _, count := range counts {
		if count >= churnRunes {
			present++
		}
	}
	if present < churnScripts {
		return false
	}
	return float64(switches)*100/float64(churnWindow) >= churnBound
}

// scriptClass is one alphabet, coarsely. Digits, punctuation, whitespace, marks
// and emoji are scriptNeutral: they belong to no alphabet and they BREAK a run,
// so a letter on either side of one is never a mid-word switch.
type scriptClass uint8

const (
	scriptNeutral scriptClass = iota
	scriptLatin
	scriptGreek
	scriptCyrillic
	scriptArmenian
	scriptHebrew
	scriptArabic
	scriptDevanagari
	scriptThai
	scriptKana
	scriptHan
	scriptHangul
	scriptOther
)

// tolerant names the alphabets that legitimately sit against each other with no
// separator. See [churnedTail].
func tolerant(class scriptClass) bool {
	switch class {
	case scriptLatin, scriptHan, scriptKana, scriptHangul:
		return true
	}
	return false
}

func scriptOf(r rune) scriptClass {
	switch {
	case r < utf8.RuneSelf:
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return scriptLatin
		}
		return scriptNeutral
	case r >= 0x00C0 && r <= 0x024F, r >= 0x1E00 && r <= 0x1EFF:
		return scriptLatin
	case r >= 0x0370 && r <= 0x03FF, r >= 0x1F00 && r <= 0x1FFF:
		return scriptGreek
	case r >= 0x0400 && r <= 0x052F:
		return scriptCyrillic
	case r >= 0x0530 && r <= 0x058F:
		return scriptArmenian
	case r >= 0x0590 && r <= 0x05FF:
		return scriptHebrew
	case r >= 0x0600 && r <= 0x06FF, r >= 0x0750 && r <= 0x077F:
		return scriptArabic
	case r >= 0x0900 && r <= 0x097F:
		return scriptDevanagari
	case r >= 0x0E00 && r <= 0x0E7F:
		return scriptThai
	case r >= 0x3040 && r <= 0x30FF:
		return scriptKana
	case r >= 0x3400 && r <= 0x4DBF, r >= 0x4E00 && r <= 0x9FFF, r >= 0xF900 && r <= 0xFAFF:
		return scriptHan
	case r >= 0x1100 && r <= 0x11FF, r >= 0xAC00 && r <= 0xD7AF:
		return scriptHangul
	case r >= 0xFF21 && r <= 0xFF5A:
		// Fullwidth Latin. It is the same alphabet typed on a CJK keyboard.
		return scriptLatin
	case unicode.IsLetter(r):
		return scriptOther
	}
	return scriptNeutral
}
