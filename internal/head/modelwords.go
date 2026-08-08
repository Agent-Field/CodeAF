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

// CompilerAnswerPrefix marks the user's answer where the head splices it back
// onto the instruction that raised the question. It is the rail's durable
// record that an ask was already put to the user: every deterministic
// recognizer reads the last answer as authoritative and none may raise that
// question again, because a recognizer that re-reads the older words asks the
// same question forever.
const CompilerAnswerPrefix = "Answer to compiler question:"

// SpliceCompilerAnswer is the one way an answer rejoins its instruction.
func SpliceCompilerAnswer(instruction, answer string) string {
	return instruction + "\n\n" + CompilerAnswerPrefix + " " + strings.TrimSpace(answer)
}

// LastCompilerAnswer returns the most recent answer spliced onto instruction.
// Answers accumulate in order, so the last one is the live decision.
func LastCompilerAnswer(instruction string) (string, bool) {
	index := strings.LastIndex(instruction, CompilerAnswerPrefix)
	if index < 0 {
		return "", false
	}
	return strings.TrimSpace(instruction[index+len(CompilerAnswerPrefix):]), true
}

// answeredCompilerQuestions lists every answer this instruction already
// carries, oldest first. The compiler declares them settled to the provider so
// the reasoning half of the rail cannot re-ask them either.
func answeredCompilerQuestions(instruction string) []string {
	var answers []string
	for remaining := instruction; ; {
		index := strings.Index(remaining, CompilerAnswerPrefix)
		if index < 0 {
			return answers
		}
		remaining = remaining[index+len(CompilerAnswerPrefix):]
		answer := remaining
		if end := strings.Index(answer, "\n\n"+CompilerAnswerPrefix); end >= 0 {
			answer = answer[:end]
		}
		if answer = strings.TrimSpace(answer); answer != "" {
			answers = append(answers, answer)
		}
	}
}

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
	// answerNamePattern reads a name out of a free-text answer, where the
	// framing verb the sentence patterns need is usually absent: "kimi 3" and
	// "moonshotai/kimi-k2" are both whole answers to "which kimi do you mean".
	answerNamePattern = regexp.MustCompile(
		`(?i)\b([a-z][a-z0-9.+\-]*(?:/[a-z0-9.+\-]+)*)\b`)
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
	// Answered means these words were read out of the user's answer to a
	// question this ask already asked. The choice is settled: whatever the
	// catalog says, the job proceeds and nothing asks again.
	Answered bool
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

// RecognizeModelWords reads a task ask for the model the user asked for. An
// ask that already carries an answer is read answer-first: the answer is
// younger than the words that raised the question, and reading those words
// again is how the same question came back forever.
func RecognizeModelWords(instruction string) (ModelWords, bool) {
	answer, answered := LastCompilerAnswer(instruction)
	if !answered {
		return recognizeModelWords(instruction)
	}
	if words, ok := recognizeModelWords(answer); ok {
		words.Answered = true
		return words, true
	}
	if names := answerModelNames(answer); len(names) > 0 {
		// The answer is the name by itself — an option value, or the user's own
		// words. Saying it at all is saying "model", so it earns a receipt.
		return ModelWords{Names: names, Explicit: true, Answered: true}, true
	}
	// The answer settled some other question. The original model words still
	// stand, but they have had their one ask.
	words, ok := recognizeModelWords(instruction)
	words.Answered = true
	return words, ok
}

// answerModelNames reads the candidate names out of a bare answer, in order.
// Pure numbers are option keys, never models.
func answerModelNames(answer string) []string {
	var names []string
	seen := make(map[string]bool)
	for _, match := range answerNamePattern.FindAllStringSubmatch(answer, -1) {
		name := strings.ToLower(strings.TrimSpace(match[1]))
		if name == "" || bareModelStopWords[name] || answerFramingWords[name] || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// answerFramingWords are the verbs an answer wraps its name in. They are not
// model words in any catalog, and excluding them keeps a phrased answer such
// as "use kimi" resolving on "kimi".
var answerFramingWords = map[string]bool{
	"use": true, "using": true, "with": true, "run": true, "please": true,
	"go": true, "just": true, "prefer": true, "pick": true, "choose": true,
	"want": true, "let": true, "s": true, "i": true, "d": true, "is": true,
	"be": true, "of": true, "by": true, "at": true, "as": true, "we": true,
	"you": true, "do": true, "mean": true, "meant": true, "option": true,
}

func recognizeModelWords(instruction string) (ModelWords, bool) {
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

// RecognizeModelWords had exactly one call site — inside Compiler.Compile,
// which only a fresh splice reaches — so model words could create a new job and
// could never move an existing one. The two natural phrasings diverged
// completely: "redo that with the better model" fell through to the router and
// became a brand-new job in a brand-new workspace, while "rerun that with the
// better model" matched the restart cue, was handled deterministically, and
// re-ran the failure on the default slot. A restart is where escalation
// actually belongs — the work exists, the user watched it go wrong, and they
// are asking for a stronger hand on the same job.
//
// store.Command has no model field and adding one is a store change this seam
// does not need: the instruction is a durable payload and the model words are
// already in it, verbatim. What the head adds is its own deterministic READING
// of them, on one marked line, in exactly the idiom CompilerAnswerPrefix
// already uses. The head cannot resolve a name — the catalog lives with the
// surface that owns the slots — so it says what it read and leaves resolution
// where the resolver is.
const (
	// RestartModelPrefix marks the head's reading of the model words on a
	// restart. It is matched rather than reconstructed, so a change to the
	// wording makes the reader return nothing — today's behaviour — instead of
	// a wrong model.
	RestartModelPrefix = "Run this restart on:"
	// RestartModelBoost is what the prefix carries when the ask named the boost
	// slot rather than a model. The slot always resolves, so this always does.
	RestartModelBoost = "the boost model"
)

// MarkRestartModel appends the head's reading of a restart's model words to the
// instruction it journals. An instruction that names no model comes back
// byte-identical, so every restart that was silent stays silent.
func MarkRestartModel(instruction string) string {
	// Idempotent, because a gated restart is journaled from the same instruction
	// the confirmation question carried: marking a marked instruction would read
	// its own mark as a second set of model words.
	if strings.Contains(instruction, RestartModelPrefix) {
		return instruction
	}
	words, wanted := RecognizeModelWords(instruction)
	if !wanted {
		return instruction
	}
	choice := RestartModelBoost
	if !words.Boost {
		if len(words.Names) == 0 {
			return instruction
		}
		choice = words.Names[0]
	}
	return strings.TrimSpace(instruction) + "\n\n" + RestartModelPrefix + " " + choice
}

// RestartModel reads back what MarkRestartModel wrote. The second return is
// whether the restart named a model at all; the first is the words to resolve,
// in the shape a ModelResolver already takes.
func RestartModel(instruction string) (ModelWords, bool) {
	index := strings.LastIndex(instruction, RestartModelPrefix)
	if index < 0 {
		return ModelWords{}, false
	}
	choice := strings.TrimSpace(firstMarkedLine(instruction[index+len(RestartModelPrefix):]))
	switch {
	case choice == "":
		return ModelWords{}, false
	case choice == RestartModelBoost:
		return ModelWords{Boost: true}, true
	default:
		return ModelWords{Names: []string{choice}, Explicit: true}, true
	}
}

// restartModelReceipt is the calm half-sentence the surgery receipt gains when
// the restart carries a model. It states the request, which is what the command
// records; whether the catalog has that model is the resolver's news to give.
func restartModelReceipt(instruction string) string {
	words, wanted := RestartModel(instruction)
	switch {
	case !wanted:
		return ""
	case words.Boost:
		return " on the stronger model"
	default:
		return " on " + words.Names[0]
	}
}

func firstMarkedLine(value string) string {
	if index := strings.IndexAny(value, "\r\n"); index >= 0 {
		return value[:index]
	}
	return value
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

// settleAnsweredAmbiguity closes a choice the user has already been asked
// about. The candidates arrive best first, so the leading one takes the job;
// the runner-up rides along only so the receipt can name the way back.
func settleAnsweredAmbiguity(choice WorkModelChoice) WorkModelChoice {
	if len(choice.Candidates) == 0 {
		return choice
	}
	choice.Model = choice.Candidates[0]
	return choice
}

// modelReceiptNote is the calm line about which model this job runs on. Empty
// is the ordinary case: no model words, nothing to say.
func modelReceiptNote(words ModelWords, choice WorkModelChoice) string {
	if model := strings.TrimSpace(choice.Model); model != "" {
		if len(choice.Candidates) > 1 {
			// The answer still fit more than one model. Proceeding and naming
			// the switch beats asking a question the user already answered.
			return fmt.Sprintf("Went with %s — say %q to switch.", model, "use "+choice.Candidates[1])
		}
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
