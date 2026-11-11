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

// namespaces isolates each identity's frozen source, three durable anchors
// and encrypted blob tree under the identity proven by its request.
type namespaces struct {
	root     string
	now      func() time.Time
	grace    directory.GraceBounds // what a retire may ask for; the zero value is the default
	watchers int                   // the per-identity watch cap; zero is directory.MaxWatchers

	after directory.After // the timers presence runs on; nil is the wall clock's

	links directory.Decider // where an approve decides a pending request; set once by New

	activation sync.Mutex
	groups     map[string]*nativeNamespace
	mu         sync.Mutex
	dirs       map[string]*directory.SQLite
	logf       func(string, ...any)
	faultMu    sync.Mutex
	faults     map[string]time.Time
}

func newNamespaces(root string, now func() time.Time, grace directory.GraceBounds, watchers int) *namespaces {
	if grace == (directory.GraceBounds{}) {
		grace = directory.DefaultGraceBounds
	}
	return &namespaces{root: root, now: now, grace: grace, watchers: watchers, dirs: map[string]*directory.SQLite{}, groups: map[string]*nativeNamespace{}}
}

func (n *namespaces) group(id string) *nativeNamespace {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.groups[id]
}

// directory resolves only the irrevocably published consensus owner. Source
// access stays inside activation and never becomes a fallback request writer.
func (n *namespaces) directory(id string) (directory.Directory, error) {
	return n.activate(context.Background(), id, nil)
}

// sqlite opens the preserved migration source, never a live request backend.
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
	d, err := n.activate(context.Background(), id, nil)
	if err != nil {
		return nil, err
	}
	disk, err := blobstore.NewDisk(filepath.Join(n.root, "blobs", id))
	if err != nil {
		return nil, err
	}
	return CommittedBlobStore{Store: disk, Authorize: func(ctx context.Context) error {
		actor, _ := ctx.Value(blobActorKey{}).(string)
		if actor == "" {
			return wireauth.ErrUnauthorized
		}
		return d.authority(ctx, actor, true)
	}}, nil
}

// Close releases every directory file this relay opened.
func (n *namespaces) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	var first error
	for id, d := range n.groups {
		if err := d.Close(); err != nil && first == nil {
			first = err
		}
		delete(n.groups, id)
	}
	for id, d := range n.dirs {
		if err := d.Close(); err != nil && first == nil {
			first = err
		}
		delete(n.dirs, id)
	}
	return first
}

// CommittedBlobStore checks current committed authority on both sides of an
// external put. A revocation or freeze during disk/R2 I/O leaves the bytes for
// reuse but returns no successful upload to that caller. The receipt command
// rechecks authority again at its own log position before any head may move.
// Namespace activation installs it only after the old writer is frozen.
type CommittedBlobStore struct {
	blobstore.Store
	Authorize func(context.Context) error
}

func (s CommittedBlobStore) PutFrame(ctx context.Context, frame []byte) (blobstore.FrameID, error) {
	if s.Authorize == nil {
		return "", wireauth.ErrUnauthorized
	}
	if err := s.Authorize(ctx); err != nil {
		return "", err
	}
	id, err := s.Store.PutFrame(ctx, frame)
	if err != nil {
		return "", err
	}
	if err = s.Authorize(ctx); err != nil {
		return "", err
	}
	return id, nil
}

// ConfirmReferences preserves the disk store's extra durability boundary through
// this authorization wrapper; embedding Store alone would hide that method.
func (s CommittedBlobStore) ConfirmReferences(ctx context.Context, head string, frames []string) error {
	if s.Authorize == nil {
		return wireauth.ErrUnauthorized
	}
	if err := s.Authorize(ctx); err != nil {
		return err
	}
	if err := blobstore.CheckReferences(ctx, s.Store, head, frames); err != nil {
		return err
	}
	return s.Authorize(ctx)
}
