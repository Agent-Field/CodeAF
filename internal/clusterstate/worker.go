package clusterstate

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
)

const MaxWorkerBeatItems = MaxRenewHolds

// WorkerRegistry charges every retained delegation, including expired/revoked
// workers. Capacity refusal never deletes records or reuses their authority.
type WorkerRegistry struct {
	Retained uint32 `json:"retained"`
}

type EnrollWorker struct {
	Certificate WorkerCertificate `json:"certificate"`
	Grants      map[string][]byte `json:"grants"`
}
type RegisterWorker struct {
	Previous     uint64 `json:"previous"`
	Incarnation  uint64 `json:"incarnation"`
	Caps         Caps   `json:"caps"`
	SealedLabels []byte `json:"sealed_labels"`
}
type DrainWorker struct {
	Worker      string `json:"worker"`
	Incarnation uint64 `json:"incarnation"`
}

// WorkerRecord carries only the existing public capability shape and opaque
// encrypted metadata. Availability does not confer a lease or authorize tools.
type WorkerRecord struct {
	Incarnation     uint64            `json:"incarnation"`
	HeartbeatDigest string            `json:"heartbeat_digest,omitempty"`
	Heartbeat       uint64            `json:"heartbeat"`
	SeenAt          int64             `json:"seen_at"`
	AvailableUntil  int64             `json:"available_until"`
	Draining        bool              `json:"draining"`
	Caps            Caps              `json:"caps"`
	SealedLabels    []byte            `json:"sealed_labels"`
	Grants          map[string][]byte `json:"grants"`
	Work            []HoldFence       `json:"work"`
}

// A batch retains each worker's original signed evidence. A scheduling client
// may collect proofs, but cannot manufacture availability on a worker's behalf.
type WorkerHeartbeat struct {
	Worker      string      `json:"worker"`
	Incarnation uint64      `json:"incarnation"`
	Sequence    uint64      `json:"sequence"`
	SeenAt      int64       `json:"seen_at"`
	Work        []HoldFence `json:"work"`
	Signature   []byte      `json:"signature"`
}
type WorkerBeatBatch struct {
	Items []WorkerHeartbeat `json:"items"`
}
type WorkerResult struct {
	Worker         string `json:"worker"`
	Code           Code   `json:"code"`
	Incarnation    uint64 `json:"incarnation"`
	AvailableUntil int64  `json:"available_until"`
	Draining       bool   `json:"draining"`
}

func WorkerHeartbeatBytes(cluster string, h WorkerHeartbeat) ([]byte, error) {
	h.Signature = nil
	b, err := Encode(struct {
		Cluster string          `json:"cluster"`
		Beat    WorkerHeartbeat `json:"beat"`
	}{cluster, h})
	return append([]byte("codeaf/worker-beat/v1\x00"), b...), err
}
func WorkerKey(worker string) RecordKey { return RecordKey{"state", "worker/" + segment(worker)} }
func WorkerRegistryKey() RecordKey      { return RecordKey{"state", "worker-registry"} }

// ValidRole keeps service, personal, anchor and delegated credentials mutually
// exclusive, including when rows are supplied by genesis or an old snapshot.
func (a Authority) ValidRole() bool {
	n := 0
	for _, yes := range []bool{a.Personal, a.Effects, a.Anchor, a.Worker != nil} {
		if yes {
			n++
		}
	}
	return n == 1
}
func workerMutation(m Mutation) bool {
	return m.EnrollWorker != nil || m.RegisterWorker != nil || m.WorkerBeatBatch != nil || m.DrainWorker != nil
}
func workerDependencies(c Command) []RecordKey {
	keys := []RecordKey{WorkerKey(c.Actor)}
	add := func(id string) { keys = append(keys, WorkerKey(id), AuthorityKey(id), DeviceKey(id)) }
	if v := c.Change.EnrollWorker; v != nil {
		add(v.Certificate.DeviceID())
		keys = append(keys, WorkerRegistryKey())
	}
	if v := c.Change.DrainWorker; v != nil {
		add(v.Worker)
	}
	if v := c.Change.WorkerBeatBatch; v != nil {
		for _, h := range v.Items {
			add(h.Worker)
			for _, work := range h.Work {
				keys = append(keys, CellKey(work.Cell))
			}
		}
	}
	if v := c.Change.RecordUpload; v != nil {
		add(v.Device)
	}
	if v := c.Change.RenewHolds; v != nil {
		add(v.Device)
		for _, h := range v.Items {
			keys = append(keys, WorkerHoldKey(v.Device, h.Cell))
		}
	}
	if v := c.Change.EnrollMember; v != nil {
		add(v.Device)
	}
	return keys
}

func workerAuthority(cluster, worker string, now int64, identity IdentityRec, a Authority, device Device) Code {
	if device.Revoked {
		return Revoked
	}
	if identity.Rotation != nil {
		return Rotated
	}
	if !a.ValidRole() || a.Worker == nil || a.Cluster != cluster || !bytes.Equal(a.Key, a.Worker.Key) || a.Worker.DeviceID() != worker || VerifyWorkerCertificate(*a.Worker, identity.SigningKey, cluster, now) != nil {
		return Unauthorized
	}
	return OK
}
func workerFromRows(cluster, worker string, now int64, identity IdentityRec, rows map[RecordKey][]byte) (Authority, WorkerRecord, Code) {
	var a Authority
	var d Device
	var w WorkerRecord
	if Decode(rows[AuthorityKey(worker)], &a) != nil || Decode(rows[WorkerKey(worker)], &w) != nil {
		return a, w, Unauthorized
	}
	if b := rows[DeviceKey(worker)]; b != nil && Decode(b, &d) != nil {
		return a, w, BadRequest
	}
	return a, w, workerAuthority(cluster, worker, now, identity, a, d)
}
func authorizeWorkerCommand(c Command, now int64, identity IdentityRec, a Authority, rows map[RecordKey][]byte) Code {
	_, w, code := workerFromRows(c.Cluster, c.Actor, now, identity, rows)
	if code != OK {
		return code
	}
	if v := c.Change.RegisterWorker; v != nil {
		// A successful registration changes the incarnation itself. Admit its
		// exact predecessor retry to dedup, but never a superseded generation.
		if c.WorkerIncarnation == w.Incarnation || v.Previous == c.WorkerIncarnation && v.Previous < ^uint64(0) && v.Incarnation == v.Previous+1 && v.Incarnation == w.Incarnation {
			return OK
		}
		return Conflict
	}
	if c.WorkerIncarnation != w.Incarnation {
		return Conflict
	}
	if w.Incarnation == 0 {
		return Conflict
	}
	if c.Change.CloseSession != nil {
		return OK
	}
	if c.Change.DrainWorker != nil {
		if c.Change.DrainWorker.Worker == c.Actor {
			return OK
		}
		return Unauthorized
	}
	if v := c.Change.WorkerBeatBatch; v != nil {
		for _, h := range v.Items {
			if h.Worker != c.Actor {
				return Unauthorized
			}
		}
		return OK
	}
	if c.Change.ClaimAssignment != nil {
		r := c.Change.ClaimAssignment.Ref
		if r.Target != c.Actor || r.Incarnation != w.Incarnation || !a.Worker.Allows(r.Cell) {
			return Unauthorized
		}
		return OK
	}
	if c.Change.FinishAssignment != nil {
		r := c.Change.FinishAssignment.Ref
		if r.Target != c.Actor || r.Incarnation != w.Incarnation || !a.Worker.Allows(r.Cell) {
			return Unauthorized
		}
		return OK
	}
	// Ownership acquisition uses only the committed conditional claim.
	// Drain does not block saving/releasing a fence already held by this worker.
	if c.Change.Publish == nil && c.Change.Beat == nil && c.Change.BeatBatch == nil && c.Change.Release == nil {
		return Unauthorized
	}
	for _, cell := range cellIDs(c.Change) {
		if !a.Worker.Allows(cell) {
			return Unauthorized
		}
		if b := rows[WorkerHoldKey(c.Actor, cell)]; b != nil {
			var hold WorkerHold
			if Decode(b, &hold) != nil {
				return BadRequest
			}
			if hold.Incarnation != w.Incarnation {
				return Conflict
			}
		}
	}
	if c.Change.Publish != nil && c.Change.Publish.V1 {
		return Unauthorized
	}
	return OK
}
func applyWorker(c Command, now int64, identity IdentityRec, rows map[RecordKey][]byte, put func(RecordKey, any), result *Result) Code {
	if v := c.Change.EnrollWorker; v != nil {
		cert := v.Certificate
		id := cert.DeviceID()
		if VerifyWorkerCertificate(cert, identity.SigningKey, c.Cluster, now) != nil || len(v.Grants) != len(cert.Cells) {
			return BadRequest
		}
		for _, cell := range cert.Cells {
			if len(v.Grants[cell]) == 0 || len(v.Grants[cell]) > MaxWorkerSealedBytes {
				return BadRequest
			}
		}
		var d Device
		if b := rows[DeviceKey(id)]; b != nil {
			if Decode(b, &d) != nil {
				return BadRequest
			}
			if d.Revoked {
				return Revoked
			}
		}
		if b := rows[AuthorityKey(id)]; b != nil {
			var a Authority
			var w WorkerRecord
			if Decode(b, &a) != nil || Decode(rows[WorkerKey(id)], &w) != nil {
				return Conflict
			}
			old, _ := Encode(a.Worker)
			incoming, _ := Encode(&cert)
			grants, _ := Encode(w.Grants)
			want, _ := Encode(v.Grants)
			if !a.ValidRole() || a.Worker == nil || !bytes.Equal(old, incoming) || !bytes.Equal(grants, want) {
				return Conflict
			}
			return OK
		}
		var registry WorkerRegistry
		if b := rows[WorkerRegistryKey()]; b != nil && Decode(b, &registry) != nil {
			return BadRequest
		}
		if registry.Retained >= MaxWorkerRecords {
			return Capacity
		}
		registry.Retained++
		put(WorkerRegistryKey(), registry)
		put(AuthorityKey(id), Authority{Cluster: c.Cluster, Key: cert.Key, Worker: &cert})
		put(WorkerKey(id), WorkerRecord{Grants: v.Grants})
		put(DeviceKey(id), Stamped(d, rows[DeviceKey(id)] != nil, Device{V: 1}, now))
		return OK
	}
	if v := c.Change.RegisterWorker; v != nil {
		_, w, code := workerFromRows(c.Cluster, c.Actor, now, identity, rows)
		if code != OK {
			return code
		}
		if v.Previous != w.Incarnation || w.Incarnation == ^uint64(0) || v.Incarnation != w.Incarnation+1 {
			return Conflict
		}
		if !publicCaps(v.Caps) || len(v.SealedLabels) > MaxWorkerSealedBytes {
			return BadRequest
		}
		w.Incarnation = v.Incarnation
		w.Heartbeat = 0
		w.HeartbeatDigest = ""
		w.SeenAt = 0
		w.AvailableUntil = 0
		w.Work = nil
		w.Caps = v.Caps
		w.SealedLabels = v.SealedLabels
		put(WorkerKey(c.Actor), w)
		result.Workers = []WorkerResult{{Worker: c.Actor, Code: OK, Incarnation: w.Incarnation, Draining: w.Draining}}
		return OK
	}
	if v := c.Change.DrainWorker; v != nil {
		_, w, code := workerFromRows(c.Cluster, v.Worker, now, identity, rows)
		if code != OK {
			return code
		}
		if w.Incarnation == 0 || v.Incarnation != w.Incarnation {
			return Conflict
		}
		w.Draining = true
		put(WorkerKey(v.Worker), w)
		result.Workers = []WorkerResult{{Worker: v.Worker, Code: OK, Incarnation: w.Incarnation, AvailableUntil: w.AvailableUntil, Draining: true}}
		return OK
	}
	if v := c.Change.WorkerBeatBatch; v != nil {
		if len(v.Items) == 0 || len(v.Items) > MaxWorkerBeatItems {
			return BadRequest
		}
		seen := map[string]bool{}
		for _, h := range v.Items {
			if h.Worker == "" || seen[h.Worker] || len(h.Work) > MaxWorkerScopes {
				return BadRequest
			}
			seen[h.Worker] = true
		}
		for _, h := range v.Items {
			a, w, code := workerFromRows(c.Cluster, h.Worker, now, identity, rows)
			if code == OK {
				code = checkWorkerHeartbeat(c.Cluster, h, now, a, w, rows)
			}
			item := WorkerResult{Worker: h.Worker, Code: code}
			if code == OK {
				w.Heartbeat = h.Sequence
				w.HeartbeatDigest = workerHeartbeatDigest(c.Cluster, h)
				w.SeenAt = h.SeenAt
				w.AvailableUntil = min(h.SeenAt+LeaseTTLMillis, a.Worker.Expires)
				w.Work = h.Work
				put(WorkerKey(h.Worker), w)
				item.Incarnation = w.Incarnation
				item.AvailableUntil = w.AvailableUntil
				item.Draining = w.Draining
			}
			result.Workers = append(result.Workers, item)
		}
		return OK
	}
	return BadRequest
}
func checkWorkerHeartbeat(cluster string, h WorkerHeartbeat, now int64, a Authority, w WorkerRecord, rows map[RecordKey][]byte) Code {
	if w.Incarnation == 0 || h.Incarnation != w.Incarnation || h.Sequence == 0 || h.Sequence < w.Heartbeat {
		return Conflict
	}
	b, err := WorkerHeartbeatBytes(cluster, h)
	if err != nil || !ed25519.Verify(a.Key, b, h.Signature) {
		return Unauthorized
	}
	// Exact evidence may be replayed after its freshness window, but keeps
	// the original deadline. A different proof cannot reuse that sequence.
	if h.Sequence == w.Heartbeat {
		if w.HeartbeatDigest != "" && w.HeartbeatDigest == workerHeartbeatDigest(cluster, h) {
			return OK
		}
		return Conflict
	}
	if h.SeenAt <= 0 || h.SeenAt > now || now-h.SeenAt > EvidenceWindowMillis || h.SeenAt < w.SeenAt {
		return BadRequest
	}
	seen := map[string]bool{}
	for _, work := range h.Work {
		if !a.Worker.Allows(work.Cell) {
			return Unauthorized
		}
		if seen[work.Cell] || work.Fence == 0 {
			return BadRequest
		}
		seen[work.Cell] = true
		var cell Cell
		if Decode(rows[CellKey(work.Cell)], &cell) != nil {
			return NotFound
		}
		if cell.Lease.Device != h.Worker || cell.Lease.Fence != work.Fence {
			return FenceStale
		}
	}
	return OK
}

// Public capability values are closed vocabularies. Free text such as a host
// label, repository path, GPU model or scheduling requirement stays sealed.
func publicCaps(c Caps) bool {
	allowed := func(s string, values ...string) bool {
		for _, v := range values {
			if s == v {
				return true
			}
		}
		return false
	}
	pointer := func(s *string, values ...string) bool { return s == nil || allowed(*s, values...) }
	return allowed(c.OS, "", "linux", "darwin", "windows", "ios", "android", "other") && allowed(c.Arch, "", "amd64", "arm64", "386", "arm", "wasm", "other") && pointer(c.Sandbox, "seatbelt", "landlock") && pointer(c.Container, "none", "container") && pointer(c.GPU, "none", "present") && allowed(c.Cow, "", "apfs", "btrfs", "xfs-reflink", "overlayfs", "none")
}

// WorkerCanClaim is the common P16 prerequisite at the claim's log position.
// A registry row, socket or downloaded grant alone never starts work.
func WorkerCanClaim(cluster, worker, cell string, incarnation uint64, now int64, identity IdentityRec, a Authority, d Device, w WorkerRecord) Code {
	if code := workerAuthority(cluster, worker, now, identity, a, d); code != OK {
		return code
	}
	if !a.Worker.Allows(cell) {
		return Unauthorized
	}
	if incarnation == 0 || incarnation != w.Incarnation || w.Draining || w.AvailableUntil <= now {
		return Conflict
	}
	return OK
}

// WorkerCanRead admits only the selected chat at the current committed
// incarnation. Staging is allowed before claiming; availability and draining
// govern new claims, not retrieval or saving work already held.
func WorkerCanRead(cluster, worker, cell string, incarnation uint64, now int64, identity IdentityRec, a Authority, d Device, w WorkerRecord) Code {
	if code := workerAuthority(cluster, worker, now, identity, a, d); code != OK {
		return code
	}
	if !a.Worker.Allows(cell) {
		return Unauthorized
	}
	if incarnation == 0 || incarnation != w.Incarnation {
		return Conflict
	}
	return OK
}

func workerHeartbeatDigest(cluster string, h WorkerHeartbeat) string {
	b, _ := WorkerHeartbeatBytes(cluster, h)
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:])
}
