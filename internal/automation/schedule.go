package automation

// schedule.go is the whole of "when": one moment, or a rhythm read in a named
// zone, and one function from a moment to the next moment after it.
//
// IT IS WRITTEN HERE AND NOT TAKEN FROM A LIBRARY. The dialect is small on
// purpose — a five-field numeric cron line, or a duration — because the person
// never has to type either: the model or the typed command writes the spec, and
// the card reads it back in words beside the exact line. A spec nobody types
// gains nothing from @yearly, seconds or month names; it gains a great deal
// from being readable in one sitting and right across a daylight-saving change,
// which is the one place the schedule this replaces was wrong.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MinInterval is the shortest rhythm an automation may have. A minute is the
// resolution of the clock itself; anything shorter would be a promise the clock
// cannot keep.
const MinInterval = time.Minute

// Schedule is when an automation wakes. Exactly one of At or Every is set.
type Schedule struct {
	// At is the one moment of a one-time automation.
	At time.Time `json:"at,omitempty"`
	// Every is a rhythm: a five-field cron line ("0 9 * * 1") or a duration of
	// at least [MinInterval] ("15m", "2h", "1d").
	Every string `json:"every,omitempty"`
	// Zone is the IANA zone a cron line's wall clock is read in. It is captured
	// when the automation is made, so a rhythm said in Toronto stays a Toronto
	// rhythm on a machine — or a remote host — whose own zone is different.
	// Empty is the machine's zone.
	Zone string `json:"zone,omitempty"`
	// Anchor is where a duration rhythm's grid starts. Every slot is
	// Anchor + k×interval, so a run that started late never moves the next one
	// later: the rhythm is the grid, not the gap since the last run.
	Anchor time.Time `json:"anchor,omitempty"`
}

// Validate refuses a schedule that could never wake, or could not be read.
func (s Schedule) Validate() error {
	every := strings.TrimSpace(s.Every)
	switch {
	case s.At.IsZero() && every == "":
		return errors.New("a schedule needs a moment or a rhythm")
	case !s.At.IsZero() && every != "":
		return errors.New("a schedule is a moment or a rhythm, not both")
	}
	if _, err := s.location(); err != nil {
		return err
	}
	if every == "" {
		return nil
	}
	rhythm, err := parseRhythm(every)
	if err != nil {
		return err
	}
	if rhythm.cron != nil {
		// A LINE THAT NEVER MATCHES IS REFUSED HERE, at the door, rather than
		// accepted and left "waiting for its time" forever. "0 0 30 2 *" is a
		// legal cron line and a schedule that never happens.
		loc, _ := s.location()
		if rhythm.cron.next(time.Now(), loc).IsZero() {
			return fmt.Errorf("the rhythm %q never happens", every)
		}
	}
	return nil
}

// Repeats reports whether this schedule is a rhythm rather than one moment.
func (s Schedule) Repeats() bool { return strings.TrimSpace(s.Every) != "" }

// First is when a new automation first wakes, made at now.
//
// A MOMENT IS ITS OWN MOMENT. A rhythm's first slot is the first one strictly
// after now, never now itself: "every Monday at 9" agreed to on a Monday at 10
// does not mean today, and "every 15m" means fifteen minutes from now.
func (s Schedule) First(now time.Time) (time.Time, error) {
	if err := s.Validate(); err != nil {
		return time.Time{}, err
	}
	if !s.Repeats() {
		return s.At, nil
	}
	return s.Next(now)
}

// Next is the first slot strictly after after. A one-time schedule has none.
func (s Schedule) Next(after time.Time) (time.Time, error) {
	if !s.Repeats() {
		return time.Time{}, nil
	}
	rhythm, err := parseRhythm(s.Every)
	if err != nil {
		return time.Time{}, err
	}
	if rhythm.cron != nil {
		loc, err := s.location()
		if err != nil {
			return time.Time{}, err
		}
		next := rhythm.cron.next(after, loc)
		if next.IsZero() {
			return time.Time{}, fmt.Errorf("the rhythm %q never happens", s.Every)
		}
		return next, nil
	}
	return gridNext(s.Anchor, rhythm.interval, after), nil
}

// Interval is a duration rhythm's length, and zero for a cron line or a moment.
// The card uses it to estimate how often a watch looks.
func (s Schedule) Interval() time.Duration {
	if !s.Repeats() {
		return 0
	}
	rhythm, err := parseRhythm(s.Every)
	if err != nil || rhythm.cron != nil {
		return 0
	}
	return rhythm.interval
}

// location is the zone the schedule is read in.
func (s Schedule) location() (*time.Location, error) {
	zone := strings.TrimSpace(s.Zone)
	if zone == "" || zone == "Local" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, fmt.Errorf("unknown time zone %q", zone)
	}
	return loc, nil
}

// gridNext is the first slot of anchor + k×interval strictly after after. An
// anchor in the future is itself the next slot once after is before it.
func gridNext(anchor time.Time, interval time.Duration, after time.Time) time.Time {
	if anchor.IsZero() {
		return after.Add(interval)
	}
	if after.Before(anchor) {
		return anchor
	}
	steps := after.Sub(anchor)/interval + 1
	return anchor.Add(steps * interval)
}

// rhythm is a parsed Every: a cron line or an interval, never both.
type rhythm struct {
	cron     *cronLine
	interval time.Duration
}

// parseRhythm tells the two dialects apart by a space, which a duration never
// has and a cron line always does.
func parseRhythm(every string) (rhythm, error) {
	spec := strings.TrimSpace(every)
	if spec == "" {
		return rhythm{}, errors.New("an empty rhythm")
	}
	if !strings.ContainsAny(spec, " \t") {
		interval, err := ParseInterval(spec)
		if err != nil {
			return rhythm{}, err
		}
		return rhythm{interval: interval}, nil
	}
	line, err := parseCron(spec)
	if err != nil {
		return rhythm{}, err
	}
	return rhythm{cron: &line}, nil
}

// ParseInterval reads a duration of at least [MinInterval]. It is Go's own
// syntax with one addition, "d" for a day, because "every 2d" is a thing people
// say and Go's parser refuses it.
func ParseInterval(text string) (time.Duration, error) {
	spec := strings.TrimSpace(text)
	if days, ok := strings.CutSuffix(spec, "d"); ok {
		count, err := strconv.Atoi(days)
		if err != nil || count < 1 {
			return 0, fmt.Errorf("cannot read the rhythm %q", text)
		}
		return time.Duration(count) * 24 * time.Hour, nil
	}
	interval, err := time.ParseDuration(spec)
	if err != nil {
		return 0, fmt.Errorf("cannot read the rhythm %q", text)
	}
	if interval < MinInterval {
		return 0, fmt.Errorf("the rhythm %q is under a minute", text)
	}
	return interval, nil
}

// ── the cron line ───────────────────────────────────────────────────────────

// cronLine is one parsed line: five sets of allowed numbers, each of which
// remembers whether it was a bare star, because the day-of-month and
// day-of-week fields mean different things when one of them is unrestricted.
type cronLine struct {
	minute, hour, dom, month, dow cronField
}

type cronField struct {
	star    bool
	allowed map[int]bool
}

func (f cronField) has(value int) bool { return f.star || f.allowed[value] }

// parseCron reads "minute hour day-of-month month day-of-week". Ranges, lists
// and steps are supported; names are not.
func parseCron(spec string) (cronLine, error) {
	parts := strings.Fields(spec)
	if len(parts) != 5 {
		return cronLine{}, fmt.Errorf("a rhythm needs five fields, or a duration: %q", spec)
	}
	var line cronLine
	var err error
	if line.minute, err = parseCronField(parts[0], 0, 59, "minute"); err != nil {
		return cronLine{}, err
	}
	if line.hour, err = parseCronField(parts[1], 0, 23, "hour"); err != nil {
		return cronLine{}, err
	}
	if line.dom, err = parseCronField(parts[2], 1, 31, "day of the month"); err != nil {
		return cronLine{}, err
	}
	if line.month, err = parseCronField(parts[3], 1, 12, "month"); err != nil {
		return cronLine{}, err
	}
	if line.dow, err = parseCronField(parts[4], 0, 7, "day of the week"); err != nil {
		return cronLine{}, err
	}
	// SUNDAY IS BOTH 0 AND 7, as every cron agrees; the rest of this file only
	// ever asks about 0.
	if line.dow.allowed[7] {
		line.dow.allowed[0] = true
		delete(line.dow.allowed, 7)
	}
	return line, nil
}

func parseCronField(text string, low, high int, name string) (cronField, error) {
	field := cronField{allowed: map[int]bool{}}
	if text == "*" {
		field.star = true
		return field, nil
	}
	for _, piece := range strings.Split(text, ",") {
		piece = strings.TrimSpace(piece)
		if piece == "" {
			return cronField{}, fmt.Errorf("an empty %s in the rhythm", name)
		}
		step := 1
		if slash := strings.IndexByte(piece, '/'); slash >= 0 {
			parsed, err := strconv.Atoi(piece[slash+1:])
			if err != nil || parsed < 1 {
				return cronField{}, fmt.Errorf("cannot read the %s step in %q", name, piece)
			}
			step = parsed
			piece = piece[:slash]
		}
		first, last := low, high
		switch {
		case piece == "*":
			// Already the whole range.
		case strings.ContainsRune(piece, '-'):
			ends := strings.SplitN(piece, "-", 2)
			start, startErr := strconv.Atoi(strings.TrimSpace(ends[0]))
			stop, stopErr := strconv.Atoi(strings.TrimSpace(ends[1]))
			if startErr != nil || stopErr != nil {
				return cronField{}, fmt.Errorf("cannot read the %s range %q", name, piece)
			}
			if start > stop {
				return cronField{}, fmt.Errorf("a backwards %s range %q", name, piece)
			}
			first, last = start, stop
		default:
			value, err := strconv.Atoi(piece)
			if err != nil {
				return cronField{}, fmt.Errorf("cannot read the %s %q", name, piece)
			}
			first = value
			// A bare number with a step means "from here on", which is what
			// "5/15" has meant since Vixie cron.
			if step == 1 {
				last = value
			}
		}
		if first < low || last > high {
			return cronField{}, fmt.Errorf("a %s outside %d-%d in %q", name, low, high, text)
		}
		for value := first; value <= last; value += step {
			field.allowed[value] = true
		}
	}
	if len(field.allowed) == 0 {
		return cronField{}, fmt.Errorf("nothing matches the %s in %q", name, text)
	}
	return field, nil
}

// cronHorizon bounds the search for a line that matches nothing at all —
// "0 0 30 2 *" is legal and never happens, and a search with no floor would
// spin forever on it. Four years covers every leap day.
const cronHorizon = 4 * 366 * 24 * 60

// next is the first minute strictly after after, read in loc, that the line
// matches.
//
// IT STEPS IN ABSOLUTE TIME AND READS THE WALL CLOCK OFF EACH CANDIDATE. The
// line this replaces built its first candidate with time.Date from the wall
// clock, and on an autumn night Go resolves an ambiguous wall time to the
// EARLIER of its two instants — so during the repeated hour its "next" was in
// the past, and an item due at 01:30 fired on every pass until 02:00. Here the
// first candidate is after's own instant plus a minute, so it can never be
// behind; time.Date is used only for the day and hour jumps, and [forward]
// refuses any jump that does not move.
//
// AND THE WALL CLOCK MAY NEVER GO BACKWARDS. A candidate whose wall time is not
// later than after's own is skipped, which is how a 01:30 line fires once on
// the night the clocks fall back rather than at 01:30 twice — the behaviour
// every cron daemon settles on. In spring the missing hour simply has no
// matching minutes, so a 02:30 line waits for the next day, as the line this
// replaces was already pinned to do.
func (c cronLine) next(after time.Time, loc *time.Location) time.Time {
	floor := wallOf(after.In(loc))
	t := after.Truncate(time.Minute).Add(time.Minute)
	for step := 0; step < cronHorizon; step++ {
		wall := t.In(loc)
		switch {
		case !c.matchesDate(wall):
			t = forward(t, time.Date(wall.Year(), wall.Month(), wall.Day()+1, 0, 0, 0, 0, loc))
		case !c.hour.has(wall.Hour()):
			t = forward(t, time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour()+1, 0, 0, 0, loc))
		case !c.minute.has(wall.Minute()):
			t = t.Add(time.Minute)
		case !wallOf(wall).after(floor):
			t = t.Add(time.Minute)
		default:
			return t
		}
	}
	return time.Time{}
}

// forward keeps the search honest: a jump built from wall-clock parts can land
// on or before where it started when a zone shifts underneath it, and a search
// that does not move is a search that does not end.
func forward(from, to time.Time) time.Time {
	if to.After(from) {
		return to
	}
	return from.Add(time.Minute)
}

// matchesDate applies the one rule of cron that surprises everybody: when BOTH
// the day of the month and the day of the week are restricted, a day matching
// either is a match. When only one is restricted, only that one is read.
func (c cronLine) matchesDate(moment time.Time) bool {
	if !c.month.has(int(moment.Month())) {
		return false
	}
	day, weekday := moment.Day(), int(moment.Weekday())
	switch {
	case c.dom.star && c.dow.star:
		return true
	case c.dom.star:
		return c.dow.has(weekday)
	case c.dow.star:
		return c.dom.has(day)
	default:
		return c.dom.has(day) || c.dow.has(weekday)
	}
}

// wall is a moment's wall clock to the minute, comparable as a tuple.
type wall struct{ year, month, day, hour, minute int }

func wallOf(t time.Time) wall {
	return wall{t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute()}
}

func (w wall) after(other wall) bool {
	a := [5]int{w.year, w.month, w.day, w.hour, w.minute}
	b := [5]int{other.year, other.month, other.day, other.hour, other.minute}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}
