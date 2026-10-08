package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// busy.go keeps what is in flight on the floor: the item the triage worker is
// reading, the items a refresh is reading again, and the whole floor's
// refresh line. It is a file of its own, <root>/busy.json, beside the items,
// because THE PROCESS DOING THE WORK IS NOT ALWAYS THE ONE DRAWING IT: the
// engine reads and the window draws, and both read this one file.
//
// A CRASH NEVER LEAVES A STALE SPINNER. Every entry carries the process id
// that wrote it, and a read drops an entry whose process is gone; a process
// starting also clears its floor ([Store.ClearBusy]) before it starts any
// work.

type busyEntry struct {
	Word string `json:"word"`
	Pid  int    `json:"pid"`
}

type busyDoc struct {
	Schema int                  `json:"schema"`
	Items  map[string]busyEntry `json:"items,omitempty"`
	All    *busyEntry           `json:"all,omitempty"`
}

// BusyPath is <root>/busy.json.
func (st *Store) BusyPath() string { return filepath.Join(st.root, "busy.json") }

// alive says whether the process that wrote an entry is still running. It is
// a variable so a test can pretend a process died.
var alive = defaultAlive

var defaultAlive = processgroup.ProcessAlive

// SetBusy marks one item as in flight with word (`reading`, `refreshing`), or
// clears it when word is "".
func (st *Store) SetBusy(id int, word string) error {
	if err := checkID(id); err != nil {
		return err
	}
	return st.changeBusy(func(d *busyDoc) {
		key := strconv.Itoa(id)
		if word == "" {
			delete(d.Items, key)
			return
		}
		if d.Items == nil {
			d.Items = map[string]busyEntry{}
		}
		d.Items[key] = busyEntry{Word: word, Pid: os.Getpid()}
	})
}

// SetBusyAll writes the whole floor's refresh line, or clears it with "".
func (st *Store) SetBusyAll(words string) error {
	return st.changeBusy(func(d *busyDoc) {
		if words == "" {
			d.All = nil
			return
		}
		d.All = &busyEntry{Word: words, Pid: os.Getpid()}
	})
}

// Busy reads what is in flight: each busy item's word by id, and the floor's
// refresh line. An entry whose process is gone is left out. Nothing in flight
// is a nil map and "".
func (st *Store) Busy() (map[int]string, string, error) {
	if st == nil {
		return nil, "", nil
	}
	d, err := st.readBusy()
	if err != nil {
		return nil, "", err
	}
	var items map[int]string
	for key, e := range d.Items {
		id, err := strconv.Atoi(key)
		if err != nil || e.Word == "" || !alive(e.Pid) {
			continue
		}
		if items == nil {
			items = map[int]string{}
		}
		items[id] = e.Word
	}
	all := ""
	if d.All != nil && alive(d.All.Pid) {
		all = d.All.Word
	}
	return items, all, nil
}

// ClearBusy is a process starting: every entry whose process is gone is
// dropped, and every source's polling mark is taken off, so the floor never
// draws work a process that crashed was doing. Entries a live process wrote
// stay, because another window may be in the middle of them.
func (st *Store) ClearBusy() error {
	if st == nil {
		return nil
	}
	err := st.changeBusy(func(d *busyDoc) {
		for key, e := range d.Items {
			if !alive(e.Pid) || e.Pid == os.Getpid() {
				delete(d.Items, key)
			}
		}
		if d.All != nil && (!alive(d.All.Pid) || d.All.Pid == os.Getpid()) {
			d.All = nil
		}
	})
	if err != nil {
		return err
	}
	return st.changeMeta(func(d *metaDoc) {
		for name, m := range d.Sources {
			m.Polling = false
			d.Sources[name] = m
		}
	})
}

func (st *Store) changeBusy(change func(*busyDoc)) error {
	if st == nil {
		return errors.New("factory store: no store")
	}
	if err := os.MkdirAll(st.root, 0o700); err != nil {
		return err
	}
	return st.underLock(filepath.Join(st.root, "busy.lock"), func() error {
		d, err := st.readBusy()
		if err != nil {
			return err
		}
		change(&d)
		d.Schema = Schema
		data, err := json.MarshalIndent(d, "", "  ")
		if err != nil {
			return err
		}
		return writeAtomic(st.BusyPath(), append(data, '\n'))
	})
}

func (st *Store) readBusy() (busyDoc, error) {
	data, err := os.ReadFile(st.BusyPath())
	if err != nil {
		if os.IsNotExist(err) {
			return busyDoc{}, nil
		}
		return busyDoc{}, err
	}
	var d busyDoc
	if err := json.Unmarshal(data, &d); err != nil {
		// A TORN RECORD OF WHAT IS IN FLIGHT IS NOTHING IN FLIGHT: it is redrawn
		// by the next unit of work, and refusing it would stop every write.
		return busyDoc{}, nil
	}
	if d.Schema > Schema {
		return busyDoc{}, fmt.Errorf("%w: busy.json", ErrNewer)
	}
	return d, nil
}
