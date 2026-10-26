package main

// App lifecycle footers belong to the conversation, but never to its model's
// transcript. This sidecar keeps close/quit and permission receipts together
// and anchors each even when timestamps are identical.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

const appFootersName = "app-footers.json"

type appKeptFooter struct {
	After int `json:"after,omitempty"`
	// A nil AfterID reads the old positional format; an empty id anchors before
	// everything. The fingerprint also distinguishes reused positional wire ids
	// after a rewind, without depending on a tool result that may arrive later.
	AfterID   *string       `json:"after_id,omitempty"`
	AfterHash string        `json:"after_hash,omitempty"`
	Entry     appwire.Entry `json:"entry"`
}

func (b *appBackend) readAppFooters(transcript string) ([]appKeptFooter, error) {
	b.footerMu.Lock()
	defer b.footerMu.Unlock()
	return appReadFooters(transcript)
}

func appReadFooters(transcript string) ([]appKeptFooter, error) {
	dir := session.DirOf(transcript)
	kept, err := appReadFootersAt(filepath.Join(dir, appFootersName))
	if err != nil || dir == filepath.Dir(transcript) {
		return kept, err
	}
	// Earlier app builds saved cell receipts beside the journal. Read both
	// files so the next write carries them forward without removing the old one.
	old, err := appReadFootersAt(filepath.Join(filepath.Dir(transcript), appFootersName))
	if err != nil {
		return nil, err
	}
	merged := make([]appKeptFooter, 0, len(old)+len(kept))
	positions := make(map[string]int, len(old)+len(kept))
	for _, f := range append(old, kept...) {
		if at, ok := positions[f.Entry.ID]; ok {
			// The corrected file wins when both copies name the same receipt.
			merged[at] = f
		} else {
			positions[f.Entry.ID] = len(merged)
			merged = append(merged, f)
		}
	}
	return merged, nil
}

func appReadFootersAt(path string) ([]appKeptFooter, error) {
	raw, err := os.ReadFile(path)
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
	var id string
	b.locked(func() { id = a.view.ID })
	return b.keepAppEntry(a, appFooter(key, id, 0, kind, words, b.now()), "")
}

// keepAppEntry uses the same sidecar for permission receipts, including their
// links and explanations. A call anchors its receipt after its tool row, even
// when the event consumer is behind the journal writer.
func (b *appBackend) keepAppEntry(a *appAgent, entry appwire.Entry, afterCall string) error {
	return b.keepAppEntries(a, []appwire.Entry{entry}, afterCall)
}

// A read-only status exchange is kept atomically, outside the model transcript.
func (b *appBackend) keepAppEntries(a *appAgent, entries []appwire.Entry, afterCall string) error {
	var transcript, id string
	b.locked(func() { transcript, id = a.transcript, a.view.ID })
	b.footerMu.Lock()
	defer b.footerMu.Unlock()
	kept, err := appReadFooters(transcript)
	if err != nil {
		return err
	}
	pending := make([]appwire.Entry, 0, len(entries))
	seen := make(map[string]bool, len(kept)+len(entries))
	for _, f := range kept {
		seen[f.Entry.ID] = true
	}
	for _, entry := range entries {
		if !seen[entry.ID] {
			pending = append(pending, entry)
			seen[entry.ID] = true
		}
	}
	if len(pending) == 0 {
		return nil
	}
	afterID, afterHash := "", ""
	if err := appRecords.WithRecord(transcript, func(record session.Record) error {
		items := appRecordItems(record)
		shapes, from := appEntryShapes(id, items, b.now(), nil)
		for i, shape := range shapes {
			afterID = shape.ID
			afterHash = appFooterAnchor(items[from[i]])
			if afterCall != "" && shape.Kind == appwire.EntryTool && shape.Call == afterCall {
				break
			}
		}
		return nil
	}); err != nil {
		return err
	}
	for _, entry := range pending {
		kept = append(kept, appKeptFooter{AfterID: &afterID, AfterHash: afterHash, Entry: entry})
	}
	raw, err := json.Marshal(kept)
	if err != nil {
		return err
	}
	path := filepath.Join(session.DirOf(transcript), appFootersName)
	temp, err := os.CreateTemp(filepath.Dir(path), appFootersName+".*.new")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
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

func appInsertFooters(all []appwire.Entry, from []int, kept []appKeptFooter, items []session.DisplayEntry) ([]appwire.Entry, []int) {
	inserted := 0
	for _, f := range kept {
		at, ok := appFooterPosition(all, from, f, inserted, items)
		if !ok {
			continue
		}
		all = append(all, appwire.Entry{})
		copy(all[at+1:], all[at:])
		all[at] = f.Entry
		from = append(from, -1)
		copy(from[at+1:], from[at:])
		from[at] = -1
		inserted++
	}
	return all, from
}

func appFooterPosition(all []appwire.Entry, from []int, f appKeptFooter, inserted int, items []session.DisplayEntry) (int, bool) {
	if f.AfterID == nil {
		return min(max(f.After+inserted, 0), len(all)), true
	}
	at := 0
	if *f.AfterID != "" {
		at = -1
		for i, entry := range all {
			if entry.ID != *f.AfterID {
				continue
			}
			if f.AfterHash != "" && (from[i] < 0 || appFooterAnchor(items[from[i]]) != f.AfterHash) {
				return 0, false
			}
			at = i + 1
			break
		}
		if at < 0 {
			return 0, false
		}
	}
	// Receipts sharing an anchor retain the order they were written in.
	for at < len(from) && from[at] == -1 {
		at++
	}
	return at, true
}

func appFooterAnchor(item session.DisplayEntry) string {
	stable := struct {
		Role, Text, Tool, CallID, Args, RunID string
		At                                    string
	}{item.Role, item.Text, item.Tool, item.CallID, item.Args, item.RunID, item.At.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")}
	raw, _ := json.Marshal(stable)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
