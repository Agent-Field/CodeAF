package clusterstate

// Coordination periods are shared by committed policy and both host schedulers.
const (
	RenewEveryMillis     int64 = 30_000
	PresenceEveryMillis  int64 = 10_000
	EvidenceWindowMillis int64 = PresenceEveryMillis * 5 / 2
	TickEveryMillis      int64 = 5_000
	IdleAfterMillis      int64 = 60_000
	MaxRenewHolds              = 64
)

// RenewHolds is signed only by the restricted ingress effects authority after
// authenticating fresh socket evidence. Evidence time fixes the expiry: replay
// can never turn an old ping into another ninety seconds of ownership.
type RenewHolds struct {
	WorkerIncarnation uint64      `json:"worker_incarnation,omitempty"`
	Device            string      `json:"device"`
	SeenAt            int64       `json:"seen_at"`
	Items             []HoldFence `json:"items"`
}
type HoldFence struct {
	Cell  string `json:"cell"`
	Fence uint64 `json:"fence"`
}

// Quiescence is a durable application marker, not a lease or permission to read.
// Index is its log position; any subsequent command invalidates the marker.
type Quiescence struct {
	Index   uint64   `json:"index"`
	Term    uint64   `json:"term"`
	Barrier uint64   `json:"barrier"`
	Voters  []uint64 `json:"voters"`
}
type PrepareQuiescence struct {
	Barrier uint64   `json:"barrier"`
	Term    uint64   `json:"term"`
	Voters  []uint64 `json:"voters"`
}

func QuiescenceKey() RecordKey { return RecordKey{"membership", "quiescence"} }

func renewHolds(c Command, now int64, rows map[RecordKey][]byte, put func(RecordKey, any), result *Result) Code {
	v := c.Change.RenewHolds
	if v.Device == "" || len(v.Items) == 0 || len(v.Items) > MaxRenewHolds || v.SeenAt <= 0 || v.SeenAt > now || now-v.SeenAt > EvidenceWindowMillis {
		return BadRequest
	}
	var actor Device
	var auth Authority
	if Decode(rows[AuthorityKey(v.Device)], &auth) != nil || !auth.ValidRole() || auth.Cluster != c.Cluster || !auth.Personal && auth.Worker == nil {
		return Unauthorized
	}
	if raw := rows[DeviceKey(v.Device)]; raw != nil && Decode(raw, &actor) != nil {
		return BadRequest
	}
	if actor.Revoked {
		return Revoked
	}
	if auth.Worker != nil {
		var identity IdentityRec
		if Decode(rows[IdentityKey()], &identity) != nil {
			return BadRequest
		}
		a, w, code := workerFromRows(c.Cluster, v.Device, now, identity, rows)
		if code != OK {
			return code
		}
		if v.WorkerIncarnation == 0 || v.WorkerIncarnation != w.Incarnation {
			return Conflict
		}
		for _, h := range v.Items {
			if !a.Worker.Allows(h.Cell) {
				return Unauthorized
			}
			if b := rows[WorkerHoldKey(v.Device, h.Cell)]; b != nil {
				var hold WorkerHold
				if Decode(b, &hold) != nil {
					return BadRequest
				}
				if hold.Incarnation != v.WorkerIncarnation {
					return Conflict
				}
			}
		}
	}
	seen := map[string]bool{}
	for _, h := range v.Items {
		if h.Cell == "" || h.Fence == 0 || seen[h.Cell] {
			return BadRequest
		}
		seen[h.Cell] = true
	}
	for _, h := range v.Items {
		var cell Cell
		code := OK
		raw := rows[CellKey(h.Cell)]
		switch {
		case raw == nil:
			code = NotFound
		case Decode(raw, &cell) != nil:
			return BadRequest
		case cell.Lease.Device != v.Device || cell.Lease.Fence != h.Fence:
			code = FenceStale
		case cell.Lease.Expires == 0 || v.SeenAt <= cell.Lease.ReleasedAt:
			code = FenceStale
		default:
			// A recent publish already covers this evidence; retain its later
			// expiry without creating a redundant application write.
			until := v.SeenAt + LeaseTTLMillis
			if until > cell.Lease.Expires {
				cell.Lease.Expires = until
				put(CellKey(h.Cell), cell)
			}
		}
		item := ItemResult{Cell: h.Cell, Code: code}
		if code == OK {
			item.View = &CellView{Cell: cell, Now: now}
		}
		result.Items = append(result.Items, item)
	}
	return OK
}

// RecordActive is evaluated one immutable row at a time so large imported
// identities never have to be materialized to decide whether they may sleep.
func RecordActive(key RecordKey, raw []byte, now int64) (bool, error) {
	if key.Namespace == "state" && len(key.Key) > 11 && key.Key[:11] == "assignment/" {
		var a Assignment
		if Decode(raw, &a) != nil {
			return false, ErrDependency
		}
		return (a.Status == "pending" || a.Status == "claimed") && a.Spec.Expires > now, nil
	}

	if key.Namespace == "state" && len(key.Key) > 7 && key.Key[:7] == "worker/" {
		var worker WorkerRecord
		if Decode(raw, &worker) != nil {
			return false, ErrDependency
		}
		return len(worker.Work) > 0 && worker.AvailableUntil > now, nil
	}

	if key.Namespace == "state" && len(key.Key) >= 5 && key.Key[:5] == "cell/" {
		var c Cell
		if Decode(raw, &c) != nil {
			return false, ErrDependency
		}
		return c.Lease.Expires > now, nil
	}
	if key == MembershipKey() {
		var h Membership
		if Decode(raw, &h) != nil || h.Validate() != nil {
			return false, ErrDependency
		}
		return h.Pending != nil || h.Leadership != nil && !h.Leadership.Completed, nil
	}
	if key.Namespace == "outbox" && len(key.Key) >= 5 && key.Key[:5] == "join/" {
		var request JoinRequest
		if Decode(raw, &request) != nil {
			return false, ErrDependency
		}
		return request.Decision != nil && request.Outcome == "", nil
	}
	return false, nil
}

// RenewalCovered suppresses a redundant batch item after an authoritative
// read. A recent publish may already cover this sample; this never invents a
// receipt or extends a released, displaced or expired lease.
func RenewalCovered(raw []byte, device string, hold HoldFence, seen int64) (bool, error) {
	if raw == nil {
		return false, nil
	}
	var c Cell
	if err := Decode(raw, &c); err != nil {
		return false, err
	}
	return seen > 0 && seen <= int64(MaxSafeInteger)-LeaseTTLMillis && c.Lease.Device == device && c.Lease.Fence == hold.Fence && c.Lease.Expires >= seen+LeaseTTLMillis, nil
}
