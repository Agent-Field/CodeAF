package taskdoor

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/run"
)

const (
	// Version is the task wire version, checked before accepting any work.
	Version = 1
	// MaxFrameBytes bounds allocation before an untrusted length is read.
	MaxFrameBytes = 1 << 20
)

// Frame retains the complete JSON object so new readers can decode a typed
// payload without losing the envelope that is also saved in the journal.
type Frame struct {
	Type    string          `json:"t"`
	Attempt string          `json:"attempt,omitempty"`
	Seq     uint64          `json:"seq,omitempty"`
	Payload json.RawMessage `json:"-"`
}

func (f Frame) Decode(into any) error { return json.Unmarshal(f.Payload, into) }

// Head identifies a task-only cell, independently of the chat's own head.
type Head struct {
	Head string `json:"head"`
	Cell string `json:"cell"`
}
type Limits struct {
	USD   float64 `json:"usd,omitempty"`
	Hours float64 `json:"hours,omitempty"`
}
type Hello struct {
	V    int           `json:"v"`
	Cert identity.Cert `json:"cert"`
	Sig  []byte        `json:"sig"`
}
type Start struct {
	Chat        string `json:"chat"`
	Task        int    `json:"task"`
	Title       string `json:"title"`
	Instruction string `json:"instruction"`
	Model       string `json:"model,omitempty"`
	Effort      string `json:"effort,omitempty"`
	MaxSteps    int    `json:"max_steps,omitempty"`
	NoProgress  int    `json:"no_progress,omitempty"`
	Input       Head   `json:"input"`
	Limits      Limits `json:"limits"`
	OriginName  string `json:"origin_name"`
}
type Started struct {
	State State `json:"state"`
}
type Refused struct {
	Reason string `json:"reason"`
}
type Watch struct {
	From uint64 `json:"from"`
}
type Step struct {
	Step run.Step `json:"step"`
}

// SpendLine carries one charge, so the origin can attribute it once rather
// than estimating spend again from an attempt's final totals.
type SpendLine struct {
	Model        string  `json:"model"`
	Seat         string  `json:"seat"`
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	CachedTokens int     `json:"cached_tokens,omitempty"`
	USD          float64 `json:"usd,omitempty"`
}
type Spend struct {
	Line SpendLine `json:"line"`
}
type Phase struct {
	Words string `json:"words"`
}
type Finished struct {
	Outcome string   `json:"outcome"`
	Report  string   `json:"report"`
	Changed []string `json:"changed"`
	Model   string   `json:"model"`
	Result  *Head    `json:"result,omitempty"`
}
type Unknown struct{}
type Stop struct{}
type Stopping struct{}
type Ack struct {
	Disposition string `json:"disposition"`
}
type Acked struct{}
type Ping struct{}
type Pong struct{}

func payloadType(payload any) string {
	switch payload.(type) {
	case Hello:
		return "hello"
	case Start:
		return "start"
	case Started:
		return "started"
	case Refused:
		return "refused"
	case Watch:
		return "watch"
	case Step:
		return "step"
	case Spend:
		return "spend"
	case Phase:
		return "phase"
	case Finished:
		return "finished"
	case Unknown:
		return "unknown"
	case Stop:
		return "stop"
	case Stopping:
		return "stopping"
	case Ack:
		return "ack"
	case Acked:
		return "acked"
	case Ping:
		return "ping"
	case Pong:
		return "pong"
	default:
		return ""
	}
}

// NewFrame gives each payload its wire type, so callers cannot accidentally
// label a stop request as a start.
func NewFrame(attempt string, payload any) (Frame, error) { return makeFrame(attempt, 0, payload) }
func makeFrame(attempt string, seq uint64, payload any) (Frame, error) {
	kind := payloadType(payload)
	if kind == "" {
		return Frame{}, errors.New("taskdoor: unsupported frame payload")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Frame{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return Frame{}, err
	}
	fields["t"], _ = json.Marshal(kind)
	if attempt != "" {
		fields["attempt"], _ = json.Marshal(attempt)
	}
	if seq != 0 {
		fields["seq"], _ = json.Marshal(seq)
	}
	raw, err = json.Marshal(fields)
	if err != nil {
		return Frame{}, err
	}
	if len(raw) > MaxFrameBytes {
		return Frame{}, frameTooLarge()
	}
	return Frame{Type: kind, Attempt: attempt, Seq: seq, Payload: raw}, nil
}
func frameTooLarge() error {
	return fmt.Errorf("taskdoor: a frame exceeds the %d-byte limit", MaxFrameBytes)
}
func parseFrame(raw []byte) (Frame, error) {
	var f Frame
	if err := json.Unmarshal(raw, &f); err != nil {
		return Frame{}, err
	}
	if f.Type == "" {
		return Frame{}, errors.New("taskdoor: a frame needs a type")
	}
	f.Payload = raw
	return f, nil
}

// ReadFrame checks the length before allocating, including on encrypted but
// not yet authorized hello connections.
func ReadFrame(r io.Reader) (Frame, error) {
	var head [4]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return Frame{}, err
	}
	size := binary.BigEndian.Uint32(head[:])
	if size > MaxFrameBytes {
		return Frame{}, frameTooLarge()
	}
	if size == 0 {
		return Frame{}, errors.New("taskdoor: empty frame")
	}
	raw := make([]byte, int(size))
	if _, err := io.ReadFull(r, raw); err != nil {
		return Frame{}, err
	}
	return parseFrame(raw)
}

// WriteFrame writes one complete length-prefixed object. Connections serialize
// this call because a large JSON object may span several encrypted records.
func WriteFrame(w io.Writer, f Frame) error {
	if len(f.Payload) > MaxFrameBytes {
		return frameTooLarge()
	}
	if len(f.Payload) == 0 {
		return errors.New("taskdoor: empty frame")
	}
	data := binary.BigEndian.AppendUint32(nil, uint32(len(f.Payload)))
	data = append(data, f.Payload...)
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
