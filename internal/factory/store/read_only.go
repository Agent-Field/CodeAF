package store

import "github.com/Agent-Field/codeaf/internal/factory"

// ReadOnly decodes one item document without locking, writing, retaining
// output or repairing repository identities. Atomic replacement lets a caller
// holding another item's lock safely read a spending snapshot by id.
func (st *Store) ReadOnly(id int) (factory.Item, error) {
	if err := checkID(id); err != nil {
		return factory.Item{}, ErrNotFound
	}
	d, err := st.read(st.ItemPath(id))
	if err != nil {
		return factory.Item{}, err
	}
	return d.Item, nil
}
