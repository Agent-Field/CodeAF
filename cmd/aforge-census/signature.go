package main

import (
	"regexp"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/callrows"
)

// ── ONE SIGNATURE PER KIND OF FAILURE ───────────────────────────────────────
//
// Four thousand failing rows in ten days are sixty kinds of failure, and the
// difference between the two numbers is entirely ids, addresses, durations and
// machine names. A census that grouped on the raw sentence would report four
// thousand singletons and say nothing; one that groups on a signature reports
// that one bound of this build's own cut nine hundred live streams.
//
// WHAT IS TAKEN OUT IS WHAT VARIES BETWEEN TWO INSTANCES OF THE SAME BUG, and
// what is left in is everything that names the bug: the status a router put in
// its own sentence, the shape of the complaint, and whether it arrived inside
// an opened stream.

var (
	// routerURL is OpenRouter's own endpoint, which appears in every transport
	// failure's `Post "…"` prefix and carries a model slug that varies per call.
	routerURL = regexp.MustCompile(`https?://[^\s"]+`)
	// address is an IP and port, or a host and port, as Go's net errors spell
	// them: `read tcp 192.168.1.9:52104->104.18.2.115:443`.
	address = regexp.MustCompile(`\[[0-9a-fA-F:]+\](?::\d+)?|\b\d{1,3}(?:\.\d{1,3}){3}(?::\d+)?(?:->\d{1,3}(?:\.\d{1,3}){3}(?::\d+)?)?`)
	// duration is how this build and its providers both spell an elapsed time:
	// `90s`, `2m30s`, `1.5s`, `150ms`.
	duration = regexp.MustCompile(`\b\d+(?:\.\d+)?(?:h\d+m)?(?:\d+(?:\.\d+)?m)?\d*(?:\.\d+)?(?:ms|µs|us|ns|s|m|h)\b`)
	// modelID is a slug with a vendor in front of it, which is how every model
	// this build names travels: `deepseek/deepseek-v4-flash-0731`.
	modelID = regexp.MustCompile(`\b[a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._:-]*`)
	// via names the machine that served, and it is the single biggest source of
	// spurious variety: one rate-limited pool writes one sentence per machine.
	via = regexp.MustCompile(`\(via [^):]+`)
	// number is everything left that is a bare figure — a token count, a
	// millisecond, an attempt.
	number = regexp.MustCompile(`\b\d[\d,]*\b`)
	// apiError is the router's own status, which is put back after the numbers
	// are stripped because it is the one figure that NAMES the failure.
	apiError = regexp.MustCompile(`API error \(<N>\)`)
	// spaces collapses what the substitutions leave behind.
	spaces = regexp.MustCompile(`\s+`)
)

// signature is the normalised shape of one failure, and "" for a row that did
// not fail.
//
// A FAILURE INSIDE AN OPENED 200 SAYS SO IN THE SIGNATURE ITSELF. It is the
// whole finding of the first census's second reading — the biggest error class
// in the file wears a success status — and a signature that dropped it would
// fold nine hundred guillotined live streams in with nine hundred requests that
// never opened.
func (r row) signature() string {
	said := strings.TrimSpace(r.Error)
	if said == "" {
		return ""
	}
	status := callrows.SaidStatus(strings.ToLower(said))
	normalised := routerURL.ReplaceAllString(said, "<URL>")
	normalised = address.ReplaceAllString(normalised, "<ADDR>")
	normalised = duration.ReplaceAllString(normalised, "<DUR>")
	normalised = modelID.ReplaceAllString(normalised, "<MODEL>")
	normalised = via.ReplaceAllString(normalised, "(via <LANE>")
	normalised = number.ReplaceAllString(normalised, "<N>")
	if status > 0 {
		// The router's status goes back in. It was taken out by the number
		// sweep above, and it is the one digit in the sentence that separates
		// "the pool is full" from "the account cannot reach this model".
		normalised = apiError.ReplaceAllString(normalised, "API error ("+itoa(status)+")")
	}
	normalised = strings.TrimSpace(spaces.ReplaceAllString(normalised, " "))
	if r.Status == 200 {
		return "IN-STREAM@200 " + normalised
	}
	return normalised
}

// itoa is strconv.Itoa under a shorter name, so the substitution above reads as
// one line. It is here rather than imported at the call site because this file
// wants no other numeric formatting.
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 4)
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
