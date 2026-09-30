package relaybill

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// Config is one run of the heavy-user shape.
type Config struct {
	Dir      string        // scratch folder for every machine's home, data and roots
	Binary   string        // the engine program
	Idle     time.Duration // how long the home screen stays open with the lease held
	Warm     int           // moves between two machines that already hold the chat
	Cold     int           // moves to a machine that has never seen it
	Gap      time.Duration // quiet between phases, so each is read from the analytics on its own minutes
	Tick     time.Duration // the home screen's own beat
	Schedule func() Schedule
	Log      func(format string, args ...any)
}

// Phase is one span of the run the analytics are read for on its own.
type Phase struct {
	Name    string         `json:"name"`
	Start   time.Time      `json:"start"`
	End     time.Time      `json:"end"`
	Seconds float64        `json:"seconds,omitempty"`
	Count   int            `json:"count,omitempty"`
	Client  map[string]int `json:"client_requests"` // what the client says it sent
}

// Manifest is what the analytics reader is given: the window of each phase.
type Manifest struct {
	URL    string    `json:"url"`
	Script string    `json:"script,omitempty"`
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Phases []Phase   `json:"phases"`
}

// run holds the state the phases share.
type run struct {
	cfg     Config
	ctx     context.Context
	counter *Counter
	id      identity.Identity
	first   *machine
	chat    cell.Cell
	holder  *machine // the machine that holds the chat now
	prior   *machine // the machine that held it before
	open    *openChat
	spare   int // machines made so far
	out     Manifest
}

// Run makes one identity, plays the shape through the real client against the
// relay named by CODEAF_SYNC_URL, and returns the window of each phase. The
// moves go first to prime two machines (unmeasured), then the idle hold, the
// warm moves and the cold moves, each in a window of its own.
func Run(ctx context.Context, cfg Config, url string, counter *Counter) (Manifest, error) {
	r := &run{cfg: cfg, ctx: ctx, counter: counter, out: Manifest{URL: url}}
	if err := r.setup(); err != nil {
		return r.out, err
	}
	steps := []func() (Phase, error){r.idle, r.warm, r.cold}
	r.out.Start = time.Now().UTC()
	for _, step := range steps {
		p, err := step()
		if err != nil {
			return r.out, err
		}
		r.out.Phases = append(r.out.Phases, p)
		r.quiet()
	}
	r.out.End = time.Now().UTC()
	return r.out, r.release()
}

// quiet waits out the gap between phases.
func (r *run) quiet() {
	r.cfg.Log("quiet for %v", r.cfg.Gap)
	select {
	case <-r.ctx.Done():
	case <-time.After(r.cfg.Gap):
	}
}

// setup makes the first machine, the project and the chat, and primes the
// chat onto a second machine and a third so that the warm moves between those
// two find their objects already there.
func (r *run) setup() error {
	first, err := newMachine(r.cfg.Dir, "m0", r.cfg.Binary, nil)
	if err != nil {
		return err
	}
	r.first, r.holder = first, first
	r.id = first.sync.Identity
	work, err := smallRepo(filepath.Join(r.cfg.Dir, "project"))
	if err != nil {
		return err
	}
	if r.chat, err = newChat(filepath.Join(r.cfg.Dir, "chats"), work); err != nil {
		return err
	}
	if r.open, err = first.open(r.ctx, r.chat, work); err != nil {
		return err
	}
	if err := r.open.say(r.ctx, "the first turn"); err != nil {
		return err
	}
	for range 2 {
		if _, err := r.move(r.freshMachine); err != nil {
			return err
		}
	}
	return nil
}

// freshMachine is a machine that has never seen the chat.
func (r *run) freshMachine() (*machine, error) {
	r.spare++
	return newMachine(r.cfg.Dir, fmt.Sprintf("m%d", r.spare), r.cfg.Binary, &r.id)
}

// move is one move of the chat: the person looks at the list on the machine
// they are going to, the chat's holder gives it up with its turn sent, and
// that machine takes it and opens it. to supplies the machine.
func (r *run) move(to func() (*machine, error)) (*machine, error) {
	dest, err := to()
	if err != nil {
		return nil, err
	}
	home := &Home{Src: dest.sync, Schedule: r.cfg.Schedule(), Tick: r.cfg.Tick}
	if err := home.Read(r.ctx, time.Now()); err != nil {
		return nil, fmt.Errorf("relaybill: list on %s: %w", dest.name, err)
	}
	if err := r.open.say(r.ctx, "a turn before the move"); err != nil {
		return nil, err
	}
	if err := r.open.close(r.ctx); err != nil {
		return nil, err
	}
	took, err := dest.continuer().Take(r.ctx, r.chat.ID)
	if err != nil {
		return nil, fmt.Errorf("relaybill: take on %s: %w", dest.name, err)
	}
	r.chat, r.prior, r.holder = took.Taken.Cell, r.holder, dest
	r.open, err = dest.open(r.ctx, r.chat, workOf(r.chat))
	return dest, err
}

// release gives the lease back at the end of the run.
func (r *run) release() error { return r.open.close(r.ctx) }

// phase runs body and records its window and what the client sent in it.
func (r *run) phase(name string, body func() error) (Phase, error) {
	before := r.counter.Snapshot()
	p := Phase{Name: name, Start: time.Now().UTC()}
	err := body()
	p.End = time.Now().UTC()
	p.Client = r.counter.Since(before)
	r.cfg.Log("phase %s: %v, %d client requests", name, p.End.Sub(p.Start).Round(time.Second), Total(p.Client))
	return p, err
}

// idle is the home screen open for cfg.Idle with the lease held and nothing
// being said: the screen's list reads and the drive side's beats.
func (r *run) idle() (Phase, error) {
	p, err := r.phase("idle_hold", func() error {
		ctx, stop := context.WithTimeout(r.ctx, r.cfg.Idle)
		defer stop()
		home := &Home{Src: r.holder.sync, Schedule: r.cfg.Schedule(), Tick: r.cfg.Tick}
		_ = home.Read(ctx, time.Now())
		home.Run(ctx)
		return nil
	})
	p.Seconds = p.End.Sub(p.Start).Seconds()
	return p, err
}

// warm is cfg.Warm moves, each back to the machine the chat just left: both
// were primed in setup, so each already holds the chat's objects.
func (r *run) warm() (Phase, error) {
	p, err := r.phase("warm_moves", func() error {
		return r.repeat(r.cfg.Warm, func() (*machine, error) { return r.prior, nil })
	})
	p.Count = r.cfg.Warm
	return p, err
}

// cold is cfg.Cold moves, each to a machine that has never seen the chat.
func (r *run) cold() (Phase, error) {
	p, err := r.phase("cold_moves", func() error { return r.repeat(r.cfg.Cold, r.freshMachine) })
	p.Count = r.cfg.Cold
	return p, err
}

func (r *run) repeat(n int, to func() (*machine, error)) error {
	for range n {
		if _, err := r.move(to); err != nil {
			return err
		}
	}
	return nil
}

// Write puts the manifest at path.
func (m Manifest) Write(path string) error {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}
