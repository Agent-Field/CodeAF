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
		return event
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
		m.streamRaw = ""
		m.streamTarget = ""
		m.streamShown = ""
		m.streamSeq = landedSeq
		m.streamProviderDone = false
	case StreamDelta:
		if m.streamMode != streamReal {
			return
		}
		m.streamRaw += event.Delta
		if reply, found := partialJSONReply(m.streamRaw); found {
			m.streamTarget = reply
		}
	case StreamFinished:
		if m.streamMode == streamReal {
			m.streamProviderDone = true
			m.normalizeLandedTarget()
			if m.streamSeq != 0 && m.streamShown == m.streamTarget {
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
	if m.streamMode == streamNone {
		m.startSimulatedStream(message)
		return
	}
	m.streamQueue = append(m.streamQueue, message)
}

func (m *Model) startSimulatedStream(message store.Message) {
	m.streamMode = streamSimulated
	m.streamRaw = ""
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
	m.streamRaw = ""
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
	m.streamRaw = ""
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
