package placeorg

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// Kind sorts chats before anything semantic happens, the way a librarian sorts
// by kind before sorting by subject. A probe never enters the tree.
type Kind string

const (
	// KindConversation is a real exchange with a person behind it.
	KindConversation Kind = "conversation"
	// KindProbe is system chatter: a smoke test, a one-word check, a canned
	// prompt sent to see whether the engine answers. It stays unplaced and
	// never counts toward a birth or a name.
	KindProbe Kind = "probe"
	// KindDuplicate is a near-copy of another chat (a retried brief, a pasted
	// preamble sent again). It is filed wherever its representative is filed
	// and never counts twice.
	KindDuplicate Kind = "duplicate"
)

// Chat is one conversation as the pipeline sees it. It is built by the bridge
// from placegraph.ChatEvidence plus what the engine knows about the folder the
// chat ran in; nothing in it comes from a model.
type Chat struct {
	ID string
	// Text is what is embedded and tokenised: the title, then the first
	// person message cut to TextRunes, with lines that repeat across many
	// chats removed first (boilerplate briefs and preambles are not subject).
	Text string
	// Title is kept separately because names and anchors weigh it more.
	Title string
	// Workspace is the chat's folder as the engine reports it; RepoKey is the
	// project identity of its git root when it has one. Both are compared,
	// never opened.
	Workspace string
	RepoKey   string
	Created   time.Time
	// Replies counts settled replies; a chat with none is a draft, not a chat.
	Replies int
}

// TextRunes bounds Chat.Text. The reference implementation used the title and
// the first four hundred characters of the first message; the bound is kept so
// a pasted log cannot dominate an embedding.
const TextRunes = 480

// Corpus is every chat the run can see, in a fixed order, with the facts the
// early stages add to each: its kind, its representative when it is a
// duplicate, and its anchors. Later stages read it and never reorder it.
type Corpus struct {
	Chats []Chat
	// Kinds and Representative are filled by the Kind and Dedup stages.
	Kinds          map[string]Kind
	Representative map[string]string
	// Anchors is filled by the Anchors stage: per chat, the weighted anchor
	// set that says what the chat is about in the person's own terms.
	Anchors map[string]AnchorSet
}

// FromEvidence builds the pipeline's view of a chat from the store's. RepoKey
// is supplied by the caller because this package never looks at a disk.
func FromEvidence(e placegraph.ChatEvidence, repoKey string, created time.Time) Chat {
	return Chat{ID: e.ChatID, Title: e.Title, Workspace: e.Workspace, RepoKey: repoKey, Created: created, Replies: e.Replies}
}
