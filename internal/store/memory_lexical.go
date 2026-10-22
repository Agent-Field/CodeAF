package store

import (
	"strings"
	"unicode"
)

var memoryTechnicalShort = map[string]bool{"ui": true, "ci": true, "pr": true, "ai": true, "os": true, "db": true, "id": true, "ip": true, "io": true, "go": true, "js": true, "ts": true, "py": true, "vm": true, "qa": true, "ux": true, "sh": true, "c": true, "r": true}
var memoryCueStops = func() map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields("a an and are as at be been being but by can could did do does doing for from had has have how i if in into is it its me memory mine my of on or our please say says should tell than that the their them then there these they this those to us was were what when where which who why will with would you your about status project task review work current currently") {
		out[w] = true
	}
	return out
}()

func memoryTopicWords(text string) []string { return memoryRawTopics(text) }
func memoryTopicToken(word string) bool     { return len(word) >= 3 || memoryTechnicalShort[word] }
func memoryCueToken(word string) bool {
	return (len(word) >= 2 || memoryTechnicalShort[word]) && !memoryCueStops[word]
}

// Technical two-letter tokens are retained through a small identifier lexicon;
// arbitrary capitals are recognized before case folding by memoryRawTopics.
func memoryRawTopics(text string) []string {
	words := strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	out := []string{}
	for _, w := range words {
		lower := strings.ToLower(w)
		if len(w) == 2 && w == strings.ToUpper(w) {
			out = append(out, lower)
			continue
		}
		if memoryTopicToken(lower) {
			out = append(out, lower)
		}
	}
	for entity := range memoryEntities(text) {
		if strings.ContainsAny(entity, "/.0123456789") {
			out = append(out, "entity:"+entity)
		}
	}
	return out
}
