package blobstore

import (
	"context"
	"io"
)

// TrafficWatch hears the store requests one caller makes while they run: when
// each begins and ends, and the bytes it moves as they move (up is request
// body read by the transport, down is answer read). A take shows how far it
// has got from these, and stops one whose bytes stopped arriving while a
// request waits; whole-request counts come too late for both on a slow link,
// where one frame can take a minute.
type TrafficWatch interface {
	Begin()
	Moved(up, down int64)
	End()
}

type trafficKey struct{}

// WithTraffic returns ctx whose store requests report to w. A store that is
// not on the wire, or a nil w, reports nothing.
func WithTraffic(ctx context.Context, w TrafficWatch) context.Context {
	if w == nil {
		return ctx
	}
	return context.WithValue(ctx, trafficKey{}, w)
}

func trafficOf(ctx context.Context) TrafficWatch {
	w, _ := ctx.Value(trafficKey{}).(TrafficWatch)
	return w
}

// trafficReader reports every read to w, as up or down bytes.
type trafficReader struct {
	r    io.Reader
	w    TrafficWatch
	down bool
}

func (r trafficReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		if r.down {
			r.w.Moved(0, int64(n))
		} else {
			r.w.Moved(int64(n), 0)
		}
	}
	return n, err
}
