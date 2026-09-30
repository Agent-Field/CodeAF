package rotate

import (
	"context"
	"time"
)

// The rates a plan assumes, in bytes per second, for the wait it names. They
// are a home connection's, not a measurement: the plan says "about".
const (
	assumedDown = 4 << 20
	assumedUp   = 2 << 20
)

// Plan is what a rotation would do, told before anything changes.
type Plan struct {
	Chats     int           // chats the old identity lists
	Missing   int           // of those, chats whose objects are not all on this machine
	DownBytes int64         // what fetching the missing ones costs
	UpBytes   int64         // what publishing all of them costs
	Wait      time.Duration // an estimate of both
}

// Plan asks the old relay what it lists and this machine what it already holds.
// It changes nothing.
func (e Env) Plan(ctx context.Context) (Plan, error) {
	old := e.oldSide()
	l, err := old.Dir.List(ctx)
	if err != nil {
		return Plan{}, err
	}
	p := Plan{Chats: len(l.Cells)}
	for id, c := range l.Cells {
		p.UpBytes += int64(c.Size)
		want, err := e.Engine(e.Identity).Want(ctx, e.Cell(id), c.Head)
		if err != nil {
			return Plan{}, err
		}
		if len(want) > 0 {
			p.Missing++
			p.DownBytes += int64(c.Size)
		}
	}
	p.Wait = time.Duration(p.DownBytes/assumedDown+p.UpBytes/assumedUp) * time.Second
	return p, nil
}
