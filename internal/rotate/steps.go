package rotate

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
)

// oldSide is the old identity's relay as this machine can reach it before the
// journal exists.
func (e Env) oldSide() Relay {
	return e.Connect(signer{e.Identity.PublicKey(), e.Device})
}

// carried lists the chats of a directory listing in id order. Ids are ULIDs, so
// a chat comes before any branch made from it.
func carried(l directory.Listing) []Item {
	items := make([]Item, 0, len(l.Cells))
	for id, c := range l.Cells {
		items = append(items, Item{ID: id, Head: c.Head})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

// fetchAll brings every chat this machine lacks, at its newest durable head,
// into its store under the old keys, so the rotation has something to re-seal.
// Nothing is changed anywhere else, and it can be cancelled with the context.
func (e Env) fetchAll(ctx context.Context) error {
	old := e.oldSide()
	l, err := old.Dir.List(ctx)
	if err != nil {
		return err
	}
	for _, it := range carried(l) {
		if err := e.complete(ctx, old.Store, it); err != nil {
			return err
		}
	}
	return nil
}

// complete makes the store hold everything the item's head needs, under the
// old keys. A chat that is already whole costs one question to the engine.
func (e Env) complete(ctx context.Context, from blobstore.Store, it Item) error {
	eng, c := e.Engine(e.Identity), e.Cell(it.ID)
	f := cellsync.Fetcher{Engine: eng, Store: from, Inbox: func(c cell.Cell) string { return c.Root + ".inbox" }}
	return f.Complete(ctx, c, it.Head)
}

// seal carries every chat and the vault to the new identity, and the device
// that does it. Each finished chat is written down, so a crash repeats at most
// the chat that was in flight.
func (r *run) seal(ctx context.Context) error {
	if len(r.j.Items) == 0 {
		l, err := r.old.Dir.List(ctx)
		if err != nil {
			return err
		}
		r.j.Items = carried(l)
		if err := save(r.Home, r.j); err != nil {
			return err
		}
	}
	if err := r.registerDevice(ctx); err != nil {
		return err
	}
	for i := range r.j.Items {
		if err := r.carry(ctx, i); err != nil {
			return err
		}
	}
	if err := r.carryVault(ctx); err != nil {
		return err
	}
	return r.advance(Sealed)
}

func (r *run) registerDevice(ctx context.Context) error {
	rec, err := r.Record(r.newID)
	if err != nil {
		return err
	}
	return r.new.Dir.PutDevice(ctx, r.newDev.ID(), rec)
}

// carry moves one chat: its objects sealed under the new keys go up first, the
// directory record goes last, and the chat is marked done only after both.
func (r *run) carry(ctx context.Context, i int) error {
	it := &r.j.Items[i]
	if it.Done {
		return nil
	}
	if err := r.complete(ctx, r.old.Store, *it); err != nil {
		return err
	}
	ex, err := (&cellsync.Publisher{Engine: r.Engine(r.newID), Store: r.new.Store}).Upload(ctx, r.Cell(it.ID), it.Head)
	if err != nil {
		return err
	}
	if err := r.record(ctx, *it); err != nil {
		return err
	}
	it.HeadRID, it.Done = ex.HeadRID, true
	r.say("carried chat %d of %d", i+1, len(r.j.Items))
	if err := save(r.Home, r.j); err != nil {
		return err
	}
	return r.after("item:" + it.ID)
}

// record makes the chat's directory record in the new identity, from the old
// one, with its title sealed under the new metadata key and no lease held.
func (r *run) record(ctx context.Context, it Item) error {
	v, err := r.old.Dir.Cell(ctx, it.ID)
	if err != nil {
		return err
	}
	title, err := r.retitle(v.Cell.Title)
	if err != nil {
		return err
	}
	_, err = r.new.Dir.Create(ctx, it.ID, directory.CellInit{
		Head: it.Head, Class: v.Cell.Class, Title: title, ParentCell: v.Cell.ParentCell,
		Size: v.Cell.Size, OrphanTurns: v.Cell.OrphanTurns,
	})
	if err != nil && !errors.Is(err, directory.ErrExists) {
		return err
	}
	if err := r.letGo(ctx, it.ID); err != nil {
		return err
	}
	if v.Cell.Archived {
		return r.new.Dir.Archive(ctx, it.ID)
	}
	return nil
}

// retitle opens a title under the old metadata key and seals it under the new.
func (r *run) retitle(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	plain, err := directory.OpenName(directory.MetadataKey(r.Identity.CellKey()), sealed)
	if err != nil {
		return "", err
	}
	return directory.SealName(directory.MetadataKey(r.newID.CellKey()), plain)
}

// letGo gives back the lease a Create left this device holding, so nobody holds
// a chat just because it was carried over. A lease it no longer holds is left.
func (r *run) letGo(ctx context.Context, id string) error {
	v, err := r.new.Dir.Cell(ctx, id)
	if err != nil {
		return err
	}
	if v.Cell.Lease.Device != r.newDev.ID() || v.Cell.Lease.Expires <= v.Now {
		return nil
	}
	return r.new.Dir.Release(ctx, id, v.Cell.Lease.Fence)
}

// carryVault reseals the vault under the new cell key and seeds the new relay
// with it. The local vault file is not touched until the switch.
func (r *run) carryVault(ctx context.Context) error {
	enc, err := keys.StageReseal(r.Home, r.Identity.CellKey(), r.newID.CellKey())
	if err != nil || enc == nil {
		return err
	}
	l, err := r.new.Dir.List(ctx)
	if err != nil {
		return err
	}
	if err := r.vaultSyncer().PushSealed(ctx, enc, l.Identity.Vault); err != nil {
		return err
	}
	r.j.Vault = true
	return save(r.Home, r.j)
}

// verify asks the new relay for what the journal says it holds: every chat at
// the carried head, every head's remote id, and the vault.
func (r *run) verify(ctx context.Context) error {
	l, err := r.new.Dir.List(ctx)
	if err != nil {
		return err
	}
	for _, it := range r.j.Items {
		if err := r.holds(ctx, l, it); err != nil {
			return err
		}
	}
	if r.j.Vault && l.Identity.Vault == "" {
		return errors.New("rotate: the new relay does not hold the vault")
	}
	return r.advance(Verified)
}

func (r *run) holds(ctx context.Context, l directory.Listing, it Item) error {
	if got := l.Cells[it.ID].Head; got != it.Head {
		return fmt.Errorf("rotate: the new relay has chat %s at %q, not %q", it.ID, got, it.Head)
	}
	have, err := r.new.Store.Has(ctx, []string{it.HeadRID})
	if err != nil {
		return err
	}
	if !slices.Equal(have, []bool{true}) {
		return fmt.Errorf("rotate: the new relay lacks the newest turn of chat %s", it.ID)
	}
	return nil
}

// switchOver is the one local point of no return: the vault, then the identity,
// then the device, each safe to repeat. The identity on disk tells which of them
// already happened.
func (r *run) switchOver(ctx context.Context) error {
	if r.Identity.ID() != r.j.NewID {
		if err := r.stageAndInstall(); err != nil {
			return err
		}
	}
	if err := keys.CommitReseal(r.Home); err != nil {
		return err
	}
	if err := identity.RecordPredecessor(r.Home, r.j.OldID); err != nil {
		return err
	}
	return r.advance(Switched)
}

func (r *run) stageAndInstall() error {
	if r.Identity.ID() != r.j.OldID {
		return errors.New("rotate: the identity on this machine is neither the old nor the new one of " + File)
	}
	if _, err := keys.StageReseal(r.Home, r.Identity.CellKey(), r.newID.CellKey()); err != nil {
		return err
	}
	if err := r.after("staged"); err != nil {
		return err
	}
	if err := identity.Install(r.Home, r.newID, r.newDev); err != nil {
		return err
	}
	return r.after("installed")
}

// retire asks the relay to delete the old identity after the grace period and
// ends the rotation.
func (r *run) retire(ctx context.Context) error {
	if err := r.old.Gate.Retire(ctx, time.Duration(r.j.GraceMS)*time.Millisecond); err != nil {
		return err
	}
	r.j.State = ""
	return remove(r.Home)
}
