package blobstore

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// CensusFiles counts observed regular files and their logical on-disk lengths.
type CensusFiles struct{ Count, LogicalBytes, AllocatedBytes int64 }

// DiskCensus is an observational inventory, never a reachability or deletion
// proof. Frame hints, missing index pointers and duplicate objects establish no
// liveness. All observed frames remain UnclassifiedFrames.
//
// AllocatedBytes sums filesystem-reported blocks once per observed inode where
// supported. It includes visited directories, not unvisited filesystem metadata;
// it does not establish exclusive physical allocation of shared/CoW extents.
// AllocationKnown=false means no portable allocated-size measurement is claimed.
// BytesRead records actual payload/index inspection cost. Locations contains only
// hash-verified, structurally valid frames; it is not a persistent replacement
// index. The inventory uses one bounded frame buffer plus metadata proportional
// to the observed objects, locations and inodes, not their complete payloads.
type DiskCensus struct {
	Frames, Pointers, Other                      CensusFiles
	Directories, Inodes                          int64
	AllocatedBytes                               int64
	DirectoryAllocatedBytes                      int64
	AllocationKnown                              bool
	BytesRead                                    int64
	UniqueObjects, ObjectOccurrences             int64
	UniquePayloadBytes, RepeatedPayloadBytes     int64
	ConflictingOccurrences                       int64
	VerifiedPointers, FramesWithoutIndexPointers int64
	ObjectsWithoutIndexPointers                  int64
	UnclassifiedFrames                           int64
	Locations                                    Locations `json:"-"`
	Problems                                     map[string]int64
	Complete                                     bool
	// StableSnapshot is always false: other handles/processes and concurrent puts
	// are unfenced. A disappearing/changing file is an unresolved observation.
	StableSnapshot bool
}

type censusInode struct{ device, inode uint64 }
type censusAllocation struct {
	identity censusInode
	bytes    int64
	known    bool
}
type censusObject struct {
	digest [32]byte
	size   int64
}
type diskCensusWalk struct {
	result   DiskCensus
	ctx      context.Context
	root     string
	inodes   map[censusInode]bool
	objects  map[string]censusObject
	frames   map[string]bool // true only when a matching pointer was observed
	pointers map[string]pointer
}

// Census reads the store without changing any frame, pointer or index. It does
// not hold Disk's writer mutex or fence any publisher. Cancellation returns a
// partial result; corruption/unknown layout returns ErrDamaged with counts of
// unresolved observations, which can also arise during concurrent publication.
// A successful inventory still cannot authorize reclamation: that needs an
// authenticated complete object closure and a separately fenced epoch.
func (d *Disk) Census(ctx context.Context) (DiskCensus, error) {
	walk := diskCensusWalk{ctx: ctx, root: filepath.Dir(d.frames), inodes: map[censusInode]bool{}, objects: map[string]censusObject{}, frames: map[string]bool{}, pointers: map[string]pointer{}}
	walk.result = DiskCensus{AllocationKnown: true, Locations: Locations{}, Problems: map[string]int64{}}
	if err := ctx.Err(); err != nil {
		return walk.result, err
	}
	if err := filepath.WalkDir(walk.root, walk.visit); err != nil {
		return walk.result, err
	}
	if err := ctx.Err(); err != nil {
		return walk.result, err
	}
	if err := walk.verifyPointers(); err != nil {
		return walk.result, err
	}
	for _, pointed := range walk.frames {
		if !pointed {
			walk.result.FramesWithoutIndexPointers++
		}
	}
	walk.result.UnclassifiedFrames = walk.result.Frames.Count
	var problems int64
	for _, count := range walk.result.Problems {
		problems += count
	}
	if problems != 0 {
		return walk.result, fmt.Errorf("%w: disk census has %d unresolved observations", ErrDamaged, problems)
	}
	walk.result.Complete = true
	return walk.result, nil
}

func (w *diskCensusWalk) problem(reason string) { w.result.Problems[reason]++ }

func (w *diskCensusWalk) visit(path string, entry fs.DirEntry, walkErr error) error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	if walkErr != nil {
		w.problem("walk-error")
		w.result.AllocationKnown = false
		return nil
	}
	info, err := entry.Info()
	if err != nil {
		w.problem("stat-error")
		w.result.AllocationKnown = false
		return nil
	}
	allocated := w.allocate(info)
	rel, err := filepath.Rel(w.root, path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		w.result.Directories++
		w.result.DirectoryAllocatedBytes += allocated
		if !censusDirectory(rel) {
			w.problem("unknown-directory")
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		w.result.Other.Count++
		w.result.Other.AllocatedBytes += allocated
		w.result.Other.LogicalBytes += info.Size()
		w.problem("non-regular-entry")
		return nil
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) == 2 && parts[0] == "frames" && ValidRID(parts[1]) {
		w.result.Frames.AllocatedBytes += allocated
		w.frame(path, parts[1], info)
		return nil
	}
	if len(parts) == 3 && parts[0] == "objects" && len(parts[1]) == 2 && ValidRID(parts[1]+parts[2]) {
		w.result.Pointers.AllocatedBytes += allocated
		w.index(path, parts[1]+parts[2], info)
		return nil
	}
	w.result.Other.Count++
	w.result.Other.AllocatedBytes += allocated
	w.result.Other.LogicalBytes += info.Size()
	w.problem("unknown-file")
	return nil
}

func censusDirectory(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	return rel == "." || rel == "frames" || rel == "objects" || (len(parts) == 2 && parts[0] == "objects" && isLowerHex(parts[1], 2))
}

func (w *diskCensusWalk) allocate(info fs.FileInfo) int64 {
	allocation := censusAllocated(info)
	if !allocation.known {
		w.result.AllocationKnown = false
		return 0
	}
	if !w.inodes[allocation.identity] {
		w.inodes[allocation.identity] = true
		w.result.Inodes++
		w.result.AllocatedBytes += allocation.bytes
		return allocation.bytes
	}
	return 0
}

func (w *diskCensusWalk) frame(path, id string, info fs.FileInfo) {
	w.result.Frames.Count++
	w.result.Frames.LogicalBytes += info.Size()
	w.result.UnclassifiedFrames++
	w.frames[id] = false
	raw, err := w.read(path, info, MaxFrame)
	if err != nil {
		w.problem("frame-read")
		return
	}
	if IDOf(raw) != id {
		w.problem("frame-hash")
		return
	}
	header, objects, err := Decode(raw)
	if err != nil {
		w.problem("frame-format")
		return
	}
	start := payloadStart(raw, header)
	for n, object := range objects {
		ref := header.Objects[n]
		w.result.Locations[object.RID] = append(w.result.Locations[object.RID], Location{Frame: id, Off: start + ref.Off, Len: ref.Len})
		w.result.ObjectOccurrences++
		digest := sha256.Sum256(object.Bytes)
		if prior, exists := w.objects[object.RID]; exists {
			if prior.digest != digest || prior.size != int64(len(object.Bytes)) {
				w.result.ConflictingOccurrences++
				w.problem("object-conflict")
			} else {
				w.result.RepeatedPayloadBytes += int64(len(object.Bytes))
			}
		} else {
			w.objects[object.RID] = censusObject{digest: digest, size: int64(len(object.Bytes))}
			w.result.UniquePayloadBytes += int64(len(object.Bytes))
			w.result.UniqueObjects++
		}
	}
}

func (w *diskCensusWalk) index(path, rid string, info fs.FileInfo) {
	w.result.Pointers.Count++
	w.result.Pointers.LogicalBytes += info.Size()
	raw, err := w.read(path, info, 256)
	if err != nil {
		w.problem("pointer-read")
		return
	}
	parsed, err := parsePointer(raw)
	if err != nil {
		w.problem("pointer-format")
		return
	}
	w.pointers[rid] = parsed
}

func (w *diskCensusWalk) read(path string, observed fs.FileInfo, limit int64) ([]byte, error) {
	if err := w.ctx.Err(); err != nil {
		return nil, err
	}
	if observed.Size() > limit {
		return nil, errors.New("census file exceeds format limit")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	current, err := file.Stat()
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(observed, current) {
		return nil, errors.New("census file changed before inspection")
	}
	raw, err := io.ReadAll(io.LimitReader(censusReader{ctx: w.ctx, input: file}, limit+1))
	w.result.BytesRead += int64(len(raw))
	if int64(len(raw)) > limit || int64(len(raw)) != observed.Size() {
		return nil, errors.New("census file changed or exceeds format limit")
	}
	return raw, err
}

type censusReader struct {
	ctx   context.Context
	input io.Reader
}

func (r censusReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.input.Read(buffer)
}

func (w *diskCensusWalk) verifyPointers() error {
	for rid, index := range w.pointers {
		if err := w.ctx.Err(); err != nil {
			return err
		}
		matched := false
		for _, location := range w.result.Locations[rid] {
			if location.Frame == index.Frame && location.Off == index.Off && location.Len == index.Len {
				matched = true
				break
			}
		}
		if !matched {
			w.problem("pointer-target")
			continue
		}
		w.frames[index.Frame] = true
		w.result.VerifiedPointers++
	}
	w.result.ObjectsWithoutIndexPointers = w.result.UniqueObjects - w.result.VerifiedPointers
	if w.result.ObjectsWithoutIndexPointers != 0 {
		w.result.Problems["object-without-pointer"] = w.result.ObjectsWithoutIndexPointers
	}

	return nil
}
