package main

// App lifecycle footers belong to the conversation, but never to its model's
// transcript. This sidecar keeps the three close/quit receipts together and
// anchors each before the next turn even when timestamps are identical.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

const appFootersName = "app-footers.json"

type appKeptFooter struct {
	After int           `json:"after"`
	Entry appwire.Entry `json:"entry"`
}

func (b *appBackend) readAppFooters(transcript string) ([]appKeptFooter, error) {
	b.footerMu.Lock()
	defer b.footerMu.Unlock()
	return appReadFooters(transcript)
}

func appReadFooters(transcript string) ([]appKeptFooter, error) {
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(transcript), appFootersName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var kept []appKeptFooter
	err = json.Unmarshal(raw, &kept)
	return kept, err
}

// keepAppFooter saves before emitting or starting work. A deterministic key
// lets a retry after a failed state-file write reuse the same receipt.
func (b *appBackend) keepAppFooter(a *appAgent, key, kind, words string) error {
	var transcript, id string
	b.locked(func() { transcript, id = a.transcript, a.view.ID })
	b.footerMu.Lock()
	defer b.footerMu.Unlock()
	kept, err := appReadFooters(transcript)
	if err != nil {
		return err
	}
	for _, f := range kept {
		if f.Entry.ID == key {
			return nil
		}
	}
	after := 0
	if err := appRecords.WithRecord(transcript, func(record session.Record) error {
		shapes, _ := appEntryShapes(id, appRecordItems(record), b.now(), nil)
		after = len(shapes)
		return nil
	}); err != nil {
		return err
	}
	kept = append(kept, appKeptFooter{After: after, Entry: appFooter(key, id, 0, kind, words, b.now())})
	raw, err := json.Marshal(kept)
	if err != nil {
		return err
	}
	path := filepath.Join(filepath.Dir(transcript), appFootersName)
	if err := os.WriteFile(path+".new", raw, 0o600); err != nil {
		return err
	}
	return os.Rename(path+".new", path)
}

func (b *appBackend) emitAppFooter(a *appAgent, key string) {
	var transcript string
	var live *appLive
	b.locked(func() { transcript, live = a.transcript, a.live })
	kept, err := b.readAppFooters(transcript)
	if err != nil {
		return
	}
	for _, f := range kept {
		if f.Entry.ID != key {
			continue
		}
		data := appwire.EntryAdded{Entry: f.Entry}
		if live != nil {
			live.box.Event(appwire.EventEntryAdded, data)
		} else {
			b.emit.Emit(appwire.EventEntryAdded, data)
		}
		return
	}
}

func appInsertFooters(all []appwire.Entry, from []int, kept []appKeptFooter) ([]appwire.Entry, []int) {
	for n, f := range kept {
		at := min(max(f.After+n, 0), len(all))
		all = append(all, appwire.Entry{})
		copy(all[at+1:], all[at:])
		all[at] = f.Entry
		from = append(from, -1)
		copy(from[at+1:], from[at:])
		from[at] = -1
	}
	return all, from
}
