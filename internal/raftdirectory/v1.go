package raftdirectory

import (
	"context"
	"net/http"
	"reflect"
	"time"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

// V1Host holds its actor queue through commit and through the read callback.
// Commit returns only a durable applied result. Read commits the GET command's
// ingress time, obtains a fresh current-term ReadIndex and waits for durable
// application before calling inspect. A POST already committed by this request
// uses that time and a NEW barrier without repeating its mutation. Callbacks
// receive the actor-owned context: a caller losing its response cannot cancel
// durability or poison a healthy member while committed work is completing.
type V1Host interface {
	CommitV1(context.Context, clusterstate.Command) (clusterstate.Result, error)
	ReadV1(context.Context, clusterstate.Command, bool, func(context.Context, *raftstore.SQLite, raftstore.Application) error) error
}

// V1Directory is the authenticated legacy HTTP adapter for one frozen namespace.
// It has no key with which to impersonate a personal device. ForRequest receives
// exactly the proof admitted by directory.Handler, including archived bodies.
type V1Directory struct {
	Grace   directory.GraceBounds
	Host    V1Host
	Cluster string
	Now     func() time.Time
}

var _ directory.RequestDirectory = (*V1Directory)(nil)

func (d *V1Directory) For(actor string) directory.Client {
	return directory.NewRaft(&v1Backend{owner: d, actor: actor, err: directory.ErrUnauthorized}).For(actor)
}
func (d *V1Directory) ForRequest(actor string, r *http.Request, raw []byte) directory.Client {
	p, err := reqsign.Capture(r, raw)
	b := &v1Backend{owner: d, actor: actor, err: err}
	if err == nil && len(raw) > clusterstate.MaxV1BodyBytes {
		b.err = directory.ErrTooBig
	}
	if err == nil && b.err == nil {
		now := time.Now()
		if d.Now != nil {
			now = d.Now()
		}
		b.command, b.err = clusterstate.V1Command(d.Cluster, now.UnixMilli(), p)
		if b.err != nil {
			b.err = directory.ErrBadRequest
		}

		if b.err == nil && b.command.Actor != actor {
			b.err = directory.ErrUnauthorized
		}
	}
	adapter := directory.NewRaft(b)
	if d.Grace != (directory.GraceBounds{}) {
		adapter.SetGraceBounds(d.Grace)
	}
	return adapter.For(actor)
}

type v1Backend struct {
	owner     *V1Directory
	actor     string
	command   clusterstate.Command
	committed bool
	err       error
}

func (b *v1Backend) Change(ctx context.Context, actor string, m clusterstate.Mutation) (clusterstate.Result, error) {
	if b.err != nil {
		return clusterstate.Result{}, b.err
	}
	if b.owner.Host == nil || actor != b.actor || !reflect.DeepEqual(m, b.command.Change) {
		return clusterstate.Result{}, directory.ErrUnauthorized
	}
	r, err := b.owner.Host.CommitV1(ctx, b.command)
	b.committed = err == nil
	return r, err
}

func (b *v1Backend) read(ctx context.Context, actor string, fn func(context.Context, *raftstore.SQLite, raftstore.Application) error) error {
	if b.err != nil {
		return b.err
	}
	if b.owner.Host == nil || actor != b.actor {
		return directory.ErrUnauthorized
	}
	return b.owner.Host.ReadV1(ctx, b.command, b.committed, func(ctx context.Context, s *raftstore.SQLite, app raftstore.Application) error {
		if err := authorizeV1Read(ctx, s, app, b.command); err != nil {
			return err
		}
		return fn(ctx, s, app)
	})
}

func authorizeV1Read(ctx context.Context, s *raftstore.SQLite, app raftstore.Application, c clusterstate.Command) error {
	keys := []raftstore.Key{{Namespace: raftstore.State, Key: clusterstate.IdentityKey().Key}, {Namespace: raftstore.State, Key: clusterstate.DeviceKey(c.Actor).Key}}
	rows, _, err := s.Read(ctx, app.Applied, keys)
	if err != nil {
		return err
	}
	var identity clusterstate.IdentityRec
	if clusterstate.Decode(rows[keys[0]], &identity) != nil {
		return clusterstate.ErrDependency
	}
	if _, ok := clusterstate.V1AuthorityAt(c, identity, app.LogicalTime); !ok {
		return directory.ErrUnauthorized
	}
	if rows[keys[1]] != nil {
		var device clusterstate.Device
		if clusterstate.Decode(rows[keys[1]], &device) != nil {
			return clusterstate.ErrDependency
		}
		if device.Revoked {
			return directory.ErrRevoked
		}
	}
	return nil
}

func (b *v1Backend) Listing(ctx context.Context, actor string) (directory.Listing, error) {
	var l directory.Listing
	err := b.read(ctx, actor, func(ctx context.Context, s *raftstore.SQLite, app raftstore.Application) error {
		var err error
		l, err = DisplayListing(ctx, s, app)
		return err
	})
	return l, err
}
func (b *v1Backend) ReadCell(ctx context.Context, actor, id string) (directory.CellView, error) {
	var v directory.CellView
	err := b.read(ctx, actor, func(ctx context.Context, s *raftstore.SQLite, app raftstore.Application) error {
		k := raftstore.Key{Namespace: raftstore.State, Key: clusterstate.CellKey(id).Key}
		rows, _, err := s.Read(ctx, app.Applied, []raftstore.Key{k})
		if err != nil {
			return err
		}
		if rows[k] == nil {
			return directory.ErrNotFound
		}
		v.Now = app.LogicalTime
		return clusterstate.Decode(rows[k], &v.Cell)
	})
	return v, err
}
