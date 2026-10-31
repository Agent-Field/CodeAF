package taskdoor

import (
	"context"
	"crypto/ecdh"
	"errors"
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/relay"
)

// The task door deliberately duplicates pairing's registration loop: stability
// of pairing outranks sharing the loop.
type registrationLoop struct {
	Service string
	Key     *ecdh.PrivateKey
	Say     func(string)
	Now     func() time.Time
	Wait    func(context.Context, time.Duration) error
	Ready   func(*relay.Registration)
	Hold    func(context.Context, *relay.Registration)
}

func (r *registrationLoop) say(line string) {
	if r.Say != nil {
		r.Say(line)
	}
}
func (r *registrationLoop) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}
func (r *registrationLoop) wait(ctx context.Context, d time.Duration) error {
	if r.Wait != nil {
		return r.Wait(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Run returns only on cancellation or a refusal that retrying cannot fix.
// Hold owns accepted streams until the registration ends.
func (r *registrationLoop) Run(ctx context.Context) error {
	if r.Key == nil || r.Hold == nil {
		return errors.New("pair: a registration needs a key and a stream handler")
	}
	wait := time.Second
	announced := false
	// told and throttled are each said once per spell, not once per retry.
	// told is set when the person has been told sync was lost and not yet
	// that it came back, so the two lines always arrive as a pair.
	told := false
	throttled := false
	tellLost := func() {
		if !told {
			r.say(pair.LostSyncLine)
		}
		told = true
	}
	for {
		registration, err := relay.Register(ctx, r.Service, r.Key)
		switch {
		case err == nil:
		case errors.Is(err, relay.ErrTooMany):
			if !throttled {
				r.say(pair.TooManyRegistrationsLine)
			}
			throttled = true
		case errors.Is(err, relay.ErrNameTaken) && announced:
			// This machine held the name a moment ago, so what holds it now is
			// its own dead connection, which the relay forgets when it times
			// out. That is the outage still going on, and is waited out.
			tellLost()
		case errors.Is(err, relay.ErrNameTaken):
			// RULE THREE OF THE NAME POLICY, MET FROM THIS SIDE. Something is
			// already connected under this machine's name, which in practice
			// means a second `codeaf serve` on this same machine. Trying again
			// forever would be two processes fighting over one name.
			return fmt.Errorf("this machine is already connected to sync as %s — there is only one of it, so close the other `codeaf serve`", relay.NameFor(r.Key.PublicKey().Bytes()))
		case errors.Is(err, relay.ErrUnreachable):
			if !announced {
				return pair.Unreachable(r.Service)
			}
			tellLost()
		case errors.Is(err, relay.ErrNoDoor) && announced:
			// The door was there a moment ago, so this is a relay being
			// redeployed, and is waited out like any other outage.
			tellLost()
		case errors.Is(err, relay.ErrNoDoor):
			// Trying again cannot add a door the service does not have.
			return pair.NoDoor(r.Service)
		default:
			if !announced {
				return err
			}
			r.say("sync refused this machine: " + err.Error())
		}

		if err == nil {
			announced = true
			// A LOST LINE IS ANSWERED BY A BACK LINE. The banner is reprinted only
			// when the name or code changed, so without this the last thing on
			// the screen of a machine that recovered would still say it was lost.
			if told {
				r.say(pair.BackLine(registration.Name()))
				told = false
			}
			if r.Ready != nil {
				r.Ready(registration)
			}
			since := r.now()
			r.Hold(ctx, registration)
			// Backoff resets only after the carrier stayed up for a meaningful
			// stretch; an upgrade that drops right after Register keeps
			// backing off instead of hammering the relay every second.
			if r.now().Sub(since) >= time.Minute {
				wait = time.Second
				throttled = false
			}
		}

		if err := r.wait(ctx, wait); err != nil {
			return nil
		}
		if wait *= 2; wait > time.Minute {
			wait = time.Minute
		}
	}
}
