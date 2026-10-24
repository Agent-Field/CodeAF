package clusterstate

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"sort"
)

const (
	MaxEvaluationBytes            = 1 << 20
	MaxCommandBytes               = 64 << 10
	MaxLiveSessions               = 128
	MaxRetainedSessions           = 4096
	DefaultApplicationBytes       = 8 << 20
	MinGraceMillis          int64 = 3_600_000
	MaxGraceMillis          int64 = 30 * 24 * MinGraceMillis
	DefaultGraceMillis      int64 = 7 * 24 * MinGraceMillis
)

var ErrDependency = errors.New("clusterstate: incomplete or invalid committed read-set")

// RecordKey separates durable policy, application rows and retained responses.
// Hex segments keep arbitrary opaque IDs from aliasing another record's key.
type RecordKey struct {
	Namespace string `json:"namespace"`
	Key       string `json:"key"`
}
type RecordWrite struct {
	RecordKey
	Data []byte `json:"data"`
}
type ReadRecord func(RecordKey) ([]byte, error)

func segment(s string) string          { return hex.EncodeToString([]byte(s)) }
func IdentityKey() RecordKey           { return RecordKey{"state", "identity"} }
func DeviceKey(id string) RecordKey    { return RecordKey{"state", "device/" + segment(id)} }
func CellKey(id string) RecordKey      { return RecordKey{"state", "cell/" + segment(id)} }
func AuthorityKey(id string) RecordKey { return RecordKey{"enrollment", "actor/" + segment(id)} }
func SessionKey(actor, session string) RecordKey {
	return RecordKey{"sessions", segment(actor) + "/" + segment(session)}
}
func SessionsKey() RecordKey        { return RecordKey{"sessions", "counts"} }
func JoinKey(code string) RecordKey { return RecordKey{"outbox", "join/" + segment(code)} }

// Authority is installed by authenticated genesis/enrollment, never PutDevice.
// Personal directory authority and restricted effect authority are exclusive.
// Delegated worker scopes are admitted separately.
type Authority struct {
	Cluster  string `json:"cluster"`
	Key      []byte `json:"key"`
	Personal bool   `json:"personal"`
	Effects  bool   `json:"effects,omitempty"`
	Anchor   bool   `json:"anchor,omitempty"`
}
type session struct {
	Sequence uint64 `json:"sequence"`
	Digest   string `json:"digest"`
	Closed   bool   `json:"closed"`
	Result   Result `json:"result"`
}
type sessionCounts struct {
	Live     uint32 `json:"live"`
	Retained uint32 `json:"retained"`
}

// JoinRequest is a committed dependency from a reserved public request.
// An absent dependency cannot be manufactured by a decision request.
type JoinRequest struct {
	Device    string        `json:"device"`
	Expires   int64         `json:"expires"`
	Platform  string        `json:"platform,omitempty"`
	PublicKey []byte        `json:"public_key,omitempty"`
	Decision  *JoinDecision `json:"decision,omitempty"`
	Outcome   string        `json:"outcome,omitempty"`
}

// ApplicationMeta is an immutable view at one applied index. Byte accounting
// includes record framing and keys; capacity is a refusal, never an eviction.
type ApplicationMeta struct {
	Term          uint64        `json:"term,omitempty"`
	Index         uint64        `json:"index"`
	Configuration Configuration `json:"configuration"`
	Cluster       string        `json:"cluster"`
	Now           int64         `json:"now"`
	Version       uint64        `json:"version"`
	Bytes         uint64        `json:"bytes"`
	Ceiling       uint64        `json:"ceiling"`
}
type Evaluation struct {
	Now     int64         `json:"now"`
	Version uint64        `json:"version"`
	Writes  []RecordWrite `json:"writes"`
	Result  Result        `json:"result"`
}

// Digest excludes time, signature and acknowledgement. A retry may arrive at a
// newer ingress time; its operation and durable sequence must remain identical.
func Digest(c Command) (string, error) {
	b, err := Encode(struct {
		Cluster  string   `json:"cluster"`
		Actor    string   `json:"actor"`
		Session  string   `json:"session"`
		Sequence uint64   `json:"sequence"`
		Change   Mutation `json:"change"`
	}{c.Cluster, c.Actor, c.Session, c.Sequence, c.Change})
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// SigningBytes binds all routing, sequence and time fields to this protocol.
func SigningBytes(c Command) ([]byte, error) {
	c.Signature = nil
	b, err := EncodeCommand(c)
	return append([]byte("codeaf.directory.command.v1\x00"), b...), err
}
func Sign(c Command, key ed25519.PrivateKey) (Command, error) {
	var err error
	c.RequestDigest, err = Digest(c)
	if err != nil {
		return c, err
	}
	b, err := SigningBytes(c)
	if err != nil {
		return c, err
	}
	c.Signature = ed25519.Sign(key, b)
	return c, nil
}

// Dependencies declares every possible row before evaluation. Hosts may page
// these immutable records, but must distinguish absent rows from unread rows.
func Dependencies(c Command) []RecordKey {
	keys := []RecordKey{IdentityKey(), DeviceKey(c.Actor), AuthorityKey(c.Actor), SessionKey(c.Actor, c.Session), SessionsKey()}
	m := c.Change
	keys = append(keys, membershipDependencies(m)...)
	if m.PutDevice != nil {
		keys = append(keys, DeviceKey(m.PutDevice.ID))
	}
	if m.Revoke != nil {
		keys = append(keys, DeviceKey(m.Revoke.Device))
	}
	if m.DeviceSeen != nil {
		keys = append(keys, DeviceKey(m.DeviceSeen.Device))
	}
	if m.RecordUpload != nil {
		keys = append(keys, UploadKey(UploadID(*m.RecordUpload)), DeviceKey(m.RecordUpload.Device), AuthorityKey(m.RecordUpload.Device))
	}
	if m.Create != nil {
		keys = append(keys, UploadKey(m.Create.Receipt))
	}
	if m.Publish != nil {
		keys = append(keys, UploadKey(m.Publish.Receipt))
	}
	if m.AdmitJoin != nil {
		keys = append(keys, JoinKey(m.AdmitJoin.Code))
	}
	if m.CompleteJoin != nil {
		keys = append(keys, JoinKey(m.CompleteJoin.Code))
	}
	if m.DecideJoin != nil {
		keys = append(keys, JoinKey(m.DecideJoin.Code), DeviceKey(m.DecideJoin.Device), AuthorityKey(m.DecideJoin.Device))
	}
	for _, id := range cellIDs(m) {
		keys = append(keys, CellKey(id))
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Namespace != keys[j].Namespace {
			return keys[i].Namespace < keys[j].Namespace
		}
		return keys[i].Key < keys[j].Key
	})
	out := keys[:0]
	for _, k := range keys {
		if len(out) == 0 || out[len(out)-1] != k {
			out = append(out, k)
		}
	}
	return out
}
func cellIDs(m Mutation) []string {
	switch {
	case m.Create != nil:
		return []string{m.Create.Cell}
	case m.Acquire != nil:
		return []string{m.Acquire.Cell}
	case m.Beat != nil:
		return []string{m.Beat.Cell}
	case m.Publish != nil:
		return []string{m.Publish.Cell}
	case m.Release != nil:
		return []string{m.Release.Cell}
	case m.Archive != nil:
		return []string{m.Archive.Cell}
	case m.BeatBatch != nil:
		out := []string{}
		for _, b := range m.BeatBatch.Items {
			out = append(out, b.Cell)
		}
		return out
	}
	return nil
}
func RecordBytes(k RecordKey, b []byte) uint64 {
	return uint64(12 + len(k.Namespace) + len(k.Key) + len(b))
}

// Evaluate runs only after commitment against the host's immutable applied view.
// Policy refusals are ordinary persisted results. Invalid schemas/read-sets are
// host errors and stop application rather than pretending an entry succeeded.
func Evaluate(raw []byte, meta ApplicationMeta, read ReadRecord) (Evaluation, error) {
	out := Evaluation{Now: meta.Now, Version: meta.Version, Writes: []RecordWrite{}, Result: Result{Schema: SchemaVersion, Code: Unauthorized, Now: meta.Now, Version: meta.Version}}
	if len(raw) > MaxCommandBytes {
		return out, ErrCodec
	}
	c, err := DecodeCommand(raw)
	if err != nil {
		return out, err
	}
	rows := map[RecordKey][]byte{}
	n := 0
	for _, k := range Dependencies(c) {
		b, e := read(k)
		if e != nil {
			return out, e
		}
		n += len(k.Namespace) + len(k.Key) + len(b)
		if n > MaxEvaluationBytes {
			return out, ErrDependency
		}
		rows[k] = bytes.Clone(b)
	}

	// Corrupt dependency bytes are storage evidence, never a not-found or policy
	// refusal. Validate every declared present row before interpreting any rule.
	for k, b := range rows {
		if b == nil {
			continue
		}
		var target any
		switch {
		case k == IdentityKey():
			target = new(IdentityRec)
		case k.Namespace == "state" && len(k.Key) >= 7 && k.Key[:7] == "device/":
			target = new(Device)
		case k.Namespace == "state" && len(k.Key) >= 5 && k.Key[:5] == "cell/":
			target = new(Cell)
		case k == MembershipKey():
			var h Membership
			if Decode(b, &h) != nil || h.Validate() != nil {
				return out, ErrDependency
			}
			target = new(Membership)
		case k.Namespace == "membership":
			target = new(DeviceMember)
		case k.Namespace == "tombstones" || k.Namespace == "enrollment" && len(k.Key) > 5 && k.Key[:5] == "node/":
			target = new(MemberRecord)
		case k.Namespace == "enrollment":
			target = new(Authority)
		case k == SessionsKey():
			target = new(sessionCounts)
		case k.Namespace == "sessions":
			target = new(session)
		case k.Namespace == "outbox" && len(k.Key) >= 7 && k.Key[:7] == "upload/":
			target = new(UploadReceipt)
		case k.Namespace == "outbox":
			target = new(JoinRequest)
		default:
			return out, ErrDependency
		}
		if Decode(b, target) != nil {
			return out, ErrDependency
		}
	}
	if meta.Now < 0 || meta.Now > int64(MaxSafeInteger)-LeaseTTLMillis {
		return out, ErrDependency
	}
	decode := func(k RecordKey, v any) error {
		b, ok := rows[k]
		if !ok {
			return ErrDependency
		}
		if b == nil {
			return nil
		}
		return Decode(b, v)
	}
	var auth Authority
	var actor Device
	var identity IdentityRec
	if decode(AuthorityKey(c.Actor), &auth) != nil || decode(DeviceKey(c.Actor), &actor) != nil || decode(IdentityKey(), &identity) != nil {
		return out, ErrDependency
	}
	digest, _ := Digest(c)
	signed, e := SigningBytes(c)
	if e != nil || c.Cluster != meta.Cluster || auth.Cluster != c.Cluster || (!auth.Personal && !auth.Effects && !auth.Anchor) || len(auth.Key) != ed25519.PublicKeySize || !ed25519.Verify(auth.Key, signed, c.Signature) || digest != c.RequestDigest {
		return out, nil
	}
	// Revocation precedes dedup: a saved old response cannot authorize a revoked
	// actor to resume activity. Other accepted retries preserve exact old bytes.
	if actor.Revoked {
		out.Result.Code = Revoked
		return out, nil
	}
	service := c.Change.RecordUpload != nil || c.Change.AdmitJoin != nil || c.Change.CompleteJoin != nil
	if service != auth.Effects || auth.Personal && auth.Effects || auth.Anchor && (auth.Personal || auth.Effects || c.Change.SettleMembership == nil && c.Change.SettleLeadership == nil && c.Change.CompleteLeadership == nil && c.Change.RestoreAnchors == nil && c.Change.CloseSession == nil) {
		return out, nil
	}
	if (c.Change.RestoreAnchors != nil || c.Change.CompleteLeadership != nil) && !auth.Anchor {
		return out, nil
	}
	var last session
	var counts sessionCounts
	if decode(SessionKey(c.Actor, c.Session), &last) != nil || decode(SessionsKey(), &counts) != nil {
		return out, ErrDependency
	}
	known := rows[SessionKey(c.Actor, c.Session)] != nil
	refuse := func(code Code) (Evaluation, error) { out.Result.Code = code; return out, nil }
	if known && (last.Closed || c.Sequence < last.Sequence || c.Sequence == last.Sequence && c.RequestDigest != last.Digest) {
		return refuse(Conflict)
	}
	if known && c.Sequence == last.Sequence {
		out.Result = last.Result
		return out, nil
	}
	if !service && (!known && (c.Sequence != 1 || c.Ack != 0) || known && (last.Sequence == ^uint64(0) || c.Sequence != last.Sequence+1 || c.Ack != last.Sequence)) {
		return refuse(Conflict)
	}
	if !service && !known && (counts.Live >= MaxLiveSessions || counts.Retained >= MaxRetainedSessions) {
		return refuse(Capacity)
	}
	if c.Now < 0 || c.Now > int64(MaxSafeInteger)-LeaseTTLMillis {
		return refuse(BadRequest)
	}
	out.Now = max(meta.Now, c.Now)
	out.Result.Now = out.Now
	writes := map[RecordKey][]byte{}
	put := func(k RecordKey, v any) {
		b, e := Encode(v)
		if e != nil {
			err = e
			return
		}
		writes[k] = b
	}
	code := OK
	if identity.Rotation != nil && c.Change.Rotate == nil && c.Change.AdvanceTime == nil && c.Change.DeviceSeen == nil && c.Change.CloseSession == nil && c.Change.CompleteJoin == nil {
		code = Rotated
	} else {
		if service {
			code = applyEffect(c, out.Now, rows, put)
		} else {
			if membershipMutation(c.Change) {
				code = applyMembership(c, meta, rows, put, &out.Result)
			} else {
				code = applyMutation(c, out.Now, identity, rows, put, &out.Result)
			}
		}
	}
	if err != nil {
		return out, err
	}
	changed := false
	for k, b := range writes {
		if k.Namespace == "state" && visibleChange(k, rows[k], b, out.Now) {
			changed = true
		}
	}
	if c.Change.DeviceSeen != nil {
		changed = false
	}
	if changed {
		if meta.Version == ^uint64(0) {
			return refuse(Capacity)
		}
		out.Version++
	}
	out.Result.Code = code
	out.Result.Version = out.Version
	if code != OK {
		writes = map[RecordKey][]byte{}
		out.Version = meta.Version
		out.Result.Version = meta.Version
	}
	if !known {
		counts.Live++
		counts.Retained++
	}
	if c.Change.CloseSession != nil {
		counts.Live--
	}
	save := func() {
		if service {
			return
		}
		put(SessionKey(c.Actor, c.Session), session{Sequence: c.Sequence, Digest: c.RequestDigest, Closed: c.Change.CloseSession != nil, Result: out.Result})
		put(SessionsKey(), counts)
	}
	save()

	fits := func() bool {
		size := meta.Bytes
		budget := 0
		for k, b := range writes {
			cost := RecordBytes(k, b)
			budget += int(cost)
			if old := rows[k]; old != nil {
				oldSize := RecordBytes(k, old)
				if oldSize > size {
					return false
				}
				size -= oldSize
			}
			size += cost
		}
		ceiling := meta.Ceiling
		if ceiling == 0 {
			ceiling = DefaultApplicationBytes
		}
		return budget <= MaxEvaluationBytes && (size <= ceiling || size <= meta.Bytes)
	}
	if !fits() {
		writes = map[RecordKey][]byte{}
		out.Version = meta.Version
		out.Result = Result{Schema: SchemaVersion, Code: Capacity, Now: out.Now, Version: meta.Version}
		save()
		if !fits() {
			return out, ErrDependency
		}
	}
	for k, b := range writes {
		out.Writes = append(out.Writes, RecordWrite{k, b})
	}
	sort.Slice(out.Writes, func(i, j int) bool {
		a, b := out.Writes[i].RecordKey, out.Writes[j].RecordKey
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Key < b.Key
	})
	return out, err
}

func applyMutation(c Command, now int64, identity IdentityRec, rows map[RecordKey][]byte, put func(RecordKey, any), result *Result) Code {
	m := c.Change
	device := func(id string) (Device, bool) {
		var d Device
		b := rows[DeviceKey(id)]
		if b == nil {
			return d, false
		}
		if Decode(b, &d) != nil {
			return d, false
		}
		return d, true
	}
	switch {
	case m.AdvanceTime != nil, m.CloseSession != nil:
		return OK
	case m.PutDevice != nil:
		v := m.PutDevice
		old, had := device(v.ID)
		put(DeviceKey(v.ID), Stamped(old, had, v.Device, now))
		return OK
	case m.Revoke != nil:
		v := m.Revoke
		old, had := device(v.Device)
		if !had {
			return NotFound
		}
		next, err := RevokeOf(old, c.Actor, v.Device)
		if err != nil {
			return SelfRevoke
		}
		put(DeviceKey(v.Device), next)
		return OK
	case m.SetVault != nil:
		v := m.SetVault
		if identity.Vault != v.Old {
			return CAS
		}
		identity.Vault = v.New
		put(IdentityKey(), identity)
		return OK
	case m.DeviceSeen != nil:
		if m.DeviceSeen.Device != c.Actor {
			return Unauthorized
		}
		d, had := device(c.Actor)
		if had {
			d.LastSeen = now
			put(DeviceKey(c.Actor), d)
		}
		return OK
	case m.Rotate != nil:
		v := m.Rotate
		r := identity.Rotation
		switch v.Op {
		case "freeze":
			if r == nil {
				r = &Rotation{State: "frozen", By: c.Actor, At: now}
			} else if r.By != c.Actor {
				return Rotated
			}
		case "thaw":
			if r != nil && r.State == "retired" {
				return Rotated
			}
			r = nil
		case "retire":
			if r == nil {
				return RotationStep
			}
			if r.By != c.Actor {
				return Rotated
			}
			if r.State != "retired" {
				g := v.GraceMS
				if g == 0 {
					g = DefaultGraceMillis
				}
				if g < MinGraceMillis || g > MaxGraceMillis || now > int64(MaxSafeInteger)-g {
					return BadGrace
				}
				r = &Rotation{State: "retired", By: r.By, At: r.At, RetireAt: now + g}
			}
		default:
			return BadRequest
		}
		identity.Rotation = r
		put(IdentityKey(), identity)
		return OK
	case m.DecideJoin != nil:
		v := m.DecideJoin
		var request JoinRequest
		b := rows[JoinKey(v.Code)]
		if b == nil {
			return RequestGone
		}
		if Decode(b, &request) != nil {
			return BadRequest
		}
		if request.Decision != nil {
			if reflect.DeepEqual(*request.Decision, *v) {
				return OK
			}
			return AlreadyDecided
		}
		if request.Expires <= now {
			return RequestGone
		}
		if !v.Deny && (request.Device != v.Device || v.Grant == "") {
			return BadRequest
		}
		if !v.Deny {
			old, had := device(v.Device)
			if old.Revoked {
				return Revoked
			}
			public, e := JoinCertificate(identity, request, v.Cert)
			if e != nil || validGrant(v.Grant) == false {
				return BadRequest
			}
			var prior Authority
			if b := rows[AuthorityKey(v.Device)]; b != nil {
				if Decode(b, &prior) != nil || prior.Cluster != c.Cluster || !prior.Personal || !bytes.Equal(prior.Key, public) {
					return Unauthorized
				}
			}
			put(AuthorityKey(v.Device), Authority{Cluster: c.Cluster, Key: public, Personal: true})
			record := v.Record
			if record.Platform == "" {
				record.Platform = request.Platform
			}
			record.AddedBy = identity.Identity
			put(DeviceKey(v.Device), Stamped(old, had, record, now))
		}
		request.Decision = v
		put(JoinKey(v.Code), request)
		return OK
	case m.BeatBatch != nil:
		batchRows := map[RecordKey][]byte{}
		for k, b := range rows {
			batchRows[k] = b
		}
		for _, b := range m.BeatBatch.Items {
			sub := c
			sub.Change = Mutation{Beat: &b}
			r := Result{}
			code := applyMutation(sub, now, identity, batchRows, func(k RecordKey, v any) { put(k, v); data, _ := Encode(v); batchRows[k] = data }, &r)
			result.Items = append(result.Items, ItemResult{Cell: b.Cell, Code: code, View: r.View})
		}
		return OK
	}
	ids := cellIDs(m)
	if len(ids) != 1 || ids[0] == "" {
		return BadRequest
	}
	id := ids[0]
	var cell Cell
	b := rows[CellKey(id)]
	if b != nil && Decode(b, &cell) != nil {
		return BadRequest
	}
	if m.Create != nil {
		if b != nil {
			return Exists
		}
		if !m.Create.V1 && !validReceipt(rows, m.Create.Receipt, c.Actor, m.Create.Init.Head, m.Create.Init.Frames) {
			return BadRequest
		}
		cell = Created(m.Create.Init, c.Actor, now)
	} else {
		if b == nil {
			return NotFound
		}
		var err error
		switch {
		case m.Acquire != nil:
			v := m.Acquire
			var expect *LeaseExpectation = &v.Expect
			force := v.Force
			if v.Legacy {
				expect = nil
				force = false
			}
			oldFence := cell.Lease.Fence
			cell, err = Acquire(cell, c.Actor, now, force, expect)
			if err == nil && (oldFence == ^uint64(0) || v.V1 && oldFence >= MaxSafeInteger) {
				return Capacity
			}
		case m.Beat != nil:
			cell, err = Heartbeat(cell, c.Actor, m.Beat.Beat, now)
		case m.Publish != nil:
			cell, err = PublishTo(cell, c.Actor, m.Publish.Publish, now)
			if err == nil && !m.Publish.V1 && !validReceipt(rows, m.Publish.Receipt, c.Actor, m.Publish.Publish.Head, m.Publish.Publish.Frames) {
				return BadRequest
			}
		case m.Release != nil:
			at := now
			if m.Release.Quiet {
				at = 0
			}
			cell, err = ReleaseOf(cell, c.Actor, m.Release.Fence, at)
		case m.Archive != nil:
			cell.Archived = true
		default:
			return BadRequest
		}
		switch err {
		case ErrLeaseHeld:
			return LeaseHeld
		case ErrFenceStale:
			return FenceStale
		case ErrHeadMoved:
			return HeadMoved
		}
		if err != nil {
			return BadRequest
		}
	}
	put(CellKey(id), cell)
	result.View = &CellView{Now: now, Cell: cell}
	return OK
}

// Visible versions follow the existing projection: renewed expiry within the
// same held state and advisory last-seen timestamps do not repaint Home.
func visibleChange(k RecordKey, old, next []byte, now int64) bool {
	if old == nil {
		return true
	}
	switch {
	case len(k.Key) > 5 && k.Key[:5] == "cell/":
		var a, b Cell
		if Decode(old, &a) != nil || Decode(next, &b) != nil {
			return true
		}
		if a.Lease.Expires > now {
			a.Lease.Expires = 1
		} else {
			a.Lease.Expires = 0
		}
		if b.Lease.Expires > now {
			b.Lease.Expires = 1
		} else {
			b.Lease.Expires = 0
		}
		return !reflect.DeepEqual(a, b)
	case len(k.Key) > 7 && k.Key[:7] == "device/":
		var a, b Device
		if Decode(old, &a) != nil || Decode(next, &b) != nil {
			return true
		}
		a.LastSeen = 0
		b.LastSeen = 0
		return !reflect.DeepEqual(a, b)
	default:
		return !bytes.Equal(old, next)
	}
}
