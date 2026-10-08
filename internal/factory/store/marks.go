package store

import (
	"errors"
	"strings"
)

// ── THE FLOOR'S OWN MARKS, AND ITS FOREMAN ─────────────────────────────────
//
// A mark on the floor used to be the window's alone, held on the page by id
// until codeaf exited. The foreman (the floor's own conversation, `m`) proposes
// a morning batch by marking items, and it runs in whichever process holds the
// conversation, which is not always the one drawing the floor. So the marks it
// makes are kept HERE, in the floor's one meta record beside the rail, and every
// window reads them on its next Load. The page still keeps the person's own
// `space` toggles; these are the floor's.
//
// The foreman's conversation is named here too, by its session file, so the
// second `m` opens the conversation the first one made.

// Marks is the floor's marks by item id, in the order they were made; none is
// nil and no error.
func (st *Store) Marks() ([]int, error) {
	if st == nil {
		return nil, nil
	}
	d, err := st.readMeta()
	return d.Marked, err
}

// SetMarks marks ids (on) or takes their marks off, keeping every other mark
// and the rest of the record, as one read-modify-write under the record's
// flock. A mark already there is not made twice.
func (st *Store) SetMarks(ids []int, on bool) error {
	if st == nil {
		return errors.New("factory store: no store")
	}
	return st.changeMeta(func(d *metaDoc) {
		have := map[int]bool{}
		for _, id := range d.Marked {
			have[id] = true
		}
		if on {
			for _, id := range ids {
				if id > 0 && !have[id] {
					have[id] = true
					d.Marked = append(d.Marked, id)
				}
			}
			return
		}
		drop := map[int]bool{}
		for _, id := range ids {
			drop[id] = true
		}
		kept := d.Marked[:0]
		for _, id := range d.Marked {
			if !drop[id] {
				kept = append(kept, id)
			}
		}
		d.Marked = kept
		if len(d.Marked) == 0 {
			d.Marked = nil
		}
	})
}

// Foreman is the floor's own conversation by its session file, "" when none
// was made.
func (st *Store) Foreman() (string, error) {
	if st == nil {
		return "", nil
	}
	d, err := st.readMeta()
	return d.Foreman, err
}

// SetForeman keeps the floor's own conversation's session file.
func (st *Store) SetForeman(chat string) error {
	return st.changeMeta(func(d *metaDoc) { d.Foreman = strings.TrimSpace(chat) })
}
