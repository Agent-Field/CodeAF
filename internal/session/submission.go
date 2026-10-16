package session

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ── what a person's message carried, kept beside the message ───────────────
//
// A door that writes attachments INTO the words the model reads — a path, a
// link, a paste fenced into the message — leaves the record holding only those
// words. Reopened, the person's own sentence and the files that rode on it are
// gone: what comes back is one paragraph the person never typed, and no way to
// tell a path they attached from a path they mentioned.
//
// So the door says both, once, at the moment the message is accepted
// ([Agent.SubmitMessage]), and the record keeps both on THE SAME LINE as the
// message — the model's words in the content, the person's words and the
// attachments' descriptors in its presentation mark ([messagePresentation]).
// There is no second history: a reader of the line reads both or neither, a
// compaction that re-journals the line re-journals the mark, and a line written
// before this existed reads exactly as it always did.
//
// THE DESCRIPTORS ARE FACTS THE DOOR KNEW, NEVER BYTES. A file is named by its
// path and never inlined (the model reads it with its own tools); a paste's
// words are already in the content the model reads, and the descriptor says
// only how big it was. NOTHING IS EVER DERIVED FROM THE WORDS: a line with no
// mark has no attachments, however many paths it mentions.

// MessageAttachment is one file, link or paste that rode on a person's message,
// as the door that sent the message described it. Every field is optional but
// Kind. An empty string is "the door did not say". A size is a pointer so that
// an unmeasured size and a measured zero are two answers: nil was never
// measured, and a value — zero included — is what the door measured.
type MessageAttachment struct {
	// ID is the door's own name for the attachment, unique within the message.
	ID string `json:"id,omitempty"`
	// Kind is what it is, in the door's own vocabulary ("file", "link",
	// "paste"); the session neither interprets nor restricts it.
	Kind string `json:"kind"`
	// Name is how the attachment is labelled to a person.
	Name string `json:"name,omitempty"`
	// Path is where a file was, as the door named it.
	Path string `json:"path,omitempty"`
	// URL is a link's address.
	URL string `json:"url,omitempty"`
	// Bytes is the attachment's size in bytes, nil where the door did not
	// measure it.
	Bytes *int64 `json:"bytes,omitempty"`
	// Lines is how many lines it held, nil where the door did not count them.
	Lines *int `json:"lines,omitempty"`
	// Source says which span of which recorded audio or video this file is a
	// snapshot of ([SnapshotMediaRange]). Only a "file" carries one, and only a
	// door that made the snapshot may say so: it names the source, the version the
	// engine verified, and the range. Nil on every ordinary attachment.
	Source *MediaReference `json:"source,omitempty"`
}

// Submission is a person's message with what the door knows about it beyond
// the words the model reads: their own words, when those are not the whole of
// Text, and the attachments that rode on it.
type Submission struct {
	// Text is what the model reads, exactly as [Agent.Submit] takes it.
	Text string
	// Said is the person's own words, when Text is more than them. It may be
	// empty — a message of attachments alone — which is a different answer
	// from nil, where Text is taken as theirs.
	Said *string
	// Attachments are what rode on the message, in the order it carried them.
	Attachments []MessageAttachment
	Queued      bool
	// ClientID is the door's own opaque name for this message, at most
	// [MaxClientIDBytes] bytes of valid UTF-8 with no control characters. It is
	// kept exactly as given on the message's own line and read back on its
	// entry ([PersonMark.ClientID]), so a door can recognise its message after
	// the process that sent it is gone.
	//
	// IT IS CORRELATION, NEVER IDEMPOTENCY. The session does not look it up,
	// refuse a repeat of it or skip a send because of it; two messages with one
	// ClientID are two messages. Empty is "the door named none", and the model
	// is sent nothing of it.
	ClientID string
	// Reads are the reads of references the door performed to write Text, in
	// the order it performed them, as it found them at that moment.
	Reads []MessageRead
	// Media are the pictures that ride on this message as real image parts, in
	// order, after Text; Annotation is what the person marked on them. Either
	// makes the message a composed one ([Agent.SubmitComposed]).
	Media      []SubmissionMedia
	Annotation *Annotation
}

// ── what the door read to write the message ─────────────────────────────────
//
// A door that expands a reference — a collection the person picked, flattened
// to the links the model is handed — knows at that moment what the read found:
// how many references came back, how many were gone, how many it could not
// ask about, whether it was cut. Tomorrow it knows none of that: the
// collection has moved on, and asking it again answers today's question.
//
// So the door says what it read on the message itself, as it says what was
// attached, and the record keeps it on the same line. A READ IS A FACT THE
// DOOR OBSERVED, NEVER ONE THE SESSION WORKS OUT: nothing here is looked up
// again, nothing is derived from the words, and a line without reads has
// none, however many links its words carry. The model is sent nothing of it.

// MessageReference names one thing a read was of, or one thing it returned,
// in the door's own vocabulary. Kind is required; an empty field is "the door
// did not say". It is a descriptor and never content: a file is named by its
// path, a link by its address.
type MessageReference struct {
	// Kind is what it is ("collection", "file", "link", ...); the session
	// neither interprets nor restricts it.
	Kind string `json:"kind,omitempty"`
	// ID is the door's own identity for it.
	ID string `json:"id,omitempty"`
	// Name is how it is labelled to a person.
	Name string `json:"name,omitempty"`
	// Owner is who holds it, in the door's terms, where that is not the door.
	Owner string `json:"owner,omitempty"`
	// Path is where a file was, as the door named it.
	Path string `json:"path,omitempty"`
	// URL is a link's address.
	URL string `json:"url,omitempty"`
}

// MessageRead is one read of a reference the door performed to write a
// person's message, with the counts it observed. Count is exactly how many
// references the read returned; Items are the first few of them, never more
// than [MaxReadItems] and never more than Count. Missing, Unavailable and
// Unread count what the read could not return — gone, unaskable, deeper than
// it went — and More says it was cut. Complete is the door's own answer to
// whether the read was whole, and cannot be true while anything was
// unavailable, unread or cut; a reference that was gone was counted rather
// than lost, so Missing alone does not make a read partial.
type MessageRead struct {
	Source      MessageReference   `json:"source"`
	Count       int                `json:"count"`
	Missing     int                `json:"missing,omitempty"`
	Unavailable int                `json:"unavailable,omitempty"`
	Unread      int                `json:"unread,omitempty"`
	More        bool               `json:"more,omitempty"`
	Complete    bool               `json:"complete,omitempty"`
	Warnings    []string           `json:"warnings,omitempty"`
	Items       []MessageReference `json:"items,omitempty"`
}

// The bounds of what one message's reads may hold. A count past MaxReadCount is
// not a read anybody performed at a keyboard's pace, and the bytes bound is on
// every string of every read together, because sixteen reads at their field
// bounds would otherwise be megabytes on one line.
const (
	// MaxMessageReads is how many reads one message's mark names.
	MaxMessageReads = 16
	// MaxReadItems is how many returned references one read names.
	MaxReadItems = 8
	// MaxReadWarnings is how many warnings one read carries.
	MaxReadWarnings = 8
	// MaxReadCount is the largest any one count of a read may be.
	MaxReadCount = 1_000_000
	// maxReadWarning is the longest one warning may be, in bytes.
	maxReadWarning = 512
	// maxReadsBytes is the most all of a message's reads' strings may hold.
	maxReadsBytes = 128 << 10
)

// The bounds of what one message's mark may hold. THE MARK IS WRITTEN ON EVERY
// LINE THAT CARRIES ONE AND READ ON EVERY PAGE THAT SHOWS IT, so it is small and
// fixed: a door with more to say than this is saying it somewhere other than a
// description.
const (
	// MaxMessageAttachments is how many attachments one message's mark names.
	MaxMessageAttachments = 32
	// maxAttachmentField is the longest single field of one descriptor, in
	// bytes — a path or a URL well past any a filesystem or browser keeps.
	maxAttachmentField = 4096
	// maxSaidBytes is the longest the person's own words may be. Their words
	// are a part of Text in every door that has a reason to say them apart,
	// so the bound is the content's own scale rather than a separate one.
	maxSaidBytes = 1 << 20
	// MaxClientIDBytes is the longest a message's ClientID may be. It is a
	// correlation token, not content.
	MaxClientIDBytes = 256
)

// ErrMessageMark is every refusal of a mark that does not fit, so a door can
// tell a description it got wrong from a session that would not take a turn.
var ErrMessageMark = errors.New("session: the message's description does not fit")

// SubmitMessage is [Agent.Submit] for a door that knows more about the message
// than its words: the person's own sentence, and the attachments the words were
// written from. The model reads Text and nothing else — the mark changes no
// request, no routing and no context — and the record keeps the mark on the
// message's own line, so a conversation reopened tomorrow shows what the person
// sent rather than what the model was told.
//
// A MARK THAT DOES NOT FIT REFUSES THE MESSAGE WHOLE, before anything is
// recorded, so a refusal leaves nothing behind. A submission with no mark —
// Said nil, no attachments and no reads — is exactly [Agent.Submit].
func (a *Agent) SubmitMessage(ctx context.Context, message Submission) (<-chan Event, error) {
	if len(message.Media) > 0 || message.Annotation != nil {
		// Pictures are never dropped to fit a door that cannot carry them.
		_, events, err := a.SubmitComposed(ctx, message)
		return events, err
	}
	mark, err := message.mark()
	if err != nil {
		return nil, err
	}
	return a.submitText(ctx, message.Text, mark)
}

// mark is the submission's presentation mark, checked and copied, and nil when
// it says nothing beyond the words. The copy is the record's own: nothing the
// caller does to its slices afterwards reaches a line or a reading.
func (m Submission) mark() (*messagePresentation, error) {
	if err := checkClientID(m.ClientID); err != nil {
		return nil, err
	}
	if m.Said == nil && len(m.Attachments) == 0 && len(m.Reads) == 0 && !m.Queued && m.ClientID == "" {
		return nil, nil
	}
	reads, err := keptReads(m.Reads)
	if err != nil {
		return nil, err
	}
	// EVERY BOUND IS CHECKED ON THE BYTES AS GIVEN, before anything is trimmed:
	// a trimmed field is a slice of the caller's string, so a bound checked
	// after trimming could be passed by padding, and a short kept field could
	// hold a huge string alive behind it. What is kept is a fresh copy.
	if len(m.Attachments) > MaxMessageAttachments {
		return nil, fmt.Errorf("%w: %d attachments, and one message names at most %d", ErrMessageMark, len(m.Attachments), MaxMessageAttachments)
	}
	mark := &messagePresentation{Audience: audiencePerson, Queued: m.Queued, ClientID: strings.Clone(m.ClientID)}
	// Text stays nil when the door did not say the person's words apart: the
	// line's own content is theirs, and a copy of it here would be the same
	// words written twice on one line.
	if m.Said != nil {
		if len(*m.Said) > maxSaidBytes || !utf8.ValidString(*m.Said) {
			return nil, fmt.Errorf("%w: the person's words are not text of at most %d bytes", ErrMessageMark, maxSaidBytes)
		}
		// Trimmed as [Agent.Submit] trims the words it is given.
		said := strings.Clone(strings.TrimSpace(*m.Said))
		mark.Text = &said
	}
	if len(m.Attachments) > 0 {
		mark.Attachments = make([]MessageAttachment, 0, len(m.Attachments))
	}
	for index, given := range m.Attachments {
		for _, field := range []string{given.ID, given.Kind, given.Name, given.Path, given.URL} {
			if len(field) > maxAttachmentField || !utf8.ValidString(field) {
				return nil, fmt.Errorf("%w: attachment %d has a field that is not text of at most %d bytes", ErrMessageMark, index+1, maxAttachmentField)
			}
		}
		if (given.Bytes != nil && *given.Bytes < 0) || (given.Lines != nil && *given.Lines < 0) {
			return nil, fmt.Errorf("%w: attachment %d has a negative size", ErrMessageMark, index+1)
		}
		kept := MessageAttachment{
			ID:   keptField(given.ID),
			Kind: keptField(given.Kind),
			Name: keptField(given.Name),
			Path: keptField(given.Path),
			URL:  keptField(given.URL),
		}
		if kept.Kind == "" {
			return nil, fmt.Errorf("%w: attachment %d has no kind", ErrMessageMark, index+1)
		}
		if given.Bytes != nil {
			bytes := *given.Bytes
			kept.Bytes = &bytes
		}
		if given.Lines != nil {
			lines := *given.Lines
			kept.Lines = &lines
		}
		if given.Source != nil {
			if kept.Kind != "file" {
				return nil, fmt.Errorf("%w: attachment %d is a %s, and only a file has a recorded source", ErrMessageMark, index+1, kept.Kind)
			}
			if err := checkRangeSource(given.Source); err != nil {
				return nil, fmt.Errorf("%w: attachment %d's source %v", ErrMessageMark, index+1, err)
			}
			kept.Source = cloneSource(given.Source)
			kept.Source.SourceID, kept.Source.Path, kept.Source.Version = strings.Clone(given.Source.SourceID), strings.Clone(given.Source.Path), strings.Clone(given.Source.Version)
		}
		mark.Attachments = append(mark.Attachments, kept)
	}
	mark.Reads = reads
	return mark, nil
}

// checkClientID refuses an ID the record would not keep as given: past the
// bound, not text, or holding a control character. NOTHING IS TRIMMED OR
// CHANGED TO FIT, because a door matches on the exact bytes it sent.
func checkClientID(id string) error {
	if len(id) > MaxClientIDBytes || !utf8.ValidString(id) || strings.IndexFunc(id, unicode.IsControl) >= 0 {
		return fmt.Errorf("%w: the client id is not text of at most %d bytes without control characters", ErrMessageMark, MaxClientIDBytes)
	}
	return nil
}

// KeptReads is reads as the record will keep them — trimmed, and copies of
// their own — or the refusal [Agent.SubmitMessage] would answer them with, so a
// door can refuse a message before it does anything else for it, and show the
// person exactly what a reading of the record will show them later.
func KeptReads(reads []MessageRead) ([]MessageRead, error) { return keptReads(reads) }

// keptReads is the record's own copy of a message's reads, every bound checked
// on the bytes as given, and nil when there are none. NOTHING IS TRIMMED TO
// FIT: a count is the door's observation, so a read that does not fit refuses
// the message rather than being kept as a smaller read that never happened.
func keptReads(given []MessageRead) ([]MessageRead, error) {
	if len(given) == 0 {
		return nil, nil
	}
	if len(given) > MaxMessageReads {
		return nil, fmt.Errorf("%w: %d reads, and one message names at most %d", ErrMessageMark, len(given), MaxMessageReads)
	}
	total := 0
	reads := make([]MessageRead, 0, len(given))
	for index, read := range given {
		n := index + 1
		if len(read.Items) > MaxReadItems {
			return nil, fmt.Errorf("%w: read %d names %d references, and one read names at most %d", ErrMessageMark, n, len(read.Items), MaxReadItems)
		}
		if len(read.Warnings) > MaxReadWarnings {
			return nil, fmt.Errorf("%w: read %d carries %d warnings, and one read carries at most %d", ErrMessageMark, n, len(read.Warnings), MaxReadWarnings)
		}
		for _, count := range []int{read.Count, read.Missing, read.Unavailable, read.Unread} {
			if count < 0 || count > MaxReadCount {
				return nil, fmt.Errorf("%w: read %d has a count outside 0 to %d", ErrMessageMark, n, MaxReadCount)
			}
		}
		if len(read.Items) > read.Count {
			return nil, fmt.Errorf("%w: read %d names %d references of the %d it returned", ErrMessageMark, n, len(read.Items), read.Count)
		}
		// A WHOLE READ LEFT NOTHING OUT. A reference that was gone was counted,
		// which a whole read may do; one it could not ask about, one deeper
		// than it went, or a cut, is a read that is not whole.
		if read.Complete && (read.Unavailable > 0 || read.Unread > 0 || read.More) {
			return nil, fmt.Errorf("%w: read %d says it is complete and left references unread", ErrMessageMark, n)
		}
		kept := MessageRead{
			Count: read.Count, Missing: read.Missing, Unavailable: read.Unavailable, Unread: read.Unread,
			More: read.More, Complete: read.Complete,
		}
		source, size, err := keptReference(read.Source)
		if err != nil {
			return nil, fmt.Errorf("%w: read %d's source %s", ErrMessageMark, n, err)
		}
		kept.Source, total = source, total+size
		if len(read.Items) > 0 {
			kept.Items = make([]MessageReference, 0, len(read.Items))
		}
		for at, item := range read.Items {
			ref, size, err := keptReference(item)
			if err != nil {
				return nil, fmt.Errorf("%w: read %d's reference %d %s", ErrMessageMark, n, at+1, err)
			}
			kept.Items, total = append(kept.Items, ref), total+size
		}
		if len(read.Warnings) > 0 {
			kept.Warnings = make([]string, 0, len(read.Warnings))
		}
		for at, warning := range read.Warnings {
			if len(warning) > maxReadWarning || !utf8.ValidString(warning) {
				return nil, fmt.Errorf("%w: read %d's warning %d is not text of at most %d bytes", ErrMessageMark, n, at+1, maxReadWarning)
			}
			said := keptField(warning)
			if said == "" {
				return nil, fmt.Errorf("%w: read %d's warning %d says nothing", ErrMessageMark, n, at+1)
			}
			kept.Warnings, total = append(kept.Warnings, said), total+len(warning)
		}
		if total > maxReadsBytes {
			return nil, fmt.Errorf("%w: the reads come to more than %d bytes", ErrMessageMark, maxReadsBytes)
		}
		reads = append(reads, kept)
	}
	return reads, nil
}

// keptReference is one reference as the record keeps it, and the bytes it was
// given with. It names its kind and at least one thing a person could find it
// by, or it names nothing.
func keptReference(given MessageReference) (MessageReference, int, error) {
	size := 0
	for _, field := range []string{given.Kind, given.ID, given.Name, given.Owner, given.Path, given.URL} {
		if len(field) > maxAttachmentField || !utf8.ValidString(field) {
			return MessageReference{}, 0, fmt.Errorf("has a field that is not text of at most %d bytes", maxAttachmentField)
		}
		size += len(field)
	}
	kept := MessageReference{
		Kind:  keptField(given.Kind),
		ID:    keptField(given.ID),
		Name:  keptField(given.Name),
		Owner: keptField(given.Owner),
		Path:  keptField(given.Path),
		URL:   keptField(given.URL),
	}
	if kept.Kind == "" {
		return MessageReference{}, 0, errors.New("has no kind")
	}
	if kept.ID == "" && kept.Name == "" && kept.Path == "" && kept.URL == "" {
		return MessageReference{}, 0, errors.New("names nothing")
	}
	return kept, size, nil
}

// cloneReads is a reading's own copy of a mark's reads, so a [DisplayEntry]
// handed to a caller shares no slice with the index behind it. The strings are
// shared, which a string cannot be changed through.
func cloneReads(reads []MessageRead) []MessageRead {
	if len(reads) == 0 {
		return nil
	}
	out := append([]MessageRead(nil), reads...)
	for index := range out {
		out[index].Items = slices.Clone(out[index].Items)
		out[index].Warnings = slices.Clone(out[index].Warnings)
	}
	return out
}

// audiencePerson is the audience a person's own message is marked with. It is
// the one mark a user-role line carries, and the only one [shapeEntries] reads
// a person's words and attachments from.
const audiencePerson = "person"

// keptField is one descriptor field as the record keeps it: trimmed, and a copy
// of its own rather than a window onto the caller's string.
func keptField(field string) string {
	return strings.Clone(strings.TrimSpace(field))
}

// cloneAttachments is a reading's own copy of a mark's descriptors, sizes
// included, so a [DisplayEntry] handed to a caller shares nothing with the
// index behind it.
func cloneAttachments(attachments []MessageAttachment) []MessageAttachment {
	if len(attachments) == 0 {
		return nil
	}
	out := append([]MessageAttachment(nil), attachments...)
	for index := range out {
		if size := out[index].Bytes; size != nil {
			bytes := *size
			out[index].Bytes = &bytes
		}
		if count := out[index].Lines; count != nil {
			lines := *count
			out[index].Lines = &lines
		}
		out[index].Source = cloneSource(out[index].Source)
	}
	return out
}
