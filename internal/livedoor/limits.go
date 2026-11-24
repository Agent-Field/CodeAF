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
)

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
