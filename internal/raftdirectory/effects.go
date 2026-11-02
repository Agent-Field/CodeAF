package raftdirectory

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// EffectHost owns original request persistence, commit and fresh quorum reads.
// Read must recheck service enrollment after its barrier. Local store reads or
// proposal admission cannot satisfy either method's contract.
type EffectHost interface {
	CommitEffect(context.Context, clusterstate.Command) (clusterstate.Result, error)
	ReadEffect(context.Context, clusterstate.RecordKey) ([]byte, error)
}

// LinkEffects reserves one public request for one cluster before admission.
// Delivery accepts an exact idempotency key only after the decision committed.
type LinkEffects interface {
	Bind(code, cluster string) (directory.Request, error)
	Deliver(code, cluster, decision string, d directory.Decision) error
}

// Effects has only a restricted protocol key, never the identity or content key.
// Operations have immutable content keys, so crash retries need no local counter.
type Effects struct {
	Host           EffectHost
	Cluster, Actor string
	Key            ed25519.PrivateKey
}

func (e Effects) commit(ctx context.Context, now int64, m clusterstate.Mutation) error {
	c, err := clusterstate.Sign(clusterstate.Command{Schema: clusterstate.SchemaVersion, Cluster: e.Cluster, Actor: e.Actor, Session: "effects", Sequence: 1, Now: now, Change: m}, e.Key)
	if err != nil {
		return err
	}
	r, err := e.Host.CommitEffect(ctx, c)
	if err != nil {
		return err
	}
	switch r.Code {
	case clusterstate.OK:
		return nil
	case clusterstate.Rotated:
		return directory.ErrRotated
	case clusterstate.Revoked:
		return directory.ErrRevoked
	case clusterstate.RequestGone:
		return directory.ErrRequestGone
	case clusterstate.Capacity:
		return directory.ErrFull
	default:
		return directory.ErrBadRequest
	}
}

// Upload records no success before both store durability and application commit.
// A refused CAS or later revocation retains every uploaded object for reuse.
func (e Effects) Upload(ctx context.Context, now int64, store blobstore.Store, receipt clusterstate.UploadReceipt) (string, error) {
	if len(receipt.Frames) > clusterstate.MaxUploadFrames {
		return "", directory.ErrTooBig
	}
	receipt.Frames = append([]string(nil), receipt.Frames...)
	if err := blobstore.CheckReferences(ctx, store, receipt.HeadRID, receipt.Frames); err != nil {
		return "", err
	}
	if err := e.commit(ctx, now, clusterstate.Mutation{RecordUpload: &receipt}); err != nil {
		return "", err
	}
	return clusterstate.UploadID(receipt), nil
}
func (e Effects) Admit(ctx context.Context, now int64, links LinkEffects, code string) error {
	code = directory.NormalizeCode(code)
	r, err := links.Bind(code, e.Cluster)
	if err != nil {
		return err
	}
	public, err := base64.RawURLEncoding.DecodeString(r.Pubkey)
	if err != nil || clusterstate.DeviceIDOfKey(public) != r.Device {
		return directory.ErrBadRequest
	}
	return e.commit(ctx, now, clusterstate.Mutation{AdmitJoin: &clusterstate.AdmitJoin{Code: code, PublicKey: public, Expires: r.ExpiresAt, Platform: r.Platform}})
}

// Deliver replays an outbox row after a fresh barrier. A crash after public
// delivery but before CompleteJoin repeats the same grant, never a new decision.
// Expired public requests record a terminal gone outcome instead of retrying forever.
func (e Effects) Deliver(ctx context.Context, now int64, links LinkEffects, code string) error {
	code = directory.NormalizeCode(code)
	raw, err := e.Host.ReadEffect(ctx, clusterstate.JoinKey(code))
	if err != nil {
		return err
	}
	var r clusterstate.JoinRequest
	if clusterstate.Decode(raw, &r) != nil || r.Decision == nil {
		return directory.ErrRequestGone
	}
	if r.Outcome != "" {
		return nil
	}
	v := r.Decision
	d := directory.Decision{State: directory.RequestApproved, Device: v.Device, Grant: v.Grant}
	if v.Deny {
		d = directory.Decision{State: directory.RequestDenied, Device: r.Device}
	}
	id := clusterstate.DecisionID(*v)
	outcome := "delivered"
	if err = links.Deliver(code, e.Cluster, id, d); err != nil {
		if !errors.Is(err, directory.ErrRequestGone) {
			return err
		}
		outcome = "gone"
	}
	return e.commit(ctx, now, clusterstate.Mutation{CompleteJoin: &clusterstate.CompleteJoin{Code: code, Decision: id, Outcome: outcome}})
}

// OutboxHost scans a bounded page only after a fresh service authority barrier.
// The cursor is an optimization; restarting at the beginning is always safe.
type OutboxHost interface {
	ScanEffects(context.Context, string, int) ([]clusterstate.RecordWrite, error)
}

const MaxEffectReplay = 64

func (e Effects) Replay(ctx context.Context, now int64, links LinkEffects, after string) (string, error) {
	host, ok := e.Host.(OutboxHost)
	if !ok {
		return after, directory.ErrUnreachable
	}
	rows, err := host.ScanEffects(ctx, after, MaxEffectReplay)
	if err != nil {
		return after, err
	}
	if len(rows) > MaxEffectReplay {
		return after, directory.ErrFull
	}
	for _, row := range rows {
		if row.Namespace != "outbox" || row.Key <= after {
			return after, directory.ErrBadRequest
		}
		if len(row.Key) >= 5 && row.Key[:5] == "join/" {
			var r clusterstate.JoinRequest
			if err = clusterstate.Decode(row.Data, &r); err != nil {
				return after, err
			}
			if r.Decision != nil && r.Outcome == "" {
				if err = e.Deliver(ctx, now, links, r.Decision.Code); err != nil {
					return after, err
				}
			}
		}
		after = row.Key
	}
	return after, nil
}
