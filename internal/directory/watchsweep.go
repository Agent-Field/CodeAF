package directory

import "time"

// sweepSlack is the soonest a sweep is set after now: a device that lapsed a
// moment ago is found within a second, and a burst of lapses is one wake and
// not one wake each.
const sweepSlack = time.Second

// alarm is the one pending sweep. gen tells a timer that fires after its alarm
// was replaced or cancelled that it is not the current one.
type alarm struct {
	at   int64 // unix ms the sweep is set for; 0 when none is set
	stop func() bool
	gen  uint64
}

func (a *alarm) armed() bool { return a.stop != nil }

func (a *alarm) cancel() {
	if a.stop != nil {
		a.stop()
	}
	a.stop, a.at = nil, 0
	a.gen++
}

// arm sets the sweep at the earliest moment a device that is held online goes
// silent, and never when nobody is watching: a device nobody looks at needs no
// push, and a later reader gets the right answer at read time (contract 21.12.4).
// An alarm that is already set at or before that moment stays. The caller holds p.mu.
func (p *presence) arm() {
	due, ok := p.earliestLapse()
	if len(p.watchers) == 0 || !ok {
		p.alarm.cancel()
		return
	}
	now := p.now()
	due = max(due, now+sweepSlack.Milliseconds())
	if p.alarm.armed() && p.alarm.at <= due {
		return
	}
	p.alarm.cancel()
	gen := p.alarm.gen
	p.alarm.at = due
	p.alarm.stop = p.after(time.Duration(due-now)*time.Millisecond, func() { p.sweep(gen) })
}

// lapseAt is the first millisecond device is no longer live: its newest sign of
// life among the sockets that were not told to go, plus that socket's window. A device in
// its debounce is the gap's to announce, and one whose sockets were all told to
// go was announced already; neither has a lapse. The caller holds p.mu.
func (p *presence) lapseAt(device string) (at int64, ok bool) {
	if p.waiting[device] != nil {
		return 0, false
	}
	for s := range p.socks[device] {
		if !s.lapsed.Load() {
			at, ok = max(at, s.alive.Load()+Window(s.beat).Milliseconds()+1), true
		}
	}
	return at, ok
}

// earliestLapse is the soonest lapseAt of any device. The caller holds p.mu.
func (p *presence) earliestLapse() (earliest int64, ok bool) {
	for d := range p.socks {
		if at, has := p.lapseAt(d); has && (!ok || at < earliest) {
			earliest, ok = at, true
		}
	}
	return earliest, ok
}

// sweep announces each device whose last live socket lapsed, closes those
// sockets and arms the next sweep. A timer of a replaced alarm does nothing.
func (p *presence) sweep(gen uint64) {
	p.mu.Lock()
	if p.alarm.gen != gen {
		p.mu.Unlock()
		return
	}
	p.alarm.cancel()
	stamps := p.expireLapsed(p.now())
	p.arm()
	hook := p.seen
	p.mu.Unlock()
	p.stamp(hook, stamps...)
}

// expireLapsed expires every device whose lapse is at or before now and answers
// the last_seen stamps owed. The caller holds p.mu.
func (p *presence) expireLapsed(now int64) (stamps []seenAt) {
	for d := range p.socks {
		if at, ok := p.lapseAt(d); ok && at <= now {
			stamps = append(stamps, p.expire(d, 0)...)
		}
	}
	return stamps
}
