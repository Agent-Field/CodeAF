// Package anchors derives shared evidence from the chats themselves, without
// a subject dictionary or any model calls.
package anchors

import (
	"context"
	"math"
	"strings"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// Derived is stateless so a new corpus always gets its own frequencies.
// Its zero value is ready to use.
type Derived struct{}

var _ placeorg.Anchorer = Derived{}

const (
	minDocuments     = 3
	maxDocumentShare = 0.05
)

// Anchors counts each anchor once per chat, regardless of how often a pasted
// reference repeats. Corpus-wide spelling evidence lets a proper token also
// match a lowercase mention, while its frequency includes every spelling.
func (Derived) Anchors(ctx context.Context, chats []placeorg.Chat, confirmedNames []string) (map[string]placeorg.AnchorSet, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	confirmed := map[string]bool{}
	for _, name := range confirmedNames {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, token := range tokens(name) {
			confirmed[strings.ToLower(token)] = true
		}
	}
	documents := make([]map[string]bool, len(chats))
	fixed := make([]map[string]bool, len(chats))
	proper := map[string]bool{}
	df := map[string]int{}
	for i, chat := range chats {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		text := chat.Title + "\n" + chat.Text
		documents[i] = map[string]bool{}
		for _, token := range tokens(text) {
			key := strings.ToLower(token)
			documents[i][key] = true
			if isProper(token) {
				proper[key] = true
			}
		}
		for key := range documents[i] {
			df[key]++
		}
		fixed[i] = references(text)
		if family := workspaceFamily(chat.Workspace); family != "" {
			fixed[i]["workspace:"+family] = true
		}
		if repo := strings.TrimSpace(chat.RepoKey); repo != "" {
			fixed[i]["repo:"+repo] = true
		}
	}
	sets := make([]map[string]bool, len(chats))
	frequency := map[string]int{}
	for i := range chats {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sets[i] = fixed[i]
		for key := range documents[i] {
			if confirmed[key] || (proper[key] && df[key] >= minDocuments && float64(df[key]) <= maxDocumentShare*float64(len(chats))) {
				sets[i][key] = true
			}
		}
		for key := range sets[i] {
			frequency[key]++
		}
	}
	out := make(map[string]placeorg.AnchorSet, len(chats))
	for i, chat := range chats {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		set := placeorg.AnchorSet{}
		for key := range sets[i] {
			// Smoothed IDF stays positive even for a confirmed name used by
			// every chat, so shared evidence never becomes a zero weight.
			set[key] = float32(1 + math.Log(float64(len(chats)+1)/float64(frequency[key]+1)))
		}
		out[chat.ID] = set
	}
	return out, ctx.Err()
}

// tokens keeps internal hyphens because they can identify a project, while
// punctuation and underscores separate the words in confirmed place names.
func tokens(text string) []string {
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, "-")
		if startsWithLetter(part) {
			out = append(out, part)
		}
	}
	return out
}

func startsWithLetter(s string) bool {
	for _, r := range s {
		return unicode.IsLetter(r)
	}
	return false
}

func isProper(token string) bool {
	for _, r := range token {
		if unicode.IsUpper(r) || r == '-' {
			return true
		}
	}
	return false
}
