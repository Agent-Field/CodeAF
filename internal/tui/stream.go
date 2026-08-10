package tui

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// StreamEventKind names the provider-stream boundary exported by a Commander.
type StreamEventKind int

const (
	StreamStarted StreamEventKind = iota
	StreamDelta
	// StreamThinking is a phase with nothing to draw: the model is reasoning.
	// The text of that reasoning never reaches here and is not wanted — the
	// line says the wait has a reason, and nothing else.
	StreamThinking
	StreamFinished
	StreamFailed
)

// StreamEvent is the TUI-facing stream protocol. Head deltas carry the raw
// structured response; the lens extracts only its reply field before drawing.
// Session names the room the event belongs to; empty is the zero value every
// existing test constructs and means "no room asserted" rather than "no
// room" — applyStreamEvent only drops an event whose Session is set AND
// disagrees with the model's own, so today's single-session behavior is
// unchanged either way.
type StreamEvent struct {
	Kind    StreamEventKind
	Delta   string
	Session string
}

// Streams is the live token feed a window listens to while the head answers,
// and the third of this package's three interfaces. It stays nameable on its
// own — rather than being three more methods on Commander — because it is the
// one surface whose absence the reader can see: a window that has just lost the
// resident role must stop pretending to be streaming, and adoptCommander clears
// the channel rather than merely failing to set it. It is also where a keyed,
// per-room feed lands when rooms arrive; the Session field on StreamEvent is
// already the key.
type Streams interface {
	StreamEvents() <-chan StreamEvent
}

type streamClosedMsg struct{}

// streamBatchMsg carries every event a single wake found already queued. The
// reveal is paced by the animation tick, so draining the channel in one wake
// is invisible on screen and costs one goroutine round trip instead of one per
// token.
type streamBatchMsg struct {
	events []StreamEvent
}

type streamMode int

const (
	streamNone streamMode = iota
	streamReal
	streamSimulated
)

// The reveal's pacing is a floor and not a ceiling. Two words per 120ms frame
// is the cadence a reply unrolls at when the provider is the slow half — which
// is the ordinary case, and the one this number was chosen for. It stops being
// the whole rule the moment the provider is ahead: a reply that arrived in two
// seconds used to take twenty-four to draw, with the machine idle and the reader
// watching a typewriter re-type what was already paid for.
//
// So the tick spends what it has to spend to be at most a breath behind, and no
// more than that: streamLagTokens is the backlog it is content to carry, and
// streamMaxTokensPerTick keeps the fastest frame a line of text rather than a
// paragraph appearing whole.
const (
	streamTokensPerTick = 2
	// streamLagTokens is a dozen frames of the base cadence — about a second and
	// a half of unroll still owed, which is what makes a fast reply read as
	// typing rather than as a printer.
	streamLagTokens        = 24
	streamMaxTokensPerTick = 12
)

func waitForStream(events <-chan StreamEvent) tea.Cmd {
	if events == nil {
		return nil
	}
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return streamClosedMsg{}
		}
		// Everything already queued belongs to this wake. Adjacent deltas merge
		// into one event so the reply is decoded once per wake rather than once
		// per token; a boundary event keeps its own place in the order. A close
		// seen mid-drain is answered by the next wait, which finds the channel
		// closed immediately.
		batch := []StreamEvent{event}
		draining := true
		for draining {
			select {
			case next, open := <-events:
				if !open {
					draining = false
					break
				}
				last := &batch[len(batch)-1]
				if next.Kind == StreamDelta && last.Kind == StreamDelta {
					last.Delta += next.Delta
					continue
				}
				batch = append(batch, next)
			default:
				draining = false
			}
		}
		if len(batch) == 1 {
			return batch[0]
		}
		return streamBatchMsg{events: batch}
	}
}

func (m *Model) applyStreamEvent(event StreamEvent) {
	// A keyed event for a room this model is not showing is dropped rather
	// than drawn. Today there is exactly one room and the key always matches
	// (chat.go stamps the same session the TUI was opened with), so this is a
	// no-op in practice; it exists so the day a second room's events reach
	// this channel, they render into that room and not into whichever one
	// happens to be on screen.
	if event.Session != "" && event.Session != m.sessionID {
		return
	}
	switch event.Kind {
	case StreamStarted:
		// The store poll and event channel are independent. If the durable head
		// reply won that race, convert its simulated reveal into the real stream;
		// an unrelated deliverable waits behind the live reply instead.
		landedSeq := int64(0)
		if m.streamMode == streamSimulated {
			if landed, ok := m.messageBySeq(m.streamSeq); ok {
				if landed.Role == store.RoleAgent && landed.NodeID == "" {
					landedSeq = landed.Seq
				} else {
					m.streamQueue = append([]store.Message{landed}, m.streamQueue...)
				}
			}
		}
		m.streamMode = streamReal
		m.streamRaw.Reset()
		m.streamTarget = ""
		m.streamShown = ""
		m.streamSeq = landedSeq
		m.streamProviderDone = false
		m.streamInterrupted = false
		m.streamThinking = false
	case StreamThinking:
		if m.streamMode != streamReal || m.streamThinking {
			return
		}
		m.streamThinking = true
	case StreamDelta:
		if m.streamMode != streamReal {
			return
		}
		m.streamThinking = false
		m.streamRaw.WriteString(event.Delta)
		// This is the other half of the sanitizer chokepoint (see
		// internal/tui/sanitize.go): a real provider stream's tokens land
		// here, over the in-process channel, before the durable message
		// exists for poll() to sanitize. reply is the full decoded
		// reply-so-far, so sanitizing it is what streamShown — and
		// therefore what the thread actually draws while streaming — sees.
		if reply, found := partialJSONReply(m.streamRaw.String()); found {
			m.streamTarget = sanitizeText(reply)
		}
		// A delta moves only the target. What the thread draws is streamShown,
		// which advances on the animation tick, so re-rendering here would
		// produce the identical frame — at 50 tok/s, a third of a core of it.
		return
	case StreamFinished:
		if m.streamMode == streamReal {
			m.streamProviderDone = true
			m.normalizeLandedTarget()
			// Either the reply is drawn and durable, or the call ended holding
			// nothing at all — a control belt that produced no reply of its
			// own. Both end the stream here; only the second one used to be
			// left standing, with a landed reply parked behind it forever.
			if m.streamStalled() || (m.streamSeq != 0 && m.streamShown == m.streamTarget) {
				m.finishStream()
			}
		}
	case StreamFailed:
		if m.streamMode == streamReal {
			m.finishStream()
		}
	}
	m.refreshChat()
}

func (m *Model) queueSimulatedStream(message store.Message) {
	// A stalled stream owns nothing and must not be queued behind: a reply
	// parked behind it draws an empty line for as long as the window lives.
	if m.streamStalled() {
		m.finishStream()
	}
	if m.streamMode == streamNone {
		m.startSimulatedStream(message)
		return
	}
	m.streamQueue = append(m.streamQueue, message)
}

func (m *Model) startSimulatedStream(message store.Message) {
	m.streamMode = streamSimulated
	m.streamRaw.Reset()
	m.streamTarget = message.Body
	m.streamShown = typewriterAdvance("", message.Body, 1)
	m.streamSeq = message.Seq
	m.streamProviderDone = true
	m.streamInterrupted = false
	m.streamThinking = false
}

func (m *Model) matchRealStream(message store.Message) bool {
	if m.streamMode != streamReal || m.streamSeq != 0 || message.Role != store.RoleAgent ||
		message.NodeID != "" || message.Brief != nil {
		return false
	}
	landed := strings.TrimSpace(message.Body)
	preview := strings.TrimSpace(m.streamTarget)
	if m.streamProviderDone && landed != preview {
		// A stopped turn's durable line is the words on screen plus the mark
		// that it stopped there. It is the same reply, so it lands in place and
		// only the tail types itself in; refusing it here would park the real
		// row behind a stream that can no longer finish.
		if !m.streamInterrupted || preview == "" || !strings.HasPrefix(landed, preview) {
			return false
		}
	}
	if !m.streamProviderDone && preview != "" && !strings.HasPrefix(landed, preview) {
		return false
	}
	m.streamSeq = message.Seq
	if m.streamProviderDone {
		m.streamTarget = message.Body
		// The typewriter can only extend what it has already drawn. A frozen
		// partial that is not a byte-prefix of the durable line — a stray edge
		// of whitespace is enough — would leave it unable to advance and the
		// reply unable to finish, so it redraws rather than stalls.
		if !strings.HasPrefix(m.streamTarget, m.streamShown) {
			m.streamShown = ""
		}
		if strings.TrimSpace(m.streamShown) == landed {
			m.streamShown = message.Body
			m.finishStream()
		}
	}
	return true
}

func (m *Model) normalizeLandedTarget() {
	message, ok := m.messageBySeq(m.streamSeq)
	if !ok {
		return
	}
	if strings.TrimSpace(message.Body) == strings.TrimSpace(m.streamTarget) {
		m.streamTarget = message.Body
		if strings.TrimSpace(m.streamShown) == strings.TrimSpace(message.Body) {
			m.streamShown = message.Body
		}
		return
	}
	// A valid provider stream can still carry an unstructured fallback rather
	// than the head's reply object. Its durable text takes the simulated lane so
	// even a raced, malformed router response never appears fully formed.
	m.streamMode = streamSimulated
	m.streamRaw.Reset()
	m.streamTarget = message.Body
	m.streamShown = typewriterAdvance("", message.Body, 1)
}

func (m *Model) shouldSimulateStream(message store.Message) bool {
	if message.Role == store.RoleUser || message.Seq == 0 || message.Brief != nil {
		return false
	}
	if message.Role == store.RoleAgent && message.NodeID == "" {
		return true
	}
	if message.NodeID == "" {
		return false
	}
	card := m.cardForNodeID(message.NodeID)
	return card != nil && card.State == cardSettled && card.Deliverable != nil &&
		card.Deliverable.Seq == message.Seq
}

func (m *Model) advanceStream() {
	if m.streamMode == streamNone {
		return
	}
	// The last exit no other path can be trusted to take. A stalled stream can
	// never satisfy the finish conditions below — it has no target to finish
	// drawing and no durable row to finish into — so it is ended here rather
	// than left holding the queue.
	if m.streamStalled() {
		m.finishStream()
		return
	}
	if m.streamShown != m.streamTarget {
		m.streamShown = typewriterAdvance(m.streamShown, m.streamTarget, m.streamRevealTokens())
	}
	if m.streamShown != m.streamTarget {
		return
	}
	if m.streamMode == streamSimulated || (m.streamProviderDone && m.streamSeq != 0) {
		m.finishStream()
	}
}

// streamRevealTokens is how many words this frame draws: the base cadence while
// the backlog is one the base cadence can carry — every ordinary reply — and the
// excess over that backlog otherwise, capped so no frame dumps a paragraph.
//
// A stopped reply keeps the base cadence unconditionally: what lands after an
// interruption is the same words plus the mark that they stopped there, and
// that tail belongs to the calm path.
func (m *Model) streamRevealTokens() int {
	if m.streamInterrupted {
		return streamTokensPerTick
	}
	backlog := pendingStreamTokens(m.streamShown, m.streamTarget,
		streamLagTokens+streamMaxTokensPerTick)
	tokens := backlog - streamLagTokens
	if tokens < streamTokensPerTick {
		return streamTokensPerTick
	}
	if tokens > streamMaxTokensPerTick {
		return streamMaxTokensPerTick
	}
	return tokens
}

// pendingStreamTokens counts the whitespace tokens the provider is ahead by.
// Counting stops at limit because the answer above is capped anyway, so a reply
// of any length costs the same bounded scan per frame.
func pendingStreamTokens(shown, target string, limit int) int {
	if len(target) <= len(shown) || !strings.HasPrefix(target, shown) {
		return 0
	}
	remaining := target[len(shown):]
	tokens, offset := 0, 0
	for offset < len(remaining) && tokens < limit {
		for offset < len(remaining) {
			char, size := utf8.DecodeRuneInString(remaining[offset:])
			if !unicode.IsSpace(char) {
				break
			}
			offset += size
		}
		if offset >= len(remaining) {
			break
		}
		for offset < len(remaining) {
			char, size := utf8.DecodeRuneInString(remaining[offset:])
			if unicode.IsSpace(char) {
				break
			}
			offset += size
		}
		tokens++
	}
	return tokens
}

func (m *Model) finishStream() {
	m.clearStream()
	if len(m.streamQueue) == 0 {
		return
	}
	next := m.streamQueue[0]
	m.streamQueue = m.streamQueue[1:]
	m.startSimulatedStream(next)
}

func (m *Model) clearStream() {
	m.streamMode = streamNone
	m.streamRaw.Reset()
	m.streamTarget = ""
	m.streamShown = ""
	m.streamSeq = 0
	m.streamProviderDone = false
	m.streamInterrupted = false
	m.streamThinking = false
}

func (m *Model) messageBySeq(seq int64) (store.Message, bool) {
	if seq == 0 {
		return store.Message{}, false
	}
	for _, message := range m.messages {
		if message.Seq == seq {
			return message, true
		}
	}
	return store.Message{}, false
}

func (m *Model) streamAnimating() bool {
	return m.streamMode != streamNone && m.streamShown != m.streamTarget
}

// streamStalled names the one state the stream machine cannot leave on its own:
// the provider ended the call having produced no reply of its own — the control
// belt does this on every message — and no durable row ever attached to it. The
// mode says a reply is arriving, nothing is on screen, and the finish
// conditions elsewhere all require either a target to finish drawing or a
// sequence to finish into. Left alone it holds the queue forever and the reply
// that did land renders as an empty line.
func (m *Model) streamStalled() bool {
	return m.streamMode == streamReal && m.streamProviderDone &&
		m.streamSeq == 0 && m.streamTarget == "" && m.streamShown == ""
}

// streamVisible reports whether the live stream actually draws something. A
// stream with nothing in it occupies the state machine without occupying the
// screen, and everything that yields to a stream — the awaiting line, a queued
// reply's body — must yield only to one the reader can see.
func (m *Model) streamVisible() bool {
	return m.streamMode != streamNone && (m.streamShown != "" || m.streamTarget != "")
}

func (m *Model) streamingMessage() (store.Message, bool) {
	if m.streamMode != streamReal || m.streamSeq != 0 || (m.streamShown == "" && m.streamTarget == "") {
		return store.Message{}, false
	}
	return store.Message{Role: store.RoleAgent, Body: m.streamShown}, true
}

func (m *Model) streamedBody(message store.Message) (string, bool) {
	if message.Seq == 0 {
		return "", false
	}
	if m.streamMode != streamNone && message.Seq == m.streamSeq {
		return m.streamShown, true
	}
	// A reply waiting its turn draws nothing — but only while something is
	// actually being drawn ahead of it. A landed reply that is not animating
	// and has nothing animating in front of it renders in full: no state path
	// may leave it as an empty line.
	if !m.streamVisible() {
		return "", false
	}
	for _, queued := range m.streamQueue {
		if queued.Seq == message.Seq {
			return "", true
		}
	}
	return "", false
}

// partialJSONReply decodes as much of a streamed JSON reply string as is
// complete. It tolerates a chunk ending inside an escape without inventing a
// replacement rune; the next delta resumes from the same raw prefix.
func partialJSONReply(raw string) (string, bool) {
	marker := `"reply"`
	index := strings.Index(raw, marker)
	if index < 0 {
		return "", false
	}
	rest := strings.TrimLeft(raw[index+len(marker):], " \t\r\n")
	if !strings.HasPrefix(rest, ":") {
		return "", false
	}
	rest = strings.TrimLeft(rest[1:], " \t\r\n")
	if !strings.HasPrefix(rest, `"`) {
		return "", false
	}
	rest = rest[1:]

	var decoded strings.Builder
	for offset := 0; offset < len(rest); {
		if rest[offset] == '"' {
			return decoded.String(), true
		}
		if rest[offset] != '\\' {
			char, size := utf8.DecodeRuneInString(rest[offset:])
			if char == utf8.RuneError && size == 1 {
				return decoded.String(), true
			}
			decoded.WriteRune(char)
			offset += size
			continue
		}
		if offset+1 >= len(rest) {
			return decoded.String(), true
		}
		escape := rest[offset+1]
		switch escape {
		case '"', '\\', '/':
			decoded.WriteByte(escape)
			offset += 2
		case 'b':
			decoded.WriteByte('\b')
			offset += 2
		case 'f':
			decoded.WriteByte('\f')
			offset += 2
		case 'n':
			decoded.WriteByte('\n')
			offset += 2
		case 'r':
			decoded.WriteByte('\r')
			offset += 2
		case 't':
			decoded.WriteByte('\t')
			offset += 2
		case 'u':
			if offset+6 > len(rest) {
				return decoded.String(), true
			}
			value, err := strconv.ParseUint(rest[offset+2:offset+6], 16, 16)
			if err != nil {
				return decoded.String(), true
			}
			char := rune(value)
			offset += 6
			if char >= 0xd800 && char <= 0xdbff {
				if offset+6 > len(rest) || !strings.HasPrefix(rest[offset:], `\u`) {
					return decoded.String(), true
				}
				second, err := strconv.ParseUint(rest[offset+2:offset+6], 16, 16)
				if err != nil || second < 0xdc00 || second > 0xdfff {
					return decoded.String(), true
				}
				char = utf16.DecodeRune(char, rune(second))
				offset += 6
			} else if char >= 0xdc00 && char <= 0xdfff {
				return decoded.String(), true
			}
			decoded.WriteRune(char)
		default:
			return decoded.String(), true
		}
	}
	return decoded.String(), true
}

func typewriterAdvance(shown, target string, tokens int) string {
	if tokens <= 0 || len(shown) >= len(target) || !strings.HasPrefix(target, shown) {
		return shown
	}
	remaining := target[len(shown):]
	used := 0
	for tokens > 0 && used < len(remaining) {
		for used < len(remaining) {
			char, size := utf8.DecodeRuneInString(remaining[used:])
			if !unicode.IsSpace(char) {
				break
			}
			used += size
		}
		for used < len(remaining) {
			char, size := utf8.DecodeRuneInString(remaining[used:])
			if unicode.IsSpace(char) {
				break
			}
			used += size
		}
		tokens--
	}
	if used == 0 {
		return target
	}
	return shown + remaining[:used]
}
