package store

import (
	"os"
	"reflect"
	"sync"
	"time"
)

// listRereadEvery bounds how long a document may be trusted by file identity. An
// in-place rewrite keeping its size and time looks unchanged, and a Stop or a
// budget must not rest on file times with no limit.
const listRereadEvery = time.Minute

// listIndex owns the decoded documents. Nothing returned by List aliases them.
// mu guards only these fields and is never held across a read or a write.
type listIndex struct {
	mu   sync.Mutex
	docs map[string]listedDoc
	// fullAt is when the running epoch began. Each epoch starts empty, so its
	// first look reads every document.
	fullAt time.Time
	// epoch stops a scan that began before a full read from publishing over it.
	epoch int
}

// listSnapshot is what one scan reuses. A published map is never changed, so the
// scan may read it unlocked: migration during a scan takes item locks.
func (st *Store) listSnapshot() (map[string]listedDoc, int) {
	st.list.mu.Lock()
	defer st.list.mu.Unlock()
	if now := st.now(); now.Sub(st.list.fullAt) >= listRereadEvery {
		st.list.docs, st.list.fullAt = nil, now
		st.list.epoch++
	}
	return st.list.docs, st.list.epoch
}

// listPublish keeps a scan's documents unless a newer epoch began meanwhile. A
// scan publishing over a newer one of its epoch costs only reads, because each
// file's identity is checked on every look.
func (st *Store) listPublish(docs map[string]listedDoc, epoch int) {
	st.list.mu.Lock()
	defer st.list.mu.Unlock()
	if epoch == st.list.epoch {
		st.list.docs = docs
	}
}

type listedDoc struct {
	file os.FileInfo
	doc  doc
}

// listed reads only changed documents. Stat precedes read: a rename during
// read may cause one extra read next time, never cache old bytes as a new file.
// EVERY LOOK STATS EVERY FILE. A folder timestamp is not sufficient evidence
// for a grant candidate: another process's rename must be seen on the next look,
// including a replacement whose size and modification time stayed identical.
func (st *Store) listed(path string, previous map[string]listedDoc, next map[string]listedDoc) (doc, error) {
	info, err := os.Stat(path)
	if err != nil {
		return doc{}, err
	}
	cached, ok := previous[path]
	if !ok || !sameDocument(cached.file, info) {
		d, err := st.read(path)
		if err != nil {
			return doc{}, err
		}
		cached = listedDoc{file: info, doc: d}
	}
	next[path] = cached
	// Copy before migration or retention as well as before the caller sees it.
	// A retained view must not discard bytes needed by a later retention policy.
	return copyListed(reflect.ValueOf(cached.doc)).Interface().(doc), nil
}

// copyListed copies the mutable JSON graph without encoding or decoding it
// again. Struct values retain their private fields (notably time.Time); all
// exported pointers, slices and maps are copied recursively, including fields
// added to Item later. Documents come from JSON and therefore contain no cycles.
func copyListed(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(copyListed(v.Elem()))
		return out
	case reflect.Interface:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(copyListed(v.Elem()))
		return out
	case reflect.Slice:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(copyListed(v.Index(i)))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), copyListed(iter.Value()))
		}
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		out.Set(v)
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				out.Field(i).Set(copyListed(v.Field(i)))
			}
		}
		return out
	default:
		return v
	}
}
