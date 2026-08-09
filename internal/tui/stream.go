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
	StreamFinished
	StreamFailed
)

// StreamEvent is the TUI-facing stream protocol. Head deltas carry the raw
// structured response; the lens extracts only its reply field before drawing.
type StreamEvent struct {
	Kind  StreamEventKind
	Delta string
}

type streamSource interface {
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

const streamTokensPerTick = 2

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
	case StreamDelta:
		if m.streamMode != streamReal {
			return
		}
		m.streamRaw.WriteString(event.Delta)
		if reply, found := partialJSONReply(m.streamRaw.String()); found {
			m.streamTarget = reply
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
}

func (m *Model) matchRealStream(message store.Message) bool {
	if m.streamMode != streamReal || m.streamSeq != 0 || message.Role != store.RoleAgent ||
		message.NodeID != "" || message.Brief != nil {
		return false
	}
	landed := strings.TrimSpace(message.Body)
	preview := strings.TrimSpace(m.streamTarget)
	if m.streamProviderDone && landed != preview {
		return false
	}
	if !m.streamProviderDone && preview != "" && !strings.HasPrefix(landed, preview) {
		return false
	}
	m.streamSeq = message.Seq
	if m.streamProviderDone {
		m.streamTarget = message.Body
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
		m.streamShown = typewriterAdvance(m.streamShown, m.streamTarget, streamTokensPerTick)
	}
	if m.streamShown != m.streamTarget {
		return
	}
	if m.streamMode == streamSimulated || (m.streamProviderDone && m.streamSeq != 0) {
		m.finishStream()
	}
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
