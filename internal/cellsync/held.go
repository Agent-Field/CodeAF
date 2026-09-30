package cellsync

import (
	"context"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
)

// askFloor is the smallest export worth a question. Asking costs one request
// and about seventy bytes an object, so it only pays when what would be sent
// is large; an ordinary turn sends less than this and is never asked about.
// A device whose ledger lacks a whole chat (it followed a rotation, pointed at
// a relay that already holds the chat, or lost its ledger) sends far more.
const askFloor = 64 << 10

// skipHeld removes from ex every object the store already holds, and records
// those objects as published so no later export offers them again. The ledger
// only knows what this device sent or took, so on a store it never published to
// every object looks unsent and the whole chat would cross the wire again even
// though the store dedups it. Asking is an optimisation: when the question
// fails the export is returned whole, and the puts that follow report the real
// error.
func (p *Publisher) skipHeld(ctx context.Context, c cell.Cell, ex Export) Export {
	if ex.Bytes < askFloor {
		return ex
	}
	held, err := p.holdings(ctx, ex.Frames)
	if err != nil || len(held) == 0 {
		return ex
	}
	out, err := p.sieve(ctx, c, ex.Frames, held)
	if err != nil {
		return ex
	}
	out.HeadRID = ex.HeadRID
	return out
}

// holdings asks the store, MaxHas ids to a request, which of the objects the
// frames carry it already holds.
func (p *Publisher) holdings(ctx context.Context, frames []FrameFile) (map[string]bool, error) {
	var rids []string
	for _, f := range frames {
		got, err := ridsIn(f.Path)
		if err != nil {
			return nil, err
		}
		rids = append(rids, got...)
	}
	held := map[string]bool{}
	for len(rids) > 0 {
		n := min(blobstore.MaxHas, len(rids))
		have, err := p.Store.Has(ctx, rids[:n])
		if err != nil {
			return nil, err
		}
		for i, h := range have {
			held[rids[i]] = h
		}
		rids = rids[n:]
	}
	return held, nil
}

func ridsIn(path string) ([]string, error) {
	frame, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	h, _, err := blobstore.Decode(frame)
	if err != nil {
		return nil, err
	}
	rids := make([]string, len(h.Objects))
	for i, o := range h.Objects {
		rids[i] = o.RID
	}
	return rids, nil
}

// sieve rewrites each frame into the part the store lacks, which is still to be
// sent, and the part it holds, which is recorded at once. A frame with nothing
// held is left alone. The ledger is written only for objects the store answered
// for, so a wrong record is impossible and a lost one only costs bytes.
func (p *Publisher) sieve(ctx context.Context, c cell.Cell, frames []FrameFile, held map[string]bool) (Export, error) {
	var out Export
	var have, replaced []string
	for _, f := range frames {
		rest, kept, err := divide(f, held)
		if err != nil {
			return Export{}, err
		}
		have = append(have, kept...)
		if rest.Path != f.Path {
			replaced = append(replaced, f.Path)
		}
		if rest.Objects > 0 {
			out.Frames = append(out.Frames, rest)
			out.Objects += rest.Objects
			out.Bytes += rest.Bytes
		}
	}
	if err := p.Engine.Published(ctx, c, have); err != nil {
		return Export{}, err
	}
	return out, removeAll(replaced)
}

// divide splits one frame file by whether the store holds each object. It
// answers the file still to send and, when something was held, the file of the
// held objects to record. Both files are named by content beside the original,
// which the caller removes once the record is made, so a repeat after a crash writes the same files.
func divide(f FrameFile, held map[string]bool) (rest FrameFile, kept []string, err error) {
	frame, err := os.ReadFile(f.Path)
	if err != nil {
		return FrameFile{}, nil, err
	}
	h, objects, err := blobstore.Decode(frame)
	if err != nil {
		return FrameFile{}, nil, err
	}
	have, lack := byHolding(objects, held)
	if len(have) == 0 {
		return f, nil, nil
	}
	record, err := writeBeside(f.Path, h.CellKeyID, have)
	if err != nil {
		return FrameFile{}, nil, err
	}
	if len(lack) > 0 {
		if rest, err = writeBeside(f.Path, h.CellKeyID, lack); err != nil {
			return FrameFile{}, nil, err
		}
	}
	return rest, []string{record.Path}, nil
}

// byHolding partitions objects into those the store holds and those it lacks.
func byHolding(objects []blobstore.Object, held map[string]bool) (have, lack []blobstore.Object) {
	for _, o := range objects {
		if held[o.RID] {
			have = append(have, o)
		} else {
			lack = append(lack, o)
		}
	}
	return have, lack
}

// writeBeside encodes objects as a frame file next to path, named by the
// frame's own id like every frame the engine writes.
func writeBeside(path, keyID string, objects []blobstore.Object) (FrameFile, error) {
	frame, err := blobstore.Encode(keyID, objects)
	if err != nil {
		return FrameFile{}, err
	}
	out := filepath.Join(filepath.Dir(path), blobstore.IDOf(frame)+filepath.Ext(path))
	if err := os.WriteFile(out, frame, 0o600); err != nil {
		return FrameFile{}, err
	}
	return FrameFile{Path: out, Objects: len(objects), Bytes: int64(len(frame))}, nil
}

// removeAll deletes the originals a split replaced. They are deleted only after
// the held parts are recorded, so a failure earlier leaves the export whole.
func removeAll(paths []string) error {
	for _, path := range paths {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}
