package automation

// command.go is the typed door: `/automations add …` and its siblings, read
// exactly and with no model in between, and the same line written back out of
// an automation so the list can put it in the box to be edited.
//
// IT IS A GRAMMAR OF WORDS AND VALUES, AND A VALUE IS ONE TOKEN. Every clause is
// a word followed by one value — `every 15m`, `say "time to leave"` — and a
// value with a space in it is quoted. That is less forgiving than a sentence,
// which is the point: the conversation is where a sentence goes, and this is
// the road for somebody who wants exactly what they typed, read back on a card
// before anything is saved.
//
//	add "weekly update" every "0 9 * * 1" do "draft the weekly update"
//	add leave at 18:00 say "time to leave"
//	add "ci on main" every 15m look "gh run list -b main -L 1" until "the latest run failed" once say "CI on main is red"
//	edit 3f2a… every 30m
//	run 3f2a… · pause 3f2a… · resume 3f2a… · delete 3f2a…
//
// AND IT ROUND-TRIPS. [CommandLine] writes any automation as the `edit` line
// that would make it what it is, so the line a person edits is the line they
// would have had to type, and reading it back changes nothing.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Verb is what a typed line asks for.
type Verb string

const (
	// VerbList is the bare command: open the list.
	VerbList   Verb = ""
	VerbAdd    Verb = "add"
	VerbEdit   Verb = "edit"
	VerbRun    Verb = "run"
	VerbPause  Verb = "pause"
	VerbResume Verb = "resume"
	VerbDelete Verb = "delete"
)

// Command is one typed line, read but not yet applied.
type Command struct {
	Verb Verb
	// ID is the automation every verb but add acts on.
	ID string
	// Title is add's name for the new automation.
	Title   string
	clauses []clause
}

type clause struct {
	word  string
	value string
}

// The clause words, each taking one value unless it is a flag.
var (
	clauseValued = []string{"title", "at", "in", "every", "zone", "say", "do", "look", "files", "until", "time", "usd", "folder"}
	clauseFlags  = []string{"once", "worktree", "checkout"}
)

// ClauseWords is every word a typed line may use, in the order the help names
// them, for an error that has to say what would have been understood.
func ClauseWords() []string {
	return append(append([]string(nil), clauseValued...), clauseFlags...)
}

// ParseCommand reads what follows `/automations`. An empty line is the list.
func ParseCommand(line string) (Command, error) {
	tokens, err := tokenize(line)
	if err != nil {
		return Command{}, err
	}
	if len(tokens) == 0 {
		return Command{Verb: VerbList}, nil
	}
	verb := Verb(strings.ToLower(tokens[0]))
	rest := tokens[1:]
	cmd := Command{Verb: verb}
	switch verb {
	case VerbAdd:
		if len(rest) == 0 {
			return Command{}, errors.New(`add needs a title first: add "weekly update" every "0 9 * * 1" do "…"`)
		}
		cmd.Title, rest = strings.TrimSpace(rest[0]), rest[1:]
		if cmd.Title == "" {
			return Command{}, errors.New("add needs a title first")
		}
	case VerbEdit, VerbRun, VerbPause, VerbResume, VerbDelete:
		if len(rest) == 0 || strings.TrimSpace(rest[0]) == "" {
			return Command{}, fmt.Errorf("%s needs the automation's id — the list shows it", verb)
		}
		cmd.ID, rest = strings.TrimSpace(rest[0]), rest[1:]
		if verb != VerbEdit {
			if len(rest) > 0 {
				return Command{}, fmt.Errorf("%s takes only the id, and %q came after it", verb, rest[0])
			}
			return cmd, nil
		}
	default:
		return Command{}, fmt.Errorf("unknown %q — the forms are add, edit, run, pause, resume and delete", tokens[0])
	}
	for len(rest) > 0 {
		word := strings.ToLower(rest[0])
		rest = rest[1:]
		switch {
		case contains(clauseFlags, word):
			cmd.clauses = append(cmd.clauses, clause{word: word})
		case contains(clauseValued, word):
			if word == "title" && verb != VerbEdit {
				return Command{}, errors.New("the title comes straight after add")
			}
			if len(rest) == 0 {
				return Command{}, fmt.Errorf("%s needs a value after it", word)
			}
			cmd.clauses = append(cmd.clauses, clause{word: word, value: rest[0]})
			rest = rest[1:]
		default:
			return Command{}, fmt.Errorf("unknown word %q — the words are %s", word, strings.Join(ClauseWords(), ", "))
		}
	}
	if verb == VerbEdit && len(cmd.clauses) == 0 {
		return Command{}, errors.New("edit needs at least one thing to change")
	}
	return cmd, nil
}

// Apply makes the command's clauses true of base and validates the result. For
// add, base is the new automation's defaults — the window's workspace and zone;
// for edit, it is the automation as it stands, and what the line does not
// mention stays as it was.
//
// THE ZONE IS READ FIRST, wherever it was typed, because it is what `at 18:00`
// and a cron line are read in.
func (c Command) Apply(base Automation, now time.Time) (Automation, error) {
	a := base
	if c.Verb == VerbAdd {
		a.Title = c.Title
	}
	for _, cl := range c.clauses {
		if cl.word != "zone" {
			continue
		}
		zone := strings.TrimSpace(cl.value)
		if _, err := time.LoadLocation(zone); err != nil {
			return Automation{}, fmt.Errorf("unknown zone %q", zone)
		}
		a.Schedule.Zone = zone
	}
	loc, err := a.Schedule.location()
	if err != nil {
		return Automation{}, err
	}
	for _, cl := range c.clauses {
		value := strings.TrimSpace(cl.value)
		switch cl.word {
		case "zone":
		case "title":
			a.Title = value
		case "at":
			at, err := parseMoment(value, now, loc)
			if err != nil {
				return Automation{}, err
			}
			a.Schedule.At, a.Schedule.Every, a.Schedule.Anchor = at, "", time.Time{}
		case "in":
			distance, err := parseDistance(value)
			if err != nil {
				return Automation{}, err
			}
			a.Schedule.At, a.Schedule.Every, a.Schedule.Anchor = now.Add(distance).Truncate(time.Second), "", time.Time{}
		case "every":
			a.Schedule.At, a.Schedule.Every = time.Time{}, value
			a.Schedule.Anchor = time.Time{}
			if _, err := ParseInterval(value); err == nil {
				a.Schedule.Anchor = now.Truncate(time.Second)
			}
		case "say":
			a.Action = Action{Say: value}
		case "do":
			a.Action = Action{Do: value}
		case "look":
			a.Look = lookWith(a.Look, func(l *Look) { *l = Look{Command: value, Condition: l.Condition, Once: l.Once} })
		case "files":
			a.Look = lookWith(a.Look, func(l *Look) { *l = Look{Files: value, Condition: l.Condition, Once: l.Once} })
		case "until":
			a.Look = lookWith(a.Look, func(l *Look) { l.Condition = value })
		case "once":
			a.Look = lookWith(a.Look, func(l *Look) { l.Once = true })
		case "worktree":
			a.Worktree = true
		case "checkout":
			a.Worktree = false
		case "time":
			limit, err := parseDistance(value)
			if err != nil {
				return Automation{}, fmt.Errorf("cannot read the time limit %q", value)
			}
			a.Limits.Time = limit
		case "usd":
			usd, err := strconv.ParseFloat(strings.TrimPrefix(value, "$"), 64)
			if err != nil || usd <= 0 {
				return Automation{}, fmt.Errorf("cannot read the spending limit %q", value)
			}
			a.Limits.USD = usd
		case "folder":
			folder, err := expandFolder(value)
			if err != nil {
				return Automation{}, err
			}
			a.Workspace = folder
		}
	}
	if !a.Schedule.Repeats() && !a.Schedule.At.IsZero() && !a.Schedule.At.After(now) && c.touchesWhen() {
		return Automation{}, fmt.Errorf("%s has already passed", a.Schedule.At.In(loc).Format("Mon 2 Jan 15:04"))
	}
	if err := a.Validate(); err != nil {
		return Automation{}, err
	}
	return a, nil
}

// touchesWhen reports whether the line set the schedule, which is the only time
// a moment in the past is the person's mistake rather than the automation's
// history.
func (c Command) touchesWhen() bool {
	for _, cl := range c.clauses {
		switch cl.word {
		case "at", "in", "every", "zone":
			return true
		}
	}
	return c.Verb == VerbAdd
}

// CommandLine is the `edit` line that makes an automation exactly what it is —
// what the list puts in the box for `e`, without the leading `/automations`.
//
// A WATCH THAT LOOKS THROUGH A TOOL IS WRITTEN WITHOUT ITS LOOK. A tool call's
// arguments are a JSON object nobody types on one line, so the typed door leaves
// that look as it is; the conversation is where it is changed.
func CommandLine(a Automation) string {
	parts := []string{string(VerbEdit), a.ID, "title", quote(a.Title)}
	if zone := strings.TrimSpace(a.Schedule.Zone); zone != "" {
		parts = append(parts, "zone", quote(zone))
	}
	if every := strings.TrimSpace(a.Schedule.Every); every != "" {
		parts = append(parts, "every", quote(every))
	} else if !a.Schedule.At.IsZero() {
		loc, err := a.Schedule.location()
		if err != nil {
			loc = time.Local
		}
		parts = append(parts, "at", a.Schedule.At.In(loc).Format(momentLayout))
	}
	if look := a.Look; look != nil {
		switch {
		case strings.TrimSpace(look.Command) != "":
			parts = append(parts, "look", quote(look.Command))
		case strings.TrimSpace(look.Files) != "":
			parts = append(parts, "files", quote(look.Files))
		}
		parts = append(parts, "until", quote(look.Condition))
		if look.Once {
			parts = append(parts, "once")
		}
	}
	switch {
	case strings.TrimSpace(a.Action.Say) != "":
		parts = append(parts, "say", quote(a.Action.Say))
	case strings.TrimSpace(a.Action.Do) != "":
		parts = append(parts, "do", quote(a.Action.Do))
		if a.Worktree {
			parts = append(parts, "worktree")
		}
	}
	if a.Limits.Time > 0 {
		parts = append(parts, "time", durationWord(a.Limits.Time))
	}
	if a.Limits.USD > 0 {
		parts = append(parts, "usd", strconv.FormatFloat(a.Limits.USD, 'f', -1, 64))
	}
	if folder := strings.TrimSpace(a.Workspace); folder != "" {
		parts = append(parts, "folder", quote(folder))
	}
	return strings.Join(parts, " ")
}

// momentLayout is how a one-time moment is typed and written back: the wall
// clock in the schedule's zone, to the minute.
const momentLayout = "2006-01-02T15:04"

// parseMoment reads `18:00` (today), `tomorrow 18:00`, `2026-10-12T09:00`,
// `2026-10-12 09:00`, or a full RFC 3339 time with its own offset.
func parseMoment(text string, now time.Time, loc *time.Location) (time.Time, error) {
	text = strings.TrimSpace(text)
	if at, err := time.Parse(time.RFC3339, text); err == nil {
		return at, nil
	}
	for _, layout := range []string{momentLayout, "2006-01-02 15:04"} {
		if at, err := time.ParseInLocation(layout, text, loc); err == nil {
			return at, nil
		}
	}
	day := now.In(loc)
	clock := text
	if rest, ok := strings.CutPrefix(strings.ToLower(text), "tomorrow "); ok {
		day, clock = day.AddDate(0, 0, 1), strings.TrimSpace(rest)
	}
	if t, err := time.Parse("15:04", clock); err == nil {
		return time.Date(day.Year(), day.Month(), day.Day(), t.Hour(), t.Minute(), 0, 0, loc), nil
	}
	return time.Time{}, fmt.Errorf("cannot read the moment %q — say 18:00, \"tomorrow 18:00\" or 2026-10-12T09:00", text)
}

// parseDistance reads a positive duration, with "d" for a day.
func parseDistance(text string) (time.Duration, error) {
	spec := strings.TrimSpace(text)
	if days, ok := strings.CutSuffix(spec, "d"); ok {
		count, err := strconv.Atoi(days)
		if err != nil || count < 1 {
			return 0, fmt.Errorf("cannot read %q", text)
		}
		return time.Duration(count) * 24 * time.Hour, nil
	}
	distance, err := time.ParseDuration(spec)
	if err != nil || distance <= 0 {
		return 0, fmt.Errorf("cannot read %q", text)
	}
	return distance, nil
}

// durationWord writes a limit the way it would be typed: whole days, hours or
// minutes where it is one, and Go's own spelling otherwise.
func durationWord(d time.Duration) string {
	switch {
	case d%(24*time.Hour) == 0:
		return strconv.Itoa(int(d/(24*time.Hour))) + "d"
	case d%time.Hour == 0:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	case d%time.Minute == 0:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	}
	return d.String()
}

// expandFolder reads a folder as a person types one: absolute, or under `~`.
func expandFolder(text string) (string, error) {
	text = strings.TrimSpace(text)
	if rest, ok := strings.CutPrefix(text, "~"); ok {
		house, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		text = filepath.Join(house, rest)
	}
	if !filepath.IsAbs(text) {
		return "", fmt.Errorf("the folder %q has to be a full path, or start with ~", text)
	}
	return filepath.Clean(text), nil
}

// lookWith is the watch's look with one change made, creating it when the
// automation had none.
func lookWith(look *Look, change func(*Look)) *Look {
	next := Look{}
	if look != nil {
		next = *look
	}
	change(&next)
	return &next
}

// tokenize splits a line into words, keeping a double-quoted value whole. A
// backslash escapes a quote or a backslash inside one.
func tokenize(line string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	inQuote, have := false, false
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case inQuote && r == '\\' && i+1 < len(runes) && (runes[i+1] == '"' || runes[i+1] == '\\'):
			current.WriteRune(runes[i+1])
			i++
		case r == '"':
			inQuote, have = !inQuote, true
		case !inQuote && (r == ' ' || r == '\t' || r == '\n'):
			if have {
				tokens = append(tokens, current.String())
				current.Reset()
				have = false
			}
		default:
			current.WriteRune(r)
			have = true
		}
	}
	if inQuote {
		return nil, errors.New("a quote was opened and never closed")
	}
	if have {
		tokens = append(tokens, current.String())
	}
	return tokens, nil
}

// quote writes a value so [tokenize] reads it back as one token.
func quote(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\n\"\\") {
		return value
	}
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

func contains(list []string, word string) bool {
	for _, item := range list {
		if item == word {
			return true
		}
	}
	return false
}
