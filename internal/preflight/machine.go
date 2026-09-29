package preflight

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/inventory"
)

// toolDirs are the workspace's own tool directories. A sandboxed setup turn can
// write only inside the workspace, so what it installs lands here, and a tool
// there counts as present on this machine.
var toolDirs = []string{".venv/bin", "node_modules/.bin", "bin", ".local/bin"}

// Machine is one cell's view of the device it runs on: the observed inventory,
// and the observer that keeps it true. It is the door the chat's setup command
// walks through (docs/ARCHITECTURE.md 8.4).
type Machine struct {
	store *inventory.Store
	obs   *inventory.Observer
	path  string // where a tool is looked for: the workspace's tool directories, then PATH
}

// OpenMachine opens the inventory of the cell rooted at root, whose tools run in
// workspace.
func OpenMachine(root, workspace string) (*Machine, error) {
	store, err := inventory.Open(root)
	if err != nil {
		return nil, err
	}
	return &Machine{store: store, obs: inventory.NewObserver(store, nil, nil), path: searchPath(workspace)}, nil
}

// Observer is what the executor tells about every call that ran.
func (m *Machine) Observer() *inventory.Observer { return m.obs }

// Plan is the preflight of this device against what the chat has used.
func (m *Machine) Plan() Report {
	inv := m.store.Snapshot()
	return Check(inv, LocalFor(inv, m.path))
}

// Settle looks at every tool the report called installable and records what is
// now there. It reads the machine and writes only through the observer (L13).
func (m *Machine) Settle(r Report) {
	for _, it := range r.Pending() {
		m.obs.Sight(it.Name, m.path)
	}
}

// Pending is the items that setup could act on.
func (r Report) Pending() []Item {
	var out []Item
	for _, it := range r.Items {
		if it.Bucket == Installable {
			out = append(out, it)
		}
	}
	return out
}

// searchPath is the workspace's tool directories followed by the process PATH.
func searchPath(workspace string) string {
	dirs := make([]string, 0, len(toolDirs)+1)
	for _, d := range toolDirs {
		dirs = append(dirs, filepath.Join(workspace, filepath.FromSlash(d)))
	}
	return strings.Join(append(dirs, os.Getenv("PATH")), string(os.PathListSeparator))
}
