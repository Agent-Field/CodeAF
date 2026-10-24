package clusterstate

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"
)

var ErrMigration = errors.New("codeaf: directory upgrade is incomplete; retry shortly; saved chats are still here")

// LegacyConfig is allocated once at the source gate. Only public protocol keys
// belong here; content keys and private signing keys never enter an import.
type LegacyConfig struct {
	Cluster      string   `json:"cluster"`
	Generation   uint64   `json:"generation"`
	Identity     string   `json:"identity"`
	SigningKey   []byte   `json:"signing_key"`
	Anchors      []uint64 `json:"anchors"`
	EffectsKey   []byte   `json:"effects_key"`
	Now          int64    `json:"now"`
	Certificates []string `json:"certificates,omitempty"`
}

func (c LegacyConfig) Validate() error {
	if c.Cluster == "" || c.Generation != 1 || len(c.SigningKey) != ed25519.PublicKeySize || IdentityID(c.SigningKey) != c.Identity || len(c.EffectsKey) != ed25519.PublicKeySize || bytes.Equal(c.EffectsKey, c.SigningKey) || len(c.Anchors) != 3 || c.Now < 0 || c.Now > int64(MaxSafeInteger) {
		return ErrMigration
	}
	for i, id := range c.Anchors {
		if id == 0 || id >= ^uint64(0)-1 || (i > 0 && id <= c.Anchors[i-1]) {
			return ErrMigration
		}
	}
	if len(c.Certificates) > MaxLiveSessions {
		return ErrMigration
	}
	seen := map[string]bool{}
	for _, text := range c.Certificates {
		public, e := legacyCertificate(c, text)
		if e != nil || seen[string(public)] || bytes.Equal(public, c.EffectsKey) {
			return ErrMigration
		}
		seen[string(public)] = true
	}
	return nil
}

func legacyCertificate(c LegacyConfig, text string) ([]byte, error) {
	var cert struct {
		Device string `json:"device"`
	}
	raw, e := base64.RawURLEncoding.DecodeString(text)
	if e != nil || len(raw) > 1024 || json.Unmarshal(raw, &cert) != nil {
		return nil, ErrMigration
	}
	public, e := hex.DecodeString(cert.Device)
	if e != nil {
		return nil, ErrMigration
	}
	return JoinCertificate(IdentityRec{Identity: c.Identity, SigningKey: c.SigningKey}, JoinRequest{Device: DeviceIDOfKey(public), PublicKey: public}, text)
}

// LegacyManifest inventories converted rows at a fixed source version. The
// digest covers canonical RecordWrite lines in namespace/key order, excluding
// this manifest. Snapshot row framing is applied only after genesis commits.
type LegacyManifest struct {
	Config  LegacyConfig `json:"config"`
	Version uint64       `json:"version"`
	Rows    uint64       `json:"rows"`
	Bytes   uint64       `json:"bytes"`
	Digest  string       `json:"digest"`
	Ceiling uint64       `json:"ceiling"`
}

func LegacyImportKey() RecordKey { return RecordKey{"membership", "legacy-import"} }
func (m LegacyManifest) Validate() error {
	h, e := hex.DecodeString(m.Digest)
	b, err := Encode(m)
	if m.Config.Validate() != nil || m.Rows < 2 || e != nil || len(h) != 32 || hex.EncodeToString(h) != m.Digest || err != nil || uint64(len(b))+128 > LegacyManifestReserve || m.Ceiling != max(uint64(DefaultApplicationBytes), m.Bytes+RecordBytes(LegacyImportKey(), b)) {
		return ErrMigration
	}
	return nil
}

// Bound the retained provenance independently of the number of source rows.
// Its exact encoded size is charged to the grandfathered inventory.
const LegacyManifestReserve uint64 = 4096

// LegacyRow converts the ORIGINAL v1 JSON bytes, never a JavaScript number
// roundtrip. Unknown fields refuse migration instead of silently dropping data.
// HoldUntil is sampled once while the source is frozen and recorded durably.
func LegacyRow(c LegacyConfig, kind, id string, raw []byte, holdUntil int64) (RecordWrite, error) {
	if c.Validate() != nil || len(raw) > MaxEvaluationBytes {
		return RecordWrite{}, ErrMigration
	}
	var key RecordKey
	var value any
	switch kind {
	case "identity":
		var v IdentityRec
		if legacyDecode(raw, &v) != nil || v.V != 1 || (v.Identity != "" && v.Identity != c.Identity) || (len(v.SigningKey) > 0 && !bytes.Equal(v.SigningKey, c.SigningKey)) {
			return RecordWrite{}, ErrMigration
		}
		v.Identity = c.Identity
		v.SigningKey = c.SigningKey
		key = IdentityKey()
		value = v
	case "devices":
		var v Device
		if id == "" || legacyDecode(raw, &v) != nil || v.V != 1 {
			return RecordWrite{}, ErrMigration
		}
		key = DeviceKey(id)
		value = v
	case "cells":
		var v Cell
		if id == "" || legacyDecode(raw, &v) != nil || v.V != 1 || v.Lease.Fence > MaxSafeInteger {
			return RecordWrite{}, ErrMigration
		}
		if v.Lease.Expires != 0 {
			v.Lease.Expires = max(v.Lease.Expires, holdUntil)
		}
		key = CellKey(id)
		value = v
	default:
		return RecordWrite{}, ErrMigration
	}
	data, e := Encode(value)
	if e != nil || len(data)+len(key.Key) > MaxEvaluationBytes {
		return RecordWrite{}, ErrMigration
	}
	return RecordWrite{key, data}, nil
}
func legacyDecode(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return ErrMigration
	}
	// A second value or malformed trailing bytes must never be ignored.
	var extra any
	if d.Decode(&extra) != io.EOF {
		return ErrMigration
	}
	return nil
}
func LegacyEffects(c LegacyConfig) RecordWrite {
	b, _ := Encode(Authority{Cluster: c.Cluster, Key: c.EffectsKey, Effects: true})
	return RecordWrite{AuthorityKey(DeviceIDOfKey(c.EffectsKey)), b}
}

// SealLegacyManifest counts its own retained provenance in the imported state.
// Decimal ceiling width converges before the fixed bounded manifest limit.
func SealLegacyManifest(m LegacyManifest) (LegacyManifest, error) {
	for range 8 {
		b, e := Encode(m)
		if e != nil {
			return m, e
		}
		n := max(uint64(DefaultApplicationBytes), m.Bytes+RecordBytes(LegacyImportKey(), b))
		if n == m.Ceiling {
			return m, m.Validate()
		}
		m.Ceiling = n
	}
	return m, ErrMigration
}

// LegacyAuthorities carries only certificates already authenticated by ingress.
// Unseen devices later enroll with their original root-signed certificate; an
// ID or a metadata upsert by itself never grants signing authority.
func LegacyAuthorities(c LegacyConfig) ([]RecordWrite, error) {
	if c.Validate() != nil {
		return nil, ErrMigration
	}
	out := []RecordWrite{LegacyEffects(c)}
	for _, cert := range c.Certificates {
		public, e := legacyCertificate(c, cert)
		if e != nil {
			return nil, e
		}
		b, e := Encode(Authority{Cluster: c.Cluster, Key: public, Personal: true})
		if e != nil {
			return nil, e
		}
		out = append(out, RecordWrite{AuthorityKey(DeviceIDOfKey(public)), b})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}
