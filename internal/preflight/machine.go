package preflight

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/executor"
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
	store     *inventory.Store
	obs       *inventory.Observer
	path      string // where a tool is looked for: the workspace's tool directories, then PATH
	root      string // the cell's own folder, where a takeover leaves what it found
	workspace string
}

// OpenMachine opens the inventory of the cell rooted at root, whose tools run in
// workspace.
func OpenMachine(root, workspace string) (*Machine, error) {
	store, err := inventory.Open(root)
	if err != nil {
		return nil, err
	}
	obs := inventory.NewObserver(store, nil, nil)
	obs.At(workspace)
	return &Machine{store: store, obs: obs, path: searchPath(workspace), root: root, workspace: workspace}, nil
}

// Observer is what the executor tells about every call that ran.
func (m *Machine) Observer() *inventory.Observer { return m.obs }

// Started and Ended are how the job registry tells this machine a background
// command began and ended; the observer keeps the record of what is running.
func (m *Machine) Started(job executor.Job) { m.obs.Started(job) }

// Ended implements executor.Lifecycle.
func (m *Machine) Ended(id int) { m.obs.Ended(id) }

var _ executor.Lifecycle = (*Machine)(nil)

// Plan is the preflight of this device against what the chat has used, and what
// the chat left behind that is not here.
func (m *Machine) Plan() Report {
	inv := m.store.Snapshot()
	report := Check(inv, LocalFor(inv, m.path))
	report.Resume = m.resume(inv, report)
	return report
}

// Settle looks at every tool the report called installable and records what is
// now there. It reads the machine and writes only through the observer (L13).
// The setup turn that follows has been told everything a takeover left, so it
// is not told again.
func (m *Machine) Settle(r Report) {
	for _, it := range r.Pending() {
		m.obs.Sight(it.Name, m.path)
	}
	m.unstash()
}

// here is this machine as a resume looks at it.
func (m *Machine) here() Here { return LocalHere{Root: m.root, Workspace: m.workspace} }

// resume is what the chat left that is not here now, with what only the
// takeover could see (the commands that were running elsewhere) kept from it.
func (m *Machine) resume(inv inventory.Inventory, report Report) Resume {
	r := Absent(inv, m.here(), report)
	r.Now = m.workspace
	if kept, ok := m.stashed(); ok {
		r.From, r.Was, r.Stopped, r.Detached = kept.From, kept.Was, kept.Stopped, kept.Detached
	}
	return r
}

// Arrive is the takeover's look at the machine a chat just landed on, taken
// before this machine seals a record of its own over the one it received: what
// the other machine left out or left running, set against what is here. What it
// finds is kept for the chat's next step, so whichever surface opens the chat
// tells the agent once, and it is returned for the surface to offer.
func Arrive(root, workspace, from string) (Resume, error) {
	m, err := OpenMachine(root, workspace)
	if err != nil {
		return Resume{}, err
	}
	inv := m.store.Snapshot()
	r := Compare(from, inv, m.here(), Check(inv, LocalFor(inv, m.path)))
	r.Now = workspace
	return r, m.stash(r)
}

// News is what a chat that moved here owes its agent at the next step, once: the
// same facts as the offer, as news that asks for nothing. It is empty when there
// is nothing to tell or it was told already.
func (m *Machine) News() string {
	if _, ok := m.stashed(); !ok {
		return ""
	}
	inv := m.store.Snapshot()
	news := m.resume(inv, Check(inv, LocalFor(inv, m.path))).News()
	m.unstash()
	return news
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
