package standing

// every.go is the whole of the rhythm: how "every Monday at 9" and "every 20
// minutes" become one function from a moment to the next moment after it.
//
// IT IS WRITTEN HERE AND NOT TAKEN FROM A LIBRARY. The dialect is deliberately
// small — five numeric fields and Go's own duration syntax — because a person
// never types either of them: the model writes the spec, the card reads the
// person's words back, and a spec nobody types is a spec that gains nothing
// from supporting @yearly, seconds, or three-letter month names. What it does
// gain from is being readable in one sitting and being right across a daylight
// saving change.

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// ParseEvery reads a WhenEvery's Every: a five-field cron line, or a Go
// duration of at least one minute. It answers a function from "now" to the
// next moment.
//
// The two dialects are told apart by a space, which a duration never has and a
// cron line always does.
func ParseEvery(every string) (func(now time.Time) time.Time, error) {
	spec := strings.TrimSpace(every)
	if spec == "" {
		return nil, errors.New("standing: an empty rhythm")
	}
	if !strings.ContainsAny(spec, " \t") {
		duration, err := time.ParseDuration(spec)
		if err != nil {
			return nil, errors.New("standing: cannot read the rhythm " + every)
		}
		if duration < time.Minute {
			return nil, errors.New("standing: a rhythm under a minute")
		}
		return func(now time.Time) time.Time { return now.Add(duration) }, nil
	}
	schedule, err := parseCron(spec)
	if err != nil {
		return nil, err
	}
	return schedule.next, nil
}

// cronFields is one parsed line: five sets of allowed numbers, each of which
// remembers whether it was written as a bare star, because the day-of-month and
// day-of-week fields mean different things when one of them is unrestricted.
type cronFields struct {
	minute, hour, dom, month, dow cronField
}

type cronField struct {
	star    bool
	allowed map[int]bool
}

func (f cronField) has(value int) bool { return f.star || f.allowed[value] }

// parseCron reads "minute hour day-of-month month day-of-week". Ranges, lists
// and steps are all supported; names are not.
func parseCron(spec string) (cronFields, error) {
	parts := strings.Fields(spec)
	if len(parts) != 5 {
		return cronFields{}, errors.New("standing: a rhythm needs five fields, or a duration: " + spec)
	}
	var fields cronFields
	var err error
	if fields.minute, err = parseCronField(parts[0], 0, 59, "minute"); err != nil {
		return cronFields{}, err
	}
	if fields.hour, err = parseCronField(parts[1], 0, 23, "hour"); err != nil {
		return cronFields{}, err
	}
	if fields.dom, err = parseCronField(parts[2], 1, 31, "day of the month"); err != nil {
		return cronFields{}, err
	}
	if fields.month, err = parseCronField(parts[3], 1, 12, "month"); err != nil {
		return cronFields{}, err
	}
	if fields.dow, err = parseCronField(parts[4], 0, 7, "day of the week"); err != nil {
		return cronFields{}, err
	}
	// SUNDAY IS BOTH 0 AND 7, as every cron in the world agrees, and the rest
	// of this file only ever asks about 0.
	if fields.dow.allowed[7] {
		fields.dow.allowed[0] = true
		delete(fields.dow.allowed, 7)
	}
	return fields, nil
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
			return cronField{}, errors.New("standing: an empty " + name + " in the rhythm")
		}
		step := 1
		if slash := strings.IndexByte(piece, '/'); slash >= 0 {
			parsed, err := strconv.Atoi(piece[slash+1:])
			if err != nil || parsed < 1 {
				return cronField{}, errors.New("standing: cannot read the " + name + " step in " + piece)
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
				return cronField{}, errors.New("standing: cannot read the " + name + " range " + piece)
			}
			if start > stop {
				return cronField{}, errors.New("standing: a backwards " + name + " range " + piece)
			}
			first, last = start, stop
		default:
			value, err := strconv.Atoi(piece)
			if err != nil {
				return cronField{}, errors.New("standing: cannot read the " + name + " " + piece)
			}
			first = value
			// A bare number with a step means "from here on", which is what
			// "5/15" has meant since Vixie cron.
			if step == 1 {
				last = value
			}
		}
		if first < low || last > high {
			return cronField{}, errors.New("standing: a " + name + " outside " + strconv.Itoa(low) + "-" + strconv.Itoa(high) + " in " + text)
		}
		for value := first; value <= last; value += step {
			field.allowed[value] = true
		}
	}
	if len(field.allowed) == 0 {
		return cronField{}, errors.New("standing: nothing matches the " + name + " in " + text)
	}
	return field, nil
}

// cronHorizon is how far next will look before deciding the line matches
// nothing at all — "0 0 30 2 *" is a legal line that never happens, and a
// search with no floor would spin forever on it.
const cronHorizon = 4 * 366 * 24 * 60

// next answers the first minute strictly after now that the line matches.
//
// IT STEPS IN LOCAL WALL TIME, ON PURPOSE. Every jump is built with time.Date
// in the moment's own location, so a spring-forward morning skips the hour that
// does not exist rather than firing an hour early, and a fall-back evening does
// not fire twice: the wall clock is what the person meant when they said nine.
func (f cronFields) next(now time.Time) time.Time {
	location := now.Location()
	moment := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), 0, 0, location).Add(time.Minute)
	for step := 0; step < cronHorizon; step++ {
		switch {
		case !f.matchesDate(moment):
			moment = forward(moment, time.Date(moment.Year(), moment.Month(), moment.Day()+1, 0, 0, 0, 0, location))
		case !f.hour.has(moment.Hour()):
			moment = forward(moment, time.Date(moment.Year(), moment.Month(), moment.Day(), moment.Hour()+1, 0, 0, 0, location))
		case !f.minute.has(moment.Minute()):
			moment = moment.Add(time.Minute)
		default:
			return moment
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
// either one is a match. When only one is restricted, only that one is read.
func (f cronFields) matchesDate(moment time.Time) bool {
	if !f.month.has(int(moment.Month())) {
		return false
	}
	day, weekday := moment.Day(), int(moment.Weekday())
	switch {
	case f.dom.star && f.dow.star:
		return true
	case f.dom.star:
		return f.dow.has(weekday)
	case f.dow.star:
		return f.dom.has(day)
	default:
		return f.dom.has(day) || f.dow.has(weekday)
	}
}
