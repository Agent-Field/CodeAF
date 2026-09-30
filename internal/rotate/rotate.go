package rotate

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// DefaultGrace is how long the relay keeps the old identity readable.
const DefaultGrace = 7 * 24 * time.Hour

// ErrRotated is what the relay says to a write on a replaced identity.
var ErrRotated = wireauth.ErrRotated

// ErrSomeoneFirst is said when the old identity was already rotated before this
// device asked: whoever holds the root can rotate first, and the relay cannot
// tell the owner from a thief.
var ErrSomeoneFirst = errors.New("this identity was already rotated; the new one is on the device that did it")

// ErrSwitched is said to an abandon that came too late: this machine already is
// the new identity and the only way on is forward.
var ErrSwitched = errors.New("already switched to the new identity; run codeaf identity rotate to finish")

// Gate is the relay's state machine for the identity being replaced.
type Gate interface {
	// Freeze stops every write to the identity; reads stay open. It is repeatable.
	Freeze(ctx context.Context) error
	// Thaw undoes a freeze. It fails once the identity is retired.
	Thaw(ctx context.Context) error
	// Retire makes the freeze permanent and has the relay delete the identity
	// after grace. It is repeatable.
	Retire(ctx context.Context, grace time.Duration) error
}

// Relay is one identity's side of a relay: the clients signed as a device of it.
type Relay struct {
	Dir   directory.Client
	Store blobstore.Store
	Gate  Gate
}

// Env is everything a rotation needs from the machine it runs on.
type Env struct {
	Home     string
	Identity identity.Identity // the identity on disk now
	Device   identity.Dev      // this machine's device under it
	// Connect binds the relay to a signer: the old identity's device, then the
	// new one's.
	Connect func(reqsign.Signer) Relay
	// Engine is the sync engine that seals and opens with id's keys and keeps
	// id's own published ledger.
	Engine func(id identity.Identity) cellsync.Engine
	// Cell is the folder a chat has on this machine, or a scratch one the
	// engine may hold its objects in.
	Cell func(id string) cell.Cell
	// Record is this device's directory record under id.
	Record func(id identity.Identity) (directory.Device, error)
	Grace  time.Duration // 0 is DefaultGrace
	Say    func(string)  // progress lines; nil says nothing
	// After is called once each step is durable, with its name; an error stops
	// the rotation there. Tests use it to crash at every point.
	After func(step string) error
}

func (e Env) say(format string, args ...any) {
	if e.Say != nil {
		e.Say(fmt.Sprintf(format, args...))
	}
}

func (e Env) after(step string) error {
	if e.After == nil {
		return nil
	}
	return e.After(step)
}

func (e Env) grace() time.Duration {
	if e.Grace == 0 {
		return DefaultGrace
	}
	return e.Grace
}

// Result is what a finished rotation reports.
type Result struct {
	OldID, NewID string
	Chats        int
	RetireAfter  time.Duration
}

// signer is a device's proof to the relay: its key, and the public key of the
// identity that certified it.
type signer struct {
	pub ed25519.PublicKey
	dev identity.Dev
}

func (s signer) IdentityKey() ed25519.PublicKey { return s.pub }
func (s signer) Cert() identity.Cert            { return s.dev.Cert }
func (s signer) Sign(msg []byte) []byte         { return s.dev.Sign(msg) }

// run is one rotation in progress: the journal and the two sides it moves between.
type run struct {
	Env
	j        Journal
	old, new Relay
	newID    identity.Identity
	newDev   identity.Dev
}

// Rotate replaces the identity, starting a rotation or finishing one that was
// interrupted, and answers what happened.
func (e Env) Rotate(ctx context.Context) (Result, error) {
	r, err := e.open(ctx)
	if err != nil {
		return Result{}, err
	}
	for r.j.State != "" {
		step, ok := steps[r.j.State]
		if !ok {
			return Result{}, fmt.Errorf("rotate: unknown state %q in %s", r.j.State, File)
		}
		if err := step(r, ctx); err != nil {
			return Result{}, err
		}
	}
	return Result{OldID: r.j.OldID, NewID: r.j.NewID, Chats: len(r.j.Items), RetireAfter: time.Duration(r.j.GraceMS) * time.Millisecond}, nil
}

// steps moves a rotation from each state to the next; a step that ends the
// rotation leaves the state empty.
var steps = map[State]func(*run, context.Context) error{
	Minted:   (*run).freeze,
	Frozen:   (*run).seal,
	Sealed:   (*run).verify,
	Verified: (*run).switchOver,
	Switched: (*run).retire,
}

// open resumes the journal that is there, or fetches what the new identity
// will be made of and starts one.
func (e Env) open(ctx context.Context) (*run, error) {
	j, found, err := load(e.Home)
	if err != nil {
		return nil, err
	}
	if !found {
		if err := e.fetchAll(ctx); err != nil {
			return nil, err
		}
		if j, err = e.mint(); err != nil {
			return nil, err
		}
		if err := e.after(string(Minted)); err != nil {
			return nil, err
		}
	}
	return e.resume(j)
}

// mint makes the new root, the device it certifies, and writes them down before
// anything is sent.
func (e Env) mint() (Journal, error) {
	id, err := identity.Mint()
	if err != nil {
		return Journal{}, err
	}
	dev, err := identity.NewDevice(id)
	if err != nil {
		return Journal{}, err
	}
	newDoc, err1 := id.Marshal()
	devDoc, err2 := dev.Marshal()
	oldDoc, err3 := e.Device.Marshal()
	if err := errors.Join(err1, err2, err3); err != nil {
		return Journal{}, err
	}
	j := Journal{V: 1, State: Minted, OldID: e.Identity.ID(), OldPublic: hex.EncodeToString(e.Identity.PublicKey()),
		OldDevice: oldDoc, NewID: id.ID(), New: newDoc, NewDevice: devDoc, GraceMS: e.grace().Milliseconds()}
	return j, save(e.Home, j)
}

// resume rebuilds the two sides from a journal.
func (e Env) resume(j Journal) (*run, error) {
	newID, err := identity.Unmarshal(j.New)
	if err != nil {
		return nil, err
	}
	newDev, err := identity.UnmarshalDev(j.NewDevice)
	if err != nil {
		return nil, err
	}
	oldDev, err := identity.UnmarshalDev(j.OldDevice)
	if err != nil {
		return nil, err
	}
	oldPub, err := hex.DecodeString(j.OldPublic)
	if err != nil {
		return nil, err
	}
	r := &run{Env: e, j: j, newID: newID, newDev: newDev}
	r.old = e.Connect(signer{oldPub, oldDev})
	r.new = e.Connect(signer{newID.PublicKey(), newDev})
	return r, nil
}

// advance records that the rotation reached state, then lets a test stop here.
func (r *run) advance(state State) error {
	r.j.State = state
	if err := save(r.Home, r.j); err != nil {
		return err
	}
	return r.after(string(state))
}

// freeze stops every write to the old identity, so nothing can be published
// after a chat has been carried over.
func (r *run) freeze(ctx context.Context) error {
	if err := r.old.Gate.Freeze(ctx); err != nil {
		return r.refusedFirst(err)
	}
	r.say("the old identity is read-only now; your other computers wait until you pair them again")
	return r.advance(Frozen)
}

// refusedFirst turns the relay's refusal into the sentence for it. The journal
// holds keys that can never be used now, so it goes.
func (r *run) refusedFirst(err error) error {
	if errors.Is(err, ErrRotated) {
		return errors.Join(ErrSomeoneFirst, remove(r.Home))
	}
	return err
}

// Abandon stops a rotation that has not switched: the old identity is thawed
// and the journal, which holds the unused new keys, is deleted. With no journal
// it thaws anyway, which is how another computer undoes a rotation whose
// keeper lost its disk.
func (e Env) Abandon(ctx context.Context) error {
	j, found, err := load(e.Home)
	if err != nil {
		return err
	}
	if !found {
		return e.Connect(signer{e.Identity.PublicKey(), e.Device}).Gate.Thaw(ctx)
	}
	if j.State == Switched {
		return ErrSwitched
	}
	r, err := e.resume(j)
	if err != nil {
		return err
	}
	if err := r.old.Gate.Thaw(ctx); err != nil {
		return err
	}
	return remove(e.Home)
}

// vaultSyncer is the vault's syncer for the new relay.
func (r *run) vaultSyncer() vaultsync.Syncer {
	return vaultsync.Syncer{Store: r.new.Store, Dir: r.new.Dir, CellKeyID: r.newID.CellKeyID()}
}
