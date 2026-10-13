package settings

import (
	"strconv"
	"strings"
	"time"
)

type cronField struct {
	values []bool
	wild   bool
}

type cronSchedule struct {
	minute, hour, day, month, weekday cronField
}

func nextCron(text string, at time.Time) (time.Time, bool) {
	schedule, ok := parseCron(text)
	if !ok {
		return time.Time{}, false
	}
	after := at.UTC()
	day := time.Date(after.Year(), after.Month(), after.Day(), 0, 0, 0, 0, time.UTC)
	// Eight years covers leap-day schedules across a non-leap century.
	limit := day.AddDate(8, 0, 0)
	for ; day.Before(limit); day = day.AddDate(0, 0, 1) {
		if !schedule.matchesDay(day) {
			continue
		}
		if next, ok := schedule.nextOnDay(day, after); ok {
			return next, true
		}
	}
	return time.Time{}, false
}

func (s cronSchedule) nextOnDay(day, after time.Time) (time.Time, bool) {
	for hour, allowed := range s.hour.values {
		if !allowed {
			continue
		}
		for minute, allowed := range s.minute.values {
			if !allowed {
				continue
			}
			next := day.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
			if next.After(after) {
				return next, true
			}
		}
	}
	return time.Time{}, false
}

func (s cronSchedule) matchesDay(day time.Time) bool {
	if !s.month.values[int(day.Month())] {
		return false
	}
	dom, dow := s.day.values[day.Day()], s.weekday.values[int(day.Weekday())]
	if !s.day.wild && !s.weekday.wild {
		return dom || dow
	}
	return dom && dow
}

func parseCron(text string) (cronSchedule, bool) {
	parts := strings.Fields(text)
	if len(parts) != 5 {
		return cronSchedule{}, false
	}
	limits := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	fields := make([]cronField, len(parts))
	for i, part := range parts {
		field, ok := parseCronField(part, limits[i][0], limits[i][1])
		if !ok {
			return cronSchedule{}, false
		}
		fields[i] = field
	}
	fields[4].values[0] = fields[4].values[0] || fields[4].values[7]
	return cronSchedule{fields[0], fields[1], fields[2], fields[3], fields[4]}, true
}

func parseCronField(text string, low, high int) (cronField, bool) {
	field := cronField{values: make([]bool, high+1), wild: strings.HasPrefix(text, "*")}
	for _, item := range strings.Split(text, ",") {
		base, increment, hasStep := strings.Cut(item, "/")
		step := 1
		if hasStep {
			var err error
			step, err = strconv.Atoi(increment)
			if err != nil || step <= 0 || step > high-low+1 {
				return cronField{}, false
			}
		}
		start, end, ok := cronRange(base, low, high, hasStep)
		if !ok {
			return cronField{}, false
		}
		for value := start; value <= end; value += step {
			field.values[value] = true
		}
	}
	return field, true
}

func cronRange(text string, low, high int, hasStep bool) (int, int, bool) {
	if text == "*" {
		return low, high, true
	}
	left, right, isRange := strings.Cut(text, "-")
	start, err := strconv.Atoi(left)
	if err != nil || start < low || start > high {
		return 0, 0, false
	}
	end := start
	if isRange {
		end, err = strconv.Atoi(right)
	} else if hasStep {
		end = high
	}
	return start, end, err == nil && end >= start && end <= high
}
