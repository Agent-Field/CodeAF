package clusterstate

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"reflect"
	"strconv"
	"strings"
)

// V1Request retains the original device proof through forwarding. Its body is
// admitted only after the closed legacy decoder has checked every field, so
// ignored JSON cannot smuggle private text into a replicated command.
type V1Request struct {
	Method      string `json:"method"`
	URI         string `json:"uri"`
	IdentityKey []byte `json:"identity_key"`
	Certificate string `json:"certificate"`
	At          string `json:"at"`
	Signature   []byte `json:"signature"`
	Body        []byte `json:"body"`
}

type v1Certificate struct {
	V         uint16 `json:"V"`
	Device    string `json:"device"`
	Created   int64  `json:"created"`
	Signature string `json:"sig"`
}

const V1SkewMillis int64 = 5 * 60 * 1000

// V1Command translates only the request the device actually signed. It never
// reads a new holder to manufacture an expectation or signs on the device's behalf.
func V1Command(cluster string, now int64, p V1Request) (Command, error) {
	m, err := p.Mutation()
	if err != nil {
		return Command{}, err
	}
	var cert v1Certificate
	raw, err := base64.RawURLEncoding.DecodeString(p.Certificate)
	if err != nil || len(raw) > 1024 || legacyBody(raw, &cert, false) != nil {
		return Command{}, ErrCodec
	}
	pub, err := hex.DecodeString(cert.Device)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return Command{}, ErrCodec
	}
	c := Command{V1: &p, Schema: SchemaVersion, Cluster: cluster, Actor: DeviceIDOfKey(pub), Session: "v1", Sequence: 1, Now: now, Change: m}
	c.RequestDigest, err = Digest(c)
	return c, err
}

// V1Authority checks the root, certificate, exact HTTP signing domain and the
// translated operation at the committed position. Ingress authentication alone
// cannot authorize a request delayed across revocation.
func V1Authority(c Command, identity IdentityRec) (Authority, bool) {
	return V1AuthorityAt(c, identity, c.Now)
}

// V1AuthorityAt additionally rejects a forwarded old proof after committed
// time has advanced. A sender cannot make stale ingress evidence fresh again
// by placing its original timestamp in an unsigned host field.
func V1AuthorityAt(c Command, identity IdentityRec, committedNow int64) (Authority, bool) {
	p := c.V1
	if p == nil || c.WorkerIncarnation != 0 || c.Session != "v1" || c.Sequence != 1 || c.Ack != 0 || len(c.Signature) != 0 || !bytes.Equal(p.IdentityKey, identity.SigningKey) {
		return Authority{}, false
	}
	ms, err := strconv.ParseInt(p.At, 10, 64)
	now := max(c.Now, committedNow)
	if err != nil || ms < 0 || ms > int64(MaxSafeInteger) || c.Now < 0 || now > int64(MaxSafeInteger) || ms < now-V1SkewMillis || ms > now+V1SkewMillis {
		return Authority{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(p.Certificate)
	var cert v1Certificate
	if err != nil || len(raw) > 1024 || legacyBody(raw, &cert, false) != nil {
		return Authority{}, false
	}
	pub, err := hex.DecodeString(cert.Device)
	if err != nil || DeviceIDOfKey(pub) != c.Actor {
		return Authority{}, false
	}
	pub, err = JoinCertificate(identity, JoinRequest{Device: c.Actor, PublicKey: pub}, p.Certificate)
	if err != nil {
		return Authority{}, false
	}
	msg := V1SigningMessage(p.Method, p.URI, p.At, p.Body)
	m, err := p.Mutation()
	if err != nil || !reflect.DeepEqual(m, c.Change) || m.PutDevice != nil && m.PutDevice.ID != c.Actor || !ed25519.Verify(pub, msg, p.Signature) {
		return Authority{}, false
	}
	return Authority{Cluster: c.Cluster, Key: pub, Personal: true}, true
}

// V1SigningMessage is the existing HTTP signing domain, shared by the sender,
// ingress and deterministic committed verifier to keep their bytes identical.
func V1SigningMessage(method, uri, at string, body []byte) []byte {
	h := sha256.Sum256(body)
	return []byte("codeaf-req-v1\n" + method + "\n" + uri + "\n" + at + "\n" + hex.EncodeToString(h[:]))
}

// ClientCommand refuses protocol provenance on the public v2 door. Only an
// original v1 HTTP proof may select the compatibility rules at that ingress.
func ClientCommand(c Command) error {
	if c.V1 != nil || c.Change.Create != nil && c.Change.Create.V1 || c.Change.Publish != nil && c.Change.Publish.V1 || c.Change.Acquire != nil && (c.Change.Acquire.V1 || c.Change.Acquire.Legacy) {
		return ErrCodec
	}
	return nil
}

func legacyBody(raw []byte, v any, optional bool) error {
	if optional && len(raw) == 0 {
		return nil
	}
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ErrCodec
	}
	if legacyJSONKeys(raw) != nil {
		return ErrCodec
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return ErrCodec
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return ErrCodec
	}
	return nil
}

// Duplicate fields otherwise disappear during typed decoding while their first
// value remains in the signed body replicated by the proof.
func legacyJSONKeys(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 64 {
			return ErrCodec
		}
		t, err := d.Token()
		if err != nil {
			return err
		}
		switch t {
		case json.Delim('{'):
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				s, ok := k.(string)
				if !ok || seen[s] {
					return ErrCodec
				}
				seen[s] = true
				if e = value(depth + 1); e != nil {
					return e
				}
			}
			end, e := d.Token()
			if e != nil || end != json.Delim('}') {
				return ErrCodec
			}
		case json.Delim('['):
			for d.More() {
				if e := value(depth + 1); e != nil {
					return e
				}
			}
			end, e := d.Token()
			if e != nil || end != json.Delim(']') {
				return ErrCodec
			}
		case json.Delim('}'), json.Delim(']'):
			return ErrCodec
		}
		return nil
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrCodec
	}
	return nil
}

// Mutation is shared by native and WASI ingress. V1 numeric fences keep their
// original bytes and precision; v2's decimal-string codec never parses them.
func (p V1Request) Mutation() (Mutation, error) {
	bad := func() (Mutation, error) { return Mutation{}, ErrCodec }
	path := strings.TrimPrefix(p.URI, "/fabric")
	if strings.ContainsAny(path, "?#") || !strings.HasPrefix(path, "/v1/") {
		return bad()
	}
	if p.Method == "GET" && (path == "/v1/dir/list" || path == "/v1/dir/presence" || path == "/v1/identity/rotation" || strings.HasPrefix(path, "/v1/dir/cells/") && len(strings.Split(strings.TrimPrefix(path, "/v1/dir/cells/"), "/")) == 1) {
		if len(p.Body) != 0 {
			return bad()
		}
		return Mutation{AdvanceTime: &AdvanceTime{}}, nil
	}
	decode := func(v any, optional bool) bool { return legacyBody(p.Body, v, optional) == nil }
	if path == "/v1/dir/vault" && p.Method == "POST" {
		var v VaultCAS
		if !decode(&v, false) {
			return bad()
		}
		return Mutation{SetVault: &v}, nil
	}
	if path == "/v1/identity/rotation" && p.Method == "POST" {
		var v RotateRequest
		if !decode(&v, false) {
			return bad()
		}
		return Mutation{Rotate: &v}, nil
	}
	s := strings.Split(strings.TrimPrefix(path, "/v1/dir/"), "/")
	if len(s) < 2 || len(s) > 3 || path == strings.TrimPrefix(path, "/v1/dir/") {
		return bad()
	}
	id, err := v1Segment(s[1])
	if err != nil || id == "" {
		return bad()
	}
	verb := ""
	if len(s) == 3 {
		verb = s[2]
	}
	if s[0] == "devices" {
		if p.Method == "PUT" && verb == "" {
			var v Device
			if !decode(&v, false) {
				return bad()
			}
			return Mutation{PutDevice: &PutDevice{ID: id, Device: v}}, nil
		}
		if p.Method == "POST" && verb == "revoke" && len(p.Body) == 0 {
			return Mutation{Revoke: &DeviceID{Device: id}}, nil
		}
	}
	if s[0] == "requests" && p.Method == "POST" {
		code := strings.ToLower(strings.TrimSpace(id))
		if verb == "deny" && len(p.Body) == 0 {
			return Mutation{DecideJoin: &JoinDecision{Code: code, Deny: true}}, nil
		}
		if verb == "approve" {
			var a struct {
				Device Device `json:"device"`
				Cert   string `json:"cert"`
				Grant  string `json:"grant"`
			}
			if !decode(&a, false) {
				return bad()
			}
			var cert v1Certificate
			raw, e := base64.RawURLEncoding.DecodeString(a.Cert)
			if e != nil || len(raw) > 1024 || legacyBody(raw, &cert, false) != nil {
				return bad()
			}
			pub, e := hex.DecodeString(cert.Device)
			if e != nil || len(pub) != ed25519.PublicKeySize {
				return bad()
			}
			return Mutation{DecideJoin: &JoinDecision{Code: code, Device: DeviceIDOfKey(pub), Record: a.Device, Cert: a.Cert, Grant: a.Grant}}, nil
		}
	}
	if s[0] != "cells" || p.Method != "POST" {
		return bad()
	}
	switch verb {
	case "":
		var v CellInit
		if !decode(&v, false) {
			return bad()
		}
		return Mutation{Create: &CreateCell{Cell: id, Init: v, V1: true}}, nil
	case "acquire":
		var v struct {
			Force  bool              `json:"force,omitempty"`
			Expect *LeaseExpectation `json:"expect,omitempty"`
		}
		if !decode(&v, true) {
			return bad()
		}
		a := AcquireCell{Cell: id, Force: v.Force, V1: true, Legacy: v.Expect == nil}
		if v.Expect != nil {
			a.Expect = *v.Expect
			if a.Expect.Fence > MaxSafeInteger {
				return bad()
			}
		}
		return Mutation{Acquire: &a}, nil
	case "heartbeat":
		var v Beat
		if !decode(&v, false) || v.Fence > MaxSafeInteger {
			return bad()
		}
		return Mutation{Beat: &BeatCell{Cell: id, Beat: v}}, nil
	case "publish":
		var v Publish
		if !decode(&v, false) || v.Fence > MaxSafeInteger {
			return bad()
		}
		return Mutation{Publish: &PublishCell{Cell: id, Publish: v, V1: true}}, nil
	case "release":
		var v struct {
			Quiet bool   `json:"quiet,omitempty"`
			Fence uint64 `json:"fence"`
		}
		if !decode(&v, false) || v.Fence > MaxSafeInteger {
			return bad()
		}
		return Mutation{Release: &ReleaseCell{Cell: id, Fence: v.Fence, Quiet: v.Quiet}}, nil
	case "archive":
		if len(p.Body) != 0 {
			return bad()
		}
		return Mutation{Archive: &CellID{Cell: id}}, nil
	}
	return bad()
}

func v1Segment(s string) (string, error) {
	var out []byte
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			out = append(out, s[i])
			continue
		}
		if i+2 >= len(s) {
			return "", ErrCodec
		}
		b, e := hex.DecodeString(s[i+1 : i+3])
		if e != nil {
			return "", ErrCodec
		}
		out = append(out, b[0])
		i += 2
	}
	return string(out), nil
}
