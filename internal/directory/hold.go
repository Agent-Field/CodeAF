package directory

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The presence declaration of a watch socket (contract 21.12.3). A client says
// at what period it promises to ping, in whole seconds from 1 to MaxBeat, and the
// relay counts it online for WindowFactor beats after its last sign of life. A
// socket that declares nothing is a client from before presence, which pings at
// DefaultBeat. The hosted relay reads the same numbers as maxBeat, windowFactor
// and defaultBeat in limits.js.
const (
	MaxBeat      = 60
	WindowFactor = 2.5
	DefaultBeat  = 30 * time.Second
)

// Window is how long a socket that promised to ping every beat stays live after
// its last sign of life: enough for one lost ping and a late second one.
func Window(beat time.Duration) time.Duration {
	return time.Duration(float64(beat) * WindowFactor)
}

// ParseBeat reads the beat parameters of a watch URL: DefaultBeat when there are
// none, otherwise the one value named, which must be a plain decimal from 1 to
// MaxBeat. The same value twice is one value; two different values are malformed.
func ParseBeat(values []string) (time.Duration, error) {
	beat := DefaultBeat
	for i, v := range values {
		n, ok := parseBeat(v)
		if !ok || (i > 0 && n != beat) {
			return 0, errBadRequest
		}
		beat = n
	}
	return beat, nil
}

func parseBeat(v string) (time.Duration, bool) {
	n, err := strconv.Atoi(v)
	if err != nil || !plainDecimal(v) || n < 1 || n > MaxBeat {
		return 0, false
	}
	return time.Duration(n) * time.Second, true
}

// MaxHolds is how many holds one watch socket may name. A process that holds
// more cells than this names the first MaxHolds and keeps beating for the rest.
// The hosted relay reads the same number as maxHolds in limits.js.
const MaxHolds = 16

// MaxHoldCellBytes is the longest cell id a hold may name, and maxHoldFence the
// largest fence: 2^53-1, the most a JavaScript number keeps exactly, so both
// relays read the same hold the same way.
const (
	MaxHoldCellBytes = 64
	maxHoldFence     = 1<<53 - 1
)

// Hold is one lease a watch socket names in its URL: the cell and the fence of
// the lease its holder believes it has. The relay counts the socket as proof of
// life for that lease only while the fence is still the lease's current one.
type Hold struct {
	Cell  string
	Fence uint64
}

// HoldQuery is the query string (without the leading "?") that names holds, in
// the order given; it is empty when there are none.
func HoldQuery(holds []Hold) string {
	q := make([]string, len(holds))
	for i, h := range holds {
		q[i] = "hold=" + url.QueryEscape(h.Cell+":"+strconv.FormatUint(h.Fence, 10))
	}
	return strings.Join(q, "&")
}

// ParseHolds reads the repeated hold parameters of a watch URL. The last colon
// splits a value, so a cell id may itself contain colons. A value without a
// colon, with an empty id or with a fence that is not an unsigned decimal, or
// more than MaxHolds values, is malformed, and so is an id longer than
// MaxHoldCellBytes or a fence of more than 16 digits or above 2^53-1. The same
// hold named twice counts once; two fences for one cell are two holds.
func ParseHolds(values []string) ([]Hold, error) {
	if len(values) > MaxHolds {
		return nil, errBadRequest
	}
	holds := make([]Hold, 0, len(values))
	seen := map[Hold]bool{}
	for _, v := range values {
		h, ok := parseHold(v)
		if !ok {
			return nil, errBadRequest
		}
		if !seen[h] {
			seen[h] = true
			holds = append(holds, h)
		}
	}
	return holds, nil
}

func parseHold(v string) (Hold, bool) {
	i := strings.LastIndexByte(v, ':')
	if i <= 0 || i > MaxHoldCellBytes {
		return Hold{}, false
	}
	digits := v[i+1:]
	if !plainDecimal(digits) {
		return Hold{}, false
	}
	n, err := strconv.ParseUint(digits, 10, 64)
	if err != nil || n > maxHoldFence {
		return Hold{}, false
	}
	return Hold{Cell: v[:i], Fence: n}, true
}

// plainDecimal is s matching ^[0-9]{1,16}$: no sign, no space, no leading junk.
func plainDecimal(s string) bool {
	if len(s) == 0 || len(s) > 16 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// EventsQuery is the watch query parameter that opts a socket in to event
// frames (presence, joined, revoked). A socket without it hears versions only.
const EventsQuery = "events=1"

// watchQuery is the query of a watch URL: the holds, then the events flag.
func watchQuery(holds []Hold, events bool) string {
	q := HoldQuery(holds)
	switch {
	case !events:
		return q
	case q == "":
		return EventsQuery
	}
	return q + "&" + EventsQuery
}
