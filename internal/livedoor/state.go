package livedoor

import (
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/pair"
)

// StateKind identifies the lifecycle posture of a viewer's live door link to a
// single host computer.
type StateKind int

const (
	// Connecting is the initial zero-value posture of a link before its first
	// tunnel handshake has completed.
	Connecting StateKind = iota
	// Live reports that the tunnel is open and the viewer holds both watch and
	// drive permission on the host.
	Live
	// Reconnecting reports that an active tunnel dropped and the link is waiting
	// on its jittered backoff timer or dialling a replacement tunnel.
	Reconnecting
	// WentOffline reports that the host stopped answering active liveness pings,
	// closed an idle tunnel, or had no live door registered at the relay.
	WentOffline
	// Idle reports that the viewer detached locally after IdleDetach of quiet
	// with no user action and no running turn so the relay can hibernate.
	Idle
	// Revoked reports that the host removed this viewer computer or denied watch
	// permission, stopping automatic retries for good. A host that turned
	// remote control off reads the same way with Reason [ReasonRemoteControlOff],
	// except that it is dialled again when a person next opens a view on it.
	Revoked
	// WatchOnly reports that the tunnel is open with watch permission while
	// drive permission is off.
	WatchOnly
	// StateBudget reports that the host's daily live byte allowance is exhausted
	// so sessions stay open and readable without typing until midnight UTC.
	// It is named StateBudget (with alias BudgetExhausted) because type Budget
	// in budget.go already occupies the package-level identifier Budget.
	StateBudget

	// BudgetExhausted aliases StateBudget for callers checking daily budget exhaustion.
	BudgetExhausted = StateBudget
)

func (k StateKind) String() string {
	switch k {
	case Connecting:
		return "connecting"
	case Live:
		return "live"
	case Reconnecting:
		return "reconnecting"
	case WentOffline:
		return "went-offline"
	case Idle:
		return "idle"
	case Revoked:
		return "revoked"
	case WatchOnly:
		return "watch-only"
	case StateBudget:
		return "budget"
	default:
		return "unknown"
	}
}

// State is the snapshot of a viewer link's connection posture, the closed-set
// close or refusal reason when one caused the transition, the clock reading
// when this posture began, the host permit, and the transport road name.
type State struct {
	Kind   StateKind
	Reason string
	Since  time.Time
	Permit pair.Permit
	Road   string
}

// ErrHostOffline is wrapped by a LinkConfig.Dial implementation when the relay
// reports no live door registration for the host so the link transitions
// straight to WentOffline at the fifteen-second retry cap instead of polling.
var ErrHostOffline = errors.New("livedoor: host offline")

// ReasonError carries a closed-set live door reason whose Error text is the
// exact person-facing sentence from internal/chatlist so remote.Roam treats a
// channel refusal or link failure as the host's own words.
type ReasonError struct {
	Reason string
	Host   string
	Err    error
}

func (e *ReasonError) Error() string {
	if e == nil {
		return ""
	}
	return chatlist.LiveSaid(e.Reason, e.Host)
}

func (e *ReasonError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ReasonOf extracts the closed-set live door reason from err when one is
// present on a ReasonError, RefusedError, ChannelError, or reason-bearing error.
func ReasonOf(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	var re *ReasonError
	if errors.As(err, &re) && re != nil && re.Reason != "" {
		return re.Reason, true
	}
	var ref *RefusedError
	if errors.As(err, &ref) && ref != nil && ref.Reason != "" {
		return ref.Reason, true
	}
	var ce *ChannelError
	if errors.As(err, &ce) && ce != nil && ce.Why != "" {
		return ce.Why, true
	}
	var r interface{ Reason() string }
	if errors.As(err, &r) {
		if why := r.Reason(); why != "" {
			return why, true
		}
	}
	return "", false
}
