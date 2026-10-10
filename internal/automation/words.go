package automation

// words.go says a schedule the way a person would, and exactly. Every surface
// that shows an automation — the card, the list, the tool's own answers — reads
// these, so "every Monday at 09:00" is spelled one way everywhere.

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Describe is the schedule in a person's words: "every Monday at 09:00",
// "every 15 minutes", "today at 18:00". A cron line with no plain reading is
// said as itself.
func Describe(s Schedule, now time.Time) string {
	if !s.Repeats() {
		if s.At.IsZero() {
			return ""
		}
		return Moment(s.At, now)
	}
	rhythm, err := parseRhythm(s.Every)
	if err != nil {
		return strings.TrimSpace(s.Every)
	}
	if rhythm.cron == nil {
		return "every " + intervalWords(rhythm.interval)
	}
	return cronWords(*rhythm.cron, strings.TrimSpace(s.Every))
}

// Exact is the schedule exactly: the moment with its zone, or the rhythm's own
// spelling with the zone a cron line is read in.
func Exact(s Schedule) string {
	if !s.Repeats() {
		if s.At.IsZero() {
			return ""
		}
		return s.At.Format("2006-01-02 15:04 MST")
	}
	every := strings.TrimSpace(s.Every)
	if !strings.ContainsAny(every, " \t") {
		return "every " + every
	}
	zone := strings.TrimSpace(s.Zone)
	if zone == "" {
		zone = "this machine's zone"
	}
	return "cron " + every + " · " + zone
}

// Moment is one moment said from now: "today at 18:00", "tomorrow at 09:00",
// "Mon 13 Oct at 09:00", with the year only when it is not this one.
func Moment(t, now time.Time) string {
	t = t.In(now.Location())
	clock := t.Format("15:04")
	y1, m1, d1 := now.Date()
	y2, m2, d2 := t.Date()
	today := time.Date(y1, m1, d1, 0, 0, 0, 0, now.Location())
	day := time.Date(y2, m2, d2, 0, 0, 0, 0, now.Location())
	switch days := int(day.Sub(today).Round(time.Hour) / (24 * time.Hour)); {
	case days == 0:
		return "today at " + clock
	case days == 1:
		return "tomorrow at " + clock
	case days == -1:
		return "yesterday at " + clock
	case y1 == y2:
		return t.Format("Mon 2 Jan") + " at " + clock
	}
	return t.Format("Mon 2 Jan 2006") + " at " + clock
}

func intervalWords(d time.Duration) string {
	switch {
	case d%(24*time.Hour) == 0:
		return countWords(int(d/(24*time.Hour)), "day")
	case d%time.Hour == 0:
		return countWords(int(d/time.Hour), "hour")
	case d%time.Minute == 0:
		return countWords(int(d/time.Minute), "minute")
	}
	return d.String()
}

func countWords(n int, unit string) string {
	if n == 1 {
		return unit
	}
	return strconv.Itoa(n) + " " + unit + "s"
}

var weekdayNames = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

// cronWords reads the common shapes of a cron line as a sentence, and says the
// line itself for anything else.
func cronWords(line cronLine, spec string) string {
	minute, minuteOK := single(line.minute)
	hour, hourOK := single(line.hour)
	everyDay := line.dom.star && line.month.star && line.dow.star
	switch {
	case minuteOK && hourOK && everyDay:
		return fmt.Sprintf("every day at %02d:%02d", hour, minute)
	case minuteOK && hourOK && line.dom.star && line.month.star:
		if days := dayWords(line.dow); days != "" {
			return fmt.Sprintf("%s at %02d:%02d", days, hour, minute)
		}
	case minuteOK && hourOK && line.dow.star && line.month.star:
		if day, ok := single(line.dom); ok {
			return fmt.Sprintf("on the %s of every month at %02d:%02d", ordinal(day), hour, minute)
		}
	case minuteOK && line.hour.star && everyDay:
		return fmt.Sprintf("every hour at :%02d", minute)
	}
	if step, ok := stepOf(line.minute, 60); ok && line.hour.star && everyDay {
		return "every " + countWords(step, "minute")
	}
	return "on the cron line " + spec
}

// dayWords says a day-of-week field: "every Monday", "every weekday",
// "every Monday and Thursday".
func dayWords(field cronField) string {
	if field.star {
		return ""
	}
	var days []int
	for day := 0; day <= 6; day++ {
		if field.allowed[day] {
			days = append(days, day)
		}
	}
	if len(days) == 5 && days[0] == 1 && days[4] == 5 {
		return "every weekday"
	}
	if len(days) == 2 && days[0] == 0 && days[1] == 6 {
		return "every weekend day"
	}
	names := make([]string, len(days))
	for i, day := range days {
		names[i] = weekdayNames[day]
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return "every " + names[0]
	}
	return "every " + strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

func single(field cronField) (int, bool) {
	if field.star || len(field.allowed) != 1 {
		return 0, false
	}
	for value := range field.allowed {
		return value, true
	}
	return 0, false
}

// stepOf reports a field that is exactly 0, n, 2n … within its range.
func stepOf(field cronField, span int) (int, bool) {
	if field.star || len(field.allowed) < 2 || !field.allowed[0] {
		return 0, false
	}
	step := 0
	for value := 1; value < span; value++ {
		if field.allowed[value] {
			step = value
			break
		}
	}
	if step == 0 {
		return 0, false
	}
	for value := 0; value < span; value++ {
		if field.allowed[value] != (value%step == 0) {
			return 0, false
		}
	}
	return step, true
}

func ordinal(n int) string {
	suffix := "th"
	switch {
	case n%100 >= 11 && n%100 <= 13:
	case n%10 == 1:
		suffix = "st"
	case n%10 == 2:
		suffix = "nd"
	case n%10 == 3:
		suffix = "rd"
	}
	return strconv.Itoa(n) + suffix
}
