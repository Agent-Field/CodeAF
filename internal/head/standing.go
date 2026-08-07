package head

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const standingCompilerPrompt = `You compile durable intent into one inert charter draft.

Return exactly one JSON object with this shape and no text outside it:
{"invariant":"the user's exact words","watch":{"kind":"cron|file|graph|poll","cadence":"human cadence words","schedule":"structured schedule"},"sentinel":"cheap wake-time judgment","action":"what a firing does after re-grounding","rails":{"estimated_cost_usd":0.0,"max_per_day":0,"max_per_day_justification":"","expiry":""}}

Rules:
- Preserve the instruction exactly in invariant.
- Keep the sentinel to one cheap judgment: whether the invariant is threatened or its condition occurred.
- State an action that can be re-grounded when it fires; do not freeze today's world into it.
- Use measured execution costs from context when they exist.
- Leave a rail field zero or empty only when evidence does not settle it; deterministic defaults are applied after this reading.
- Reminders are degenerate charters: cron watch, one firing, action = say the reminder.
- Return a draft only. Never claim it is active or ratified.`

var (
	standingEveryPattern    = regexp.MustCompile(`(?i)\b(?:whenever|each\s+time|every\s+(?:\w+|\d+\s+(?:minutes?|hours?|days?|weeks?)))\b`)
	standingStatePattern    = regexp.MustCompile(`(?i)\bmake\s+sure\b.+\b(?:stays?|remains?)\b`)
	standingReminderPattern = regexp.MustCompile(`(?i)\b(?:remind|notify|alert)\s+me\s+(?:when|whenever|at|on|in|tomorrow|next)\b`)
	standingWhenOncePattern = regexp.MustCompile(`(?i)\bwhen\s+i\s+say\b.*\bonce\b`)
	cadencePatterns         = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bevery\s+weekday(?:\s+at\s+\d{1,2}(?::\d{2})?)?`),
		regexp.MustCompile(`(?i)\bevery\s+\d+\s+(?:minutes?|hours?|days?|weeks?)\b`),
		regexp.MustCompile(`(?i)\bevery\s+(?:morning|afternoon|evening|night|day|week|hour)\b`),
		regexp.MustCompile(`(?i)\b(?:hourly|daily|weekly)\b`),
		regexp.MustCompile(`(?i)\b(?:whenever|each\s+time)\b`),
		regexp.MustCompile(`(?i)\btomorrow(?:\s+at\s+\d{1,2}(?::\d{2})?)?`),
		regexp.MustCompile(`(?i)\bin\s+\d+\s+(?:minutes?|hours?|days?)\b`),
	}
	measuredCostPattern = regexp.MustCompile(`(?i)(?:avg(?:erage)?\s+cost|cost)\s*[:=]?\s*\$([0-9]+(?:\.[0-9]+)?)`)
)

// RecognizesStandingIntent is the compiler's temporal reading: true means the
// ask survives one completion and therefore must cross ratification.
func RecognizesStandingIntent(instruction string) bool {
	trimmed := strings.TrimSpace(instruction)
	lower := strings.ToLower(trimmed)
	if lower == "" || standingWhenOncePattern.MatchString(lower) {
		return false
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

// standingDraftReply is the receipt for a deterministically recognized durable
// ask. It promises a draft and nothing else: ratification is still the user's.
const standingDraftReply = "Reading that as a standing rule — drafting a charter for you to confirm."

// manageStanding gives durable language the same treatment node surgery already
// has: the deterministic reading runs BEFORE the routing model, so recognized
// standing intent reaches the charter-draft path whatever the model's mood.
//
// The failure this exists for was live: "whenever a new pr comes to agentfield
// org, make sure to check for security scan and vulnerability..." routed to the
// notebook as a preference fact, because the recognizer only ran inside the
// compiler — downstream of a routing decision that never arrived there.
//
// The cue set is deliberately unchanged. A false positive costs one ratification
// card; a false negative costs the whole feature.
func (h *Head) manageStanding(user store.Message) (bool, error) {
	instruction := strings.TrimSpace(user.Body)
	if !RecognizesStandingIntent(instruction) {
		return false, nil
	}
	// "What happens every day while I'm gone?" carries the cadence words and
	// none of the intent. A question about aforge is never a rule for aforge,
	// so it goes on to the loop that can actually answer it.
	if selfQuestionPhrased(strings.ToLower(instruction)) {
		return false, nil
	}
	// The same untargeted splice the routing model would have emitted. The
	// reconciler compiles it, and the compiler's temporal path turns it into a
	// CharterSpec plus the ratification card.
	command, err := h.store.RequestCommand(store.Command{
		SessionID:   user.SessionID,
		Kind:        store.CommandSplice,
		Instruction: instruction,
		Attachments: append([]string(nil), user.Attachments...),
	})
	if err != nil {
		return true, h.postAgent(user.SessionID, commandErrorReply, 0)
	}
	return true, h.postAgent(user.SessionID, standingDraftReply, command.Seq)
}

func (c *Compiler) compileStanding(ctx context.Context, instruction, graphContext string) (Brief, error) {
	user := "Current graph context and measured self-knowledge:\n" + graphContext +
		"\n\nUser instruction (verbatim; preserve exactly):\n" + instruction
	response, err := c.client.CompleteWithMessages(ctx, []ai.Message{
		textMessage("system", standingCompilerPrompt),
		textMessage("user", user),
	}, ai.WithMaxTokens(800))
	if err != nil {
		return Brief{}, fmt.Errorf("compile standing intent: %w", err)
	}
	if response == nil {
		return Brief{}, errors.New("compile standing intent: provider returned a nil response")
	}

	var spec store.CharterSpec
	if err := decodeJSONObject(response.Text(), &spec); err != nil {
		return Brief{}, fmt.Errorf("compile standing intent: %w", err)
	}
	spec = normalizeCharterSpec(spec, instruction, graphContext)
	return Brief{
		Question: "Stand this charter up?",
		QuestionOptions: []store.QuestionOption{
			{Label: "yes, stand this up", Value: "ratify"},
			{Label: "change the cadence", Value: "cadence"},
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
	if measured, ok := measuredStandingCost(graphContext); ok {
		spec.Rails.EstimatedCostUSD = measured
	} else if spec.Rails.EstimatedCostUSD <= 0 || math.IsNaN(spec.Rails.EstimatedCostUSD) ||
		math.IsInf(spec.Rails.EstimatedCostUSD, 0) {
		spec.Rails.EstimatedCostUSD = 0.15
	}
	if spec.Rails.MaxPerDay <= 0 {
		spec.Rails.MaxPerDay = 10
		if reminder {
			spec.Rails.MaxPerDay = 1
		}
	}
	if strings.TrimSpace(spec.Rails.MaxPerDayJustification) == "" {
		if reminder {
			spec.Rails.MaxPerDayJustification = "one firing matches the reminder's once expiry"
		} else {
			worst := spec.Rails.EstimatedCostUSD * float64(spec.Rails.MaxPerDay)
			spec.Rails.MaxPerDayJustification = fmt.Sprintf(
				"caps the default worst day at about $%.2f", worst)
		}
	}
	if strings.TrimSpace(spec.Rails.Expiry) == "" {
		spec.Rails.Expiry = "never"
		if reminder {
			spec.Rails.Expiry = "once"
		}
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
	cadence := extractCadence(instruction)
	kind := store.WatchPoll
	switch {
	case strings.Contains(lower, "folder") || strings.Contains(lower, "directory") ||
		strings.Contains(lower, " file") || strings.HasPrefix(lower, "file "):
		kind = store.WatchFile
		if cadence == "" {
			cadence = "on change"
		}
	case strings.Contains(lower, "node settled") || strings.Contains(lower, "node failed") ||
		strings.Contains(lower, "spend threshold") || strings.Contains(lower, "budget threshold"):
		kind = store.WatchGraph
		if cadence == "" {
			cadence = "on graph change"
		}
	case isReminder(instruction) || strings.Contains(lower, "morning") ||
		strings.Contains(lower, "weekday") || strings.Contains(lower, "hourly") ||
		strings.Contains(lower, "daily") || strings.Contains(lower, "weekly"):
		kind = store.WatchCron
	}
	if cadence == "" {
		cadence = "about every 2 minutes"
		if strings.HasPrefix(lower, "keep ") || strings.HasPrefix(lower, "please keep ") {
			cadence = "about every 15 minutes"
		}
	}
	hint := strings.TrimSpace(proposed.Schedule)
	watch := store.CharterWatch{Kind: kind, Cadence: cadence, Schedule: hint}
	watch.Spec = store.CadenceWatchSpec(kind, cadence, hint, instruction, time.Now())
	watch.Spec.Cadence = cadence
	// The typed derivation degrades underdetermined file and graph watches to
	// a poll; the spec records what will actually run.
	watch.Kind = watch.Spec.Kind
	return watch
}

func extractCadence(instruction string) string {
	for _, pattern := range cadencePatterns {
		if cadence := strings.TrimSpace(pattern.FindString(instruction)); cadence != "" {
			return cadence
		}
	}
	return ""
}

func measuredStandingCost(context string) (float64, bool) {
	match := measuredCostPattern.FindStringSubmatch(context)
	if len(match) != 2 {
		return 0, false
	}
	cost, err := strconv.ParseFloat(match[1], 64)
	return cost, err == nil && cost >= 0 && !math.IsNaN(cost) && !math.IsInf(cost, 0)
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

func reminderAction(instruction string) string {
	lower := strings.ToLower(instruction)
	if separator := strings.LastIndex(lower, " to "); separator >= 0 {
		message := strings.TrimSpace(instruction[separator+4:])
		if message != "" {
			return "Say: " + message
		}
	}
	return "Say this reminder: " + strings.TrimSpace(instruction)
}
