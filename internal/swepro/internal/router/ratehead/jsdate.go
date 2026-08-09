package ratehead

// `new Date(string).getTime()` for the strings parseRetryAfter can actually
// hand it.
//
// Date-string parsing outside the ES5 ISO-8601 grammar is
// implementation-defined; swe-pro runs on bun, so the reference is JavaScript-
// Core's legacy parser (WTF/DateMath.cpp `parseDateFromNullTerminatedCharacters`,
// itself inherited from KJS). This file is a transcription of that algorithm,
// pinned against a bun 1.2.23 probe of ~150 header shapes.
//
// The ISO branch is deliberately ABSENT. parseRetryAfter only reaches
// `new Date` after Number.parseFloat has already returned a non-finite value,
// and every ISO-8601 spelling starts with a digit or a sign, so parseFloat
// consumes it first ("2015-10-21T07:28:00Z" becomes 2015 seconds). The only
// strings that get here start with a letter, or are the digit-led spellings
// whose parseFloat overflows ("1e400", "Infinity").
//
// Grammar covered (case-insensitive throughout):
//
//	[word …] day [sep] Month [sep] year [ HH:MM[:SS[.mmm]] ] [ zone ] [ (comment) ]
//	[word …] Month day [sep] year [ HH:MM[:SS[.mmm]] ] [ zone ] [ (comment) ]
//	Wkd Mon day HH:MM:SS year                              (asctime — no zone)
//
// Deliberate quirks kept from the reference implementation:
//   - Leading words are scanned only for a month name; a wrong or nonsense
//     weekday is ignored ("Mon, 21 Oct 2015 …" and "Foobar, 21 Oct 2015 …"
//     both parse).
//   - findMonth matches the first three characters against a packed table at
//     3-byte alignment: "Decem" is December, "De" is invalid, "October21" is
//     October.
//   - Hour 24 is legal but only at exactly 24:00:00.000; minute > 59 is fatal;
//     second > 59 is silently replaced by 0 and parsing continues.
//   - A number after the year that is NOT followed by ':' is consumed and
//     THROWN AWAY ("… 2015 07 GMT" is midnight), while a time that IS parsed
//     must be followed by a space, a comment or end-of-string
//     ("…07:28:00GMT" is Invalid Date even though "…2015GMT" is not).
//   - Two-digit years map 00-49 to 2000-2049 and 50-99 to 1950-1999; a
//     negative year is Invalid Date.
//   - Out-of-range day/month components roll over ("31 Feb 2015" is 3 Mar).
//   - Zone offsets split into HHMM only from three digits up, so "+24" is a
//     whole day and "+99" is 99 hours, but the raw number may not exceed four
//     digits ("+10000" is Invalid Date).
//
// KNOWN DIVERGENCES. Two bun corpora were run against this file: 148 realistic
// header shapes (0 mismatches) and 107 adversarial ones (8 mismatches, all
// listed here). Every one of the eight needs a Retry-After value no server
// emits, and each returns Invalid Date here where bun returns a date, so the
// header is ignored rather than mis-timed:
//
//	"Wed, 21 Oct 2015 07:28:. GMT"      empty seconds followed by a '.'
//	"Mar, 21 Oct 2015 GMT"              a MONTH name in the weekday slot
//	"Sun Nov 6 08:49:37"                asctime with the year omitted
//	"Wed, 21 Oct 2015 07:28:00 GMT,"    trailing ',' or '.' after the zone
//	"Oct/21/2015 GMT"                   month name in a slash-separated date
//	"Wed, 21 Oct 2015 07:28:00 gmt gmt" the zone spelled twice
//	"Infinity Oct 2015 GMT"             month + year with NO day; modern JSC
//	                                    defaults the day to 1, KJS (and this
//	                                    port) reject it as day > 31

import (
	"math"
	"strconv"
	"time"
)

// maxTimeMS is the ES time-value clip: |t| > 8.64e15 is Invalid Date.
const maxTimeMS = 8.64e15

// monthTable is WTF's packed month table; a hit only counts at 3-byte
// alignment, which is what rejects e.g. "rap" (inside "marapr").
const monthTable = "janfebmaraprmayjunjulaugsepoctnovdec"

func parseJSDateMS(s string) float64 {
	nan := math.NaN()

	pos := skipSpacesAndComments(s, 0)

	// Scan the leading non-digit words; the last one that looks like a month
	// wins. Assignments are unconditional, so a later non-month word RESETS a
	// month found earlier — that is the reference behaviour.
	month := -1
	wordStart := pos
	for pos < len(s) && !isASCIIDigit(s[pos]) {
		if isASCIISpace(s[pos]) || s[pos] == '(' {
			if pos-wordStart >= 3 {
				month = findMonth(s[wordStart:])
			}
			pos = skipSpacesAndComments(s, pos)
			wordStart = pos
			continue
		}
		pos++
	}
	// "January29": no delimiter between the month and the day.
	if month == -1 && wordStart != pos {
		month = findMonth(s[wordStart:])
	}

	pos = skipSpacesAndComments(s, pos)
	if pos >= len(s) {
		return nan
	}

	day, next, ok := parseUnsignedAt(s, pos)
	if !ok {
		return nan
	}
	pos = next
	if pos >= len(s) {
		return nan
	}
	if day < 1 {
		return nan
	}

	year := int64(0)
	switch {
	case day > 31:
		// "2015/10/21"
		if s[pos] != '/' {
			return nan
		}
		pos++
		if pos >= len(s) {
			return nan
		}
		year = day
		m, next, ok := parseUnsignedAt(s, pos)
		if !ok {
			return nan
		}
		month = int(m) - 1
		pos = next
		if pos >= len(s) || s[pos] != '/' {
			return nan
		}
		pos++
		if pos >= len(s) {
			return nan
		}
		d, next2, ok := parseUnsignedAt(s, pos)
		if !ok {
			return nan
		}
		day = d
		pos = next2
	case s[pos] == '/' && month == -1:
		// "10/21/2015"
		pos++
		month = int(day) - 1
		d, next, ok := parseUnsignedAt(s, pos)
		if !ok {
			return nan
		}
		day = d
		if day < 1 || day > 31 {
			return nan
		}
		pos = next
		if pos < len(s) && s[pos] == '/' {
			pos++
		}
		if pos >= len(s) {
			return nan
		}
	default:
		if s[pos] == '-' {
			pos++
		}
		pos = skipSpacesAndComments(s, pos)
		if pos < len(s) && s[pos] == ',' {
			pos++
		}
		if month == -1 {
			month = findMonth(s[pos:])
			if month == -1 {
				return nan
			}
			// Step over the month word, then over ONE delimiter if there is
			// one: "Oct 2015", "Oct-2015", "Oct/2015", "Oct,2015" and
			// "Oct2015" all reach the year.
			for pos < len(s) && isASCIILetter(s[pos]) {
				pos++
			}
			if pos < len(s) && (s[pos] == '-' || s[pos] == '/' || s[pos] == ',' || isASCIISpace(s[pos])) {
				pos++
			}
		}
	}
	if month < 0 || month > 11 {
		return nan
	}

	yearStart := pos
	afterYear := pos
	if year <= 0 && pos < len(s) {
		y, next, ok := parseUnsignedAt(s, pos)
		if !ok {
			return nan
		}
		year = y
		afterYear = next
	}
	if year < 0 {
		return nan
	}

	var hour, minute, second, millis int64
	pos = afterYear
	if pos < len(s) {
		if s[pos] == ':' {
			// There was no year: the number just consumed was the hour, and
			// the real year trails the time (asctime).
			year = -1
			pos = yearStart
		} else {
			if s[pos] == ',' {
				pos++
			}
			pos = skipSpacesAndComments(s, pos)
		}

		haveTime := false
		if pos < len(s) && isASCIIDigit(s[pos]) {
			h, next, _ := parseUnsignedAt(s, pos)
			pos = next
			if pos < len(s) && s[pos] == ':' {
				haveTime = true
				hour = h
				pos++
				if m, next, ok := parseUnsignedAt(s, pos); ok {
					minute = m
					pos = next
				}
				if pos < len(s) && s[pos] == ':' {
					pos++
					if sec, next, ok := parseUnsignedAt(s, pos); ok {
						second = sec
						pos = next
					}
					// A '.' only starts a fraction when a digit follows it;
					// "07:28:00." is Invalid Date.
					if pos+1 < len(s) && s[pos] == '.' && isASCIIDigit(s[pos+1]) {
						pos++
						start := pos
						for pos < len(s) && isASCIIDigit(s[pos]) {
							pos++
						}
						millis = fractionToMillis(s[start:pos])
					}
				}
			}
			// else: the number was not a time after all — discard it.
		}

		if haveTime {
			if pos < len(s) && !isASCIISpace(s[pos]) && s[pos] != '(' {
				return nan
			}
			if hour < 0 || hour > 24 {
				return nan
			}
			if hour == 24 && (minute != 0 || second != 0 || millis != 0) {
				return nan
			}
			if minute < 0 || minute > 59 {
				return nan
			}
			if second < 0 || second > 59 {
				second = 0
			}
		}
	}

	pos = skipSpacesAndComments(s, pos)

	offsetMinutes := int64(0)
	haveZone := false
	badZone := false
	// The zone is looked for twice, because asctime puts the year AFTER the
	// time and a zone may still trail it ("Sun Nov  6 08:49:37 1994 GMT").
	readZone := func() {
		if haveZone || pos >= len(s) {
			return
		}
		if s[pos] == '+' || s[pos] == '-' {
			off, next, ok := parseZoneOffset(s, pos)
			if !ok {
				badZone = true
				return
			}
			offsetMinutes, pos, haveZone = off, next, true
			return
		}
		mins, width, hit := matchZoneName(s, pos)
		if !hit {
			return
		}
		offsetMinutes, pos, haveZone = mins, pos+width, true
		// "GMT+05:30" / "UTC+0530"
		if pos < len(s) && (s[pos] == '+' || s[pos] == '-') {
			off, next, ok := parseZoneOffset(s, pos)
			if !ok {
				badZone = true
				return
			}
			offsetMinutes, pos = off, next
		}
	}

	readZone()
	if badZone {
		return nan
	}

	pos = skipSpacesAndComments(s, pos)
	if year == -1 {
		y, next, ok := parseUnsignedAt(s, pos)
		if !ok {
			return nan
		}
		year = y
		pos = skipSpacesAndComments(s, next)
		readZone()
		if badZone {
			return nan
		}
		pos = skipSpacesAndComments(s, pos)
	}
	if pos != len(s) {
		return nan
	}
	if year < 0 {
		return nan
	}
	if year < 100 {
		if year < 50 {
			year += 2000
		} else {
			year += 1900
		}
	}
	// Far outside the ±8.64e15 ms clip anyway; guards the day arithmetic.
	if year > 400000 {
		return nan
	}

	if !haveZone {
		// No zone spelled out: JS reads the fields as LOCAL wall time.
		// time.Date normalizes out-of-range day/hour the same way MakeDay and
		// MakeTime do.
		local := time.Date(int(year), time.Month(month+1), int(day),
			int(hour), int(minute), int(second), int(millis)*int(time.Millisecond),
			time.Local)
		return clipTime(float64(local.UnixMilli()))
	}

	days := makeDay(year, int64(month), day)
	ms := ((days*86400+hour*3600+minute*60+second)*1000 + millis) - offsetMinutes*60000
	return clipTime(float64(ms))
}

func clipTime(ms float64) float64 {
	if math.Abs(ms) > maxTimeMS {
		return math.NaN()
	}
	return ms
}

// makeDay is ES MakeDay for an already-0-based, already-in-range month: the
// day count from the epoch to the first of the month, plus (day - 1). Day
// values outside 1..31 roll over exactly as they do in JS.
func makeDay(year int64, month int64, day int64) int64 {
	return daysFromCivil(year, month+1, 1) + day - 1
}

// daysFromCivil is Howard Hinnant's proleptic-Gregorian day count, which
// matches the ES day-number arithmetic for every year in range.
func daysFromCivil(y int64, m int64, d int64) int64 {
	if m <= 2 {
		y--
	}
	var era int64
	if y >= 0 {
		era = y / 400
	} else {
		era = (y - 399) / 400
	}
	yoe := y - era*400
	var mp int64
	if m > 2 {
		mp = m - 3
	} else {
		mp = m + 9
	}
	doy := (153*mp+2)/5 + d - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}

// fractionToMillis reads the digits after '.' as a millisecond count: the
// first three digits, right-padded, and the rest dropped.
func fractionToMillis(digits string) int64 {
	buf := []byte("000")
	for i := 0; i < 3 && i < len(digits); i++ {
		buf[i] = digits[i]
	}
	v, _ := strconv.ParseInt(string(buf), 10, 64)
	return v
}

// isASCIISpace is WTF's isASCIISpace, which does NOT include U+00A0 or U+FEFF
// (jscompat.Trim already removed those before the string got here).
func isASCIISpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}

// skipSpacesAndComments skips ASCII spaces and parenthesised comments, which
// may nest. An unterminated '(' swallows the rest of the string.
func skipSpacesAndComments(s string, pos int) int {
	nesting := 0
	for ; pos < len(s); pos++ {
		c := s[pos]
		switch {
		case c == '(':
			nesting++
		case c == ')' && nesting > 0:
			nesting--
		case nesting == 0 && !isASCIISpace(c):
			return pos
		}
	}
	return pos
}

func findMonth(s string) int {
	if len(s) < 3 {
		return -1
	}
	needle := [3]byte{asciiLower(s[0]), asciiLower(s[1]), asciiLower(s[2])}
	for i := 0; i+3 <= len(monthTable); i += 3 {
		if monthTable[i] == needle[0] && monthTable[i+1] == needle[1] && monthTable[i+2] == needle[2] {
			return i / 3
		}
	}
	return -1
}

func asciiLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// parseUnsignedAt reads a date COMPONENT: leading ASCII whitespace then
// decimal digits, with no sign — "Wed 21 Oct +2015 GMT" is Invalid Date, so a
// sign is never part of a day/year/hour/minute/second. The whitespace skip is
// load-bearing: it is what lets "Oct 21, 2015 …" find the year after the
// comma, since the caller only consumes the comma itself.
func parseUnsignedAt(s string, pos int) (int64, int, bool) {
	i := pos
	for i < len(s) && isASCIISpace(s[i]) {
		i++
	}
	start := i
	var value int64
	saturated := false
	for i < len(s) && isASCIIDigit(s[i]) {
		if value > (math.MaxInt64-9)/10 {
			saturated = true
		} else {
			value = value*10 + int64(s[i]-'0')
		}
		i++
	}
	if i == start {
		return 0, pos, false
	}
	if saturated {
		value = math.MaxInt64
	}
	return value, i, true
}

// parseLongAt is strtol — the signed form, used only for a zone offset.
func parseLongAt(s string, pos int) (int64, int, bool) {
	i := pos
	for i < len(s) && isASCIISpace(s[i]) {
		i++
	}
	negative := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		negative = s[i] == '-'
		i++
	}
	start := i
	var value int64
	saturated := false
	for i < len(s) && isASCIIDigit(s[i]) {
		if value > (math.MaxInt64-9)/10 {
			saturated = true
		} else {
			value = value*10 + int64(s[i]-'0')
		}
		i++
	}
	if i == start {
		return 0, pos, false
	}
	if saturated {
		value = math.MaxInt64
	}
	if negative {
		value = -value
	}
	return value, i, true
}

// zoneNames is the WTF known-zone table, longest spellings first so that
// "UTC" is not truncated to "UT". Offsets are minutes east of UTC.
var zoneNames = []struct {
	name    string
	minutes int64
}{
	{"UTC", 0},
	{"GMT", 0},
	{"EST", -300},
	{"EDT", -240},
	{"CST", -360},
	{"CDT", -300},
	{"MST", -420},
	{"MDT", -360},
	{"PST", -480},
	{"PDT", -420},
	{"UT", 0},
	{"Z", 0},
}

func matchZoneName(s string, pos int) (int64, int, bool) {
	for _, zone := range zoneNames {
		if hasPrefixFold(s, pos, zone.name) {
			return zone.minutes, len(zone.name), true
		}
	}
	return 0, 0, false
}

func hasPrefixFold(s string, pos int, prefix string) bool {
	if pos+len(prefix) > len(s) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if asciiLower(s[pos+i]) != asciiLower(prefix[i]) {
			return false
		}
	}
	return true
}

// parseZoneOffset reads "+HH", "+HMM", "+HHMM" or "+HH:MM", returning minutes
// east of UTC. Fewer than three digits is an HOUR count ("+24" is a full day,
// "+99" is 99 hours); three or more splits into HHMM. Fields are not
// individually range-checked — "+9999" really is 99h99m — but the raw number
// may not exceed four digits.
func parseZoneOffset(s string, pos int) (int64, int, bool) {
	value, next, ok := parseLongAt(s, pos)
	if !ok {
		return 0, pos, false
	}
	pos = next
	if value < -9999 || value > 9999 {
		return 0, pos, false
	}
	sign := int64(1)
	if value < 0 {
		sign = -1
		value = -value
	}
	if pos < len(s) && s[pos] == ':' {
		// A bare "+05:" is five hours flat.
		minutes, next, ok := parseLongAt(s, pos+1)
		if !ok {
			minutes, next = 0, pos+1
		}
		return (value*60 + minutes) * sign, next, true
	}
	if value >= 100 {
		return (value/100*60 + value%100) * sign, pos, true
	}
	return value * 60 * sign, pos, true
}

func isASCIILetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
