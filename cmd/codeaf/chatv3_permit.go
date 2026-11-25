package main

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// permitAskEvery is how often Home may ask the permit book again whether the
// one-time question is due. The answer changes rarely (when this computer first
// reads its roster, or when the person answers), so a slow read is enough, and
// it keeps the update loop from ever waiting on a file.
const permitAskEvery = 30 * time.Second

// permitAsker is the terminal's [tui3.PermitAsker]: the one question Home asks,
// once, of a person who paired computers before remote control existed, whether
// those computers may now also type into chats here.
//
// IT ANSWERS FROM MEMORY. Home asks on every build of its panel, which is the
// update loop, so [permitAsker.AskOnce] returns what the last background read
// found and starts the next read in the background when one is due. The read is
// the permit book and the device list, both off the loop.
type permitAsker struct {
	// who is this computer's account id, "" when it has none yet.
	who     func() string
	devices func(context.Context) ([]tui3.DeviceRow, error)
	book    func() *pair.PermitBook
	now     func() time.Time
	// spawn starts background work. It is guard.Go outside tests.
	spawn func(name string, fn func())

	mu         sync.Mutex
	identity   string
	names, ids []string
	ask        bool
	answered   bool
	refreshing bool
	checkedAt  time.Time
}

// newPermitAsker returns the asker over this computer's own door, or nil when
// there is no door. REMOTE CONTROL IS ASKED EACH TIME, never at start: a person
// turns it on in /settings in a window that is already open, and the one
// question about older pairings has to come with it, not at the next launch.
// With it off the asker says there is nothing to ask and does nothing else, so
// Home is exactly what it was before the setting existed.
func newPermitAsker(door *approvalsDoor) tui3.PermitAsker {
	if door == nil {
		return nil
	}
	return whenRemoteControlIsOn{asker: newDoorPermitAsker(door), on: config.RemoteControlOn}
}

// whenRemoteControlIsOn asks the question only while remote control is on. It
// is a wrapper and not a check inside the asker so that, with the setting off,
// no roster is read, no book is opened and no background work is started.
type whenRemoteControlIsOn struct {
	asker tui3.PermitAsker
	on    func() bool
}

func (w whenRemoteControlIsOn) AskOnce() ([]string, bool) {
	if !w.on() {
		return nil, false
	}
	return w.asker.AskOnce()
}

func (w whenRemoteControlIsOn) Answer(allow bool) error { return w.asker.Answer(allow) }

var _ tui3.PermitAsker = whenRemoteControlIsOn{}

func newDoorPermitAsker(door *approvalsDoor) *permitAsker {
	dir := door.dir
	return &permitAsker{
		who: func() string {
			id, err := identity.Load(dir)
			if err != nil {
				return ""
			}
			return id.ID()
		},
		devices: door.Devices, book: door.book, now: time.Now, spawn: guard.Go,
	}
}

// AskOnce says whether Home should ask now and about which computers.
func (p *permitAsker) AskOnce() ([]string, bool) {
	names, ask, due := p.snapshot()
	if due {
		p.spawn("chatv3/permit-ask", p.refresh)
	}
	return names, ask
}

// snapshot is what the asker holds now, and whether a fresh reading is due, in
// which case it also marks one as under way so that only one is started.
func (p *permitAsker) snapshot() (names []string, ask, due bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	due = !p.answered && !p.refreshing && (p.checkedAt.IsZero() || p.now().Sub(p.checkedAt) >= permitAskEvery)
	if due {
		p.refreshing = true
	}
	return slices.Clone(p.names), p.ask, due
}

// refresh reads the book and the roster once and keeps what they say.
func (p *permitAsker) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), approveStep)
	defer cancel()
	names, ids, ask, who := p.read(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.refreshing = false
	p.checkedAt = p.now()
	if p.answered {
		return
	}
	p.identity, p.names, p.ids, p.ask = who, names, ids, ask
}

// read says whether the one-time question is due and about whom. It is due when
// the book holds a legacy snapshot nobody has answered for and at least one of
// those computers is still on the roster.
func (p *permitAsker) read(ctx context.Context) (names, ids []string, ask bool, who string) {
	who = p.who()
	if who == "" {
		return nil, nil, false, ""
	}
	book := p.book()
	if !book.NeedsAsk(who) {
		return nil, nil, false, who
	}
	rows, err := p.devices(ctx)
	if err != nil {
		return nil, nil, false, who
	}
	for _, row := range rows {
		if row.Self || row.Revoked || row.ID == "" {
			continue
		}
		if _, class := book.For(who, row.ID); class != pair.ClassLegacy {
			continue
		}
		ids = append(ids, row.ID)
		names = append(names, row.Name)
	}
	return names, ids, len(ids) > 0, who
}

// Answer records the person's decision and ends the question: true lets the
// older computers type, false keeps them watching. The surface calls it on the
// update loop, so the memory changes at once and the book is written in the
// background; if that write fails the question comes back, because a decision
// that was not kept is a decision not made, and the safe state, watching only,
// is what stands until then.
func (p *permitAsker) Answer(allow bool) error {
	who, ids := p.settle()
	if who == "" {
		return nil
	}
	p.spawn("chatv3/permit-answer", func() {
		if err := p.book().Asked(who, allow, ids); err != nil {
			p.reopen()
		}
	})
	return nil
}

// settle ends the question in memory and answers who was asked and about which
// computers.
func (p *permitAsker) settle() (who string, ids []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	who, ids = p.identity, slices.Clone(p.ids)
	p.answered, p.ask, p.names, p.ids = true, false, nil, nil
	return who, ids
}

// reopen brings the question back, because the decision was not written down.
func (p *permitAsker) reopen() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.answered, p.checkedAt = false, time.Time{}
}

var _ tui3.PermitAsker = (*permitAsker)(nil)
