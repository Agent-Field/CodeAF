package factory

import (
	"errors"
	"fmt"
	"sort"
)

// MarkKeeper is a store that keeps the floor's own marks, by item id
// (internal/factory/store's *Store is one). It is asked by assertion on the
// same terms as [RailKeeper]: a store that does not keep them has no
// [Seam.Mark] door, and a floor read from it carries no [Snapshot.Marked].
type MarkKeeper interface {
	Marks() ([]int, error)
	SetMarks(ids []int, on bool) error
}

// localMarks hangs the mark door on a local seam and folds the store's marks
// into every Load.
//
// A MARK IS ONLY EVER ON A NEW ITEM. The door refuses any other, and the read
// leaves out a kept mark whose item has since moved on (launched, hidden,
// shipped), so a mark is spent by the item leaving `new` without anybody
// taking it off: the floor never offers `L` on an item that is already a
// stream.
func localMarks(seam *Seam, st ItemStore) {
	keeper, ok := st.(MarkKeeper)
	if !ok || seam.Load == nil {
		return
	}
	load := seam.Load
	seam.Load = func() (Snapshot, error) {
		snap, err := load()
		if err != nil {
			return snap, err
		}
		marks, merr := keeper.Marks()
		if merr != nil {
			return snap, nil
		}
		FoldMarks(&snap, marks)
		return snap, nil
	}
	seam.Mark = func(ids []int, on bool) error {
		if len(ids) == 0 {
			return nil
		}
		if on {
			items, err := st.List()
			if err != nil {
				return err
			}
			state := map[int]State{}
			for _, it := range items {
				state[it.ID] = it.State
			}
			for _, id := range ids {
				s, ok := state[id]
				if !ok {
					return fmt.Errorf("there is no item %d", id)
				}
				if s != StateNew {
					return errors.New("only a new item can be selected")
				}
			}
		}
		return keeper.SetMarks(ids, on)
	}
}

// FoldMarks writes marks onto a snapshot: [Snapshot.Marked] is every marked
// id whose item is on the floor and new, smallest first, and each of those
// items is Marked. A surface that reads a snapshot from a seam it did not
// build calls it too, so a mark draws the same whoever folded it.
func FoldMarks(snap *Snapshot, marks []int) {
	if snap == nil || len(marks) == 0 {
		return
	}
	want := map[int]bool{}
	for _, id := range marks {
		want[id] = true
	}
	var kept []int
	for i := range snap.Items {
		it := &snap.Items[i]
		if want[it.ID] && it.State == StateNew {
			it.Marked = true
			kept = append(kept, it.ID)
		}
	}
	sort.Ints(kept)
	snap.Marked = kept
}
