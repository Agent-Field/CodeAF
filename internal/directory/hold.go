package directory

import (
	"net/url"
	"strconv"
	"strings"
)

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
