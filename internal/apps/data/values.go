package data

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// stamp is how every datetime is stored: UTC with milliseconds, fixed width,
// so the stored text sorts in time order and compares with a bound string.
const stamp = "2006-01-02T15:04:05.000Z"

// dateLayout is how a date is stored.
const dateLayout = "2006-01-02"

// The bounds of one value.
const (
	textMax     = 10_000
	longtextMax = 1 << 20
	personMax   = 200
	listMax     = 1000
)

var (
	emailShape = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	phoneShape = regexp.MustCompile(`^[0-9+\-().\s/xX#*]+$`)
)

// coercion is what a value is read with: the clock for $now and today, and
// whether strings stand for numbers, bools and lists (a CSV file has nothing
// else).
type coercion struct {
	now     time.Time
	lenient bool
	// me is the person's id, which "me" and "$me" are stored as.
	me string
}

// coerce checks v against f and answers the value to store: a number for a
// number, a canonical enum value, a date in one spelling. nil (or an empty
// string) clears the field. A ref's target is checked by the writer, which
// can see the records.
func (f *Field) coerce(v any, c coercion) (any, error) {
	if v == nil {
		return nil, nil
	}
	if s, ok := v.(string); ok && strings.TrimSpace(s) == "" && f.Type != TypeBool {
		return nil, nil
	}
	switch f.Type {
	case TypeText, TypeLongtext:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("is text")
		}
		if !utf8.ValidString(s) {
			return nil, fmt.Errorf("is not valid text")
		}
		limit := textMax
		if f.Type == TypeLongtext {
			limit = longtextMax
		}
		if len(s) > limit {
			return nil, fmt.Errorf("is longer than %d bytes", limit)
		}
		return s, nil
	case TypeNumber, TypeMoney:
		n, err := number(v)
		if err != nil {
			return nil, err
		}
		if f.Type == TypeMoney {
			n = math.Round(n*100) / 100
		}
		return n, nil
	case TypeBool:
		switch x := v.(type) {
		case bool:
			return x, nil
		case string:
			switch strings.ToLower(strings.TrimSpace(x)) {
			case "true", "yes", "y", "1":
				return true, nil
			case "false", "no", "n", "0", "":
				return false, nil
			}
		}
		return nil, fmt.Errorf("is true or false")
	case TypeEnum:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("is one of %s", strings.Join(f.Values, ", "))
		}
		return f.enumValue(s)
	case TypeEnums:
		list, err := stringList(v, c.lenient)
		if err != nil {
			return nil, err
		}
		out := []any{}
		seen := map[string]bool{}
		for _, s := range list {
			canon, err := f.enumValue(s)
			if err != nil {
				return nil, err
			}
			if !seen[canon.(string)] {
				seen[canon.(string)] = true
				out = append(out, canon)
			}
		}
		if len(out) == 0 {
			return nil, nil
		}
		return out, nil
	case TypePerson:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("is a person")
		}
		s = strings.TrimSpace(s)
		if (s == "$me" || s == Me) && c.me != "" {
			s = c.me
		}
		if len(s) > personMax || strings.ContainsAny(s, "\r\n\x00") {
			return nil, fmt.Errorf("is not a person")
		}
		return s, nil
	case TypeRef:
		s, ok := v.(string)
		if !ok || !validID(strings.TrimSpace(s)) {
			return nil, fmt.Errorf("is the id of a %s record", f.To)
		}
		return strings.TrimSpace(s), nil
	case TypeRefs:
		list, err := stringList(v, c.lenient)
		if err != nil {
			return nil, err
		}
		out := []any{}
		seen := map[string]bool{}
		for _, s := range list {
			s = strings.TrimSpace(s)
			if !validID(s) {
				return nil, fmt.Errorf("holds ids of %s records", f.To)
			}
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil, nil
		}
		return out, nil
	case TypeDate:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("is a date like 2026-12-31")
		}
		at, err := readTime(s, c.now)
		if err != nil {
			return nil, fmt.Errorf("is a date like 2026-12-31")
		}
		return at.Format(dateLayout), nil
	case TypeDatetime:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("is a time like 2026-12-31T09:00:00Z")
		}
		at, err := readTime(s, c.now)
		if err != nil {
			return nil, fmt.Errorf("is a time like 2026-12-31T09:00:00Z")
		}
		return at.UTC().Format(stamp), nil
	case TypeEmail:
		s, ok := v.(string)
		s = strings.TrimSpace(s)
		if !ok || len(s) > 254 || !emailShape.MatchString(s) {
			return nil, fmt.Errorf("is an email address")
		}
		return s, nil
	case TypePhone:
		s, ok := v.(string)
		s = strings.TrimSpace(s)
		digits := 0
		for _, r := range s {
			if r >= '0' && r <= '9' {
				digits++
			}
		}
		if !ok || len(s) > 64 || digits < 3 || !phoneShape.MatchString(s) {
			return nil, fmt.Errorf("is a phone number")
		}
		return s, nil
	case TypeURL:
		s, ok := v.(string)
		s = strings.TrimSpace(s)
		if !ok || len(s) > 2048 || !webAddress(s) {
			return nil, fmt.Errorf("is a web address")
		}
		return s, nil
	}
	return nil, fmt.Errorf("has an unknown type %s", f.Type)
}

// webAddress accepts http and https addresses, and a bare host such as
// acme.com. Any other scheme is refused: a stored javascript: address would
// be a link the desk draws.
func webAddress(s string) bool {
	if strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	probe := s
	if !strings.Contains(s, "://") {
		probe = "https://" + s
	}
	u, err := url.Parse(probe)
	if err != nil || u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || strings.Contains(host, ".")
}

// enumValue is s as one of f's values, matched without regard to case.
func (f *Field) enumValue(s string) (any, error) {
	s = strings.TrimSpace(s)
	for _, v := range f.Values {
		if strings.EqualFold(v, s) {
			return v, nil
		}
	}
	return nil, fmt.Errorf("is one of %s", strings.Join(f.Values, ", "))
}

// number reads any number a JSON decoder or a Go caller can hand in, and a
// numeric string ("48,000" included, the way a spreadsheet writes it).
func number(v any) (float64, error) {
	var n float64
	switch x := v.(type) {
	case float64:
		n = x
	case float32:
		n = float64(x)
	case int:
		n = float64(x)
	case int64:
		n = float64(x)
	case int32:
		n = float64(x)
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return 0, fmt.Errorf("is a number")
		}
		n = f
	case string:
		clean := strings.NewReplacer(",", "", "$", "", " ", "", "_", "").Replace(strings.TrimSpace(x))
		f, err := strconv.ParseFloat(clean, 64)
		if err != nil {
			return 0, fmt.Errorf("is a number")
		}
		n = f
	default:
		return 0, fmt.Errorf("is a number")
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, fmt.Errorf("is a number")
	}
	return n, nil
}

// stringList reads a list of strings; leniently, "a, b" is two.
func stringList(v any, lenient bool) ([]string, error) {
	var out []string
	switch x := v.(type) {
	case []string:
		out = x
	case []any:
		for _, item := range x {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("is a list of words")
			}
			out = append(out, s)
		}
	case string:
		if !lenient {
			return nil, fmt.Errorf("is a list")
		}
		for _, part := range strings.Split(x, ",") {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
	default:
		return nil, fmt.Errorf("is a list")
	}
	if len(out) > listMax {
		return nil, fmt.Errorf("holds more than %d items", listMax)
	}
	return out, nil
}

// readTime reads a date or a time: $now, now and today, a date (midnight
// where the person is), or an RFC 3339 time.
func readTime(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "$now", "now":
		return now, nil
	case "today", "$today":
		y, m, d := now.Local().Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.Local), nil
	}
	if at, err := time.ParseInLocation(dateLayout, s, time.Local); err == nil {
		return at, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}

// coerceDefault is a field's default as a value, with $me and $now left for
// the write to resolve. It is checked once, when an app is registered.
func coerceDefault(f *Field, me string) (any, error) {
	return f.coerce(f.Default, coercion{now: time.Now(), me: me})
}
