package chat

import "strconv"

// A second attempt is a fact about the work, and nothing on this surface said it.
//
// store.Node.Attempt has been on every row since the graph existed and no
// reader anywhere drew it. That is fine on the ordinary job, which is on its
// first go and for which "first attempt" is noise — and it is exactly wrong on
// the job a person is staring at wondering why it has been going so long. A
// long clock with no reason beside it reads as stuck; the same clock beside
// "2nd attempt" reads as retried, which is what it is.

// attemptWords spells an attempt count for a surface, and says nothing at all
// about a first attempt. Numerals rather than words because this is a receipt
// cell sitting beside a clock and a cost, and it has to hold the same width
// discipline they do.
func attemptWords(attempt uint64) string {
	if attempt <= 1 {
		return ""
	}
	return strconv.FormatUint(attempt, 10) + attemptOrdinal(attempt) + " attempt"
}

func attemptOrdinal(attempt uint64) string {
	if attempt%100 >= 11 && attempt%100 <= 13 {
		return "th"
	}
	switch attempt % 10 {
	case 1:
		return "st"
	case 2:
		return "nd"
	case 3:
		return "rd"
	default:
		return "th"
	}
}
