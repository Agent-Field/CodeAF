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
