package relayserve

import (
	"time"
)

// unavailable keeps the local cause that a generic 503 cannot explain. Each
// cause has its own one-minute budget, and a noisy failing store cannot grow
// the relay's diagnostic memory without bound.
func (n *namespaces) unavailable(phase string, err error) {
	if n.logf == nil || err == nil {
		return
	}
	key := err.Error()
	n.faultMu.Lock()
	defer n.faultMu.Unlock()
	now := n.now()
	if last, ok := n.faults[key]; ok && now.Sub(last) < time.Minute {
		return
	}
	if n.faults == nil {
		n.faults = map[string]time.Time{}
	}
	if len(n.faults) >= 64 {
		var oldest string
		for cause, at := range n.faults {
			if oldest == "" || at.Before(n.faults[oldest]) {
				oldest = cause
			}
		}
		delete(n.faults, oldest)
	}
	n.faults[key] = now
	n.logf("relay unavailable phase=%s cause=%q", phase, err.Error())
}
