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
	// VoiceRegister is the register every reply is written in, and it is true
	// of a machine nobody has taught anything yet. It used to ride inside the
	// learned-preference section, which meant a fresh install — the exact
	// moment a person is meeting the product for the first time and has the
	// least vocabulary for it — was the one install that got no anti-jargon
	// instruction at all. The rule that covers every string nobody thought to
	// enumerate cannot be conditional on the user having already complained
	// about something else.
	//
	// The banned list is named rather than gestured at because "jargon" is not
	// a word a model can check a sentence against; these are the words this
	// system uses for itself, and each one has a plain replacement that says
	// the same thing to a person who has never read the source.
	VoiceRegister = `Voice for anything the user will read:
- Use plain speech in the user's terms.
- Keep internal plumbing and jargon backstage. The names this machinery uses for itself are never the words a person reads: not node, leaf, graph, splice, subtree, worker, charter, craft, rail, firing, notebook, or a raw id. Say the thing itself — a step, the work, a standing rule, the way you already do this, a daily limit, a run, what you have learned.
- Do not open with an apology or preamble.`
	// voicePreferenceHeader introduces what the notebook actually taught. It is
	// separate from the register so the register is a stable PREFIX of the full
	// section: the first learned preference appends bytes rather than rewriting
	// the segment a cached prompt already paid for.
	voicePreferenceHeader = `Follow these standing user preferences:`
	voiceDoctrine         = VoiceRegister + "\n" + voicePreferenceHeader
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
	// The budget bounds what the NOTEBOOK contributes, not the constant
	// register in front of it. Measuring the whole section against it made the
	// bound a function of how long the doctrine happens to be, which is how
	// naming the backstage words outright silently spent every preference slot.
	budget := len(voiceDoctrine) + voiceSectionBytes
	for _, preference := range preferences {
		line := "- " + preference
		remaining := budget - section.Len() - 1
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

// VoicePrompt installs the register on every prompt and the learned
// preferences on top of it when the notebook has any. The register is
// unconditional on purpose: it is the one instruction that covers strings
// nobody enumerated, including strings that do not exist yet, and gating it on
// learned preferences meant a fresh machine spoke the implementation's
// language until the user complained about something unrelated.
//
// The bytes stay cache-friendly. Whatever this returns begins
// prompt + "\n\n" + VoiceRegister in both branches, so learning a first voice
// preference extends the prompt rather than rewriting it.
func VoicePrompt(graph *store.Store, prompt string, contextCues ...string) string {
	section := VoiceSection(graph, contextCues...)
	if section == "" {
		section = VoiceRegister
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
