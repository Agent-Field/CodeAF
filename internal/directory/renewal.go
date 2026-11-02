package directory

import "github.com/Agent-Field/codeaf/internal/clusterstate"

// A cycle retains one sample per admitted holding socket. Its immutable tuples
// and original renewal callback disappear when the bounded page is consumed.
type holdSample struct {
	sub    *Sub
	at     int64
	offset int
}

// renewCommitted shares one period across the identity. Presence between cycles
// updates the socket's latest sample without retaining a history of pings.
func (f *Feed) renewCommitted(current *Sub, at int64) error {
	f.renewMu.Lock()
	defer f.renewMu.Unlock()
	if len(current.keys) == 0 || at < f.renewAt {
		return nil
	}
	if f.renewPage == nil {
		f.mu.Lock()
		for s := range f.subs {
			if s.renew == nil || len(s.keys) == 0 {
				continue
			}
			seen := s.alive.Load()
			if s == current {
				seen = at
			}
			f.renewPage = append(f.renewPage, holdSample{sub: s, at: seen})
		}
		f.mu.Unlock()
	}
	remaining := clusterstate.MaxRenewHolds
	var currentErr error
	for len(f.renewPage) > 0 && remaining > 0 {
		sample := &f.renewPage[0]
		s := sample.sub
		st := f.Status()
		if !s.shut.Load() && !s.lapsed.Load() && !st.Stopped[s.device] && st.Rotation == nil && sample.at > 0 && sample.at <= at && at-sample.at <= clusterstate.EvidenceWindowMillis {
			end := min(len(s.keys), sample.offset+remaining)
			holds := make([]Hold, 0, end-sample.offset)
			for _, key := range s.keys[sample.offset:end] {
				holds = append(holds, Hold{Cell: key.cell, Fence: key.fence})
			}
			remaining -= len(holds)
			sample.offset = end
			receipts, err := s.renew(s.device, holds, sample.at)
			if err != nil {
				if s == current {
					currentErr = err
				} else {
					s.lapse()
				}
			} else {
				s.receiptMu.Lock()
				for _, receipt := range receipts {
					found := false
					for i, old := range s.receipts {
						if old.Cell == receipt.Cell && old.Fence == receipt.Fence {
							s.receipts[i] = receipt
							found = true
							break
						}
					}
					if !found {
						s.receipts = append(s.receipts, receipt)
					}
				}
				s.receiptMu.Unlock()
				if end < len(s.keys) {
					continue
				}
			}
		}
		f.renewPage[0] = holdSample{}
		f.renewPage = f.renewPage[1:]
	}
	if len(f.renewPage) == 0 {
		f.renewPage = nil
		f.renewAt = at + clusterstate.RenewEveryMillis
	}
	return currentErr
}
