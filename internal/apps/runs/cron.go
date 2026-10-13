package runs

import (
	"strconv"
	"strings"
	"time"
)

type cronSpec struct {
	min, hour, dom, mon, dow cronField
}

type cronField struct {
	any  bool
	vals map[int]bool
}

func parseCron(text string) (cronSpec, bool) {
	parts := strings.Fields(text)
	if len(parts) != 5 {
		return cronSpec{}, false
	}
	min, ok1 := parseField(parts[0], 0, 59)
	hour, ok2 := parseField(parts[1], 0, 23)
	dom, ok3 := parseField(parts[2], 1, 31)
	mon, ok4 := parseField(parts[3], 1, 12)
	dow, ok5 := parseField(parts[4], 0, 6)
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 {
		return cronSpec{}, false
	}
	return cronSpec{min: min, hour: hour, dom: dom, mon: mon, dow: dow}, true
}

func (c cronSpec) match(t time.Time) bool {
	return c.min.match(t.Minute()) && c.hour.match(t.Hour()) &&
		c.dom.match(t.Day()) && c.mon.match(int(t.Month())) &&
		c.dow.match(int(t.Weekday()))
}

func due(spec string, now, last time.Time) bool {
	parsed, ok := parseCron(spec)
	if !ok || !parsed.match(now) {
		return false
	}
	return last.Before(now.Truncate(time.Minute))
}

func parseField(text string, lo, hi int) (cronField, bool) {
	if text == "*" {
		return cronField{any: true}, true
	}
	field := cronField{vals: map[int]bool{}}
	for _, part := range strings.Split(text, ",") {
		if strings.HasPrefix(part, "*/") {
			n, err := strconv.Atoi(strings.TrimPrefix(part, "*/"))
			if err != nil || n <= 0 {
				return cronField{}, false
			}
			for v := lo; v <= hi; v += n {
				field.vals[v] = true
			}
			continue
		}
		if a, b, ok := strings.Cut(part, "-"); ok {
			from, err1 := strconv.Atoi(a)
			to, err2 := strconv.Atoi(b)
			if err1 != nil || err2 != nil || from > to {
				return cronField{}, false
			}
			for v := from; v <= to; v++ {
				field.vals[v] = true
			}
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return cronField{}, false
		}
		field.vals[n] = true
	}
	return field, true
}

func (f cronField) match(n int) bool {
	if f.any {
		return true
	}
	return f.vals[n]
}

func backoff(spec string, attempt int) time.Duration {
	d, ok := parseDur(spec)
	if !ok {
		d = time.Second
	}
	for i := 1; i < attempt; i++ {
		if d >= 10*time.Minute {
			return 10 * time.Minute
		}
		d *= 2
	}
	if d > 10*time.Minute {
		return 10 * time.Minute
	}
	return d
}
