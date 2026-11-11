package filememo

import "time"

// TimestampSettle covers coarse filesystem timestamp ticks before caching.
const TimestampSettle = 2 * time.Second

// TimestampsSettled rejects recent and future writes, including a writer that
// restored modification time while change time remains inside its coarse tick.
func TimestampsSettled(now, modified, changed time.Time, settle time.Duration) bool {
	return now.Sub(modified) >= settle && now.Sub(changed) >= settle
}
