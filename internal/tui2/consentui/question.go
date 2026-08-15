package consentui

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/sanitize"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// One question, in the two halves it actually has.
//
// The DURABLE half is the row: its text, its options, its default. Nothing here
// may edit it, reorder it, or shorten it — 12.5 makes rendering-what-was-asked a
// law, and a consent question is the worst possible place to start paraphrasing.
//
// The PRESENTED half is what the caller adds: the blast radius in words, which
// of the row's own options means "no", the diff being consented to, and the
// patterns an option would whitelist. All four are things only the caller can
// know, and none of them changes what was asked.

// DetailKind is how one detail row reads. The three are the diff vocabulary of
// 5.17's glyph table and nothing more: this is a detail VIEW, not a diff engine.
type DetailKind uint8

const (
	// DetailContext is an unchanged line.
	DetailContext DetailKind = iota
	// DetailAdd is a line the action would add.
	DetailAdd
	// DetailDel is a line the action would remove.
	DetailDel
)

// DetailLine is one row of the toggled detail view.
type DetailLine struct {
	Kind DetailKind
	Text string
}

// Option is one answer, carried verbatim from the durable row plus the letter
// it wears and the scope it would grant.
type Option struct {
	// Index is the 1-based position in the durable option list. It is the wire
	// form: internal/head's selectQuestionOption reads "2" and every surface
	// that shows options numbers them the same way, so a digit is always a
	// legal answer even when a letter collides.
	Index int
	// Key is the mnemonic letter, derived from Label (see assignKeys). It is
	// empty only when the label offers no free letter at all.
	Key string
	// Label is the durable option label, unedited. It is what the answer posts
	// and what the resolution records; internal/consent matches its own
	// constants against it exactly.
	Label string
	// Hint is the durable hint, drawn under the label at the chrome tier.
	Hint string
	// Value is the durable option value, carried through untouched for callers
	// that encode routing in it.
	Value string
	// Scope lists the EXACT patterns choosing this option would whitelist
	// (10.4.18). Non-empty means the option cannot be taken on the fast path:
	// the patterns are shown and confirmed first.
	Scope []string
	// Rejecting marks the option that means "no". Choosing it opens the
	// steering input (10.4.16). It is set by naming a durable label in
	// [Presentation.Reject] — this package never decides which answer is the
	// refusal, because that is a fact about the question, not about the dialog.
	Rejecting bool
}

// Question is one pending consent question ready to draw.
type Question struct {
	// Seq is the durable identity — store.AgentQuestion.Seq. Everything the
	// wiring journals names this number, and the queue is keyed on it.
	Seq       int64
	SessionID string
	Category  store.QuestionCategory
	Class     store.QuestionClass
	Urgency   store.QuestionUrgency

	// Prompt is the question as asked, sanitized and never summarized (12.5).
	Prompt string
	// Consequence is the blast radius in words (5.20 rule 2), supplied by the
	// caller. Empty draws no row rather than an invented one.
	Consequence string

	Options []Option

	// Detail is the diff-style view `t` toggles, and the thing a denial still
	// renders (10.4.17). Empty means the question carries none and `t` is not
	// offered — an affordance that does nothing is worse than an absent one.
	Detail      []DetailLine
	DetailTitle string

	// Default is the 1-based option enter takes, or 0 for none. It comes from
	// the durable DefaultAnswer.
	Default int
}

// HasDetail reports whether this question carries a detail view.
func (q Question) HasDetail() bool { return len(q.Detail) > 0 }

// Presentation is the caller's half: everything the durable row cannot know.
type Presentation struct {
	// Consequence names the blast radius in words (5.20 rule 2).
	Consequence string
	// Reject names, by its exact durable label, the option that means "no".
	// Matching is case- and space-insensitive; a label that matches nothing
	// simply marks nothing, so a typo degrades to "no steering offered" rather
	// than to a wrong answer wired to the steering path.
	Reject string
	// Detail is the diff-style view.
	Detail      []DetailLine
	DetailTitle string
	// Scope maps a durable option label to the exact patterns choosing it would
	// whitelist (10.4.18).
	Scope map[string][]string
}

// FromStore dresses one durable question row for the dialog.
//
// Everything drawn comes off the row. The skin adds words around it and marks
// which of the row's OWN options is the refusal; it can neither add an answer
// nor remove one, which is what keeps "never invent an answer kind" a property
// of the type rather than a rule someone has to remember.
func FromStore(row store.AgentQuestion, skin Presentation) Question {
	question := Question{
		Seq:         row.Seq,
		SessionID:   row.SessionID,
		Category:    row.Category,
		Class:       row.Class,
		Urgency:     row.Urgency,
		Prompt:      PromptOf(row.Text),
		Consequence: sanitize.Text(strings.TrimSpace(skin.Consequence)),
		DetailTitle: sanitize.Text(strings.TrimSpace(skin.DetailTitle)),
	}
	for _, line := range skin.Detail {
		question.Detail = append(question.Detail, DetailLine{
			Kind: line.Kind, Text: sanitize.Text(strings.TrimRight(line.Text, "\r\n")),
		})
	}
	scope := make(map[string][]string, len(skin.Scope))
	for label, patterns := range skin.Scope {
		scope[foldLabel(label)] = patterns
	}
	reject := foldLabel(skin.Reject)
	for index, option := range row.Options {
		label := sanitize.Text(strings.TrimSpace(option.Label))
		if label == "" {
			continue
		}
		built := Option{
			Index: index + 1,
			Label: label,
			Hint:  sanitize.Text(strings.TrimSpace(option.Hint)),
			Value: strings.TrimSpace(option.Value),
		}
		folded := foldLabel(label)
		if reject != "" && folded == reject {
			built.Rejecting = true
		}
		for _, pattern := range scope[folded] {
			if pattern = sanitize.Text(strings.TrimSpace(pattern)); pattern != "" {
				built.Scope = append(built.Scope, pattern)
			}
		}
		question.Options = append(question.Options, built)
	}
	question.Default = defaultIndex(row.DefaultAnswer, question.Options)
	return question.normalize()
}

// PromptOf is the words a question actually asked.
//
// store.QuestionMessageBody writes "prompt\n\n```\n{json}\n```" — the same
// question twice, once for people and once for the components that parse the
// payload. This strips the machine copy and nothing else: the fence must close
// the text, and what is inside it must open with `{`, or the text is returned
// whole. A body that merely ENDS in a code block — a question quoting a
// snippet — keeps its code block, which is the case a looser rule would eat.
func PromptOf(text string) string {
	trimmed := strings.TrimRight(text, " \t\r\n")
	const fence = "\n```\n"
	if strings.HasSuffix(trimmed, "```") {
		if open := strings.LastIndex(trimmed, fence); open >= 0 {
			body := strings.TrimSpace(trimmed[open+len(fence) : len(trimmed)-len("```")])
			if strings.HasPrefix(body, "{") && strings.HasSuffix(body, "}") {
				trimmed = strings.TrimRight(trimmed[:open], " \t\r\n")
			}
		}
	}
	return sanitize.Text(strings.TrimSpace(trimmed))
}

// normalize fills in everything the dialog needs and the caller should not have
// to: option indices, mnemonic letters, and a default that is in range. It is
// called by [FromStore] and again by [Model.Push], so a hand-built Question — a
// test, or a caller assembling a question from something other than a row — is
// exactly as safe as one that came off the store.
func (q Question) normalize() Question {
	kept := q.Options[:0:0]
	for _, option := range q.Options {
		if strings.TrimSpace(option.Label) == "" {
			continue
		}
		option.Index = len(kept) + 1
		kept = append(kept, option)
	}
	q.Options = assignKeys(kept)
	if q.Default < 1 || q.Default > len(q.Options) {
		q.Default = 0
	}
	return q
}

// reservedKeys are the letters the dialog itself owns. An option may never be
// given one, because a mnemonic that silently shadows `t` would make the detail
// view unreachable on exactly the questions that have one.
var reservedKeys = map[byte]bool{'t': true, 'f': true}

// assignKeys derives one letter per option from the option's OWN label
// (10.4.17). The order of preference is the initial of each word, then any
// letter in the label, then any free letter at all — so the common shapes fall
// out right without a table:
//
//	"yes, start it" / "hold it — I'll trim it first"  ->  y / h
//	"allow" / "allow for this session" / "deny"       ->  a / s / d
//
// The second example is 10.4.17's own a/s/d, arrived at rather than hardcoded:
// `allow` takes a, `for` and `this` are reserved by the dialog, so the session
// option lands on s, and deny takes d. That is the whole trick — the letters in
// the doc are a description of what this rule produces on that vocabulary, not
// a set of answer kinds to go build.
func assignKeys(options []Option) []Option {
	if len(options) == 0 {
		return options
	}
	used := make(map[byte]bool, len(options))
	out := make([]Option, len(options))
	copy(out, options)
	for pass := 0; pass < 2; pass++ {
		for i := range out {
			if out[i].Key != "" {
				continue
			}
			if key, ok := pickKey(out[i].Label, used, pass == 0); ok {
				out[i].Key = string(key)
				used[key] = true
			}
		}
	}
	for i := range out {
		if out[i].Key != "" {
			continue
		}
		for c := byte('a'); c <= 'z'; c++ {
			if !used[c] && !reservedKeys[c] {
				out[i].Key, used[c] = string(c), true
				break
			}
		}
	}
	return out
}

// pickKey takes the first free letter of a label. initialsOnly is the first
// pass: every option gets a shot at a word initial before any option starts
// eating letters from the middle of its label, so "allow" cannot take the `s`
// that "session" needs.
func pickKey(label string, used map[byte]bool, initialsOnly bool) (byte, bool) {
	atWordStart := true
	for i := 0; i < len(label); i++ {
		c := lowerASCII(label[i])
		if c < 'a' || c > 'z' {
			atWordStart = true
			continue
		}
		start := atWordStart
		atWordStart = false
		if initialsOnly && !start {
			continue
		}
		if !used[c] && !reservedKeys[c] {
			return c, true
		}
	}
	return 0, false
}

func lowerASCII(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// foldLabel is the comparison form for matching a caller's words against a
// durable label: trimmed, lowercased, inner whitespace collapsed. Consent's own
// labels carry an em dash and an apostrophe, so anything stricter than this
// would fail on the one question this package exists for.
func foldLabel(label string) string {
	return strings.ToLower(strings.Join(strings.Fields(label), " "))
}

// defaultIndex reads the durable DefaultAnswer, which is a 1-based index
// ("1", as internal/consent writes it) or a label.
func defaultIndex(answer string, options []Option) int {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return 0
	}
	if number, err := strconv.Atoi(answer); err == nil {
		if number >= 1 && number <= len(options) {
			return number
		}
		return 0
	}
	folded := foldLabel(answer)
	for _, option := range options {
		if foldLabel(option.Label) == folded {
			return option.Index
		}
	}
	return 0
}

// Result is one answered question, in the exact shape the wiring needs to
// journal it. Nothing in this package writes to the store; this is the whole
// of what it produces.
//
// # The wiring contract
//
// The answer is ONE user turn naming its question:
//
//	thread.Post(graph, store.Message{
//	    SessionID:   r.SessionID,
//	    Role:        store.RoleUser,
//	    Body:        r.Body,          // the durable label, verbatim
//	    QuestionSeq: r.QuestionSeq,   // names the question; never guesses
//	})
//
// That is the same door the old surface used (internal/tui's postUserMessage
// sets QuestionSeq from the focused card). internal/head's answerAgentQuestion
// picks the turn up, store.QuestionForAnswer matches it by the named sequence,
// selectQuestionOption resolves [Body] against the durable option list, and
// store.ResolveQuestion records the label as the resolution. internal/consent's
// Serve loop then sees an answered question and releases or keeps the hold by
// comparing that resolution against consent.Approve. Nothing between those
// steps is this package's business, and no step of it may be short-circuited by
// resolving the question here: the desk's release is driven by the resolution,
// and a dialog that resolved directly would release a hold with no user turn in
// the journal to point at.
//
// [Steering] is a SECOND turn and never rides inside the answer: it is a
// redirect, not a resolution, and folding it into the answer body would post a
// paragraph where selectQuestionOption expects a label. Post it after the
// answer, with QuestionSeq zero, so the head reads it as ordinary direction.
type Result struct {
	QuestionSeq int64
	SessionID   string
	Category    store.QuestionCategory

	// Option is the durable option chosen, whole.
	Option Option
	// Body is what to post: the durable label, verbatim.
	Body string

	// Steering is the typed redirect that came with a rejection (10.4.16).
	// Empty for every other answer.
	Steering string
	// Rejected reports that the chosen option was the question's own "no".
	Rejected bool

	// Scope is the exact pattern list this answer whitelists, as confirmed —
	// which is not necessarily what was offered, because the fullscreen
	// escalation may have edited it (10.4.18).
	Scope []string

	// Detail is what was on screen when the answer was given. It rides out
	// whatever the answer was, so a denial can still render what was rejected
	// (10.4.17) — the transcript keeps the diff, not just the "no".
	Detail      []DetailLine
	DetailTitle string
}
