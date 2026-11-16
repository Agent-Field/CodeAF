package cellsync

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
)

// The fake's half of the read seam (read.go): the path-scoped want, peek and
// read the real engine answers, over the fake's graph. The fake snapshot names
// its files directly, so the path to a file is the snapshot and the file's own
// object; a chunked file adds its chunks.

var (
	_ PathEngine = (*FakeEngine)(nil)
	_ PathEngine = cellstore.SyncEngine{}
)

// WantPath implements PathEngine: the snapshot, then the file's object, then —
// when ranged — the chunks overlapping [start, end). It never follows the
// snapshot's parent or any other file, which is the whole point of it.
func (f *FakeEngine) WantPath(_ context.Context, c cell.Cell, head, path string, ranged bool, start, end int64) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fc := f.cell(c)
	snap, ok, err := fc.snapAt(head)
	if err != nil || !ok {
		return []string{head}, err
	}
	rid, ok := snap.Files[path]
	if !ok {
		return nil, fmt.Errorf("fake engine: %w: %s", ErrNoPath, path)
	}
	raw, ok := fc.objects[rid]
	if !ok {
		return []string{rid}, nil
	}
	kind, payload, err := unpack(raw)
	if err != nil || !ranged || kind != kindFile {
		return nil, err
	}
	var idx fileIndex
	if err := json.Unmarshal(payload, &idx); err != nil {
		return nil, err
	}
	var want []string
	at := int64(0)
	for _, ch := range idx.Chunks {
		if at < end && at+ch.Len > start {
			if _, held := fc.objects[ch.RID]; !held {
				want = append(want, ch.RID)
			}
		}
		at += ch.Len
	}
	return want, nil
}

// Peek implements PathEngine without the network: an object on the way that
// is not held is ErrIncomplete.
func (f *FakeEngine) Peek(_ context.Context, c cell.Cell, head, path string) (Peek, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fc := f.cell(c)
	idx, err := fc.indexAt(head, path)
	if err != nil {
		return Peek{}, err
	}
	out := Peek{Kind: "file", Len: idx.Len}
	at := int64(0)
	for _, ch := range idx.Chunks {
		_, held := fc.objects[ch.RID]
		out.Chunks = append(out.Chunks, PeekChunk{Start: at, End: at + ch.Len, Held: held})
		at += ch.Len
	}
	return out, nil
}

// ReadRange implements PathEngine from held chunks only.
func (f *FakeEngine) ReadRange(_ context.Context, c cell.Cell, head, path string, start, end int64, out string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fc := f.cell(c)
	idx, err := fc.indexAt(head, path)
	if err != nil {
		return 0, err
	}
	end = min(end, idx.Len)
	var buf []byte
	at := int64(0)
	for _, ch := range idx.Chunks {
		if at < end && at+ch.Len > start {
			raw, held := fc.objects[ch.RID]
			if !held {
				return 0, fmt.Errorf("fake engine: %w: chunk %s", ErrIncomplete, ch.RID)
			}
			_, piece, _ := unpack(raw)
			lo, hi := max(start-at, 0), min(end-at, ch.Len)
			buf = append(buf, piece[lo:hi]...)
		}
		at += ch.Len
	}
	return int64(len(buf)), os.WriteFile(out, buf, 0o600)
}

// ImportPath implements PathEngine: the fake's import, which needs no folder.
func (f *FakeEngine) ImportPath(ctx context.Context, c cell.Cell, head, inbox string) (int, error) {
	return f.Import(ctx, c, head, inbox)
}

// snapAt is head's snapshot when it is held.
func (fc *fakeCell) snapAt(head string) (snapshot, bool, error) {
	raw, ok := fc.objects[head]
	if !ok {
		return snapshot{}, false, nil
	}
	_, payload, err := unpack(raw)
	if err != nil {
		return snapshot{}, false, err
	}
	s, err := decodeSnapshot(payload)
	return s, err == nil, err
}

// indexAt is path's chunk list at head; a whole blob is one chunk.
func (fc *fakeCell) indexAt(head, path string) (fileIndex, error) {
	snap, ok, err := fc.snapAt(head)
	if err != nil {
		return fileIndex{}, err
	}
	if !ok {
		return fileIndex{}, fmt.Errorf("fake engine: %w: snapshot %s", ErrIncomplete, head)
	}
	rid, ok := snap.Files[path]
	if !ok {
		return fileIndex{}, fmt.Errorf("fake engine: %w: %s", ErrNoPath, path)
	}
	raw, ok := fc.objects[rid]
	if !ok {
		return fileIndex{}, fmt.Errorf("fake engine: %w: index %s", ErrIncomplete, rid)
	}
	kind, payload, err := unpack(raw)
	if err != nil {
		return fileIndex{}, err
	}
	if kind != kindFile {
		return fileIndex{Len: int64(len(payload)), Chunks: []fileChunk{{RID: rid, Len: int64(len(payload))}}}, nil
	}
	var idx fileIndex
	return idx, json.Unmarshal(payload, &idx)
}
