package homes

import (
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Voice (5.24): "the mic lives on the place-line row of every composer and
// targets the selected surface. Dictation into a steer line renders visibly
// one-way (↦), so voice never smuggles a chat into a room that has none."
//
// The second sentence is the whole reason this is a component and not a glyph
// somebody adds to a line. Speech has no addressee in it. A person dictating
// into a worker's steer line is writing MAIL — absorbed between turns, never
// answered (5.11) — and if the affordance looks the same as dictating into a
// chat, voice becomes the one input that can put a sentence somewhere the
// person did not think they were putting it. So the mark travels with the mic,
// and it is the composer's own prompt glyph, so the two can never disagree.
//
// # What already exists, and what this adds
//
// The dictation stack is REAL and shipped: internal/voice owns microphone
// capture and OpenRouter speech-to-text behind two small interfaces
// (voice.Recorder, voice.Transcriber), cmd/aforge/chat.go constructs both, and
// (*command.Commander).VoiceRecorder / .VoiceTranscriber hand them over. The
// old chat drives them from internal/tui/voice.go with a four-state machine —
// idle, starting, recording, finalizing — chunked by a VAD so partial text
// merges into the draft while the person is still talking.
//
// What was missing is 5.24's half: a PLACE for the control and a rule about
// where the words land. The old surface has neither — the mic is a key with a
// transient hint line, it draws no cell of its own, and nothing in it knows
// whether the draft is bound to a chat or to a steer line. So this file adds
// the slot on the place-line row, the five states mapped 1:1 onto the machine
// that exists, and the one-way mark. It runs nothing and records nothing.

// MicState is what the mic is doing. The five map onto internal/tui/voice.go's
// four-state machine one for one, plus the refusal that surface has no state
// for:
//
//	voiceIdle       → [MicIdle], or [MicUnavailable] when the seam is nil
//	voiceStarting   → [MicStarting]
//	voiceRecording  → [MicListening]
//	voiceFinalizing → [MicTranscribing]
type MicState uint8

const (
	// MicUnavailable is no recorder or no transcriber — the nil seam the old
	// surface answers with "mic unavailable — check System Settings › Privacy ›
	// Microphone". It is the zero value, and it draws NOTHING: a permanently
	// dead control on every composer in the product would be four hundred
	// frames a day of an affordance that lies (5.20). The wiring says why in
	// the hint line, where a sentence fits.
	MicUnavailable MicState = iota
	// MicIdle is ready and not listening.
	MicIdle
	// MicStarting is opening the device — the window in which the old machine
	// discovers the microphone permission was refused.
	MicStarting
	// MicListening is taking audio now.
	MicListening
	// MicTranscribing is turning what it heard into text (the machine's
	// finalizing state).
	MicTranscribing
	// MicRefused is a mic that will not open HERE, with [Mic.Reason] saying
	// why: a settled row, a service room, a visitor window. It draws the idle
	// glyph in the chrome tier and the reason beside it — the affordance
	// stating its own limit rather than disappearing (5.20 rule 3). This is the
	// state the old surface has no room for, because it has no cell.
	MicRefused
)

// String names the state.
func (m MicState) String() string {
	switch m {
	case MicIdle:
		return "idle"
	case MicStarting:
		return "starting"
	case MicListening:
		return "listening"
	case MicTranscribing:
		return "transcribing"
	case MicRefused:
		return "refused"
	}
	return "unavailable"
}

// Mic is the affordance on a composer's place-line row. It is a value: the
// wiring owns the state machine, this owns the rendering.
type Mic struct {
	// State is what it is doing.
	State MicState
	// Target is the composer the dictation will land in — the SELECTED
	// surface's, not the focused pane's, because 5.24 says the mic targets the
	// selected surface and the two differ exactly when a person is reading one
	// room while another streams.
	//
	// [rail.ComposerSteer] is the case the mark exists for.
	// [rail.ComposerDisabled] and [rail.ComposerNone] mean there is nothing to
	// dictate into, and [Mic.Render] draws the refusal rather than the mic.
	Target rail.ComposerMode
	// Elapsed is how long the mic has been open. It is a LIVE cell and belongs
	// to a live region only (8.1.2). Zero drops it.
	Elapsed time.Duration
	// Reason is why the mic is refused, in the wiring's own words.
	Reason string
}

// Empty reports whether the mic draws nothing.
func (m Mic) Empty() bool { return m.State == MicUnavailable }

// glyph is the mic's cell.
//
// 5.17 has no mic and this package does not mint one. A microphone in the
// unicode chrome ranges is either an emoji (banned: double-width, untintable,
// confetti) or a dingbat with no width guarantee, and 12.7's tier would have
// nothing to upgrade it to. So the mic reuses the two step dots the vocabulary
// already owns — hollow at rest, filled while open — which is the same "is this
// running" reading they carry everywhere else, at one tintable cell, with a
// nerd-font tier already bound to them.
func (m Mic) glyph(g tokens.GlyphSet) string {
	switch m.State {
	case MicListening:
		return g.Glyph(tokens.GStepDone)
	case MicStarting, MicTranscribing:
		return g.Glyph(tokens.GStepRunning)
	}
	return g.Glyph(tokens.GStepPending)
}

// token is the mic's colour: cyan while it is alive, chrome otherwise. It never
// turns amber — a mic that is listening is not asking for anything, and 5.16
// spends amber on the one meaning.
func (m Mic) token() tokens.Token {
	switch m.State {
	case MicStarting, MicListening, MicTranscribing:
		return tokens.ResolveToken(tokens.HueAlive, tokens.StateLive)
	}
	return tokens.TextTertiary
}

// oneWay reports whether the dictation will land somewhere that cannot answer.
func (m Mic) oneWay() bool { return m.Target == rail.ComposerSteer }

// Text is the mic's cells as plain text, for width fitting and for a test that
// wants to read the row without an escape parser.
func (m Mic) Text() string { return m.text(tokens.Plain) }

func (m Mic) text(g tokens.GlyphSet) string {
	if m.Empty() {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.glyph(g))
	if m.oneWay() {
		b.WriteByte(' ')
		b.WriteString(g.Glyph(tokens.GPromptSteer))
	}
	if m.State == MicListening && m.Elapsed > 0 {
		b.WriteByte(' ')
		b.WriteString(tokens.Elapsed(m.Elapsed))
	}
	if m.State == MicRefused && m.Reason != "" {
		b.WriteByte(' ')
		b.WriteString(clean(m.Reason))
	}
	return b.String()
}

// Width is how many cells the mic wants on the place line.
func (m Mic) Width(st *tokens.Styler) int {
	return blocks.Width(m.text(glyphSetOf(st)))
}

// Render draws the mic into width cells. It never wraps, never panics, and
// draws nothing at all when there is no recogniser.
//
// The one-way mark is drawn in the SAME token as the steer prompt it previews,
// so a person who has learned that ↦ means "this does not come back" reads it
// the same way here as in the composer below.
func (m Mic) Render(st *tokens.Styler, width int) string {
	if m.Empty() || width <= 0 {
		return ""
	}
	g := glyphSetOf(st)
	profile, focus := tokens.NoColor, tokens.FocusNormal
	if st != nil {
		profile, focus = st.Profile(), st.Focus()
	}
	var l lineBuf
	var buf strings.Builder
	l.reset(width)
	l.add(m.glyph(g), m.token())
	if m.oneWay() {
		l.add(" ", tokens.TextTertiary)
		l.add(g.Glyph(tokens.GPromptSteer), tokens.TextTertiary)
	}
	if m.State == MicListening && m.Elapsed > 0 {
		l.add(" ", tokens.TextTertiary)
		l.add(tokens.Elapsed(m.Elapsed), tokens.TextTertiary)
	}
	if m.State == MicRefused && m.Reason != "" {
		l.add(" ", tokens.TextTertiary)
		l.add(cleanFor(profile, m.Reason), tokens.TextTertiary)
	}
	return l.emit(&buf, profile, focus, width, false, tokens.Ground)
}

// glyphSetOf reads a Styler's tier, defaulting to the designed floor for a nil
// one — the same degradation every other constructor here makes.
func glyphSetOf(st *tokens.Styler) tokens.GlyphSet {
	if st == nil {
		return tokens.Plain
	}
	return st.GlyphSet()
}
