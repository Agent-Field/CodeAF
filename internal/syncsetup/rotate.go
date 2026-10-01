package syncsetup

import (
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
	return rotate.Env{
		Home: s.Home, Identity: s.Identity, Device: s.Device, Say: say,
		Connect: s.relayOf,
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
	return rotate.Relay{Dir: dir, Store: blobstore.NewHTTP(s.Relay, sign, &http.Client{}), Gate: directory.Gate{Client: dir}}
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
