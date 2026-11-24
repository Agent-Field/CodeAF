//go:build linux

package sessionhost

import "time"

const uidQuarantine = 24 * time.Hour

func (r *registry) allocate(cfg Config, live processTable, now time.Time) int {
	used := make(map[int]bool, len(r.sessions))
	for _, row := range r.sessions {
		used[row.UID] = true
	}
	for uid, until := range r.quarantine {
		if until.After(now) {
			used[uid] = true
		} else {
			delete(r.quarantine, uid)
		}
	}
	for offset := 0; offset < cfg.UIDCount; offset++ {
		uid := cfg.UIDBase + offset
		if !used[uid] && len(live[uid]) == 0 {
			return uid
		}
	}
	return 0
}
