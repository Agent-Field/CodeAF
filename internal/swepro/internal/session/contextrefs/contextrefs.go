// Package contextrefs ports src/session/context-refs.ts:1-80 from swe-pro
// commit 3b25a1a. It deduplicates repeated prompt blocks within an LLM
// conversation and aggregates run-scoped statistics across conversations.
//
// Fidelity notes:
//   - ref hashes are the first six sha256 hex characters;
//   - kind sanitization preserves ASCII case, replaces every non-ASCII-safe
//     run with one hyphen, collapses existing hyphen runs too, and slices at
//     24 UTF-16 units;
//   - duplicate character counts use JavaScript UTF-16 String.length.
package contextrefs

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"unicode/utf16"
)

type contentEntry struct {
	refID string
	kind  string
}

// ContextRefs is one conversation's content registry.
type ContextRefs struct {
	ConversationID string `json:"conversationId"`

	mu           sync.Mutex
	byContent    map[string]contentEntry
	dedupedChars int
}

// RegisterResult reports the stable reference and whether content is new.
type RegisterResult struct {
	RefID        string `json:"refId"`
	FirstMention bool   `json:"firstMention"`
}

// ContextStats is one conversation's registry statistics.
type ContextStats struct {
	Registered   int `json:"registered"`
	DedupedChars int `json:"dedupedChars"`
}

// ContextRefsStore is a run-scoped registry keyed by conversation ID.
type ContextRefsStore struct {
	mu             sync.Mutex
	byConversation map[string]*ContextRefs
}

// StoreStats aggregates all conversation registries.
type StoreStats struct {
	Registered    int `json:"registered"`
	DedupedChars  int `json:"dedupedChars"`
	Conversations int `json:"conversations"`
}

func utf16Len(s string) int {
	return len(utf16.Encode([]rune(s)))
}

func safeKind(kind string) string {
	var out strings.Builder
	out.Grow(len(kind))
	lastHyphen := false
	for _, r := range kind {
		safe := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' || r == '_'
		if safe {
			out.WriteRune(r)
			lastHyphen = false
			continue
		}
		// Both an existing '-' and a replacement '-' participate in /-+/g.
		if !lastHyphen {
			out.WriteByte('-')
			lastHyphen = true
		}
	}
	value := out.String()
	if len(value) > 24 {
		value = value[:24] // sanitized output is ASCII, so byte and UTF-16 indices agree.
	}
	return value
}

func refTag(kind, content string) string {
	sum := sha256.Sum256([]byte(content))
	hash := hex.EncodeToString(sum[:])[:6]
	return "[ctx:" + safeKind(kind) + "-" + hash + "]"
}

// CreateContextRefs creates an empty per-conversation registry.
func CreateContextRefs(conversationID string) *ContextRefs {
	return &ContextRefs{
		ConversationID: conversationID,
		byContent:      map[string]contentEntry{},
	}
}

// Register registers content or returns its existing reference.
func (refs *ContextRefs) Register(kind, content string) RegisterResult {
	if content == "" {
		return RegisterResult{RefID: "", FirstMention: true}
	}
	refs.mu.Lock()
	defer refs.mu.Unlock()
	if existing, ok := refs.byContent[content]; ok {
		refs.dedupedChars += utf16Len(content)
		return RegisterResult{RefID: existing.refID, FirstMention: false}
	}
	refID := refTag(kind, content)
	refs.byContent[content] = contentEntry{refID: refID, kind: kind}
	return RegisterResult{RefID: refID, FirstMention: true}
}

// Render emits the one-line repeat reference.
func (refs *ContextRefs) Render(refID string) string {
	return refID + " — see earlier in this conversation; content unchanged"
}

// Stats returns per-conversation counts.
func (refs *ContextRefs) Stats() ContextStats {
	refs.mu.Lock()
	defer refs.mu.Unlock()
	return ContextStats{
		Registered:   len(refs.byContent),
		DedupedChars: refs.dedupedChars,
	}
}

// EmbedContextBlock embeds content on first mention and renders a one-line
// reference on subsequent mentions.
func EmbedContextBlock(refs *ContextRefs, kind, content string) string {
	registered := refs.Register(kind, content)
	if registered.RefID == "" {
		return content
	}
	if registered.FirstMention {
		return registered.RefID + "\n" + content
	}
	return refs.Render(registered.RefID)
}

// CreateContextRefsStore creates an empty run-scoped store.
func CreateContextRefsStore() *ContextRefsStore {
	return &ContextRefsStore{byConversation: map[string]*ContextRefs{}}
}

// ForConversation returns the stable registry for conversationID.
func (store *ContextRefsStore) ForConversation(conversationID string) *ContextRefs {
	store.mu.Lock()
	defer store.mu.Unlock()
	refs := store.byConversation[conversationID]
	if refs == nil {
		refs = CreateContextRefs(conversationID)
		store.byConversation[conversationID] = refs
	}
	return refs
}

// Stats aggregates registered bodies and deduplicated UTF-16 characters.
func (store *ContextRefsStore) Stats() StoreStats {
	store.mu.Lock()
	refs := make([]*ContextRefs, 0, len(store.byConversation))
	for _, conversation := range store.byConversation {
		refs = append(refs, conversation)
	}
	conversations := len(store.byConversation)
	store.mu.Unlock()

	stats := StoreStats{Conversations: conversations}
	for _, conversation := range refs {
		current := conversation.Stats()
		stats.Registered += current.Registered
		stats.DedupedChars += current.DedupedChars
	}
	return stats
}
