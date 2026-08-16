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
	// PartRoomSwitch says the conversation continues in another room, and names
	// it. It is the one cross-seam contract of the chats layer (chat-simplify
	// 5.4): when a thread splits, the engine journals this part on a row in the
	// OLD room and the surface applies it on its ordinary poll — composer and
	// view re-point, and the head simply serves the new room. The coupling is
	// journal-only on purpose. An in-process channel between the answer gate and
	// the window would be a second seam, unreadable after the fact and absent
	// entirely on a rebuild; a typed part is a fact both halves can read, and a
	// surface that has never heard of it renders the row's prose and is merely
	// out of date rather than wrong.
	//
	// Text is the new room's session id and nothing else. No title rides here:
	// the scribe names the new room on its ordinary post-turn lane, moments
	// later, and a name copied onto this part would be a second truth that is
	// stale before it is read.
	PartRoomSwitch PartKind = "room-switch"
	// PartAside is a side-channel exchange, kept whole and shown collapsed.
	//
	// 8.2.9's law is that statements are durable and questions may be ephemeral:
	// a curiosity question about running work is asked and answered without
	// taking a turn's worth of the orchestrator's context. "Ephemeral" there
	// means ephemeral to the MODEL's context, never to the record — journal-is-
	// truth and 5.20's no-dead-air rule both still hold — so the exchange lands
	// as one collapsed row that can be opened, referred back to, and searched.
	// The body is the collapsed line; this part is what opening it shows.
	PartAside PartKind = "aside"
)

// QuestionPart is the ask, said in types instead of smuggled through prose.
//
// Part 2.11's indictment lands here more sharply than anywhere else: a question
// existed in five places at once — a durable question row, a message row, a JSON
// blob inside that message's body, an options column beside it, and an FTS copy
// of the lot — and the surface that had to draw it recovered its components with
// a hand-rolled brace scanner over the body while the typed columns went unread.
// 13.3's first bug is that scanner's bill coming due: the moment a renderer drew
// the journal honestly, the smuggled JSON appeared on screen as an agent's own
// words.
//
// So this part is the whole render contract, and the rule that keeps it from
// becoming a sixth place the same fact lives is CardPart's rule: it carries what
// nothing else carries, and REFERS to everything else.
//
//   - The options are NOT here. They are Message.Options on the same row —
//     already typed, already normalized by this package, already durable, and
//     already the thing a reply of "3" is validated against. Copying them here
//     would be a second truth that ages.
//   - The prompt is NOT here. It is the message's text part, and the body.
//   - What IS here is everything the body used to smuggle and nothing else
//     records: how the question is drawn, whether it may be answered in free
//     text, which option stands if the person says nothing, and what the
//     question is about.
//
// Seq refers to the durable agent-question lifecycle, and it is the same number
// Message.QuestionSeq carries. Zero is legal and means exactly one thing: this
// ask lives on the message alone, with no lifecycle row behind it — the shape
// every conversational askback has always had.
type QuestionPart struct {
	Seq int64 `json:"seq"`
	// Kind is how the question is drawn: a numbered list, an inline yes/no
	// strip, or a plain prompt. It was previously recoverable only by parsing
	// the body, which is why a confirm question and a choose question were
	// indistinguishable to anything that did not brace-scan.
	Kind QuestionKind `json:"kind,omitempty"`
	// Class is the consent axis (9.4). It rides on the part because the surface
	// that draws the question is the surface that must not offer to answer a
	// consent question on the person's behalf, and it should not have to open a
	// second table to find out which kind it is holding.
	Class QuestionClass `json:"class,omitempty"`
	// Category is the gate this question is asked under, which is what a
	// "don't ask me this again" affordance acts on.
	Category QuestionCategory `json:"category,omitempty"`
	// Default is the option key that stands if the person says nothing. It is a
	// key rather than a label because the label is the option's to change.
	Default string `json:"default,omitempty"`
	// AllowFree says whether an answer outside the options is accepted. It is
	// spelled positively and defaults to false, so a part written by a producer
	// that forgot the field offers the narrower affordance rather than the wider
	// one — the same direction Class defaults in, and for the same reason.
	AllowFree bool `json:"allow_free,omitempty"`
	// NodeID and CharterID are what the question is about, when it is about
	// something. Both are references; neither carries a title, a status or a
	// spend, because those are the referent's to answer.
	NodeID    string `json:"node_id,omitempty"`
	CharterID string `json:"charter_id,omitempty"`
}

// validQuestionKind reports a drawable spelling. It is separate from the
// question package's own vocabulary check because an unrecognized kind here is
// not an error — it means this part says nothing about how to draw, which is the
// same as a part that never named a kind.
func validQuestionKind(kind QuestionKind) bool {
	switch kind {
	case QuestionChoose, QuestionConfirm, QuestionText:
		return true
	default:
		return false
	}
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

// AsidePart is one side-channel exchange, kept in full under a collapsed line.
//
// Both halves are stored because both are the record: the question is what was
// asked, and dropping it would leave an answer to nothing. They are stored HERE
// rather than as two text parts because a reader has to be able to tell which is
// which, and a pair of anonymous blocks cannot say.
type AsidePart struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
	// Model is what served the answer, recorded for the same reason a reply
	// records it: an aside is a real provider call that really cost money, and
	// "which model said that" is unanswerable afterwards without this.
	Model string `json:"model,omitempty"`
}

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
	Aside    *AsidePart

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

// QuestionRef points one part at a durable question by sequence. It carries the
// conservative class explicitly rather than by omission, so the value a reader
// gets from the constructor is the value the normalizer would have given it.
func QuestionRef(seq int64) MessagePart {
	return MessagePart{Kind: PartQuestion, Question: &QuestionPart{Seq: seq, Class: QuestionConsent}}
}

// QuestionBlock is the full render contract for one ask. It exists beside
// QuestionRef rather than replacing it because the two say different things: a
// ref names a lifecycle row and leaves the drawing to whoever finds it, and a
// block is the ask as it should appear, whether or not a lifecycle row exists.
func QuestionBlock(question QuestionPart) MessagePart {
	return MessagePart{Kind: PartQuestion, Question: &question}
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

// AsideRef carries one side-channel exchange under its collapsed line.
func AsideRef(aside AsidePart) MessagePart {
	return MessagePart{Kind: PartAside, Aside: &aside}
}

// RoomSwitchRef points the conversation at another room by id.
func RoomSwitchRef(sessionID string) MessagePart {
	return MessagePart{Kind: PartRoomSwitch, Text: strings.TrimSpace(sessionID)}
}

// RoomSwitchTarget reads the room a part points at, and reports false for every
// other kind. It exists so no reader has to know that this part spells its
// payload in Text — the one place that knowledge lives is here.
func RoomSwitchTarget(part MessagePart) (string, bool) {
	if part.Kind != PartRoomSwitch {
		return "", false
	}
	target := strings.TrimSpace(part.Text)
	return target, target != ""
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
	Aside    *AsidePart    `json:"aside,omitempty"`
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
		Aside:    p.Aside,
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
		Aside:    envelope.Aside,
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
	case PartText, PartQuestion, PartCard, PartProgress, PartArtifact, PartEnded, PartAside, PartRoomSwitch:
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
		if part.Question == nil {
			return MessagePart{}, fmt.Errorf("question part names no question")
		}
		reference := *part.Question
		reference.Default = strings.TrimSpace(reference.Default)
		reference.NodeID = strings.TrimSpace(reference.NodeID)
		reference.CharterID = strings.TrimSpace(reference.CharterID)
		if !validQuestionKind(reference.Kind) {
			// An unreadable spelling says nothing about how to draw, and saying
			// nothing is a state this part already has a value for. Keeping a
			// kind nobody recognizes would make every renderer guess.
			reference.Kind = ""
		}
		if reference.Class != QuestionInformational {
			// The conservative default is law (9.4): silence never widens
			// autonomy, so anything that is not explicitly informational reads
			// back as consent — on the part exactly as in the table.
			reference.Class = QuestionConsent
		}
		// A question part must name a question one way or the other: a durable
		// lifecycle row, or a drawable ask living on this message alone. Neither
		// is a part that refers to nothing, which is what QuestionRef(0) is.
		if reference.Seq <= 0 && reference.Kind == "" {
			return MessagePart{}, fmt.Errorf("question part names no question")
		}
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

	case PartRoomSwitch:
		// A switch with no room to switch to is the one way this part can be
		// wrong, and it is the only thing there is to check: the id is the whole
		// payload, and whether that room exists is the reader's question rather
		// than the writer's — the row survives the room being reaped.
		target := strings.TrimSpace(part.Text)
		if target == "" {
			return MessagePart{}, fmt.Errorf("room-switch part names no room")
		}
		return MessagePart{Kind: PartRoomSwitch, Text: target}, nil

	case PartAside:
		if part.Aside == nil {
			return MessagePart{}, fmt.Errorf("aside part carries no exchange")
		}
		aside := *part.Aside
		aside.Question = strings.TrimSpace(aside.Question)
		aside.Answer = strings.TrimSpace(aside.Answer)
		aside.Model = strings.TrimSpace(aside.Model)
		// Both halves or neither. An answer with no question is an answer to
		// nothing, and a question with no answer is a turn that never happened —
		// the collapsed line claims an exchange, so the block under it has to be
		// one.
		if aside.Question == "" || aside.Answer == "" {
			return MessagePart{}, fmt.Errorf("aside part is missing one half of the exchange")
		}
		return MessagePart{Kind: PartAside, Aside: &aside}, nil

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
