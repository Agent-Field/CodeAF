package relayserve

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/directory"
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
	root string
	now  func() time.Time

	mu   sync.Mutex
	dirs map[string]*directory.SQLite
}

func newNamespaces(root string, now func() time.Time) *namespaces {
	return &namespaces{root: root, now: now, dirs: map[string]*directory.SQLite{}}
}

// directory opens (once) the SQLite directory of one identity. A SQLite file
// is one identity's directory, so the file is per identity: <root>/directory/<id>.db.
func (n *namespaces) directory(id string) (directory.Directory, error) {
	if err := checkedID(id); err != nil {
		return nil, err
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
	n.dirs[id] = d
	return d, nil
}

// blobs opens one identity's disk store at <root>/blobs/<id>/.
func (n *namespaces) blobs(id string) (blobstore.Store, error) {
	if err := checkedID(id); err != nil {
		return nil, err
	}
	return blobstore.NewDisk(filepath.Join(n.root, "blobs", id))
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
