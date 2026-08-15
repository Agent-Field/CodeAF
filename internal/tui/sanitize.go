package tui

import (
	"github.com/Agent-Field/aforge-v2/internal/sanitize"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// This file is the old TUI's half of the chat-rebuild sanitizer chokepoint
// (audit-notes/chat-rebuild.md Part 10.2 items 6-7). The rule the rest of
// the package can lean on: by the time a store.Message or store.AgentQuestion
// reaches m.messages, m.nodeMessages, m.streamShown, or the question dock,
// its text has already passed through internal/sanitize — nothing downstream
// (view.go, cards.go, clipboard.go, ...) needs to sanitize again, and nothing
// downstream should, since sanitize.Text on already-clean text is a cheap
// no-op but not a free one.
//
// Two call sites cover every message this window ever draws:
//
//  1. poll()'s background goroutine (model.go), which is the one place
//     store.Message / store.AgentQuestion / a node's trace tail enter the
//     model at all — sanitizeMessageBodies and sanitizeQuestionText are
//     called there, once per poll, before the results reach applyPoll.
//  2. applyStreamEvent's StreamDelta case (stream.go), the one path that
//     bypasses the poll entirely: a real provider stream's tokens arrive
//     over an in-process channel and are drawn as they land, before the
//     durable message (and poll 1's sanitizing of it) exists yet.
//
// User-typed drafts never go through either seam: the optimistic local
// echo (sendMessage / node.go's steer composer) is drawn straight from
// what the user just typed, and that is intentional — it is trusted,
// performance-sensitive input, exactly the case the sanitizer's own fast
// path is not needed for. Once that draft round-trips through the store
// and comes back via poll, it is sanitized like everything else; the
// no-op fast path makes that idempotent and free for ordinary text.
//
// What this does NOT cover (left for Wave 2, which kills prose-smuggled
// structure outright — audit notes 4.2): QuestionOption.Label/Hint text,
// and Command receipt/reason text rendered outside a message body. Both
// are lower-risk (today mostly head-authored templated strings, not raw
// model/tool output) but are not yet run through this chokepoint; a Wave 2
// builder adding a genuinely model-authored option label should route it
// through sanitize.Text before it ships.

// sanitizeText is the one call every seam in this file routes through. It
// is sanitize.Text today; the day Wave 2's token layer lands a real Table,
// this is the single line that becomes sanitize.TextWithPalette(s, palette)
// — every call site above stays untouched.
func sanitizeText(s string) string {
	return sanitize.Text(s)
}

// sanitizeMessageBodies sanitizes every message's Body in place. It is the
// single spot poll() calls for both the chat page's messages and the node/
// task page's nodeMessages — same shape, same threat, same fix.
func sanitizeMessageBodies(messages []store.Message) {
	for i := range messages {
		messages[i].Body = sanitizeText(messages[i].Body)
	}
}

// sanitizeQuestionText sanitizes an AgentQuestion's free-text Text field in
// place. Text is worker-authored (an ask, not a template), and the question
// dock (question_dock.go) draws it directly rather than through a
// store.Message, so it needs its own pass alongside sanitizeMessageBodies.
func sanitizeQuestionText(questions []store.AgentQuestion) {
	for i := range questions {
		questions[i].Text = sanitizeText(questions[i].Text)
	}
}
