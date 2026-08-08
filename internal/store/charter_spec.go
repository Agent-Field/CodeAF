package store

// This file is the dissolution seam between the head's temporal compiler and
// the canonical charter store. The head compiles conversation into a
// CharterSpec — the user's cadence words beside a typed watch — and
// DraftCharter turns that spec into one ordinary proposed Charter journaled
// through CreateCharter. There is exactly one charters table and one event
// vocabulary: a "draft awaiting ratification" IS a proposed charter, ratifying
// it IS SetCharterStatus(active) carrying the user's words as evidence, and a
// cadence edit IS ReviseCharter with a freshly derived typed WatchSpec.

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CharterWatch preserves the user's cadence words beside their executable
// schedule. Surfaces speak Cadence; the standing engine consumes Spec.
// Schedule is the compiler's optional structured hint (a glob, predicate, or
// schedule sketch) and is never executed directly.
type CharterWatch struct {
	Kind     WatchKind `json:"kind"`
	Cadence  string    `json:"cadence"`
	Schedule string    `json:"schedule,omitempty"`
	Spec     WatchSpec `json:"spec,omitempty"`
}

// CharterSpecRails bound every firing before a charter can be ratified.
// Expiry is the compiler's word: "never", or "once" for reminders that must
// fire a single time and then retire.
type CharterSpecRails struct {
	EstimatedCostUSD       float64 `json:"estimated_cost_usd"`
	MaxPerDay              int     `json:"max_per_day"`
	MaxPerDayJustification string  `json:"max_per_day_justification"`
	Expiry                 string  `json:"expiry"`
}

// CharterSpec is the compiled, still-inert form of standing intent.
type CharterSpec struct {
	Invariant string           `json:"invariant"`
	Watch     CharterWatch     `json:"watch"`
	Sentinel  string           `json:"sentinel"`
	Action    string           `json:"action"`
	SayOnly   bool             `json:"say_only,omitempty"`
	Rails     CharterSpecRails `json:"rails"`
}

// defaultPerFiringBudgetUSD backstops a spec whose measured cost never
// arrived; a charter cannot exist with a non-positive per-firing rail.
const defaultPerFiringBudgetUSD = 0.15

// reminderExpiryWindow keeps a fired-once reminder alive long enough to be
// delivered late, then retires it before a second scheduled day.
const reminderExpiryWindow = 23 * time.Hour

// DraftCharter compiles a head spec into one canonical proposed charter.
// The id is assigned by the caller from its source command, so retries
// converge on the same identity instead of drafting twice.
func (s *Store) DraftCharter(id, sessionID string, sourceCommandSeq int64, spec CharterSpec) (Charter, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Charter{}, fmt.Errorf("draft charter: %w: empty id", ErrInvalid)
	}
	if sourceCommandSeq < 0 {
		return Charter{}, fmt.Errorf("draft charter: %w: invalid source command", ErrInvalid)
	}
	if err := validateCharterSpec(spec); err != nil {
		return Charter{}, fmt.Errorf("draft charter: %w", err)
	}
	if existing, found, err := s.Charter(id); err != nil {
		return Charter{}, fmt.Errorf("draft charter: %w", err)
	} else if found {
		return existing, nil
	}

	now := time.Now()
	watch := spec.Watch.Spec
	if !watchSpecPopulated(watch) {
		watch = CadenceWatchSpec(spec.Watch.Kind, spec.Watch.Cadence, spec.Watch.Schedule, spec.Invariant, now)
	}
	watch.Cadence = strings.TrimSpace(spec.Watch.Cadence)
	watch.CadenceGuessed = spec.Watch.Spec.CadenceGuessed

	rails := CharterRails{
		PerFiringBudgetUSD: spec.Rails.EstimatedCostUSD,
		MaxFiringsPerDay:   spec.Rails.MaxPerDay,
	}
	if rails.PerFiringBudgetUSD <= 0 || math.IsNaN(rails.PerFiringBudgetUSD) || math.IsInf(rails.PerFiringBudgetUSD, 0) {
		rails.PerFiringBudgetUSD = defaultPerFiringBudgetUSD
	}
	if strings.EqualFold(strings.TrimSpace(spec.Rails.Expiry), "once") {
		// "Once" is only an expiry when the schedule itself happens once. A
		// recurring rule that also said "once" means one firing a day, and
		// reading it as an expiry is how "remind me every Sunday" used to die
		// twenty-three hours after it was ratified — before its first Sunday.
		if RecurringWatch(watch) {
			rails.MaxFiringsPerDay = 1
		} else {
			first, err := initialCharterDue(watch, now)
			if err != nil {
				return Charter{}, fmt.Errorf("draft charter: %w", err)
			}
			expires := first.Add(reminderExpiryWindow)
			rails.ExpiresAt = &expires
			rails.MaxFiringsPerDay = 1
		}
	}

	action := CharterAction{Template: reminderTemplate(spec.Action), SayOnly: spec.SayOnly}
	evidence := "drafted from conversation"
	if sourceCommandSeq > 0 {
		evidence = fmt.Sprintf("drafted from conversation command %d", sourceCommandSeq)
	}
	charter, err := NewCharter(id, spec.Invariant, watch, spec.Sentinel, action, rails,
		CharterProposed, Ratification{
			Origin: OriginUser, SessionID: strings.TrimSpace(sessionID), Evidence: evidence,
		})
	if err != nil {
		return Charter{}, fmt.Errorf("draft charter: %w", err)
	}
	if err := s.CreateCharter(charter); err != nil {
		return Charter{}, err
	}
	created, found, err := s.Charter(id)
	if err != nil {
		return Charter{}, fmt.Errorf("draft charter: %w", err)
	}
	if !found {
		return Charter{}, fmt.Errorf("draft charter: %w: %q vanished after creation", ErrNotFound, id)
	}
	return created, nil
}

func validateCharterSpec(spec CharterSpec) error {
	if strings.TrimSpace(spec.Invariant) == "" || strings.TrimSpace(spec.Sentinel) == "" ||
		strings.TrimSpace(spec.Action) == "" {
		return fmt.Errorf("%w: invariant, sentinel, and action are required", ErrInvalid)
	}
	switch spec.Watch.Kind {
	case WatchCron, WatchFile, WatchGraph, WatchPoll:
	default:
		return fmt.Errorf("%w: unknown watch kind %q", ErrInvalid, spec.Watch.Kind)
	}
	if strings.TrimSpace(spec.Watch.Cadence) == "" {
		return fmt.Errorf("%w: cadence is required", ErrInvalid)
	}
	rails := spec.Rails
	if rails.EstimatedCostUSD < 0 || math.IsNaN(rails.EstimatedCostUSD) || math.IsInf(rails.EstimatedCostUSD, 0) ||
		rails.MaxPerDay <= 0 || strings.TrimSpace(rails.MaxPerDayJustification) == "" ||
		strings.TrimSpace(rails.Expiry) == "" {
		return fmt.Errorf("%w: complete non-negative rails are required", ErrInvalid)
	}
	return nil
}

func watchSpecPopulated(watch WatchSpec) bool {
	return watch.Cron != nil || watch.File != nil || watch.Graph != nil || watch.Poll != nil
}

// RecurringWatch is true for every watch that comes back around. Only a cron
// at-schedule — one named instant — happens exactly once. Expiry is decided
// from this and never from the words: "remind me every Sunday" and "remind me
// tomorrow at 9" are the same sentence shape and opposite lifetimes.
func RecurringWatch(watch WatchSpec) bool {
	if watch.Kind == WatchCron && watch.Cron != nil {
		return watch.Cron.Kind != CronAt
	}
	return true
}

func reminderTemplate(action string) string {
	action = strings.TrimSpace(action)
	for _, prefix := range []string{"Say this reminder: ", "Say: "} {
		if strings.HasPrefix(action, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(action, prefix))
		}
	}
	return action
}

// CadenceWatchSpec turns conversational cadence words into the engine's typed
// WatchSpec. This is the head's human-language mapping table with a structured
// output: cron cadences become CronSchedules, and file or graph watches whose
// structured parts cannot be derived deterministically degrade to a poll of
// the invariant itself rather than guessing at globs or predicates.
func CadenceWatchSpec(kind WatchKind, cadence, hint, condition string, now time.Time) WatchSpec {
	switch kind {
	case WatchFile:
		if glob := fileGlobHint(hint); glob != "" {
			return WatchSpec{Kind: WatchFile, File: &FileWatch{Glob: glob, Cadence: CadenceInterval(cadence)}}
		}
	case WatchGraph:
		if threshold, ok := spendThresholdHint(condition + " " + hint); ok {
			return WatchSpec{Kind: WatchGraph, Graph: &GraphWatch{
				Predicate: GraphSpendThreshold, ThresholdUSD: threshold, Cadence: CadenceInterval(cadence),
			}}
		}
	case WatchCron:
		schedule := CadenceSchedule(cadence, now)
		return WatchSpec{Kind: WatchCron, Cron: &schedule}
	}
	condition = strings.TrimSpace(condition)
	if condition == "" {
		condition = strings.TrimSpace(cadence)
	}
	return WatchSpec{Kind: WatchPoll, Poll: &PollWatch{Condition: condition, Cadence: CadenceInterval(cadence)}}
}

// CadenceSchedule maps cadence words onto one structured CronSchedule.
// Unknown words remain a short every-minutes interval instead of being
// treated as cron syntax.
//
// Every wall time it produces is local: "Sundays at 9" is nine in the morning
// in the zone this process runs in, and NextCronDue searches real instants in
// that zone. A named weekday wins over every other reading, because a person
// who said a day meant the day — "every sunday morning" is a weekly rule at
// nine, not a daily one.
func CadenceSchedule(cadence string, now time.Time) CronSchedule {
	schedule, _ := cadenceSchedule(cadence, now)
	return schedule
}

// RecognizedCadence is whether these words say anything about time that this
// engine can act on. It is how a caller tells a cadence apart from a default:
// words that fall through to the two-minute fallback are not a rhythm anyone
// chose, and a surface that presents them as one is lying quietly.
func RecognizedCadence(cadence string) bool {
	_, recognized := cadenceSchedule(cadence, time.Now())
	return recognized
}

// WeekdayNamed is whether these words name a day of the week. Callers deciding
// which watch family a sentence implies need it: a named day is a clock rule
// however the rest of the sentence reads.
func WeekdayNamed(text string) bool {
	_, named := cadenceWeekday(strings.ToLower(text))
	return named
}

func cadenceSchedule(cadence string, now time.Time) (CronSchedule, bool) {
	lower := strings.ToLower(strings.TrimSpace(cadence))
	hour, minute, stated := cadenceClock(lower)
	if weekday, named := cadenceWeekday(lower); named {
		if !stated {
			hour, minute = dayPartClock(lower)
		}
		return CronSchedule{Kind: CronWeekly, Weekday: weekday, Hour: hour, Minute: minute}, true
	}
	switch {
	case strings.Contains(lower, "weekday"):
		if !stated {
			hour, minute = dayPartClock(lower)
		}
		return CronSchedule{Kind: CronWeekdays, Hour: hour, Minute: minute}, true
	case strings.Contains(lower, "hourly") || strings.Contains(lower, "every hour"):
		return CronSchedule{Kind: CronEveryHours, Interval: 1}, true
	case strings.Contains(lower, "daily") || strings.Contains(lower, "every day") ||
		strings.Contains(lower, "morning") || strings.Contains(lower, "afternoon") ||
		strings.Contains(lower, "evening") || strings.Contains(lower, "night"):
		if !stated {
			hour, minute = dayPartClock(lower)
		}
		return CronSchedule{Kind: CronDaily, Hour: hour, Minute: minute}, true
	case strings.Contains(lower, "weekly") || strings.Contains(lower, "every week"):
		// A week with no day named still lands on a real day: today's, at a
		// stated or default hour. The old reading — an interval of 168 hours —
		// was measured from the ratification instant and drifted off any
		// weekday the user could have had in mind.
		if !stated {
			hour, minute = dayPartClock(lower)
		}
		return CronSchedule{Kind: CronWeekly, Weekday: now.Weekday(), Hour: hour, Minute: minute}, true
	case strings.Contains(lower, "tomorrow"):
		day := now.AddDate(0, 0, 1)
		if !stated {
			hour, minute = dayPartClock(lower)
		}
		at := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, now.Location())
		return CronSchedule{Kind: CronAt, At: at}, true
	case stated:
		// A clock and nothing else — "at 6pm", "remind me at 8". It means the
		// next time that hour comes round, which is today when today still has
		// it and tomorrow when it does not.
		at := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
		if !at.After(now) {
			at = at.AddDate(0, 0, 1)
		}
		return CronSchedule{Kind: CronAt, At: at}, true
	}
	if count, unit, ok := cadenceCount(lower); ok {
		if strings.HasPrefix(lower, "in ") {
			return CronSchedule{Kind: CronAt, At: now.Add(time.Duration(count) * unit)}, true
		}
		switch unit {
		case time.Minute:
			if count < 60 {
				return CronSchedule{Kind: CronEveryMinutes, Interval: count}, true
			}
		case time.Hour:
			if count < 24 {
				return CronSchedule{Kind: CronEveryHours, Interval: count}, true
			}
		case 24 * time.Hour:
			return CronSchedule{Kind: CronEveryHours, Interval: count * 24}, true
		case 7 * 24 * time.Hour:
			return CronSchedule{Kind: CronEveryHours, Interval: count * 7 * 24}, true
		}
	}
	return CronSchedule{Kind: CronEveryMinutes, Interval: 2}, false
}

// CadenceInterval is the polling-cadence reading of the same words, used by
// file, graph, and poll watches whose wake-up is an interval, not a schedule.
func CadenceInterval(cadence string) time.Duration {
	lower := strings.ToLower(strings.TrimSpace(cadence))
	if _, named := cadenceWeekday(lower); named {
		return 7 * 24 * time.Hour
	}
	switch {
	case strings.Contains(lower, "hourly") || strings.Contains(lower, "every hour"):
		return time.Hour
	case strings.Contains(lower, "daily") || strings.Contains(lower, "every day") ||
		strings.Contains(lower, "morning") || strings.Contains(lower, "afternoon") ||
		strings.Contains(lower, "evening"):
		return 24 * time.Hour
	case strings.Contains(lower, "weekly") || strings.Contains(lower, "every week"):
		return 7 * 24 * time.Hour
	case strings.Contains(lower, "weekday"):
		return 24 * time.Hour
	}
	if count, unit, ok := cadenceCount(lower); ok {
		return time.Duration(count) * unit
	}
	return 2 * time.Minute
}

// RetimeWatch applies new cadence words to an existing typed watch: cron
// watches get a freshly mapped schedule while file, graph, and poll watches
// keep their structure and change only how often they are examined.
func RetimeWatch(watch WatchSpec, cadence string, now time.Time) WatchSpec {
	cadence = strings.TrimSpace(cadence)
	retimed := watch
	retimed.Cadence = cadence
	// Retiming only ever happens because someone said when. Whatever was
	// guessed before, this rhythm is theirs.
	retimed.CadenceGuessed = false
	switch watch.Kind {
	case WatchCron:
		schedule := CadenceSchedule(cadence, now)
		retimed.Cron = &schedule
	case WatchFile:
		if watch.File != nil {
			file := *watch.File
			file.Cadence = CadenceInterval(cadence)
			retimed.File = &file
		}
	case WatchGraph:
		if watch.Graph != nil {
			graph := *watch.Graph
			graph.Cadence = CadenceInterval(cadence)
			retimed.Graph = &graph
		}
	case WatchPoll:
		if watch.Poll != nil {
			poll := *watch.Poll
			poll.Cadence = CadenceInterval(cadence)
			retimed.Poll = &poll
		}
	}
	return retimed
}

func cadenceCount(cadence string) (int, time.Duration, bool) {
	fields := strings.Fields(cadence)
	for index, field := range fields {
		if index+1 >= len(fields) {
			continue
		}
		count, err := strconv.Atoi(field)
		if err != nil || count <= 0 {
			continue
		}
		switch strings.Trim(fields[index+1], ".,;:") {
		case "minute", "minutes":
			return count, time.Minute, true
		case "hour", "hours":
			return count, time.Hour, true
		case "day", "days":
			return count, 24 * time.Hour, true
		case "week", "weeks":
			return count, 7 * 24 * time.Hour, true
		}
	}
	return 0, 0, false
}

var (
	// weekdayNames is the closed set of days, longest spellings first so
	// "sundays" and "sunday" both resolve to the same day.
	weekdayNames = []struct {
		name string
		day  time.Weekday
	}{
		{"sunday", time.Sunday}, {"monday", time.Monday}, {"tuesday", time.Tuesday},
		{"wednesday", time.Wednesday}, {"thursday", time.Thursday},
		{"friday", time.Friday}, {"saturday", time.Saturday},
	}
	// clockAtPattern reads a time introduced by "at": "at 8", "at 9:30",
	// "at 8 pm". The bare-number form needs the preposition, so "every 2 hours"
	// is never mistaken for two o'clock.
	clockAtPattern = regexp.MustCompile(`\bat\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\b`)
	// clockMeridiemPattern reads a time that names its half of the day —
	// "8pm", "7:15 am" — which needs no preposition to be unambiguous.
	clockMeridiemPattern = regexp.MustCompile(`\b(\d{1,2})(?::(\d{2}))?\s*(am|pm)\b`)
	// clockColonPattern reads a bare "19:30".
	clockColonPattern = regexp.MustCompile(`\b(\d{1,2}):(\d{2})\b`)
)

// cadenceWeekday finds a named day in cadence words. "every sunday",
// "on sundays", "change it to tuesday" all resolve; a day named inside another
// word does not.
func cadenceWeekday(cadence string) (time.Weekday, bool) {
	for _, candidate := range weekdayNames {
		index := strings.Index(cadence, candidate.name)
		if index < 0 {
			continue
		}
		before := index == 0 || !isWordByte(cadence[index-1])
		end := index + len(candidate.name)
		if end < len(cadence) && cadence[end] == 's' {
			end++
		}
		after := end >= len(cadence) || !isWordByte(cadence[end])
		if before && after {
			return candidate.day, true
		}
	}
	return 0, false
}

func isWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

// cadenceClock reads a stated wall time out of cadence words, returning false
// when the user never said one. It is the difference between "every sunday"
// (no time stated — a day part or the default answers) and "sundays at 8pm"
// (a time stated, which must survive into the schedule exactly).
func cadenceClock(cadence string) (hour, minute int, stated bool) {
	for _, pattern := range []*regexp.Regexp{clockMeridiemPattern, clockAtPattern, clockColonPattern} {
		match := pattern.FindStringSubmatch(cadence)
		if match == nil {
			continue
		}
		value, err := strconv.Atoi(match[1])
		if err != nil || value < 0 || value > 23 {
			continue
		}
		if len(match) > 2 && match[2] != "" {
			parsed, err := strconv.Atoi(match[2])
			if err != nil || parsed < 0 || parsed > 59 {
				continue
			}
			minute = parsed
		}
		meridiem := ""
		if len(match) > 3 {
			meridiem = match[3]
		}
		switch {
		case meridiem == "pm" && value < 12:
			value += 12
		case meridiem == "am" && value == 12:
			value = 0
		}
		if value > 23 {
			continue
		}
		return value, minute, true
	}
	return 0, 0, false
}

// dayPartClock is the hour a part of the day means when no clock was stated.
// Nine in the morning is the default for everything else, which is what a
// standing rule with no time in it has always meant here.
func dayPartClock(cadence string) (hour, minute int) {
	switch {
	case strings.Contains(cadence, "afternoon"):
		return 13, 0
	case strings.Contains(cadence, "evening"):
		return 18, 0
	case strings.Contains(cadence, "night"):
		return 21, 0
	default:
		return 9, 0
	}
}

// fileGlobHint accepts only hints that plausibly name filesystem paths; a
// bare word or cron sketch never becomes a glob.
func fileGlobHint(hint string) string {
	hint = strings.TrimSpace(hint)
	if hint == "" || strings.EqualFold(hint, "event") || strings.ContainsAny(hint, " \t\n") {
		return ""
	}
	if strings.ContainsAny(hint, "/*") || strings.Contains(hint, ".") {
		return hint
	}
	return ""
}

func spendThresholdHint(text string) (float64, bool) {
	dollar := strings.IndexByte(text, '$')
	if dollar < 0 || dollar+1 >= len(text) {
		return 0, false
	}
	rest := text[dollar+1:]
	end := 0
	for end < len(rest) && (rest[end] >= '0' && rest[end] <= '9' || rest[end] == '.') {
		end++
	}
	value, err := strconv.ParseFloat(rest[:end], 64)
	if err != nil || value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

// charterSpecFromCanonical projects the executable charter back into the
// head's ratification-card spelling for surfaces that read a spec.
func charterSpecFromCanonical(charter Charter) CharterSpec {
	rails := charter.guardrails
	spec := CharterSpecRails{
		EstimatedCostUSD:       rails.EstimatedCostUSD,
		MaxPerDay:              rails.MaxPerDay,
		MaxPerDayJustification: rails.MaxPerDayJustification,
		Expiry:                 rails.Expiry,
	}
	if spec.EstimatedCostUSD == 0 {
		spec.EstimatedCostUSD = rails.PerFiringBudgetUSD
	}
	if spec.MaxPerDay == 0 {
		spec.MaxPerDay = rails.MaxFiringsPerDay
	}
	if spec.Expiry == "" && rails.ExpiresAt != nil {
		spec.Expiry = rails.ExpiresAt.Local().Format("2006-01-02 15:04")
	}
	return CharterSpec{
		Invariant: charter.Invariant,
		Watch: CharterWatch{Kind: charter.Watch.Kind, Cadence: charter.Watch.Cadence,
			Schedule: charter.Watch.String(), Spec: charter.Watch},
		Sentinel: charter.SentinelHint,
		Action:   charter.Action.Template,
		SayOnly:  charter.Action.SayOnly,
		Rails:    spec,
	}
}

func charterSpecFromRecord(record charterRecord) CharterSpec {
	return charterSpecFromCanonical(Charter{
		Invariant: record.Invariant, Watch: record.Watch, SentinelHint: record.SentinelHint,
		Action: record.Action, guardrails: record.Rails,
	})
}
