package blobstore

import "time"

// A request's deadline is a fixed allowance plus the time its body needs at the
// slowest link the store still supports. A fixed limit is wrong both ways: it
// kills a large frame on a slow link, and it waits far too long for a small
// request to a dead relay.
const (
	// BaseTimeout is the allowance every request gets, however small.
	BaseTimeout = 30 * time.Second
	// MinThroughput is the slowest sustained rate, in bytes per second, at
	// which a body is still expected to arrive.
	MinThroughput = 32 << 10
)

// Deadline is how long a request carrying n body bytes may take.
func Deadline(n int) time.Duration {
	return BaseTimeout + time.Duration(n)*time.Second/MinThroughput
}
