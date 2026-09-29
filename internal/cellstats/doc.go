// Package cellstats keeps the device-local cost counters of contract §11: one
// JSON line per sync flush, holding counts only. A line carries no time, no
// path, no title and no content, so the file is safe to show and safe to send
// when its owner chooses to. Nothing here ever leaves the device by itself.
package cellstats
