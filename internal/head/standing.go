package head

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/shaped"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const standingCompilerPrompt = `You compile durable intent into one inert charter draft.

Return exactly one JSON object with this shape and no text outside it:
{"invariant":"the user's exact words","watch":{"kind":"cron|file|graph|poll","cadence":"human cadence words","schedule":"structured schedule"},"sentinel":"cheap wake-time judgment","action":"what a firing does after re-grounding","rails":{"max_per_day":0,"expiry":""}}

Rules:
- Preserve the instruction exactly in invariant.
- Keep the sentinel to one cheap judgment: whether the invariant is threatened or its condition occurred.
- State an action that can be re-grounded when it fires; do not freeze today's world into it.
- Never write a cost, a rate or any figure in dollars. What a firing costs is measured from this system's own journaled runs after you have answered, and a number written here would be a guess presented to the user as a measurement.
- max_per_day is how many firings a day the intent could reasonably need; leave it 0 when the words do not settle it and a default is applied after this reading.
- Reminders are degenerate charters: cron watch, one firing, action = say the reminder.
- Return a draft only. Never claim it is active or ratified.`

var (
	standingEveryPattern    = regexp.MustCompile(`(?i)\b(?:whenever|each\s+time|every\s+(?:\w+|\d+\s+(?:minutes?|hours?|days?|weeks?)))\b`)
	standingStatePattern    = regexp.MustCompile(`(?i)\bmake\s+sure\b.+\b(?:stays?|remains?)\b`)
	standingReminderPattern = regexp.MustCompile(`(?i)\b(?:remind|notify|alert)\s+me\s+(?:when|whenever|at|on|in|tomorrow|next)\b`)
	standingWhenOncePattern = regexp.MustCompile(`(?i)\bwhen\s+i\s+say\b.*\bonce\b`)
	// standingOnceClausePattern separates the two words spelled "once". One is
	// a count — "run the benchmark once" — and it is the reason the exclusion
	// below exists at all: an ask that happens a single time is not durable
	// intent. The other is a temporal conjunction — "once the deploy is green",
	// "once it lands" — which is the exact shape a sentinel is FOR, and the
	// plain substring check was reading it as the first and killing it.
	//
	// The discriminator is grammatical rather than semantic: the conjunction is
	// followed by a clause, the count is followed by nothing or by punctuation.
	// So a subject pronoun or determiner after the word, or any word followed
	// by a verb of completion, means the sentence is naming a condition and not
	// a quantity.
	standingOnceClausePattern = regexp.MustCompile(`(?i)\bonce\s+(?:it|its|it'?s|they|the|that|this|there|we|you|[a-z]+\s+(?:is|are|has|have|was|were|finish|finishes|lands|completes|passes))\b`)
	// clockTail is the time-of-day a cadence phrase may carry with it: "at 9",
	// "at 8pm", "at 6:30 pm", "in the morning". It is spelled once and appended
	// to every pattern that can be qualified by one, because the clock is the
	// half of "every Sunday at 9am" that used to fall on the floor.
	clockTail       = `(?:\s+(?:morning|afternoon|evening|night))?(?:\s+at\s+\d{1,2}(?::\d{2})?\s*(?:am|pm)?|\s+\d{1,2}(?::\d{2})?\s*(?:am|pm))?`
	weekdayName     = `(?:sun|mon|tues|wednes|thurs|fri|satur)days?`
	cadencePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bevery\s+weekday` + clockTail),
		regexp.MustCompile(`(?i)\bevery\s+\d+\s+(?:minutes?|hours?|days?|weeks?)\b`),
		// A named day is the commonest standing rule there is and was the one
		// shape no pattern here knew. It reads with or without a leading word,
		// so "every sunday", "on sundays" and "sunday mornings" all land.
		regexp.MustCompile(`(?i)\b(?:every|each|on)\s+` + weekdayName + clockTail),
		regexp.MustCompile(`(?i)\b` + weekdayName + clockTail),
		regexp.MustCompile(`(?i)\bevery\s+(?:morning|afternoon|evening|night|day|week|hour)` + clockTail),
		regexp.MustCompile(`(?i)\b(?:hourly|daily|weekly)` + clockTail),
		regexp.MustCompile(`(?i)\b(?:whenever|each\s+time)\b`),
		regexp.MustCompile(`(?i)\btomorrow` + clockTail),
		regexp.MustCompile(`(?i)\bin\s+\d+\s+(?:minutes?|hours?|days?)\b`),
		// A bare clock is a cadence on its own: "remind me at 6pm to leave".
		regexp.MustCompile(`(?i)\bat\s+\d{1,2}(?::\d{2})?\s*(?:am|pm)?\b`),
		regexp.MustCompile(`(?i)\b\d{1,2}(?::\d{2})?\s*(?:am|pm)\b`),
	}
)

// RecognizesStandingIntent is the compiler's temporal reading: true means the
// ask survives one completion and therefore must cross ratification.
func RecognizesStandingIntent(instruction string) bool {
	trimmed := strings.TrimSpace(instruction)
	lower := strings.ToLower(trimmed)
	if lower == "" || standingWhenOncePattern.MatchString(lower) {
		return false
	}
	// "once the deploy is green" is a condition to be watched for, which is what
	// a sentinel is; "run the benchmark once" is a count, which is not durable
	// intent at all. The exclusion below used to read both as the count and so
	// no temporal ask could ever become a sentinel — the whole class of "do this
	// when that happens" fell through to an ordinary one-shot job that runs now.
	if standingOnceClausePattern.MatchString(lower) {
		return true
	}
	if strings.Contains(lower, " once") &&
		!strings.Contains(lower, "whenever") && !strings.Contains(lower, "each time") &&
		!standingReminderPattern.MatchString(lower) && !standingStatePattern.MatchString(lower) &&
		!strings.HasPrefix(lower, "keep ") && !strings.HasPrefix(lower, "please keep ") &&
		!strings.HasPrefix(lower, "watch ") && !strings.HasPrefix(lower, "please watch ") {
		return false
	}
	if standingEveryPattern.MatchString(lower) || standingStatePattern.MatchString(lower) ||
		standingReminderPattern.MatchString(lower) {
		return true
	}
	if strings.HasPrefix(lower, "keep ") || strings.HasPrefix(lower, "please keep ") ||
		strings.HasPrefix(lower, "watch ") || strings.HasPrefix(lower, "please watch ") ||
		strings.HasPrefix(lower, "monitor ") || strings.HasPrefix(lower, "please monitor ") {
		return true
	}
	return false
}

func (c *Compiler) compileStanding(ctx context.Context, instruction, graphContext string) (Brief, error) {
	user := "Current graph context and measured self-knowledge:\n" + graphContext +
		"\n\nUser instruction (verbatim; preserve exactly):\n" + instruction
	// The room a charter needs, and the repair when it does not fit, are the
	// shared seam's business (internal/shaped). What was here was a flat 800 that
	// a reasoning model spent entirely on deliberation — `finish_reason:"length"`,
	// `content:null`, and the whole standing route failed — followed by a
	// hand-rolled retry at double the room. Both were the ordinary compiler's
	// mistakes made a second time in a second file, which is the argument for
	// there being one seam at all. The echo term is the same one the compile has:
	// a charter restates the rule it is standing up.
	var spec store.CharterSpec
	if _, err := shaped.Answer(ctx, c.client, shaped.Ask{
		Lane: "compile",
		Messages: []ai.Message{
			textMessage("system", standingCompilerPrompt),
			textMessage("user", user),
		},
		Echo: instruction,
	}, &spec); err != nil {
		return Brief{}, fmt.Errorf("compile standing intent: %w", err)
	}
	spec = normalizeCharterSpec(spec, instruction, graphContext)
	return Brief{
		Question: "Stand this rule up?",
		QuestionOptions: []store.QuestionOption{
			{Label: "yes, stand this up", Value: "ratify"},
			{Label: "change when it runs", Value: "cadence"},
			{Label: "once, not standing", Value: "once"},
		},
		Charter: &spec,
	}, nil
}

func normalizeCharterSpec(spec store.CharterSpec, instruction, graphContext string) store.CharterSpec {
	spec.Invariant = instruction
	spec.Watch = standingWatch(instruction, spec.Watch)
	reminder := isReminder(instruction)
	spec.SayOnly = reminder
	if strings.TrimSpace(spec.Sentinel) == "" {
		spec.Sentinel = "Decide whether the standing condition occurred or the invariant is threatened."
	}
	if reminder {
		spec.Action = reminderAction(instruction)
	} else if strings.TrimSpace(spec.Action) == "" {
		spec.Action = "Re-ground the request at firing time, then carry it out: " + instruction
	}
	// Every figure in the rails is computed from journaled measurement, and the
	// model's own numbers are discarded rather than defaulted around. 13.3's
	// head edge is exactly this line: a proposal that said "$20.00 a run" over a
	// measured $0.0017 because the guess survived whenever the measurement could
	// not be read, and because the justification beside it was free prose with a
	// dollar figure in it. money.go argues the whole rule.
	spec.Rails = standingRails(spec.Rails, reminder, graphContext)
	recurring := store.RecurringWatch(spec.Watch.Spec)
	if strings.TrimSpace(spec.Rails.Expiry) == "" {
		spec.Rails.Expiry = "never"
		// A reminder ends when its one moment passes — and only then. Reading
		// every reminder as one-shot is what killed "remind me every sunday"
		// inside a day; the schedule already knows which kind this is.
		if reminder && !recurring {
			spec.Rails.Expiry = "once"
		}
	}
	if recurring && strings.EqualFold(strings.TrimSpace(spec.Rails.Expiry), "once") {
		spec.Rails.Expiry = "never"
	}
	return spec
}

// standingWatch keeps the head's human-language reading — which watch family
// the words imply and the cadence words themselves — and compiles it straight
// into the engine's typed WatchSpec. The compiler's schedule string survives
// only as a structured hint (a file glob, a threshold sketch); it is never
// executed.
func standingWatch(instruction string, proposed store.CharterWatch) store.CharterWatch {
	lower := strings.ToLower(instruction)
	cadence, guessed := standingCadence(instruction, proposed)
	kind := store.WatchPoll
	switch {
	case strings.Contains(lower, "folder") || strings.Contains(lower, "directory") ||
		strings.Contains(lower, " file") || strings.HasPrefix(lower, "file "):
		kind = store.WatchFile
		if cadence == "" {
			// A file watch has a natural rhythm — the file changing — so this
			// is the family answering, not a rhythm nobody chose.
			cadence, guessed = "on change", false
		}
	case strings.Contains(lower, "node settled") || strings.Contains(lower, "node failed") ||
		strings.Contains(lower, "spend threshold") || strings.Contains(lower, "budget threshold"):
		kind = store.WatchGraph
		if cadence == "" {
			cadence, guessed = "on graph change", false
		}
	case isReminder(instruction) || store.WeekdayNamed(lower) || strings.Contains(lower, "morning") ||
		strings.Contains(lower, "weekday") || strings.Contains(lower, "hourly") ||
		strings.Contains(lower, "daily") || strings.Contains(lower, "weekly"):
		kind = store.WatchCron
	}
	if cadence == "" {
		cadence = "about every 2 minutes"
		if strings.HasPrefix(lower, "keep ") || strings.HasPrefix(lower, "please keep ") {
			cadence = "about every 15 minutes"
		}
		guessed = true
	}
	hint := strings.TrimSpace(proposed.Schedule)
	watch := store.CharterWatch{Kind: kind, Cadence: cadence, Schedule: hint}
	watch.Spec = store.CadenceWatchSpec(kind, cadence, hint, instruction, time.Now())
	watch.Spec.Cadence = cadence
	watch.Spec.CadenceGuessed = guessed
	// The typed derivation degrades underdetermined file and graph watches to
	// a poll; the spec records what will actually run.
	watch.Kind = watch.Spec.Kind
	return watch
}

// standingCadence reads the rhythm in three descending degrees of authority:
// the user's own words, then the temporal compiler's reading of them, then
// nothing.
//
// The middle one is the repair. The compiler is asked for "human cadence
// words" and answers with them, and standingWatch used to throw that answer
// away — only the structured schedule hint survived, which cron never reads —
// and substitute a literal two minutes. So a model that had correctly
// understood "every sunday" watched the sentence say "about every 2 minutes"
// on the card. The deterministic patterns stay in front of it because they are
// free and exact; the model catches everything a pattern list never will.
//
// The second return is whether the rhythm is a guess. Nothing here invents one
// silently: an unreadable cadence comes back empty and the caller both defaults
// AND says that it did.
func standingCadence(instruction string, proposed store.CharterWatch) (string, bool) {
	if cadence := extractCadence(instruction); cadence != "" {
		return cadence, false
	}
	if cadence := strings.TrimSpace(proposed.Cadence); cadence != "" && store.RecognizedCadence(cadence) {
		return cadence, false
	}
	return "", true
}

func extractCadence(instruction string) string {
	for _, pattern := range cadencePatterns {
		if cadence := strings.TrimSpace(pattern.FindString(instruction)); cadence != "" {
			return cadence
		}
	}
	return ""
}

func isReminder(instruction string) bool {
	lower := strings.ToLower(instruction)
	return strings.Contains(lower, "remind me") || strings.Contains(lower, "notify me") ||
		strings.Contains(lower, "alert me")
}

func normalizeQuestionOptions(options []store.QuestionOption) []store.QuestionOption {
	if len(options) == 0 {
		return nil
	}
	normalized := make([]store.QuestionOption, 0, len(options))
	for _, option := range options {
		option.Label = strings.TrimSpace(option.Label)
		option.Value = strings.TrimSpace(option.Value)
		if option.Label != "" {
			normalized = append(normalized, option)
		}
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

// reminderAction lifts the thing to be said out of "remind me to X".
//
// The separator is the FIRST " to " after the cue, not the last one. Taking the
// last one is a plausible-looking mistake that quietly destroys the reminder:
// "remind me to submit the report to finance" split on the trailing " to " and
// promised to say "finance". The word after "remind me" begins the message and
// everything from there is the message, including any further "to" inside it.
func reminderAction(instruction string) string {
	lower := strings.ToLower(instruction)
	cue := -1
	for _, phrase := range []string{"remind me", "notify me", "alert me"} {
		if index := strings.Index(lower, phrase); index >= 0 && (cue < 0 || index < cue) {
			cue = index + len(phrase)
		}
	}
	if cue < 0 {
		cue = 0
	}
	if separator := strings.Index(lower[cue:], " to "); separator >= 0 {
		message := strings.TrimSpace(instruction[cue+separator+len(" to "):])
		if message != "" {
			return "Say: " + message
		}
	}
	return "Say this reminder: " + strings.TrimSpace(instruction)
}
