// Package redact takes the SHAPE of a secret out of text before anything keeps
// it, shows it, or sends it to a model.
//
// ── WHY THIS EXISTS ──
//
// A worker ran `gh auth token`. The tool result came back as thirty-six
// characters of live OAuth credential, and those characters were then written
// verbatim into two task journals on the person's disk, drawn on their screen,
// and sent back to the model on every request for the rest of the run. None of
// the three ever needed them. A journal is a record of what happened, and "a
// token came back" is the whole of what happened. A screen is being read by the
// person whose token it already is. AND THE MODEL NEVER NEEDS A CREDENTIAL'S
// CHARACTERS: a shell command that has to USE one already has it in its own
// environment, which is exactly why `gh auth token` was run inside `TOKEN=$(…)`
// rather than for the model to read.
//
// ── THE TABLE IS SHAPES, NOT SERVICES ──
//
// Every row of [shapes] is one SHAPE a secret takes, written as one pattern
// with one comment saying which secrets wear it. There is deliberately no
// branch on a service name, no list of hosts, and no per-provider function: a
// provider that starts minting `sk-` keys tomorrow is covered the day it
// launches, and a provider that invents a genuinely new shape is one more row
// here rather than a change anywhere else.
//
// ── NO RANDOM BYTE SURVIVES ──
//
// A marker keeps a few leading characters so a person reading the record can
// tell WHICH kind of thing was taken out — `[redacted token · gho_…]` says a
// GitHub token was here where a bare `[redacted]` says only that something was.
// The characters it keeps are the token's OWN FIXED PREFIX and never one byte
// of its random part, which is why [shape.lead] is a per-row number rather than
// a constant four: `gho_` is four characters of prefix, `github_pat_` is
// eleven, and both are printed on every token of their kind anyway. A shape
// whose secret has no fixed prefix keeps nothing.
package redact

import (
	"regexp"
	"strings"
)

// shape is one recognisable form of a secret.
type shape struct {
	// name is what a person is told was taken out. It goes into the marker
	// verbatim, so it is a PERSON'S word for the thing — "token", "private
	// key" — and never a name for the machinery that found it.
	name string
	// re is the pattern. Every row that begins with a word character starts
	// with `\b`, and that guard is load-bearing rather than tidiness: without
	// it a long base64 payload — an inlined image, a `data:` URI — hits these
	// prefixes by chance in the middle of a run of letters, and a picture would
	// come back to the model with a hole punched in it.
	re *regexp.Regexp
	// group is the submatch that is the SECRET, for a row whose pattern has to
	// match more than the secret in order to recognise it. 0 is the whole
	// match, which is what most rows want.
	group int
	// lead is how many leading characters of the secret survive into the
	// marker. It is the length of that shape's FIXED prefix and nothing more
	// (see the package comment); 0 means nothing survives.
	lead int
}

// shapes is the table, and ORDER MATTERS IN EXACTLY ONE WAY: the rows that
// recognise a secret by its own prefix run before the `Bearer` row, which
// recognises a credential only by the word in front of it. A GitHub token in an
// Authorization header should read as a GitHub token, so the specific rows get
// first refusal and the catch-all takes whatever is left.
var shapes = []shape{
	{
		// A private key block, whole. Everything between the two fences goes,
		// fences included: the body is base64 whose first bytes say nothing,
		// and the fence lines are worth more to an attacker with the body than
		// they are to a person reading the record without it.
		//
		// THE CLOSING FENCE IS REQUIRED, and the cost of that is stated rather
		// than hidden: a key cut in half by an output cap keeps its half. The
		// alternative — running to the end of the text when no fence arrives —
		// would let one line of prose that merely MENTIONS a BEGIN fence
		// swallow every character after it, which is a worse failure and a much
		// commoner one.
		name: "private key",
		re:   regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
	},
	{
		// GitHub's tokens, every prefix it mints: `ghp_` personal, `gho_`
		// OAuth — the one this table was written for — `ghu_` user-to-server,
		// `ghs_` server-to-server, `ghr_` refresh. All are the prefix and then
		// at least thirty-six characters of base62.
		name: "token",
		re:   regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}`),
		lead: 4,
	},
	{
		// GitHub's fine-grained tokens, which are a different shape entirely:
		// a longer fixed prefix, and a body that carries underscores.
		name: "token",
		re:   regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{22,}`),
		lead: 11,
	},
	{
		// The `sk-` family, which is now most of the industry: OpenAI's
		// `sk-…` and `sk-proj-…`, Anthropic's `sk-ant-api03-…`, OpenRouter's
		// `sk-or-v1-…`. Twenty characters of body is the floor, and it is a
		// floor with a known cost: a hyphenated path segment that happens to
		// begin `sk-` and run twenty characters is redacted too. That is the
		// side to be wrong on — a mangled filename in a record is an
		// inconvenience, a live key in one is an incident.
		name: "token",
		re:   regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`),
		lead: 3,
	},
	{
		// AWS access key ids: `AKIA` for a long-lived one, `ASIA` for the
		// temporary one an assumed role hands out. Sixteen uppercase is the
		// documented length and the pattern takes MORE than sixteen where the
		// run is longer, so a longer id is redacted whole rather than left with
		// a tail hanging off the marker.
		name: "token",
		re:   regexp.MustCompile(`\bA[KS]IA[0-9A-Z]{16,}`),
		lead: 4,
	},
	{
		// Slack, whose whole family is `xox` and one letter saying which kind —
		// bot, user, app, refresh — and then a hyphen.
		name: "token",
		re:   regexp.MustCompile(`\bxox[a-z]-[A-Za-z0-9-]{10,}`),
		lead: 5,
	},
	{
		// A JWT, which is not one service's shape but a format: three
		// base64url segments joined by dots, the first two of which are encoded
		// JSON and therefore both begin `eyJ`. Requiring the SECOND `eyJ` and
		// both dots is what keeps this row off ordinary base64 — a `data:` URI
		// carrying JSON opens `eyJ` too, and has no dot after it.
		name: "token",
		re:   regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`),
		lead: 3,
	},
	{
		// The credential in an `Authorization: Bearer …` header, which is the
		// one place a secret of ANY shape announces itself — the word in front
		// of it is the whole of what makes it recognisable. The word itself is
		// left standing, because a header with its scheme intact still reads as
		// a header; only the credential after it is replaced, and nothing of it
		// survives, because a credential with no fixed prefix has no leading
		// character that is not somebody's key.
		name:  "token",
		re:    regexp.MustCompile(`(?i)\bbearer\s+([A-Za-z0-9._~+/=-]{16,})`),
		group: 1,
	},
}

// Secrets replaces every secret-shaped span in text with a marker naming what
// was taken out of it.
//
// It is PURE and it is TOTAL: the same text in gives the same text out, it
// touches no disk and no clock, and text with nothing secret-shaped in it comes
// back as the identical string. That is what lets it sit on the one door every
// tool result passes through without anything downstream having to know it is
// there.
//
// WHAT IT COSTS, because it runs on every result: roughly 10 MB/s on an M3
// (BenchmarkSecretsOnALargeCleanResult), which is one pass per row of the table
// over text that has nothing in it. Every tool on the belt truncates its own
// output at 50KB (internal/exec/bare's truncation), so the largest result this
// can be handed costs well under a millisecond — beside a shell call that took
// seconds to produce it. There is no screening pass and no cache, because at
// that ratio either would be more machinery than it saved.
func Secrets(text string) string {
	if text == "" {
		return text
	}
	for _, s := range shapes {
		text = s.apply(text)
	}
	return text
}

// apply rewrites one shape's spans, and only its spans — the text between two
// matches is copied through byte for byte, so a result that carries one token
// in a thousand lines loses the token and nothing else.
func (s shape) apply(text string) string {
	found := s.re.FindAllStringSubmatchIndex(text, -1)
	if found == nil {
		return text
	}
	var out strings.Builder
	out.Grow(len(text))
	written := 0
	for _, span := range found {
		start, end := span[2*s.group], span[2*s.group+1]
		// A group that did not participate reports -1. No row in the table can
		// produce one today, and the guard costs a comparison rather than a
		// panic the day a row with an optional group is added.
		if start < 0 || start < written {
			continue
		}
		out.WriteString(text[written:start])
		out.WriteString(s.marker(text[start:end]))
		written = end
	}
	out.WriteString(text[written:])
	return out.String()
}

// marker is the sentence that stands where the secret was.
//
// The lead is sliced by BYTES rather than runes, which is safe for the reason
// it is safe nowhere else in this repository: every character a lead covers is
// part of an ASCII prefix spelled literally in that row's own pattern, so the
// cut can only ever land on a boundary.
func (s shape) marker(secret string) string {
	if s.lead > 0 && len(secret) > s.lead {
		return "[redacted " + s.name + " · " + secret[:s.lead] + "…]"
	}
	return "[redacted " + s.name + "]"
}
