package preflight

import "github.com/Agent-Field/codeaf/internal/inventory"

// Machine is one cell's view of the device it runs on: the observed inventory,
// and the observer that keeps it true. It is the door the chat's setup command
// walks through (docs/ARCHITECTURE.md 8.4).
type Machine struct {
	store *inventory.Store
	obs   *inventory.Observer
	caps  func(inventory.Inventory) Capabilities
}

// OpenMachine opens the inventory of the cell rooted at root.
func OpenMachine(root string) (*Machine, error) {
	store, err := inventory.Open(root)
	if err != nil {
		return nil, err
	}
	return &Machine{store: store, obs: inventory.NewObserver(store, nil, nil), caps: LocalFor}, nil
}

// Observer is what the executor tells about every call that ran.
func (m *Machine) Observer() *inventory.Observer { return m.obs }

// Plan is the preflight of this device against what the chat has used.
func (m *Machine) Plan() Report {
	inv := m.store.Snapshot()
	return Check(inv, m.caps(inv))
}

// Settle looks at every tool the report called installable and records what is
// now there. It reads the machine and writes only through the observer (L13).
func (m *Machine) Settle(r Report) {
	for _, it := range r.Pending() {
		m.obs.Sight(it.Name)
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
