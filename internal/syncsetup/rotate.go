package syncsetup

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/rotate"
)

// ErrRotating is what a machine says while a rotation it started is unfinished:
// its identity is about to change, so nothing may sync under the old one.
var ErrRotating = errors.New("a rotation is unfinished on this computer; run codeaf identity rotate to finish it")

// OpenForRotation is Open for the one caller that must work while a rotation is
// pending: the rotation itself. It needs the identity too, and a relay.
func OpenForRotation(home string) (*Sync, bool, error) { return open(home) }

// scratchDir is where the objects of a chat this machine does not hold are
// fetched before they are sealed again; it is removed when the rotation ends.
func scratchDir(home string) string { return filepath.Join(home, "rotation-scratch") }

// Rotation is the rotation of this machine's identity over eng, the engine every
// chat here is sealed and opened by. held finds the folder a chat has on this
// machine; a chat it does not know is fetched into a scratch folder.
func (s *Sync) Rotation(eng cellstore.Engine, held func(id string) (cell.Cell, bool), name string, say func(string)) rotate.Env {
	eng.WorkspaceOf = func(c cell.Cell) string { return workspaceOf(c.Root) }
	hostedRelay := ""
	if IsHostedRelay(s.Relay) {
		hostedRelay = normalizeStorageURL(s.Relay)
	}
	return rotate.Env{
		Home: s.Home, Identity: s.Identity, Device: s.Device, Say: say,
		HostedRelay: hostedRelay,
		Connect:     s.relayOf,
		StorageAllowed: func(ctx context.Context, successor blobstore.Store) (bool, error) {
			if !IsHostedRelay(s.Relay) {
				return true, nil
			}
			counts, err := s.Store.(interface {
				Stats(context.Context) (blobstore.Counts, error)
			}).Stats(ctx)
			if err != nil {
				return false, err
			}
			if counts.Storage == "off" || successor == nil {
				return counts.Storage != "off", nil
			}
			next, err := successor.(interface {
				Stats(context.Context) (blobstore.Counts, error)
			}).Stats(ctx)
			return err == nil && next.Storage != "off", err
		},
		Engine: func(id identity.Identity) cellsync.Engine {
			keys := cellstore.SyncKeys{CellKey: id.CellKey(), Dedup: id.DedupSecret()}
			return landing{Engine: eng.Sync(keys, cellstore.LedgerName(s.Relay, id.ID()))}
		},
		Cell: func(id string) cell.Cell {
			if c, ok := held(id); ok {
				return c
			}
			return cell.Cell{ID: id, Root: filepath.Join(scratchDir(s.Home), id)}
		},
		Record: func(id identity.Identity) (directory.Device, error) { return deviceRecord(id, name) },
	}
}

// Tidy removes what a rotation left to be cleaned: the scratch folders.
func (s *Sync) Tidy() error { return os.RemoveAll(scratchDir(s.Home)) }

// relayOf binds this machine's relay to a signer: the clients of a device of
// whichever identity it belongs to.
func (s *Sync) relayOf(sig reqsign.Signer) rotate.Relay {
	sign := reqsign.SignFor(sig, time.Now)
	dir := directory.NewHTTP(s.Relay, sign, &http.Client{Timeout: requestTimeout})
	store := GateStore(blobstore.NewHTTP(s.Relay, sign, &http.Client{}), s.Home, s.Relay, reqsign.IDOf(sig.IdentityKey()))
	if IsHostedRelay(s.Relay) && reqsign.IDOf(sig.IdentityKey()) != s.Identity.ID() {
		allowed := store.CanStore
		legacy := false
		store.CanStore = func() bool {
			if legacy || allowed() {
				return true
			}
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			defer cancel()
			counts, err := store.Stats(ctx)
			if err == nil && counts.Storage == "" {
				legacy = true
				return true
			}
			return err == nil && SaveStorage(s.Home, s.Relay, reqsign.IDOf(sig.IdentityKey()), counts.Storage) == nil && allowed()
		}
	}
	return rotate.Relay{Dir: dir, Store: store, Gate: directory.Gate{Client: dir, Store: store, Source: reqsign.IDOf(sig.IdentityKey())}}
}

// deviceRecord is a device's directory record under id: its name sealed under
// id's metadata key, and what the machine can do.
func deviceRecord(id identity.Identity, name string) (directory.Device, error) {
	sealed, err := directory.SealName(directory.MetadataKey(id.CellKey()), name)
	return directory.Device{
		V: 1, Name: sealed, AddedBy: id.ID(), Platform: runtime.GOOS,
		Caps: directory.Caps{OS: runtime.GOOS, Arch: runtime.GOARCH, Cow: "none"},
	}, err
}
