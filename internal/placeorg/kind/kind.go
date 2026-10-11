// Package kind sorts chats into conversations, probes and duplicates using
// structure only: how long the first message is, whether anyone answered it,
// whether many chats open the same way, and whether it has the shape of a
// request to echo a literal back.
//
// This file holds NO list of subjects, titles or trigger words. A list would
// be tuned to one person's library and silently wrong on the next; every
// signal here is a property of the text's shape or of the corpus as a whole.
package kind

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

const (
	// maxProbeRunes is the longest first message that can be a probe. A real
	// request that carries context is longer than anything canned.
	maxProbeRunes = 80
	// minShared is how many distinct chats must open the same way before the
	// opening counts as canned rather than a coincidence.
	minShared = 3
	// openingTokens is how many leading tokens make up an opening.
	openingTokens = 2
	// maxCannedTokens bounds a canned message: openings that lead into a long
	// sentence are ordinary requests that happen to start alike.
	maxCannedTokens = 8
)

// Kinder implements placeorg.Kinder with structural signals alone.
type Kinder struct{}

var _ placeorg.Kinder = Kinder{}

// New returns the stage; it holds no state, so one value serves every run.
func New() Kinder { return Kinder{} }

// short is a chat whose first message is small enough to be a probe.
type short struct {
	id      string
	message string
	tokens  []string
	replies int
}

// Kinds labels every chat. Duplicates are decided first, from the map the
// dedup stage produced, because a retried probe is still a duplicate and a
// duplicate must not inflate the tally that makes an opening "canned".
func (Kinder) Kinds(ctx context.Context, chats []placeorg.Chat, representative map[string]string) (map[string]placeorg.Kind, error) {
	kinds := make(map[string]placeorg.Kind, len(chats))
	var shorts []short
	for _, c := range chats {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if rep, ok := representative[c.ID]; ok && rep != c.ID {
			kinds[c.ID] = placeorg.KindDuplicate
			continue
		}
		kinds[c.ID] = placeorg.KindConversation
		if m := FirstMessage(c); m != "" && utf8.RuneCountInString(m) <= maxProbeRunes {
			shorts = append(shorts, short{id: c.ID, message: m, tokens: tokens(m), replies: c.Replies})
		}
	}
	canned := cannedOpenings(shorts)
	shouting := echoFirstWords(shorts)
	for _, s := range shorts {
		if s.replies == 0 || canned[opening(s.tokens)] || shouting[firstToken(s.tokens)] && hasLiteral(s.message) {
			kinds[s.id] = placeorg.KindProbe
		}
	}
	return kinds, nil
}

// FirstMessage recovers the person's first message from Chat.Text, which is
// the title, a newline, then that message. A chat without a title has the
// message alone.
func FirstMessage(c placeorg.Chat) string {
	t := strings.TrimSpace(c.Text)
	if c.Title != "" {
		t = strings.TrimSpace(strings.TrimPrefix(t, strings.TrimSpace(c.Title)))
	}
	return t
}

// cannedOpenings returns the openings that at least minShared distinct chats
// share, counting only messages short enough to be canned and free of a
// question mark (a question is a person asking, not a script running). A
// message that repeats whole is counted as its own opening too.
func cannedOpenings(shorts []short) map[string]bool {
	seen := map[string]int{}
	for _, s := range shorts {
		if len(s.tokens) == 0 || len(s.tokens) > maxCannedTokens || strings.ContainsAny(s.message, "?？") {
			continue
		}
		seen[opening(s.tokens)]++
	}
	out := map[string]bool{}
	for k, n := range seen {
		if n >= minShared {
			out[k] = true
		}
	}
	return out
}

// echoFirstWords returns first words that open at least minShared distinct
// short messages carrying a literal. A script that asks for a literal back
// reuses one imperative verb; people do not spell their requests that way
// three times with a payload each time.
func echoFirstWords(shorts []short) map[string]bool {
	seen := map[string]int{}
	for _, s := range shorts {
		if hasLiteral(s.message) {
			seen[firstToken(s.tokens)]++
		}
	}
	out := map[string]bool{}
	for k, n := range seen {
		if k != "" && n >= minShared {
			out[k] = true
		}
	}
	return out
}

// hasLiteral reports whether the message hands over a payload by shape: text
// after a colon, a quoted span, or a token that is a code rather than a word
// (it holds a digit, an underscore, or is written wholly in capitals).
func hasLiteral(m string) bool {
	if i := strings.Index(m, ":"); i >= 0 && strings.TrimSpace(m[i+1:]) != "" {
		return true
	}
	if strings.ContainsAny(m, "\"“”") {
		return true
	}
	for _, f := range strings.Fields(m) {
		w := strings.TrimFunc(f, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' })
		if isCode(w) {
			return true
		}
	}
	return false
}

// isCode is true for a token with a digit or underscore, or of three or more
// letters all upper-case.
func isCode(w string) bool {
	letters, upper := 0, 0
	for _, r := range w {
		switch {
		case unicode.IsDigit(r), r == '_':
			return true
		case unicode.IsLetter(r):
			letters++
			if unicode.IsUpper(r) {
				upper++
			}
		}
	}
	return letters >= 3 && upper == letters
}

// tokens splits a message into lower-cased runs of letters and digits.
func tokens(m string) []string {
	return strings.FieldsFunc(strings.ToLower(m), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

func firstToken(t []string) string {
	if len(t) == 0 {
		return ""
	}
	return t[0]
}

// opening is the first openingTokens tokens joined, or the whole message when
// it is shorter.
func opening(t []string) string {
	if len(t) > openingTokens {
		t = t[:openingTokens]
	}
	return strings.Join(t, " ")
}
