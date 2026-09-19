package enginehost

// collab.go is the host's half of cross-chat delivery: find the engine that
// owns a conversation, reconnect it, or refuse to pretend the line was
// recorded.
//
// A HOST CAN IDLE-RETIRE, and that is the product, not a fault. Collaboration
// still has to land: an authorized delivery either wakes the process that
// holds the recipient's journal, or leaves the envelope pending in the
// durable outbox. Claiming recorded here would be a second in-process journal,
// which is the one thing Wave 3 forbids. Citing a chat as evidence is not an
// authorized delivery and must not spawn anything (J16, J23).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/wscollab"
)

// Delivery is what this package will say about one recipient. Recorded is
// never true here: the journal seam is the only writer that may set that
// fact, and a retired host has no seam.
type Delivery struct {
	Live     bool
	Pending  bool
	Recorded bool
}

// Locator finds the engine host that owns a conversation. It is
// [wscollab.HostFinder], bound at load the way the run engine binds
// [session.RegisterRunEngine].
//
// Spawn, when set, is how an authorized delivery reconstitutes a host that
// has idle-retired. Nil is the honest default: leave the envelope pending
// rather than invent a success.
type Locator struct {
	Spawn func(workspace string) error
}

var (
	_ wscollab.HostFinder = Locator{}
	_ wscollab.Host       = (*recipient)(nil)
)

// Find locates the host that owns this conversation. An unknown id is
// [wscollab.ErrRetired]: there is nobody to wake, and the outbox stays pending.
func (l Locator) Find(_ context.Context, conversationID string) (wscollab.Host, error) {
	return l.lookup(conversationID)
}

// Cite names a chat as evidence and does not dial, attach, or spawn. The
// router already keeps this path off [Find]; the method exists so a caller
// that only has a Locator cannot wake a host by accident.
func (l Locator) Cite(conversationID string) wscollab.Citation {
	return wscollab.Citation{SessionID: conversationID, Woke: false}
}

// Place is the host's answer for one delivery: reconnect an authorized
// recipient, or leave a pending envelope. Evidence (authorized false) is
// always pending and never recorded.
func (l Locator) Place(ctx context.Context, conversationID string, authorized bool) Delivery {
	if !authorized {
		return Delivery{Pending: true}
	}
	rec, err := l.lookup(conversationID)
	if err != nil {
		return Delivery{Pending: true}
	}
	if err := rec.Wake(ctx, conversationID); err != nil {
		return Delivery{Pending: true}
	}
	return Delivery{Live: rec.listening()}
}

func (l Locator) lookup(conversationID string) (*recipient, error) {
	workspace, err := workspaceOf(conversationID)
	if err != nil {
		return nil, err
	}
	return &recipient{workspace: workspace, spawn: l.Spawn}, nil
}

// recipient is one workspace's host as the router sees it.
type recipient struct {
	workspace string
	spawn     func(workspace string) error
}

// Alive is "this host can be asked to take a line": it is listening, or an
// authorized delivery has a spawn and may reconstitute it. A retired host
// with no spawn is not alive, so the router leaves the envelope pending and
// does not call [recipient.Wake].
func (r *recipient) Alive() bool {
	if r.listening() {
		return true
	}
	return r.spawn != nil
}

// Wake reconnects a live host, or spawns one for an authorized delivery.
// Failure is [wscollab.ErrRetired]: pending, never recorded.
func (r *recipient) Wake(_ context.Context, _ string) error {
	if r.listening() {
		return nil
	}
	if r.spawn == nil {
		return wscollab.ErrRetired
	}
	_, err := Attach(r.workspace, func() error { return r.spawn(r.workspace) })
	if err != nil {
		return wscollab.ErrRetired
	}
	return nil
}

func (r *recipient) listening() bool { return hostHolds(r.workspace) }

// hostHolds is a host that has taken its lock and still has a socket, asked
// WITHOUT dialling. A Dial is a surface: it would count as work and race the
// reaper (stale_test.go states why). The lock and the socket file are what a
// host publishes without being spoken to.
func hostHolds(workspace string) bool {
	socket, err := SocketPath(workspace)
	if err != nil {
		return false
	}
	if _, err := os.Stat(socket); err != nil {
		return false
	}
	dir, err := where(workspace)
	if err != nil {
		return false
	}
	held, err := takeLock(filepath.Join(dir, lockName))
	if err != nil {
		return true
	}
	_ = releaseLock(held)
	return false
}

func workspaceOf(conversationID string) (string, error) {
	id := strings.TrimSpace(conversationID)
	if id == "" {
		return "", fmt.Errorf("engine host: no conversation to deliver to")
	}
	root := home.Join("v3", "projects")
	buckets, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return "", wscollab.ErrRetired
		}
		return "", err
	}
	return workspaceIn(root, buckets, id)
}

func workspaceIn(root string, buckets []os.DirEntry, id string) (string, error) {
	for _, bucket := range buckets {
		if !bucket.IsDir() {
			continue
		}
		if workspace, ok := workspaceFromMeta(filepath.Join(root, bucket.Name(), id, "meta.json")); ok {
			return workspace, nil
		}
	}
	return "", wscollab.ErrRetired
}

// conversationMeta is the one field this lookup needs from a session folder.
// The rest of meta.json belongs to internal/session; reading it here would
// import the package the collab door exists to keep out of this cycle.
type conversationMeta struct {
	Workspace string `json:"workspace"`
}

func workspaceFromMeta(path string) (string, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var meta conversationMeta
	if json.Unmarshal(raw, &meta) != nil {
		return "", false
	}
	workspace := strings.TrimSpace(meta.Workspace)
	return workspace, workspace != ""
}
