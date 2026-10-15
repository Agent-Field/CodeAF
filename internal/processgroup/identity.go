package processgroup

// identity.go lets a recorded group outlive the process that recorded it.
//
// A Group is captured at launch and checked before every signal (group.go). A
// kept-running service is a group that has to be found again by a LATER
// process — codeaf quit or crashed, and the next one opens — so the identity
// has to travel through a file. What travels is exactly what the gate checks:
// the leader's pid and the start identity it had at launch, plus the boot it
// was recorded in.
//
// THE BOOT IS PART OF THE IDENTITY. On Linux the start identity is clock ticks
// since boot, which is unique within one boot and says nothing across two: a
// pid handed out again after a restart can carry the very same tick count. So
// an identity recorded in another boot restores as a group whose identity is
// unknown, and an unknown identity is never signalled and never alive — the
// same one-sided default the rest of this package keeps.

import "sync"

// Identity is a recorded group leader in a form a file can hold.
type Identity struct {
	PID   int    `json:"pid"`
	Start uint64 `json:"start"`
	// Boot names the boot the identity was recorded in. Without a boot identity,
	// a persisted pid and start count cannot safely prove the original process.
	Boot string `json:"boot,omitempty"`
}

// Identity reports what the group was recorded as, and false when no identity
// was readable at launch — a group that can never be proven again.
func (g Group) Identity() (Identity, bool) {
	if !g.known || g.pid <= 0 || BootID() == "" {
		return Identity{}, false
	}
	return Identity{PID: g.pid, Start: g.start, Boot: BootID()}, true
}

// Restore is a group recorded by an earlier process. It is known only when it
// was recorded in this boot, so every signal and liveness answer below goes
// through the same identity gate a group captured in this process does.
func Restore(id Identity) Group {
	if id.PID <= 0 {
		return Group{}
	}
	if id.Start == 0 || id.Boot == "" || BootID() == "" || id.Boot != BootID() {
		return Group{pid: id.PID}
	}
	return Group{pid: id.PID, start: id.Start, known: true}
}

// SameBoot reports a proven matching boot. Missing identity proves neither
// that a boot is the same nor that it changed.
func SameBoot(id Identity) bool { return id.Boot != "" && BootID() != "" && id.Boot == BootID() }

// DifferentBoot proves an intervening machine restart. A caller deciding
// whether to start a saved service must not treat unreadable identity as a
// restart: the original process may still be running.
func DifferentBoot(id Identity) bool { return id.Boot != "" && BootID() != "" && id.Boot != BootID() }

var (
	bootOnce sync.Once
	bootID   string
)

// BootID names the running boot, read once, or "" where the platform cannot.
func BootID() string {
	bootOnce.Do(func() { bootID = readBootID() })
	return bootID
}
