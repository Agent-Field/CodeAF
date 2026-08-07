package head

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// Model words are read deterministically, exactly like standing and service
// intent: which model runs a job is the user's decision, not a provider's.
//
// Two shapes exist. A SLOT word ("with the better model", "use the boost
// model", "with model 2") names the boost slot and always resolves, because
// the slot always has a model. A NAME word ("use gemini", "with the opus
// model") names a model and is resolved against the live catalog by the
// surface; only a name the ask marked explicitly — it said "model" — earns a
// receipt line when nothing matches, because a bare word that resolves to
// nothing was probably never a model word at all.
const (
	// ModelSlotBoost is the slot a boost word resolves to, and ModelSlotWork is
	// the candidacy filter a named model is resolved inside — the same two
	// slots the model palette uses.
	ModelSlotBoost = "boost"
	ModelSlotWork  = "work"
	// MaxModelCandidates bounds one ambiguity question. More options than this
	// is a list, not a choice.
	MaxModelCandidates = 4
)

var (
	boostModelPattern = regexp.MustCompile(
		`(?i)\b(?:with|use|using|on|run\s+(?:it\s+)?(?:with|on))\s+(?:the\s+)?(?:better|best|stronger|smarter|bigger|boost(?:ed)?)\s+model\b`)
	boostSlotPattern    = regexp.MustCompile(`(?i)\bboost\s+model\b|\bmodel\s*#?\s*2\b`)
	explicitNamePattern = regexp.MustCompile(
		`(?i)\b(?:with|use|using|on|run\s+(?:it\s+)?(?:with|on))\s+(?:the\s+)?([a-z0-9][a-z0-9.+\-]*(?:/[a-z0-9.+\-]+)*)\s+model\b`)
	explicitPrefixPattern = regexp.MustCompile(
		`(?i)\b(?:with|use|using|on)\s+(?:the\s+)?model\s+([a-z0-9][a-z0-9.+\-]*(?:/[a-z0-9.+\-]+)*)`)
	bareNamePattern = regexp.MustCompile(
		`(?i)\b(?:with|use|using|on)\s+(?:the\s+)?([a-z0-9][a-z0-9.+\-]*(?:/[a-z0-9.+\-]+)*)`)
	qualityWordPattern = regexp.MustCompile(
		`(?i)\b(?:best|highest|top)\s+quality\b|\bhigh(?:est)?[\- ]fidelity\b|\bmake\s+it\s+(?:really\s+)?good\b|\bfinal\s+(?:deliverable|artifact|version|cut)\b|\bproduction[\- ]quality\b`)
)

// bareModelStopWords are the words that follow "use" or "with" in ordinary
// prose. Excluding them keeps catalog resolution from being asked silly
// questions; everything else is still filtered by whether it actually matches
// a model.
var bareModelStopWords = map[string]bool{
	"a": true, "an": true, "the": true, "it": true, "this": true, "that": true,
	"them": true, "these": true, "those": true, "care": true, "caution": true,
	"me": true, "my": true, "your": true, "our": true, "any": true, "all": true,
	"model": true, "models": true, "same": true, "default": true, "no": true,
	"and": true, "or": true, "for": true, "to": true, "in": true, "on": true,
	"one": true, "two": true, "each": true, "every": true, "some": true,
}

// ModelWords is the deterministic reading of the model words in a task ask.
type ModelWords struct {
	// Boost means the ask named the boost slot rather than a model.
	Boost bool
	// Names are candidate model words in the order they appeared. The surface
	// resolves them against the catalog; the first that resolves wins.
	Names []string
	// Explicit means the ask said "model" beside the name, so an unresolvable
	// name is worth one calm receipt line rather than silence.
	Explicit bool
}

// WorkModelChoice is the surface's answer about the model words in one ask.
// Exactly one of Model and Candidates is meaningful: a resolution, or the
// shortlist behind one choose question. Both empty means nothing matched.
type WorkModelChoice struct {
	Model      string
	Candidates []string
	// Requested is the word the user actually used, for the receipt.
	Requested string
}

// ModelResolver resolves recognized model words against the live catalog and
// the surface's slots. Nil leaves every job on the default work model.
type ModelResolver func(ModelWords) WorkModelChoice

// RecognizeModelWords reads a task ask for the model the user asked for.
func RecognizeModelWords(instruction string) (ModelWords, bool) {
	trimmed := strings.TrimSpace(instruction)
	if trimmed == "" {
		return ModelWords{}, false
	}
	if boostModelPattern.MatchString(trimmed) || boostSlotPattern.MatchString(trimmed) {
		return ModelWords{Boost: true}, true
	}
	for _, pattern := range []*regexp.Regexp{explicitNamePattern, explicitPrefixPattern} {
		if match := pattern.FindStringSubmatch(trimmed); len(match) == 2 {
			if name := strings.ToLower(strings.TrimSpace(match[1])); !bareModelStopWords[name] {
				return ModelWords{Names: []string{name}, Explicit: true}, true
			}
		}
	}
	var names []string
	seen := make(map[string]bool)
	for _, match := range bareNamePattern.FindAllStringSubmatch(trimmed, -1) {
		name := strings.ToLower(strings.TrimSpace(match[1]))
		if name == "" || bareModelStopWords[name] || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	if len(names) == 0 {
		return ModelWords{}, false
	}
	return ModelWords{Names: names}, true
}

// RecognizesQualityIntent reads the user asking for quality rather than for
// routine work. It is the signal media tools need to reach for "best".
func RecognizesQualityIntent(instruction string) bool {
	return qualityWordPattern.MatchString(instruction)
}

// qualityBriefSentence is deterministic, appended to the goal the same way
// attached documents are: a compiler that overlooks the words cannot lose them.
const qualityBriefSentence = `The user asked for quality, not routine output: pass model:"best" to generate_image, speak, generate_music, or generate_video for the final artifact.`

func anchorQualityWords(goal, instruction string) string {
	if !RecognizesQualityIntent(instruction) {
		return goal
	}
	return strings.TrimSpace(goal) + "\n\n" + qualityBriefSentence
}

// modelChoiceQuestion is the one askback an ambiguous model word earns. The
// options are phrased as the ask itself, so the answer that comes back through
// the ordinary compiler-question rail re-compiles into an exact resolution.
func modelChoiceQuestion(requested string) string {
	if requested = strings.TrimSpace(requested); requested == "" {
		return "Which model do you mean?"
	}
	return fmt.Sprintf("Which %s do you mean?", requested)
}

func modelChoiceOptions(candidates []string) []store.QuestionOption {
	options := make([]store.QuestionOption, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate = strings.TrimSpace(candidate); candidate != "" {
			options = append(options, store.QuestionOption{
				Label: "use " + candidate, Value: candidate,
			})
		}
	}
	return options
}

// modelReceiptNote is the calm line about which model this job runs on. Empty
// is the ordinary case: no model words, nothing to say.
func modelReceiptNote(words ModelWords, choice WorkModelChoice) string {
	if model := strings.TrimSpace(choice.Model); model != "" {
		return "Running on " + model + "."
	}
	if !words.Explicit {
		return ""
	}
	requested := strings.TrimSpace(choice.Requested)
	if requested == "" && len(words.Names) > 0 {
		requested = words.Names[0]
	}
	if requested == "" {
		return ""
	}
	return fmt.Sprintf("I don't have a model matching %q — running on the usual one.", requested)
}
