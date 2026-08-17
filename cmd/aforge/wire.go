package main

import (
	"io"
	"os"

	"github.com/Agent-Field/aforge-v2/internal/wirelog"
)

// v3Wire is the door's half of the byte meter: the writer the v3 surface should
// paint through, and the close that finishes its log.
//
// Off — which is every launch that has not set AFORGE_WIRE_LOG — it answers a
// nil writer and a close that does nothing, and tui3.Run leaves the output
// option unset, so Bubble Tea paints into os.Stdout with nothing in between.
// There is no wrapper on the frame path, not even an empty one; the whole cost
// of the instrument when it is off is the LookupEnv below.
//
// The nil is returned as a plain untyped nil and never as a (*wirelog.Meter)(nil)
// dressed in an io.Writer, which is the one way this could go wrong: an
// interface holding a nil pointer is not nil, tui3 would hand it to Bubble Tea,
// and the first frame would paint into nothing.
func v3Wire() (io.Writer, func()) {
	meter := wirelog.FromEnv(os.Stdout)
	if meter == nil {
		return nil, func() {}
	}
	return meter, func() { _ = meter.Close() }
}
