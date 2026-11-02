package taskdoor

import (
	"context"
	"crypto/ecdh"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/relay"
)

type Config struct {
	Service  string
	Identity identity.Identity
	Device   identity.Dev
	Roster   func(context.Context) (map[string]bool, error)
	Root     string
	Executor Executor
	Say      func(string)
	Clock    Clock
	// Home is the profile whose chosen computer name admission uses. Empty
	// uses the configured codeaf home, independently of the attempt store root.
	Home string
}

// Serve holds this device's task door beside the separate machine door. Runs
// are children of Serve's lifetime, while watches are children of a connection;
// losing one watch therefore cannot stop the work it was following.
func Serve(ctx context.Context, config Config) error {
	if config.Service == "" || config.Roster == nil || config.Executor == nil {
		return errors.New("taskdoor: a door needs a relay, roster and executor")
	}
	if config.Device.Cert.Verify(config.Identity.PublicKey()) != nil {
		return errors.New("taskdoor: this device does not belong to the identity")
	}
	if config.Root == "" {
		return errors.New("taskdoor: an attempt store folder is required")
	}
	root, err := filepath.Abs(config.Root)
	if err != nil {
		return err
	}
	config.Root = root
	if err := os.MkdirAll(config.Root, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(config.Root, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, true); err != nil {
		return errors.New("this computer is already taking tasks")
	}
	defer filelock.Unlock(lock)
	clock := clockOrWall(config.Clock)
	store, err := openAttempts(config.Root, clock)
	if err != nil {
		return err
	}
	key, err := DoorKey(config.Identity, config.Device.ID())
	if err != nil {
		return err
	}
	if config.Home == "" {
		config.Home = home.Dir()
	}
	server := &door{config: config, store: store, clock: clock, name: devname.Name(config.Home), roster: &rosterCache{fetch: config.Roster, clock: clock}}
	runCtx, cancel := context.WithCancel(ctx)
	defer server.workers.Wait()
	// Defer order matters: workers must see cancellation before Serve waits,
	// including when the registration fails for a reason other than shutdown.
	defer cancel()
	for _, a := range store.running() {
		server.execute(runCtx, a, true)
	}
	announced := false
	return (&registrationLoop{
		Service: config.Service, Key: key, Say: config.Say, Now: clock.Now,
		Wait: func(ctx context.Context, d time.Duration) error {
			ticker := clock.NewTicker(d)
			defer ticker.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C():
				return nil
			}
		},
		Ready: func(*relay.Registration) {
			if !announced {
				announced = true
				server.say(TakingTasksLine)
			}
		},
		Hold: func(ctx context.Context, r *relay.Registration) { server.hold(runCtx, r, key) },
	}).Run(ctx)
}

type door struct {
	config  Config
	store   *attemptStore
	roster  *rosterCache
	clock   Clock
	name    string
	workers sync.WaitGroup
}

func (d *door) say(line string) {
	if d.config.Say != nil {
		d.config.Say(line)
	}
}

func (d *door) hold(ctx context.Context, registration *relay.Registration, key *ecdh.PrivateKey) {
	stop := context.AfterFunc(ctx, func() { _ = registration.Close() })
	defer stop()
	var connections sync.WaitGroup
	defer connections.Wait()
	defer registration.Close()
	for {
		stream, err := registration.Accept()
		if err != nil {
			return
		}
		connections.Add(1)
		go func() { defer connections.Done(); d.answer(ctx, stream, key) }()
	}
}
func (d *door) answer(ctx context.Context, stream io.ReadWriteCloser, key *ecdh.PrivateKey) {
	defer stream.Close()
	setDeadline(stream, time.Now().Add(pair.HandshakeWithin))
	handshakeCtx, endHandshake := context.WithTimeout(ctx, pair.HandshakeWithin)
	defer endHandshake()
	stop := context.AfterFunc(ctx, func() { _ = stream.Close() })
	defer stop()
	tunnel, err := pair.AcceptWithKey(stream, key)
	if err != nil {
		return
	}
	device, err := acceptHello(handshakeCtx, tunnel, d.config.Identity, d.config.Device, d.roster, d.name)
	if err != nil {
		return
	}
	endHandshake()
	setDeadline(stream, time.Time{})
	conn := newConn(ctx, tunnel, d.clock)
	var watch *doorWatch
	// The connection closes before waiting for watches, so a blocked event
	// write and a waiting replay are both released on every return path.
	defer func() { _ = conn.Close(); watch.close() }()
	for {
		frame, err := conn.Receive()
		if err != nil {
			return
		}
		if !d.allowConnection(ctx, conn, device, frame.Attempt) {
			return
		}
		reply := func(payload any) error { return conn.Send(frame.Attempt, payload) }
		switch frame.Type {
		case "start":
			var request Start
			if err := frame.Decode(&request); err != nil {
				_ = reply(Refused{Reason: "that task request is not readable"})
				continue
			}
			a, fresh, err := d.store.admit(frame.Attempt, request, Busy(d.name))
			if err != nil {
				if reply(Refused{Reason: err.Error()}) != nil {
					return
				}
				continue
			}
			// Run is launched after the durable admission even if the origin drops
			// before it can hear started. Retrying then finds the same attempt.
			if fresh {
				d.execute(ctx, a, false)
			}
			if reply(Started{State: a.State()}) != nil {
				return
			}
		case "watch":
			var request Watch
			if frame.Decode(&request) != nil {
				return
			}
			a := d.store.get(frame.Attempt)
			if a == nil {
				if reply(Unknown{}) != nil {
					return
				}
				continue
			}
			watch.close()
			conn.startWatch()
			watch = d.startWatch(ctx, conn, a, request.From, device)
		case "stop":
			if d.store.get(frame.Attempt) == nil {
				if reply(Unknown{}) != nil {
					return
				}
				continue
			}
			if err := d.config.Executor.Stop(frame.Attempt); err != nil {
				if reply(Refused{Reason: err.Error()}) != nil {
					return
				}
				continue
			}
			if reply(Stopping{}) != nil {
				return
			}
		case "ack":
			var ack Ack
			if frame.Decode(&ack) != nil || (ack.Disposition != "landed" && ack.Disposition != "abandoned") {
				if reply(Refused{Reason: "choose landed or abandoned for this task"}) != nil {
					return
				}
				continue
			}
			if a := d.store.get(frame.Attempt); a != nil && a.State().Phase == "running" {
				if err := d.config.Executor.Stop(frame.Attempt); err != nil {
					if reply(Refused{Reason: err.Error()}) != nil {
						return
					}
					continue
				}
			}
			watch.close()
			watch = nil
			conn.watching.Store(false)
			if err := d.store.forget(frame.Attempt); err != nil {
				if reply(Refused{Reason: err.Error()}) != nil {
					return
				}
				continue
			}
			if reply(Acked{}) != nil {
				return
			}
		default:
			return
		}
	}
}
func (d *door) execute(ctx context.Context, a *Attempt, adopt bool) {
	d.workers.Add(1)
	go func() {
		defer d.workers.Done()
		defer func() {
			if ctx.Err() == nil {
				d.store.released(a)
			}
		}()
		journal := attemptJournal{a: a}
		var err error
		if adopt {
			err = d.config.Executor.Adopt(ctx, a, journal)
		} else {
			err = d.config.Executor.Run(ctx, a, journal)
		}
		if ctx.Err() != nil {
			return
		}
		if a.State().Phase != "running" {
			return
		}
		report := "the task ended without a result"
		if err != nil {
			report = err.Error()
		}
		outcome := "incomplete"
		if adopt {
			outcome = "interrupted"
		}
		if err := journal.Append(Finished{Outcome: outcome, Report: report}); err != nil && !errors.Is(err, os.ErrNotExist) {
			d.say("could not save the task result: " + err.Error())
		}
	}()
}
func (d *door) watch(ctx context.Context, conn *Conn, a *Attempt, from uint64, device string) {
	check := d.clock.NewTicker(time.Second)
	defer check.Stop()
	for {
		if !d.allowConnection(ctx, conn, device, a.ID) {
			return
		}
		frames, changed, finished, err := a.after(from)
		if err != nil {
			_ = conn.Send(a.ID, Unknown{})
			return
		}
		for _, frame := range frames {
			if !d.allowConnection(ctx, conn, device, a.ID) {
				return
			}
			select {
			case <-ctx.Done():
				return
			default:
			}
			if err := conn.sendFrame(frame); err != nil {
				return
			}
			from = frame.Seq
		}
		if finished {
			conn.watching.Store(false)
			return
		}
	waiting:
		for {
			select {
			case <-ctx.Done():
				return
			case <-conn.done:
				return
			case <-changed:
				break waiting
			case <-check.C():
				// Permission refresh must not turn a quiet watch into a journal scan every second.
				if !d.allowConnection(ctx, conn, device, a.ID) {
					return
				}
			}
		}
	}
}

// doorWatch joins a replaced watch before opening its successor, so two
// replay goroutines can never interleave events on one connection.
type doorWatch struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (w *doorWatch) close() {
	if w != nil {
		w.cancel()
		<-w.done
	}
}
func (d *door) startWatch(ctx context.Context, conn *Conn, a *Attempt, from uint64, device string) *doorWatch {
	ctx, cancel := context.WithCancel(ctx)
	watch := &doorWatch{cancel: cancel, done: make(chan struct{})}
	go func() { defer close(watch.done); defer cancel(); d.watch(ctx, conn, a, from, device) }()
	return watch
}

// A handshake establishes identity; the bounded roster still owns continuing permission.
func (d *door) allowConnection(ctx context.Context, conn *Conn, device, attempt string) bool {
	active, err := d.roster.allows(ctx, device)
	reason := ""
	if err != nil {
		reason = "could not check your devices right now"
	} else if !active {
		reason = RemovedComputerLine
	}
	if reason == "" {
		return true
	}
	_ = conn.Send(attempt, Refused{Reason: reason})
	_ = conn.Close()
	return false
}
