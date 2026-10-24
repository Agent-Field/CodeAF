package raftabi

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"io"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

// Hosted effects use only a source-provisioned protocol key. The key is an ABI
// input, never a command field, snapshot record or diagnostic value.
func hostedEffect(d *decoder) ([]byte, error) {
	cluster, now, seed, raw := d.str(), d.u64(), d.blob(), d.blob()
	if d.end() != nil || len(seed) != ed25519.SeedSize || now > cs.MaxSafeInteger {
		return nil, errFrame
	}
	var change cs.Mutation
	j := json.NewDecoder(bytes.NewReader(raw))
	j.DisallowUnknownFields()
	if j.Decode(&change) != nil || j.Decode(new(any)) != io.EOF {
		return nil, errFrame
	}
	if change.DeviceSeen == nil && change.RenewHolds == nil && change.AdmitJoin == nil && change.CompleteJoin == nil && change.RecordUpload == nil {
		return nil, errFrame
	}
	key := ed25519.NewKeyFromSeed(seed)
	command, err := cs.Sign(cs.Command{Schema: cs.SchemaVersion, Cluster: cluster, Actor: cs.DeviceIDOfKey(key.Public().(ed25519.PublicKey)), Session: "effects", Sequence: 1, Now: int64(now), Change: change}, key)
	if err != nil {
		return nil, err
	}
	raw, err = cs.EncodeCommand(command)
	if err != nil || len(raw) > cs.MaxCommandBytes {
		return nil, errFrame
	}
	e := encoder{}
	e.blob(raw)
	return e.b, nil
}

// Projection uses the declared Go shape so sealed map values named fence or
// size remain opaque and v1 integer fields keep their original numeric wire.
func hostedProjection(d *decoder) ([]byte, error) {
	kind, raw := d.str(), d.blob()
	if d.end() != nil {
		return nil, errFrame
	}
	var value any
	switch kind {
	case "identity":
		value = new(cs.IdentityRec)
	case "device":
		value = new(cs.Device)
	case "cell", "list_cell":
		value = new(cs.Cell)
	case "result", "view":
		value = new(cs.Result)
	default:
		return nil, errFrame
	}
	if err := cs.Decode(raw, value); err != nil {
		return nil, err
	}
	if kind == "list_cell" {
		value.(*cs.Cell).Frames = nil
	}
	if kind == "view" {
		value = value.(*cs.Result).View
	}
	wire, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	e := encoder{}
	e.blob(wire)
	return e.b, nil
}

// Ingress proves the original signature before external I/O. Every later
// authorization uses a fresh committed view, including the root certificate,
// revocation and rotation; socket presence supplies none of that authority.
func hostedAuthority(d *decoder) ([]byte, error) {
	identity, device, actor, cert, write := d.blob(), d.blob(), d.str(), d.str(), d.u32()
	if d.end() != nil || write > 1 {
		return nil, errFrame
	}
	var root cs.IdentityRec
	var rec cs.Device
	if cs.Decode(identity, &root) != nil || len(device) > 0 && cs.Decode(device, &rec) != nil {
		return nil, cs.ErrDependency
	}
	code := cs.OK
	if cs.PersonalCertificate(root, actor, cert) != nil {
		code = cs.Unauthorized
	} else if rec.Revoked {
		code = cs.Revoked
	} else if write == 1 && root.Rotation != nil {
		code = cs.Rotated
	}
	e := encoder{}
	e.str(string(code))
	return e.b, nil
}
