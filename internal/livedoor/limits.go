package livedoor

import "time"

const (
	// MaxLine caps a single EventWire JSON line so one oversized event cannot pin
	// memory at the host or viewer above the tunnel.
	MaxLine = 256 << 10

	// ChunkBytes caps a single multiplexer frame payload so concurrent channels
	// interleave fairly on one tunnel and fit well inside a Noise plaintext record.
	ChunkBytes = 16 << 10

	// ChannelWindow bounds unacknowledged data bytes in flight on one multiplexed
	// channel so one unread chat cannot stall other chats on the same link.
	ChannelWindow = 256 << 10

	// LinkWindow bounds total unacknowledged data bytes in flight across the entire
	// tunnel so a blind relay cannot buffer unbounded data for a slow viewer.
	LinkWindow = 1 << 20

	// QueueMax bounds the per-channel outbound queue at the host before a lagging
	// viewer is reset back to Resume.
	QueueMax = 4 << 20

	// RingBytes bounds the holder's replay ring so a reset-then-Resume cycle
	// either catches up within fixed memory or signals an explicit gap.
	RingBytes = 1 << 20

	// Rate paces outbound data payload bytes per second in each direction so a
	// chatty session stays below the relay abuse breaker.
	Rate = 1 << 20

	// RateBurst is the token-bucket burst allowance paired with Rate so an initial
	// welcome tail or burst of deltas up to one megabyte leaves without delay.
	RateBurst = 1 << 20

	// DailyBudget is the per-day carrier byte allowance before live sessions
	// degrade to watch-only until midnight.
	DailyBudget = 256 << 20

	// MaxLive caps concurrent live-intent relay streams on the host so two of the
	// sixteen relay stream slots stay reserved for Move.
	MaxLive = 14

	// MaxTunnelsPerDevice caps simultaneous live tunnels from the same viewer
	// device so a reconnecting link may briefly overlap its dying predecessor.
	MaxTunnelsPerDevice = 2

	// MaxChannelsPerLink caps concurrent open channels on a single multiplexed
	// tunnel so one viewer cannot allocate unbounded channel state.
	MaxChannelsPerLink = 32

	// FirstLineMax bounds the initial handshake line read on a local holder socket
	// before the full session line limit applies.
	FirstLineMax = 64 << 10

	// MaxDirect caps concurrent direct tunnels on one host. A direct tunnel
	// holds no relay stream, so this number is its own and not a share of
	// MaxLive; the files door's admission count (filesdoor.MaxDirect) is the
	// same number and a syncsetup test pins that they agree.
	MaxDirect = 14

	// DirectRate is the pacing of a direct tunnel in bytes per second, in each
	// direction, and its burst too. The 1 MiB/s Rate exists to keep the relay
	// under its abuse breaker and its message bill; a direct path has neither,
	// so this bucket exists for symmetry and never binds on a network.
	DirectRate = 64 << 20

	// SignalMax bounds one signalling payload. It is half of ChunkBytes so a
	// signal always fits one frame with room to spare, and far more than a
	// data-channel-only session description needs.
	SignalMax = 8 << 10

	// SignalRate is how many signalling frames a link may carry in a minute
	// before it is closed as `too-many`. An attempt is an offer, an answer and
	// a handful of candidates, so this allows several attempts a minute.
	SignalRate = 64

	// PendingOffers caps the direct attempts a host has in flight at once, over
	// every viewer, so a flood of offers cannot make a host build peer
	// connections without limit.
	PendingOffers = 4
)

var (
	// PingEvery is the interval between multiplexer pings while a viewed chat is
	// active; tests shrink it with t.Cleanup.
	PingEvery = 3 * time.Second

	// DeadAfter is the silence window after a ping before an unanswered link is
	// declared dead; tests shrink it with t.Cleanup.
	DeadAfter = 6 * time.Second

	// ActiveWindow is how long after a user action a viewed link remains in active
	// ping mode before going quiet to keep the relay hibernated.
	ActiveWindow = 30 * time.Second

	// IdleDetach is how long an inactive viewer with no running turn stays attached
	// before closing channels with reason idle so the relay costs nothing at rest.
	IdleDetach = 10 * time.Minute

	// WriteDeadline bounds how long the holder waits on a remote guest socket write
	// before dropping that guest so a local surface never stalls.
	WriteDeadline = 2 * time.Second

	// DirectPingEvery and DirectDeadAfter are the liveness of a direct tunnel.
	// The mux ping IS how a viewer learns a direct path has died, and it costs
	// the relay nothing, so it runs every second and declares the road dead
	// after DirectPingEvery+DirectDeadAfter of silence (three seconds).
	DirectPingEvery = 1 * time.Second
	DirectDeadAfter = 2 * time.Second

	// DirectSettle is how long after a view's first channel opens that the
	// viewer makes its first direct attempt, so the first paint never waits on
	// the path finding.
	DirectSettle = 500 * time.Millisecond

	// DirectConnectWithin is the time one attempt gets to finish ICE, DTLS,
	// SCTP, the handshake and the hello before it is abandoned.
	DirectConnectWithin = 8 * time.Second

	// DirectBackoff is how long a viewer waits after each failed attempt before
	// the next, ending in a ten-minute wait that repeats; DirectStableAfter is
	// how long a direct tunnel must stay up before the ladder starts over.
	DirectBackoff = []time.Duration{5 * time.Second, 15 * time.Second, 60 * time.Second, 5 * time.Minute, 10 * time.Minute}

	DirectStableAfter = 60 * time.Second

	// OfferTTL is how long a host keeps an attempt that has not produced a
	// stream before it lets the attempt go.
	OfferTTL = 12 * time.Second
)

// DirectLimits are the limits a host serves a direct tunnel with: the road's
// own pacing and liveness, and everything else (the line cap, the credit
// windows, the write deadline) as for the relay, because those bound memory
// above the tunnel and not the cost of the road.
func DirectLimits() Limits {
	l := DefaultLimits()
	l.Rate = DirectRate
	l.RateBurst = DirectRate
	l.PingEvery = DirectPingEvery
	l.DeadAfter = DirectDeadAfter
	return l
}

// Limits carries the byte, count, and duration caps for a live door link so a
// test or handshake payload can override individual values from the defaults.
type Limits struct {
	MaxLine             int           `json:"max_line,omitempty"`
	ChunkBytes          int           `json:"chunk_bytes,omitempty"`
	ChannelWindow       int           `json:"channel_window,omitempty"`
	LinkWindow          int           `json:"link_window,omitempty"`
	QueueMax            int           `json:"queue_max,omitempty"`
	RingBytes           int           `json:"ring_bytes,omitempty"`
	Rate                int           `json:"rate,omitempty"`
	RateBurst           int           `json:"rate_burst,omitempty"`
	DailyBudget         int64         `json:"daily_budget,omitempty"`
	MaxLive             int           `json:"max_live,omitempty"`
	MaxTunnelsPerDevice int           `json:"max_tunnels_per_device,omitempty"`
	MaxChannelsPerLink  int           `json:"max_channels_per_link,omitempty"`
	FirstLineMax        int           `json:"first_line_max,omitempty"`
	PingEvery           time.Duration `json:"ping_every,omitempty"`
	DeadAfter           time.Duration `json:"dead_after,omitempty"`
	ActiveWindow        time.Duration `json:"active_window,omitempty"`
	IdleDetach          time.Duration `json:"idle_detach,omitempty"`
	WriteDeadline       time.Duration `json:"write_deadline,omitempty"`
}

// DefaultLimits returns a Limits populated from the package constants and
// current duration variables.
func DefaultLimits() Limits {
	return Limits{
		MaxLine:             MaxLine,
		ChunkBytes:          ChunkBytes,
		ChannelWindow:       ChannelWindow,
		LinkWindow:          LinkWindow,
		QueueMax:            QueueMax,
		RingBytes:           RingBytes,
		Rate:                Rate,
		RateBurst:           RateBurst,
		DailyBudget:         DailyBudget,
		MaxLive:             MaxLive,
		MaxTunnelsPerDevice: MaxTunnelsPerDevice,
		MaxChannelsPerLink:  MaxChannelsPerLink,
		FirstLineMax:        FirstLineMax,
		PingEvery:           PingEvery,
		DeadAfter:           DeadAfter,
		ActiveWindow:        ActiveWindow,
		IdleDetach:          IdleDetach,
		WriteDeadline:       WriteDeadline,
	}
}

// directDefaults fills the fields where a direct tunnel differs from a relay
// one, wherever the caller left them unset, so a direct tunnel served with a
// zero Limits is still paced and watched as the direct road is.
func (l Limits) directDefaults() Limits {
	if l.Rate <= 0 {
		l.Rate = DirectRate
	}
	if l.RateBurst <= 0 {
		l.RateBurst = DirectRate
	}
	if l.PingEvery <= 0 {
		l.PingEvery = DirectPingEvery
	}
	if l.DeadAfter <= 0 {
		l.DeadAfter = DirectDeadAfter
	}
	return l
}

// withDefaults fills any non-positive field in l from DefaultLimits so a test
// can set only the single limit it cares about in a config struct.
func (l Limits) withDefaults() Limits {
	d := DefaultLimits()
	if l.MaxLine <= 0 {
		l.MaxLine = d.MaxLine
	}
	if l.ChunkBytes <= 0 {
		l.ChunkBytes = d.ChunkBytes
	}
	if l.ChannelWindow <= 0 {
		l.ChannelWindow = d.ChannelWindow
	}
	if l.LinkWindow <= 0 {
		l.LinkWindow = d.LinkWindow
	}
	if l.QueueMax <= 0 {
		l.QueueMax = d.QueueMax
	}
	if l.RingBytes <= 0 {
		l.RingBytes = d.RingBytes
	}
	if l.Rate <= 0 {
		l.Rate = d.Rate
	}
	if l.RateBurst <= 0 {
		l.RateBurst = d.RateBurst
	}
	if l.DailyBudget <= 0 {
		l.DailyBudget = d.DailyBudget
	}
	if l.MaxLive <= 0 {
		l.MaxLive = d.MaxLive
	}
	if l.MaxTunnelsPerDevice <= 0 {
		l.MaxTunnelsPerDevice = d.MaxTunnelsPerDevice
	}
	if l.MaxChannelsPerLink <= 0 {
		l.MaxChannelsPerLink = d.MaxChannelsPerLink
	}
	if l.FirstLineMax <= 0 {
		l.FirstLineMax = d.FirstLineMax
	}
	if l.PingEvery <= 0 {
		l.PingEvery = d.PingEvery
	}
	if l.DeadAfter <= 0 {
		l.DeadAfter = d.DeadAfter
	}
	if l.ActiveWindow <= 0 {
		l.ActiveWindow = d.ActiveWindow
	}
	if l.IdleDetach <= 0 {
		l.IdleDetach = d.IdleDetach
	}
	if l.WriteDeadline <= 0 {
		l.WriteDeadline = d.WriteDeadline
	}
	return l
}
