package clusterstate

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"reflect"
)

const (
	MaxUploadFrames = 512
	MaxJoinGrant    = 4096
)

// UploadReceipt names only durable references. The host checks the encrypted
// store before proposing it; neither a personal device nor a worker can mint it.
// Frames remain the existing incremental hint, not a plaintext closure manifest.
type UploadReceipt struct {
	Cell              string   `json:"cell,omitempty"`
	WorkerIncarnation uint64   `json:"worker_incarnation,omitempty"`
	Device            string   `json:"device"`
	Head              string   `json:"head"`
	HeadRID           string   `json:"head_rid"`
	Frames            []string `json:"frames,omitempty"`
}
type AdmitJoin struct {
	Code      string `json:"code"`
	PublicKey []byte `json:"public_key"`
	Expires   int64  `json:"expires"`
	Platform  string `json:"platform,omitempty"`
}
type CompleteJoin struct {
	Code     string `json:"code"`
	Decision string `json:"decision"`
	Outcome  string `json:"outcome"`
}

func UploadKey(id string) RecordKey { return RecordKey{"outbox", "upload/" + segment(id)} }
func UploadID(r UploadReceipt) string {
	b, _ := Encode(r)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func DecisionID(d JoinDecision) string {
	b, _ := Encode(d)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func DeviceIDOfKey(public []byte) string {
	h := sha256.Sum256(public)
	return "dev_" + hex.EncodeToString(h[:16])
}
func IdentityID(public []byte) string {
	h := sha256.Sum256(public)
	return "id_" + hex.EncodeToString(h[:16])
}

func validReceipt(rows map[RecordKey][]byte, id, actor, head string, frames []string, cell string, incarnation uint64) bool {
	var r UploadReceipt
	return id != "" && Decode(rows[UploadKey(id)], &r) == nil && UploadID(r) == id && r.Device == actor && r.Head == head && (incarnation == 0 || r.Cell == cell && r.WorkerIncarnation == incarnation) && reflect.DeepEqual(normalFrames(r.Frames), normalFrames(frames))
}
func normalFrames(f []string) []string {
	if len(f) == 0 {
		return nil
	}
	return f
}

// JoinCertificate verifies the original identity certificate at the committed
// decision, rather than trusting ingress that may have raced revocation.
func JoinCertificate(identity IdentityRec, request JoinRequest, text string) ([]byte, error) {
	var cert struct {
		V       uint16 `json:"V"`
		Device  string `json:"device"`
		Created int64  `json:"created"`
		Sig     string `json:"sig"`
	}
	raw, e := base64.RawURLEncoding.DecodeString(text)
	if e != nil || len(raw) > 1024 || json.Unmarshal(raw, &cert) != nil || cert.V != 1 || cert.Created < 0 || cert.Created > int64(MaxSafeInteger) {
		return nil, ErrCodec
	}
	public, e := hex.DecodeString(cert.Device)
	sig, se := hex.DecodeString(cert.Sig)
	if e != nil || se != nil || len(public) != ed25519.PublicKeySize || !bytes.Equal(public, request.PublicKey) || DeviceIDOfKey(public) != request.Device || len(identity.SigningKey) != ed25519.PublicKeySize || IdentityID(identity.SigningKey) != identity.Identity {
		return nil, ErrCodec
	}
	cert.Sig = ""
	body, _ := json.Marshal(cert)
	if !ed25519.Verify(identity.SigningKey, append([]byte("codeaf/device-cert/v1\n"), body...), sig) {
		return nil, ErrCodec
	}
	return public, nil
}
func validGrant(text string) bool {
	if len(text) > base64.RawURLEncoding.EncodedLen(MaxJoinGrant) {
		return false
	}
	b, e := base64.RawURLEncoding.DecodeString(text)
	return e == nil && len(b) > 0 && len(b) <= MaxJoinGrant
}

// Effects use content-addressed, immutable keys rather than expiring request
// sessions. Replaying delivery after a lost acknowledgement changes no grant.
func applyEffect(c Command, now int64, rows map[RecordKey][]byte, put func(RecordKey, any)) Code {
	switch {
	case c.Change.RecordUpload != nil:
		r := *c.Change.RecordUpload
		if r.Device == "" || r.Head == "" || r.HeadRID == "" || len(r.Frames) > MaxUploadFrames {
			return BadRequest
		}
		var d Device
		var auth Authority
		if b := rows[DeviceKey(r.Device)]; b != nil {
			if Decode(b, &d) != nil {
				return BadRequest
			}
		}
		if d.Revoked {
			return Revoked
		}
		if Decode(rows[AuthorityKey(r.Device)], &auth) != nil || !auth.ValidRole() || auth.Cluster != c.Cluster || !auth.Personal && auth.Worker == nil {
			return Unauthorized
		}
		if auth.Worker != nil {
			var identity IdentityRec
			if Decode(rows[IdentityKey()], &identity) != nil {
				return BadRequest
			}
			a, w, code := workerFromRows(c.Cluster, r.Device, now, identity, rows)
			if code != OK {
				return code
			}
			if r.WorkerIncarnation == 0 || r.WorkerIncarnation != w.Incarnation {
				return Conflict
			}
			if !a.Worker.Allows(r.Cell) {
				return Unauthorized
			}
		}
		k := UploadKey(UploadID(r))
		if rows[k] == nil {
			put(k, r)
		}
		return OK
	case c.Change.AdmitJoin != nil:
		v := c.Change.AdmitJoin
		if v.Code == "" || len(v.PublicKey) != ed25519.PublicKeySize || v.Expires <= now || v.Expires > int64(MaxSafeInteger) {
			return BadRequest
		}
		k := JoinKey(v.Code)
		if raw := rows[k]; raw != nil {
			var r JoinRequest
			if Decode(raw, &r) != nil {
				return BadRequest
			}
			if bytes.Equal(r.PublicKey, v.PublicKey) && r.Expires == v.Expires && r.Platform == PlatformOf(v.Platform) {
				return OK
			}
			return Conflict
		}
		put(k, JoinRequest{Device: DeviceIDOfKey(v.PublicKey), PublicKey: v.PublicKey, Expires: v.Expires, Platform: PlatformOf(v.Platform)})
		return OK
	case c.Change.CompleteJoin != nil:
		v := c.Change.CompleteJoin
		k := JoinKey(v.Code)
		var r JoinRequest
		if Decode(rows[k], &r) != nil || r.Decision == nil {
			return RequestGone
		}
		if DecisionID(*r.Decision) != v.Decision || v.Outcome != "delivered" && v.Outcome != "gone" {
			return Conflict
		}
		if r.Outcome != "" && r.Outcome != v.Outcome {
			return Conflict
		}
		r.Outcome = v.Outcome
		put(k, r)
		return OK
	}
	return Unauthorized
}
