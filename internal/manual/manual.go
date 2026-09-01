// Package manual is what aforge knows about itself, embedded in the binary.
//
// Everything aforge can say about its own capabilities, mechanisms and reasons
// has to come from somewhere. The design docs are the wrong somewhere: they are
// on disk rather than in the binary, they are written to persuade rather than
// to answer, and they go stale the moment a build lands. Improvisation is the
// worse somewhere — a model asked "how does boost work" will always produce a
// fluent answer, and there is no way for the user to tell a remembered one from
// an invented one. So the pages here are the single authoritative source, they
// ship inside the binary, and the completeness tests beside the feature
// registries fail the build when a landed feature is not among them.
//
// TWO PRODUCTS, TWO FOLDERS, NO MIXING (corpus.go says why at length):
//
//   - pages/ is the RESIDENT — the employee that keeps working while the
//     terminal is closed. Read through this package's own functions, which is
//     the shape internal/head and internal/tui already call.
//   - chat/ is the V3 CHAT — the conversation surface in internal/tui3 over the
//     engine in internal/session. Read through [Chat].
//
// A page belongs to exactly one of them. When a mechanism genuinely exists in
// both products it is written twice, in each one's own vocabulary, because the
// two answers are not the same answer: the resident's is about work that
// outlives the window and the chat's is about the session you are sitting in.
package manual

import (
	"fmt"
	"strings"
)

// The pages ship packed, not raw: a megabyte of Markdown is otherwise a
// megabyte of binary, and a manual is the most compressible thing aforge
// carries. internal/packed says how and why. The folders below are the only
// tracked source of truth. `make build` generates ignored archives and selects
// packed_source.go; ordinary Go commands select raw_source.go so a clean
// checkout still compiles without committing a shared binary merge hotspot.

//go:generate go run github.com/Agent-Field/aforge-v2/internal/packed/cmd/pack -o pages.pack.gz pages
//go:generate go run github.com/Agent-Field/aforge-v2/internal/packed/cmd/pack -o chat.pack.gz chat

const (
	// DefaultResults is how many sections one question is answered from. Four
	// is a topic and its neighbours; more is a document, and a model handed a
	// document quotes the wrong half of it.
	DefaultResults = 4
	// SectionBodyCap bounds one section's text where it is rendered for a
	// model. Pages are written to sit well under this; the cap exists so a
	// future long page degrades by truncation rather than by budget.
	SectionBodyCap = 2400
)

// resident is the employee's own account of itself. The package-level functions
// below all read it, because they were this package's whole API before there
// was a second product to describe and every caller of them means the resident.
var resident = newCorpus(residentFiles, "pages/*.md")

// chat is the v3 chat's account of itself, reached through [Chat].
var chat = newCorpus(chatFiles, "chat/*.md")

// Chat is the v3 chat surface's manual: what it can do, how a mechanism works,
// and why it behaved the way it did. It is a separate corpus from the resident
// pages and cannot reach them, which is the point — see corpus.go.
func Chat() *Corpus { return chat }

// Search ranks the resident's manual against a question. k at or below zero
// asks for the default; the result is ordered best first and is empty only when
// the question shares no word with any page.
func Search(query string, k int) []Section { return resident.Search(query, k) }

// Page returns one whole resident page by name — "daily-rhythm", not
// "daily-rhythm.md".
func Page(name string) (string, bool) { return resident.Page(name) }

// Pages lists every resident page name, in reading order.
func Pages() []string { return resident.Pages() }

// Sections exposes the parsed resident manual for the completeness tests that
// keep it honest as features land.
func Sections() []Section { return resident.Sections() }

// Context is the one-call shape both the belt tool and the router's grounding
// path want: search, then render, or nothing at all.
func Context(query string, k int) string { return resident.Context(query, k) }

// Mentions reports whether a term appears anywhere in the resident's manual.
// The completeness tests are written against it, so a feature that lands
// without a page fails the build rather than becoming something aforge
// improvises about.
func Mentions(term string) bool { return resident.Mentions(term) }

// Cued reports whether a message reaches for the resident manual's own
// vocabulary. It is half of the head's self-question trigger.
func Cued(message string) bool { return resident.Cued(message) }

// Cues is the derived vocabulary itself, for tests and for anything that wants
// to see what the trigger will fire on.
func Cues() []string { return resident.Cues() }

// Render turns sections into the block a model reads. Page and heading stay
// attached so a quoted answer can be traced back to the page that authorized
// it. It belongs to no corpus — sections carry their own provenance.
func Render(sections []Section) string {
	blocks := make([]string, 0, len(sections))
	for _, section := range sections {
		body := section.Body
		if len(body) > SectionBodyCap {
			body = body[:SectionBodyCap] + "…"
		}
		blocks = append(blocks, fmt.Sprintf("[%s · %s]\n%s", section.Page, section.Title, body))
	}
	return strings.Join(blocks, "\n\n")
}
