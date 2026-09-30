package blobstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Disk keeps a store in a directory with exactly two prefixes and nothing else:
//
//	frames/<FrameID>              a frame, written once
//	objects/<rid[0:2]>/<rid[2:]>  a pointer line "<FrameID> <off> <len>\n"
//
// The frame is the truth and the pointer is only an index into it, so a
// pointer is written after its frame is durable. A crash between the two
// leaves a frame nobody points at, which is harmless, and never a pointer to
// bytes that are not there.
type Disk struct {
	frames, objects string

	mu sync.Mutex // one writer at a time, so conflict checks cannot race a write

	// The two seams below exist so a test can break the write at the exact
	// place the crash order matters. Production leaves them as they are.
	sync       func([]*os.File) error // makes a batch of written files durable
	afterFrame func() error           // runs once the frame is durable, before any pointer
}

// NewDisk opens a store under root, creating the two prefixes when needed.
func NewDisk(root string) (*Disk, error) {
	d := &Disk{
		frames:     filepath.Join(root, "frames"),
		objects:    filepath.Join(root, "objects"),
		sync:       flushFiles,
		afterFrame: func() error { return nil },
	}
	for _, dir := range []string{d.frames, d.objects} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("blobstore: open disk store: %w", err)
		}
	}
	return d, nil
}

// PutFrame implements Store: check, make the frame durable, then point at it.
func (d *Disk) PutFrame(ctx context.Context, frame []byte) (FrameID, error) {
	h, objects, err := Decode(frame)
	if err != nil {
		return "", err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.conflict(ctx, objects); err != nil {
		return "", err
	}
	id := IDOf(frame)
	if err := d.writeOnce(filepath.Join(d.frames, id), frame); err != nil {
		return "", err
	}
	if err := d.afterFrame(); err != nil {
		return "", err
	}
	return id, d.point(id, payloadStart(frame, h), h.Objects)
}

// conflict reports ErrConflict when any object is already held with other bytes.
func (d *Disk) conflict(ctx context.Context, objects []Object) error {
	for _, o := range objects {
		have, err := d.Get(ctx, o.RID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if !bytes.Equal(have, o.Bytes) {
			return ErrConflict
		}
	}
	return nil
}

// point writes one pointer per object that has none yet, as one group commit.
// An object already stored keeps its pointer: the bytes are identical, and a
// pointer is never rewritten.
func (d *Disk) point(id FrameID, start uint64, refs []ObjectRef) error {
	entries := make([]entry, len(refs))
	for i, ref := range refs {
		p := pointer{Frame: id, Off: start + ref.Off, Len: ref.Len}
		entries[i] = entry{d.pointerPath(ref.RID), p.line()}
	}
	return d.writeAllOnce(entries)
}

// Get implements Store: follow the pointer into the frame and read the object.
func (d *Disk) Get(_ context.Context, rid string) ([]byte, error) {
	if err := checkGet(rid); err != nil {
		return nil, err
	}
	line, err := os.ReadFile(d.pointerPath(rid))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("blobstore: read pointer: %w", err)
	}
	p, err := parsePointer(line)
	if err != nil {
		return nil, err
	}
	return d.readObject(p)
}

// GetMany implements Store: the prefix of rids that fits in one frame, read one
// pointer at a time.
func (d *Disk) GetMany(ctx context.Context, rids []string) ([]Object, error) {
	if err := checkMany(rids); err != nil {
		return nil, err
	}
	return gatherPrefix(rids, func(rid string) ([]byte, error) { return d.Get(ctx, rid) })
}

// readObject reads the bytes a pointer names. A pointer whose frame is gone or
// short is damage, and is reported as damage rather than as an absent object.
func (d *Disk) readObject(p pointer) ([]byte, error) {
	f, err := os.Open(filepath.Join(d.frames, p.Frame))
	if err != nil {
		return nil, fmt.Errorf("%w: pointer names a missing frame: %v", ErrDamaged, err)
	}
	defer f.Close()
	buf := make([]byte, p.Len)
	if _, err := f.ReadAt(buf, int64(p.Off)); err != nil {
		return nil, fmt.Errorf("%w: pointer runs past its frame: %v", ErrDamaged, err)
	}
	return buf, nil
}

// Has implements Store; a pointer that exists is enough, as it is only ever
// written after the frame it names. It waits for a put in flight, so a client
// that timed out on a slow put and asks whether it landed gets the answer
// after the write ends and not a false "no" from the middle of it.
func (d *Disk) Has(_ context.Context, rids []string) ([]bool, error) {
	if err := checkHas(rids); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	have := make([]bool, len(rids))
	for i, rid := range rids {
		_, err := os.Stat(d.pointerPath(rid))
		have[i] = err == nil
	}
	return have, nil
}

func (d *Disk) pointerPath(rid string) string {
	return filepath.Join(d.objects, rid[:2], rid[2:])
}
