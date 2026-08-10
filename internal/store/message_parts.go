package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
)

// A message used to be one string, and every structured thing it needed to say
// was smuggled through that string: a question's options recovered by a brace
// scanner over the prose, a deliverable carried as its own only copy, a turn
// cut off mid-sentence journaled as if it had finished. Parts end that. A
// message body stays exactly what it was — the readable line — and beside it
// rides an ordered list of typed blocks saying what the message actually
// carries.
//
// Two of the kinds below are laws rather than conveniences:
//
//   - PartArtifact is the artifact law's carrier. Anything the person will use
//     outside the conversation is born on disk and referenced here by path.
//     Prose may describe it; prose may not BE it.
//   - PartEnded is the truncation law's carrier. A turn that ended by anything
//     other than its own completion says so here, so a half-answer cannot be
//     presented as a whole one.
//
// A message with no parts is a legacy prose message and behaves exactly as it
// always has. Nil is the normal state of this field and costs nothing to read.

// PartKind names one typed block in a message's ordered parts list. The set is
// open on purpose: a part whose kind this build does not recognize is carried
// through reads, writes and rebuilds byte for byte rather than dropped, so an
// older reader cannot silently erase a newer writer's record.
type PartKind string

const (
	// PartText is readable prose. The message body remains the whole rendered
	// line for every surface that has not learned parts; a text part is that
	// same prose addressable as a block.
	PartText PartKind = "text"
	// PartQuestion refers to the durable agent-question lifecycle by sequence,
	// which is the same number Message.QuestionSeq carries. The options live on
	// the question, not in the body.
	PartQuestion PartKind = "question"
	// PartCard refers to a graph node the message is about — the work surface's
	// unit, named rather than described.
	PartCard PartKind = "card"
	// PartProgress is replaceable structured progress: the same shape the
	// progress column already carries, addressable per block.
	PartProgress PartKind = "progress"
	// PartArtifact refers to a deliverable on disk. It never carries the bytes.
	PartArtifact PartKind = "artifact"
	// PartEnded says how the turn that produced this message ended.
	PartEnded PartKind = "ended"
)

// QuestionPart names the durable question this block belongs to.
type QuestionPart struct {
	Seq int64 `json:"seq"`
}

// CardPart names the graph node this block is about. Only the reference is
// stored: a title, a status and a spend are the node's to answer, and copying
// them here would be a second truth that ages.
type CardPart struct {
	NodeID string `json:"node_id"`
}

// ProgressPart is one replaceable progress block. NodeID is optional: compile
// progress belongs to a pending splice that has no node yet.
type ProgressPart struct {
	NodeID string `json:"node_id,omitempty"`
	Phase  string `json:"phase"`
	Done   int    `json:"done,omitempty"`
	Total  int    `json:"total,omitempty"`
	Latest string `json:"latest,omitempty"`
}

// ArtifactPart is the artifact law made storable: a reference to something on
// disk, never the thing itself. Bytes is the length as written, which is what
// lets a surface say "1.6 KB" without opening the file and what lets a later
// reader notice the file has changed underneath the message.
type ArtifactPart struct {
	Path string `json:"path"`
	MIME string `json:"mime,omitempty"`
	// Bytes is the length of the file at the moment it was referenced.
	Bytes int64 `json:"bytes,omitempty"`
	// NodeID is the work this artifact came out of, empty when the head made it
	// directly in conversation.
	NodeID string `json:"node_id,omitempty"`
}

// EndKind says how a turn ended. Exactly one of these is true of every turn,
// and only the three abnormal ones are ever written: a turn that finished
// because it was finished needs no mark, and marking it would put a row on
// every message in the journal to say nothing happened.
type EndKind string

const (
	// EndCompleted is the turn ending on its own terms. Defined so the
	// vocabulary is total; not written by the engine.
	EndCompleted EndKind = "completed"
	// EndLength is the output cap. This is the shape of the failure in session
	// bd3c78ed: 1,611 characters of an SVG, cut at exactly 600 completion
	// tokens, journaled as if whole.
	EndLength EndKind = "length"
	// EndStreamDrop is a stream that stopped without ever saying why — no
	// terminal frame, or a provider-side error in place of one.
	EndStreamDrop EndKind = "stream-drop"
	// EndInterrupted is the person stopping the turn. What they saw is kept;
	// this says they are the reason it goes no further.
	EndInterrupted EndKind = "interrupted"
)

// EndedPart is the truncation law's carrier. FinishReason is the provider's own
// word, kept verbatim beside our reading of it: the vocabulary of finish
// reasons is not ours and grows without asking, so the raw string is the only
// thing that stays true when it does.
type EndedPart struct {
	How          EndKind `json:"how"`
	FinishReason string  `json:"finish_reason,omitempty"`
}

// MessagePart is one typed block. Exactly one payload field is meaningful, and
// which one is decided by Kind; a part of an unrecognized kind keeps its
// original bytes and re-emits them unchanged.
type MessagePart struct {
	Kind     PartKind
	Text     string
	Question *QuestionPart
	Card     *CardPart
	Progress *ProgressPart
	Artifact *ArtifactPart
	Ended    *EndedPart

	// raw holds a part this build does not understand, exactly as it arrived.
	// Forward compatibility is not politeness here: parts are journaled, and a
	// rebuild run by an older binary would otherwise rewrite the projection
	// with everything it failed to recognize deleted.
	raw json.RawMessage
}

// TextPart, QuestionRef, CardRef, ArtifactRef and EndedMark are the
// constructors. They exist so a caller never has to remember which payload
// field pairs with which kind, which is the one way to build an invalid part.
func TextPart(text string) MessagePart { return MessagePart{Kind: PartText, Text: text} }

// QuestionRef points one part at a durable question by sequence.
func QuestionRef(seq int64) MessagePart {
	return MessagePart{Kind: PartQuestion, Question: &QuestionPart{Seq: seq}}
}

// CardRef points one part at a graph node.
func CardRef(nodeID string) MessagePart {
	return MessagePart{Kind: PartCard, Card: &CardPart{NodeID: nodeID}}
}

// ProgressRef carries one structured progress block.
func ProgressRef(progress ProgressPart) MessagePart {
	return MessagePart{Kind: PartProgress, Progress: &progress}
}

// ArtifactRef points one part at a deliverable on disk.
func ArtifactRef(artifact ArtifactPart) MessagePart {
	return MessagePart{Kind: PartArtifact, Artifact: &artifact}
}

// EndedMark carries how the turn ended.
func EndedMark(ended EndedPart) MessagePart {
	return MessagePart{Kind: PartEnded, Ended: &ended}
}

// maxMessageParts bounds one message's block list. A message is a thing a
// person reads; past this it is a document, and a document is an artifact.
const maxMessageParts = 64

// MaxMessagePartsBytes bounds the encoded parts column. It matches the body
// bound for the same reason the body has one: the journal is a record, not a
// content store, and anything larger belongs on disk with an ArtifactPart
// pointing at it.
const MaxMessagePartsBytes = 16 << 10

// partEnvelope is the wire shape: a kind, and the kind's payload under a key
// named after it. Text is a bare string because a text block is a string, and
// wrapping it in an object would buy nothing but a level of nesting.
type partEnvelope struct {
	Kind     PartKind      `json:"kind"`
	Text     string        `json:"text,omitempty"`
	Question *QuestionPart `json:"question,omitempty"`
	Card     *CardPart     `json:"card,omitempty"`
	Progress *ProgressPart `json:"progress,omitempty"`
	Artifact *ArtifactPart `json:"artifact,omitempty"`
	Ended    *EndedPart    `json:"ended,omitempty"`
}

// MarshalJSON writes the envelope for a known kind and the original bytes for
// an unknown one.
func (p MessagePart) MarshalJSON() ([]byte, error) {
	if !knownPartKind(p.Kind) {
		if len(p.raw) > 0 {
			return append([]byte(nil), p.raw...), nil
		}
		// A part built in Go under a kind this build does not know still has a
		// kind, and that is the whole of what can honestly be said about it.
		return json.Marshal(partEnvelope{Kind: p.Kind})
	}
	return json.Marshal(partEnvelope{
		Kind:     p.Kind,
		Text:     p.Text,
		Question: p.Question,
		Card:     p.Card,
		Progress: p.Progress,
		Artifact: p.Artifact,
		Ended:    p.Ended,
	})
}

// UnmarshalJSON reads a known kind into its typed payload and keeps an unknown
// one whole.
func (p *MessagePart) UnmarshalJSON(data []byte) error {
	var envelope partEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		// An unknown kind may carry a payload shape that collides with one of
		// ours. Probe for the kind alone before deciding this is malformed.
		var probe struct {
			Kind PartKind `json:"kind"`
		}
		if probeErr := json.Unmarshal(data, &probe); probeErr != nil || knownPartKind(probe.Kind) {
			return err
		}
		*p = MessagePart{Kind: probe.Kind, raw: compactPart(data)}
		return nil
	}
	if !knownPartKind(envelope.Kind) {
		*p = MessagePart{Kind: envelope.Kind, raw: compactPart(data)}
		return nil
	}
	*p = MessagePart{
		Kind:     envelope.Kind,
		Text:     envelope.Text,
		Question: envelope.Question,
		Card:     envelope.Card,
		Progress: envelope.Progress,
		Artifact: envelope.Artifact,
		Ended:    envelope.Ended,
	}
	return nil
}

// compactPart keeps an unrecognized part's bytes without its formatting. The
// bytes are what round-trips; the whitespace is not part of the record.
func compactPart(data []byte) json.RawMessage {
	var buffer bytes.Buffer
	if err := json.Compact(&buffer, data); err != nil {
		return append(json.RawMessage(nil), data...)
	}
	return json.RawMessage(buffer.Bytes())
}

func knownPartKind(kind PartKind) bool {
	switch kind {
	case PartText, PartQuestion, PartCard, PartProgress, PartArtifact, PartEnded:
		return true
	default:
		return false
	}
}

func validEndKind(kind EndKind) bool {
	switch kind {
	case EndCompleted, EndLength, EndStreamDrop, EndInterrupted:
		return true
	default:
		return false
	}
}

// ClassifyEnd reads the provider's finish_reason into our vocabulary. streamed
// says whether the call was made with a stream observer attached, and it is not
// decoration: an empty finish reason means two different things on the two
// paths. On a stream it means no terminal frame ever arrived — the connection
// ended mid-answer. On a single response it means the endpoint simply did not
// say, which is not evidence of anything.
func ClassifyEnd(finishReason string, streamed bool) EndKind {
	switch strings.ToLower(strings.TrimSpace(finishReason)) {
	case "":
		if streamed {
			return EndStreamDrop
		}
		return EndCompleted
	case "length", "max_tokens", "max_output_tokens", "model_length", "output_limit":
		return EndLength
	case "error", "network_error":
		// The provider stopped for its own reasons and told us so. That is the
		// same fact a dropped stream states, arriving by a politer route.
		return EndStreamDrop
	default:
		// stop, end_turn, tool_calls, function_call, content_filter and every
		// slug an endpoint invents tomorrow: the turn produced a terminal frame
		// on its own. The raw string is kept on the part either way, so a
		// vocabulary we guessed wrong about is still recoverable from the
		// journal.
		return EndCompleted
	}
}

// EndedFor is the mark a turn earns, or nil when it earned none. Returning nil
// for the ordinary case is the point: the truncation law puts a mark on a turn
// that did not finish, and puts nothing at all on the turns that did.
func EndedFor(finishReason string, streamed bool) *EndedPart {
	how := ClassifyEnd(finishReason, streamed)
	if how == EndCompleted {
		return nil
	}
	return &EndedPart{How: how, FinishReason: strings.TrimSpace(finishReason)}
}

// InterruptedEnd is the mark for a turn the person stopped. It has no finish
// reason because no provider was ever asked to give one.
func InterruptedEnd() *EndedPart { return &EndedPart{How: EndInterrupted} }

// normalizeMessageParts validates and trims one block list. It is the single
// gate: the write path refuses what fails here, and the read path degrades to
// legacy prose rather than surfacing something invalid.
func normalizeMessageParts(parts []MessagePart) ([]MessagePart, error) {
	if len(parts) == 0 {
		return nil, nil
	}
	if len(parts) > maxMessageParts {
		return nil, fmt.Errorf("%w: message carries %d parts (limit %d)", ErrInvalid, len(parts), maxMessageParts)
	}
	normalized := make([]MessagePart, 0, len(parts))
	for index, part := range parts {
		clean, err := normalizeMessagePart(part)
		if err != nil {
			return nil, fmt.Errorf("%w: part %d: %s", ErrInvalid, index, err)
		}
		normalized = append(normalized, clean)
	}
	return normalized, nil
}

func normalizeMessagePart(part MessagePart) (MessagePart, error) {
	if strings.TrimSpace(string(part.Kind)) == "" {
		return MessagePart{}, fmt.Errorf("part has no kind")
	}
	switch part.Kind {
	case PartText:
		if strings.TrimSpace(part.Text) == "" {
			return MessagePart{}, fmt.Errorf("text part is empty")
		}
		return MessagePart{Kind: PartText, Text: part.Text}, nil

	case PartQuestion:
		if part.Question == nil || part.Question.Seq <= 0 {
			return MessagePart{}, fmt.Errorf("question part names no question")
		}
		reference := *part.Question
		return MessagePart{Kind: PartQuestion, Question: &reference}, nil

	case PartCard:
		if part.Card == nil {
			return MessagePart{}, fmt.Errorf("card part names no node")
		}
		reference := *part.Card
		reference.NodeID = strings.TrimSpace(reference.NodeID)
		if reference.NodeID == "" {
			return MessagePart{}, fmt.Errorf("card part names no node")
		}
		return MessagePart{Kind: PartCard, Card: &reference}, nil

	case PartProgress:
		if part.Progress == nil {
			return MessagePart{}, fmt.Errorf("progress part has no progress")
		}
		progress := *part.Progress
		progress.NodeID = strings.TrimSpace(progress.NodeID)
		progress.Phase = strings.TrimSpace(progress.Phase)
		progress.Latest = strings.TrimSpace(progress.Latest)
		if progress.Phase == "" || progress.Done < 0 || progress.Total < 0 ||
			progress.Done > progress.Total || (progress.Total == 0 && progress.Done != 0) {
			return MessagePart{}, fmt.Errorf("invalid progress")
		}
		return MessagePart{Kind: PartProgress, Progress: &progress}, nil

	case PartArtifact:
		if part.Artifact == nil {
			return MessagePart{}, fmt.Errorf("artifact part names no file")
		}
		artifact := *part.Artifact
		artifact.Path = strings.TrimSpace(artifact.Path)
		artifact.MIME = strings.TrimSpace(artifact.MIME)
		artifact.NodeID = strings.TrimSpace(artifact.NodeID)
		if artifact.Path == "" {
			return MessagePart{}, fmt.Errorf("artifact part names no file")
		}
		if artifact.Bytes < 0 {
			return MessagePart{}, fmt.Errorf("artifact part has a negative length")
		}
		return MessagePart{Kind: PartArtifact, Artifact: &artifact}, nil

	case PartEnded:
		if part.Ended == nil || !validEndKind(part.Ended.How) {
			return MessagePart{}, fmt.Errorf("ended part says nothing about how the turn ended")
		}
		ended := *part.Ended
		ended.FinishReason = strings.TrimSpace(ended.FinishReason)
		return MessagePart{Kind: PartEnded, Ended: &ended}, nil

	default:
		// An unknown kind is passed through exactly as it arrived. There is
		// nothing to validate: this build does not know what would be valid,
		// and guessing is how records get deleted.
		return MessagePart{Kind: part.Kind, raw: part.raw}, nil
	}
}

// encodeMessageParts renders the column value for one already-normalized list.
func encodeMessageParts(parts []MessagePart) (string, error) {
	if len(parts) == 0 {
		return "null", nil
	}
	encoded, err := json.Marshal(parts)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// decodeMessageParts reads the column. It never fails the read: a message whose
// parts cannot be understood is a message that reads as the prose it has always
// been, which is exactly the behaviour of every build before parts existed. A
// projection defect must not take the thread down with it.
//
// The three comparisons at the top are the whole of the legacy read path. They
// take the column's bytes rather than a string, and answer without allocating,
// without decoding and without touching the heap — which matters because that
// path runs for every message in every database on every poll tick, and for
// exactly none of them is there anything to decode.
func decodeMessageParts(encoded []byte, seq int64, target *[]MessagePart) {
	*target = nil
	if len(encoded) == 0 || bytes.Equal(encoded, nullColumn) || bytes.Equal(encoded, emptyPartsColumn) {
		return
	}
	var parts []MessagePart
	if err := json.Unmarshal(encoded, &parts); err != nil {
		notePartsFault(seq, err)
		return
	}
	normalized, err := normalizeMessageParts(parts)
	if err != nil {
		notePartsFault(seq, err)
		return
	}
	*target = normalized
}

// The two column values that mean "this message has no parts". They are package
// variables rather than conversions at each comparison so the legacy branch
// stays free of the heap.
var (
	nullColumn       = []byte("null")
	emptyPartsColumn = []byte("[]")
)

// partsFaultOnce keeps a corrupt column from becoming a corrupt log. The thread
// is re-read on a poll timer, so an unreadable row would otherwise print its
// complaint several times a second for as long as the surface is open.
var partsFaultOnce sync.Once

func notePartsFault(seq int64, err error) {
	partsFaultOnce.Do(func() {
		log.Printf("store: message %d has an unreadable parts column (%v); "+
			"messages with unreadable parts read as plain prose", seq, err)
	})
}
