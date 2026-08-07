package resident

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

const (
	// voiceSectionBytes keeps learned style from crowding out the work itself.
	voiceSectionBytes = 400
	voiceDoctrine     = `Voice for anything the user will read:
- Use plain speech in the user's terms.
- Keep internal plumbing and jargon backstage.
- Do not open with an apology or preamble.
Follow these standing user preferences:`
)

// The store's safe FTS query keeps twelve terms. These cover the durable
// communication corrections the distiller is taught to write.
var voiceQueryTerms = []string{
	"answer", "answers", "reply", "format", "tone", "language",
	"short", "preamble", "apologizing", "bullets", "inline", "markdown",
}

var voiceWordPrefixes = []string{
	"answer", "apolog", "bullet", "concise", "detail", "format", "heading",
	"inline", "intro", "language", "length", "list", "markdown", "number",
	"preamble", "reply", "response", "short", "tone", "verbose", "wording",
}

// VoiceSection assembles the shared speech doctrine with active, user-scoped
// voice preferences relevant to the current context. SearchFacts is deliberate:
// rendering a preference is a counted notebook read. No matching preference
// returns an empty section so existing prompts keep their exact bytes.
func VoiceSection(graph *store.Store, contextCues ...string) string {
	if graph == nil {
		return ""
	}
	terms := strings.Join(voiceQueryTerms, " ")
	if context := strings.TrimSpace(strings.Join(contextCues, "\n")); context != "" {
		terms += "\n" + context
	}
	facts, err := graph.SearchFacts(store.FactQuery{
		Terms: terms,
		Kind:  store.FactPreference,
		Limit: 16,
	})
	if err != nil {
		return ""
	}

	matched := make([]store.Fact, 0, len(facts))
	for _, fact := range facts {
		if fact.Scope == "user" && isVoicePreference(fact.Body) {
			matched = append(matched, fact)
		}
	}
	if len(matched) == 0 {
		return ""
	}
	// Retrieval order is relevance order, which reshuffles the whole list when
	// the corpus shifts and rewrites this section for no gain. Oldest first is
	// the only order under which a newly learned preference APPENDS: the lines
	// above it keep their bytes, and a prompt that opens with this section keeps
	// its cached prefix instead of paying for the whole thing again.
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].Seq < matched[j].Seq })
	preferences := make([]string, 0, len(matched))
	for _, fact := range matched {
		preferences = append(preferences, strings.TrimSpace(fact.Body))
	}

	var section strings.Builder
	section.WriteString(voiceDoctrine)
	for _, preference := range preferences {
		line := "- " + preference
		remaining := voiceSectionBytes - section.Len() - 1
		if remaining <= 3 {
			break
		}
		if len(line) > remaining {
			if section.Len() != len(voiceDoctrine) {
				continue
			}
			line = clipVoiceLine(line, remaining)
		}
		section.WriteByte('\n')
		section.WriteString(line)
	}
	return section.String()
}

// VoicePrompt appends the assembled contract only when the notebook contributes
// learned voice. That compatibility rule keeps every empty-notebook prompt
// byte-identical to the prompt that preceded adaptive voice.
func VoicePrompt(graph *store.Store, prompt string, contextCues ...string) string {
	section := VoiceSection(graph, contextCues...)
	if section == "" {
		return prompt
	}
	return prompt + "\n\n" + section
}

func isVoicePreference(body string) bool {
	words := strings.FieldsFunc(strings.ToLower(body), func(r rune) bool {
		return !unicode.IsLetter(r)
	})
	for _, word := range words {
		for _, prefix := range voiceWordPrefixes {
			if strings.HasPrefix(word, prefix) {
				return true
			}
		}
	}
	return false
}

func clipVoiceLine(line string, limit int) string {
	if len(line) <= limit {
		return line
	}
	cut := limit - 3
	for cut > 0 && !utf8.ValidString(line[:cut]) {
		cut--
	}
	return strings.TrimSpace(line[:cut]) + "..."
}
