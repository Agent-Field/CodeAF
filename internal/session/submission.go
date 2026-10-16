package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
}

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
)

// errSubmissionMark is every refusal of a mark that does not fit, so a door
// can tell a description it got wrong from a session that would not take a turn.
var errSubmissionMark = errors.New("session: the message's description does not fit")

// SubmitMessage is [Agent.Submit] for a door that knows more about the message
// than its words: the person's own sentence, and the attachments the words were
// written from. The model reads Text and nothing else — the mark changes no
// request, no routing and no context — and the record keeps the mark on the
// message's own line, so a conversation reopened tomorrow shows what the person
// sent rather than what the model was told.
//
// A MARK THAT DOES NOT FIT REFUSES THE MESSAGE WHOLE, before anything is
// recorded, so a refusal leaves nothing behind. A submission with no mark —
// Said nil and no attachments — is exactly [Agent.Submit].
func (a *Agent) SubmitMessage(ctx context.Context, message Submission) (<-chan Event, error) {
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
	if m.Said == nil && len(m.Attachments) == 0 {
		return nil, nil
	}
	// EVERY BOUND IS CHECKED ON THE BYTES AS GIVEN, before anything is trimmed:
	// a trimmed field is a slice of the caller's string, so a bound checked
	// after trimming could be passed by padding, and a short kept field could
	// hold a huge string alive behind it. What is kept is a fresh copy.
	if len(m.Attachments) > MaxMessageAttachments {
		return nil, fmt.Errorf("%w: %d attachments, and one message names at most %d", errSubmissionMark, len(m.Attachments), MaxMessageAttachments)
	}
	mark := &messagePresentation{Audience: audiencePerson}
	// Text stays nil when the door did not say the person's words apart: the
	// line's own content is theirs, and a copy of it here would be the same
	// words written twice on one line.
	if m.Said != nil {
		if len(*m.Said) > maxSaidBytes || !utf8.ValidString(*m.Said) {
			return nil, fmt.Errorf("%w: the person's words are not text of at most %d bytes", errSubmissionMark, maxSaidBytes)
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
				return nil, fmt.Errorf("%w: attachment %d has a field that is not text of at most %d bytes", errSubmissionMark, index+1, maxAttachmentField)
			}
		}
		if (given.Bytes != nil && *given.Bytes < 0) || (given.Lines != nil && *given.Lines < 0) {
			return nil, fmt.Errorf("%w: attachment %d has a negative size", errSubmissionMark, index+1)
		}
		kept := MessageAttachment{
			ID:   keptField(given.ID),
			Kind: keptField(given.Kind),
			Name: keptField(given.Name),
			Path: keptField(given.Path),
			URL:  keptField(given.URL),
		}
		if kept.Kind == "" {
			return nil, fmt.Errorf("%w: attachment %d has no kind", errSubmissionMark, index+1)
		}
		if given.Bytes != nil {
			bytes := *given.Bytes
			kept.Bytes = &bytes
		}
		if given.Lines != nil {
			lines := *given.Lines
			kept.Lines = &lines
		}
		mark.Attachments = append(mark.Attachments, kept)
	}
	return mark, nil
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
	}
	return out
}
