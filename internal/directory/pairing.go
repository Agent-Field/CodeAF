package directory

// Joined says a device was approved into the identity. It is what the watch
// feed tells the other devices, in the same turn as the approve.
type Joined struct {
	Device   string
	Name     string // sealed, as stored
	Platform string
	At       int64 // directory ms
}

// pairing is what a backend needs to answer ApproveRequest and DenyRequest: the
// request store to decide in, and an optional listener for new devices. Memory
// and SQLite embed it, so the two verbs are written once.
type pairing struct {
	links  Decider
	onJoin func(Joined)
}

// SetLinks gives the backend the request store its devices decide in. Call it
// before the directory is used; without one every request is ErrRequestGone.
func (p *pairing) SetLinks(d Decider) { p.links = d }

// SetOnJoin registers the listener told of each approved device, after its
// record is written and before the approval is kept. Call it before use.
func (p *pairing) SetOnJoin(f func(Joined)) { p.onJoin = f }

// approve decides code as approved. write stores the device record for the
// request and answers it as stored; it runs only when the decision is new.
func (p *pairing) approve(code string, a Approval, write func(Request) (Device, error)) error {
	dec, err := decisionOf(a)
	if err != nil {
		return err
	}
	return p.decide(code, dec, func(r Request) error {
		if r.Device != dec.Device {
			return ErrBadRequest
		}
		d, err := write(r)
		if err == nil {
			p.joined(r, d)
		}
		return err
	})
}

// deny decides code as denied. guard runs the caller's usual checks (not
// revoked, identity not replaced) without writing anything.
func (p *pairing) deny(code string, guard func() error) error {
	return p.decide(code, Decision{State: RequestDenied}, func(Request) error { return guard() })
}

func (p *pairing) decide(code string, d Decision, apply func(Request) error) error {
	if p.links == nil {
		return ErrRequestGone
	}
	return p.links.Decide(code, d, apply)
}

func (p *pairing) joined(r Request, d Device) {
	if p.onJoin == nil {
		return
	}
	p.onJoin(Joined{Device: r.Device, Name: d.Name, Platform: d.Platform, At: d.Created})
}
