package relayserve

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// identityID is the only shape of identity id that may become part of a path:
// "id_" and 32 lowercase hex. The id arrives from a verified signature, but the
// check is repeated here because a path built from a string is a traversal
// waiting for the day some other caller passes one that was never verified.
var identityID = regexp.MustCompile(`^id_[0-9a-f]{32}$`)

func checkedID(id string) error {
	if !identityID.MatchString(id) {
		return fmt.Errorf("relayserve: %q is not an identity id", id)
	}
	return nil
}

// namespaces gives each identity its own directory file and its own blob
// tree under one root, so an identity can only reach what its id names.
type namespaces struct {
	root     string
	now      func() time.Time
	grace    directory.GraceBounds // what a retire may ask for; the zero value is the default
	watchers int                   // the per-identity watch cap; zero is directory.MaxWatchers

	after directory.After // the timers presence runs on; nil is the wall clock's

	links directory.Decider // where an approve decides a pending request; set once by New

	mu   sync.Mutex
	dirs map[string]*directory.SQLite
}

func newNamespaces(root string, now func() time.Time, grace directory.GraceBounds, watchers int) *namespaces {
	if grace == (directory.GraceBounds{}) {
		grace = directory.DefaultGraceBounds
	}
	return &namespaces{root: root, now: now, grace: grace, watchers: watchers, dirs: map[string]*directory.SQLite{}}
}

// directory opens (once) the SQLite directory of one identity. A SQLite file
// is one identity's directory, so the file is per identity: <root>/directory/<id>.db.
func (n *namespaces) directory(id string) (directory.Directory, error) {
	return n.sqlite(id)
}

// sqlite is directory with its concrete type, for what only a relay may ask.
func (n *namespaces) sqlite(id string) (*directory.SQLite, error) {
	if err := checkedID(id); err != nil {
		return nil, err
	}
	if n.gone(id) {
		return nil, wireauth.ErrGone
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if d, ok := n.dirs[id]; ok {
		return d, nil
	}
	dir := filepath.Join(n.root, "directory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	d, err := directory.OpenSQLite(filepath.Join(dir, id+".db"), n.now)
	if err != nil {
		return nil, err
	}
	d.SetLinks(n.links)
	d.SetGraceBounds(n.grace)
	if n.watchers > 0 {
		d.SetMaxWatchers(n.watchers)
	}
	if n.after != nil {
		d.SetAfter(n.after)
	}
	n.dirs[id] = d
	return d, nil
}

// blobs opens one identity's disk store at <root>/blobs/<id>/. A replaced
// identity's store takes no frame: reads stay open for the grace period.
func (n *namespaces) blobs(id string) (blobstore.Store, error) {
	d, err := n.sqlite(id)
	if err != nil {
		return nil, err
	}
	disk, err := blobstore.NewDisk(filepath.Join(n.root, "blobs", id))
	if err != nil {
		return nil, err
	}
	return readOnlyOnceReplaced{Store: disk, rotation: d.Rotated}, nil
}

// readOnlyOnceReplaced is a store that refuses a put when its identity has been
// replaced by a rotation, and is the store unchanged otherwise.
type readOnlyOnceReplaced struct {
	blobstore.Store
	rotation func() *directory.Rotation
}

func (r readOnlyOnceReplaced) PutFrame(ctx context.Context, frame []byte) (blobstore.FrameID, error) {
	if r.rotation() != nil {
		return "", wireauth.ErrRotated
	}
	return r.Store.PutFrame(ctx, frame)
}

// Close releases every directory file this relay opened.
func (n *namespaces) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	var first error
	for id, d := range n.dirs {
		if err := d.Close(); err != nil && first == nil {
			first = err
		}
		delete(n.dirs, id)
	}
	return first
}

// revoked says whether device has been stopped by its identity. It opens the
// identity's directory (once) and then answers from memory, so the check costs
// no query per request.
func (n *namespaces) revoked(identity, device string) (bool, error) {
	d, err := n.sqlite(identity)
	if err != nil {
		return false, err
	}
	return d.Revoked(device), nil
}
