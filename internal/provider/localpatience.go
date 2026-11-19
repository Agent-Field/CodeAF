package provider

import (
	"net/http"
	"sync"
	"time"

	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/lane/control"
)

// ── ONE MULTIPLIER ON EVERY WAIT, DECIDED PER REQUEST ───────────────────────
//
// internal/lane's localpatience.go says why a machine of the person's own needs
// more patience than a hosted endpoint and how the number is chosen; this file is
// where the transport READS it. Every bound that decides a call has failed is
// multiplied by [Client.patience] at the one place it is built, so that the
// header deadline, the idle watchdog, the three silence bounds, the wall, the
// non-streamed deadline and the controller's ceiling can never come to disagree
// about how patient this request is.
//
// A HOSTED ENDPOINT UNDER `auto` IS ONE, AND ONE IS THE IDENTITY: every function
// here hands back the very value it was given when the factor is one or less, so
// the figures this build shipped with are not approximated, they are untouched.

// patience is how many times the hosted bound this client's requests are held to.
//
// It is read per request and not captured at build, because the person's row can
// change while a conversation is open and the very next request has to go out
// under it; and it is a method of the client because the client is what knows
// where it is sending — the base URL and, for a connected provider, which row it
// is.
func (c *Client) patience() float64 {
	if c == nil {
		return 1
	}
	return lanes.PatienceFor(c.config.BaseURL, c.config.SourceID)
}

// localEndpoint says this client's requests go to a machine of the person's own.
// It is [lanes.IsLocalEndpoint] and nothing else, asked in one place, because
// "is this local" has exactly one answer in this build.
func (c *Client) localEndpoint() bool {
	if c == nil {
		return false
	}
	return lanes.IsLocalEndpoint(c.config.BaseURL, c.config.SourceID)
}

// stretchPlan is a call's plan with its two waits in the client's patience: the
// ceiling the controller acts at, and the deadline after which the turn stops
// trying this model. A plan under a patience of one is returned untouched.
//
// THE FLOOR IS NOT STRETCHED. It is the shortest silence worth acting on, a fact
// about racing the network, and a slow model does not make the network slower.
func (c *Client) stretchPlan(plan control.Plan) control.Plan {
	factor := c.patience()
	if !(factor > 1) {
		return plan
	}
	plan.Ceiling = lanes.Stretch(plan.Ceiling, factor)
	if !plan.Began.IsZero() && !plan.Deadline.IsZero() {
		plan.Deadline = plan.Began.Add(lanes.Stretch(plan.Deadline.Sub(plan.Began), factor))
	}
	return plan
}

// ── THE HEADER DEADLINE, PER FACTOR ─────────────────────────────────────────
//
// http.Transport.ResponseHeaderTimeout is a property of the TRANSPORT, so a
// header bound that depends on who is being asked needs a transport per bound.
// There are very few distinct bounds in a process — the hosted one, the local
// default, and whatever number the person wrote — so each gets one clone of the
// shared pool, built the first time it is asked for and kept. The hosted bound is
// the original streaming transport itself, which is what keeps a hosted call on
// exactly the connection pool and the deadline it has always had.

var (
	patientMu   sync.Mutex
	patientPool = map[time.Duration]*http.Transport{}
)

// streamTransportFor is the streaming transport whose header deadline is
// [responseHeaderTimeout] times factor.
func streamTransportFor(factor float64) *http.Transport {
	base := streamTransport()
	bound := lanes.Stretch(responseHeaderTimeout, factor)
	if bound == responseHeaderTimeout {
		return base
	}
	patientMu.Lock()
	defer patientMu.Unlock()
	if transport, built := patientPool[bound]; built {
		return transport
	}
	transport := base.Clone()
	transport.ResponseHeaderTimeout = bound
	patientPool[bound] = transport
	return transport
}
