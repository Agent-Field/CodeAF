package store

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// A READ OF A DOCUMENT THAT HAS NOT CHANGED DECODES NOTHING (PERF.md, the
// idle-cost law). Every factory request settles the floor, every worker pull
// walks it, and every job chat's watch looks at it; on a floor of several
// hundred jobs, opening and decoding each document on each of those looks kept
// an idle engine on a core or more. So a decoded document is kept, per floor,
// beside the identity of the file it came from, and a read whose `stat` still
// names that same file answers a copy of the kept document instead of opening
// it again.
//
// THE IDENTITY IS THE FILE, NOT ITS CLOCK. Every store write lands as a fresh
// file renamed into place (writeAtomic), so a write always changes the file's
// identity (device and inode) whatever the folder's clock resolution; size and
// modification time also catch a hand edit in place. The identity kept is the
// one `fstat` reported on the very descriptor the bytes were read from, so a
// write landing between a `stat` and a read can only make the next look read
// again, never keep new bytes under an old name or old bytes under a new one.
//
// WHAT IS HANDED OUT IS ALWAYS A COPY (cloneDoc). Callers edit the items they
// read, in place and through their slices and maps, and none of them may
// reach the kept document.

// docCaches holds one cache per floor folder, shared by every Store over it,
// since each chat opens a store of its own.
var docCaches sync.Map // root -> *docCache

type docCache struct {
	mu   sync.Mutex
	docs map[string]keptDoc // file name -> document
}

type keptDoc struct {
	file os.FileInfo
	d    doc
}

func (st *Store) cache() *docCache {
	if x, ok := docCaches.Load(st.root); ok {
		return x.(*docCache)
	}
	x, _ := docCaches.LoadOrStore(st.root, &docCache{docs: map[string]keptDoc{}})
	return x.(*docCache)
}

func (c *docCache) get(name string, info os.FileInfo) (doc, bool) {
	c.mu.Lock()
	kept, ok := c.docs[name]
	c.mu.Unlock()
	if !ok || !sameFile(kept.file, info) {
		return doc{}, false
	}
	return cloneDoc(kept.d), true
}

func (c *docCache) put(name string, info os.FileInfo, d doc) {
	c.mu.Lock()
	c.docs[name] = keptDoc{file: info, d: d}
	c.mu.Unlock()
}

func (c *docCache) drop(name string) {
	c.mu.Lock()
	delete(c.docs, name)
	c.mu.Unlock()
}

// keepOnly forgets every document whose file is no longer in the folder.
func (c *docCache) keepOnly(names map[string]bool) {
	c.mu.Lock()
	for name := range c.docs {
		if !names[name] {
			delete(c.docs, name)
		}
	}
	c.mu.Unlock()
}

// sameFile is whether b is still the file a was taken from.
func sameFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func (st *Store) read(path string) (doc, error) {
	name := filepath.Base(path)
	cache := st.cache()
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			cache.drop(name)
			return doc{}, ErrNotFound
		}
		return doc{}, err
	}
	if d, ok := cache.get(name, info); ok {
		return d, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			cache.drop(name)
			return doc{}, ErrNotFound
		}
		return doc{}, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return doc{}, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return doc{}, err
	}
	st.reads.Add(1)
	d, err := decodeDoc(name, data)
	if err != nil {
		cache.drop(name)
		return doc{}, err
	}
	cache.put(name, opened, d)
	return cloneDoc(d), nil
}

// decodeDoc is a document's bytes as the store reads them.
func decodeDoc(name string, data []byte) (doc, error) {
	var d doc
	if err := json.Unmarshal(data, &d); err != nil {
		return doc{}, fmt.Errorf("factory store: cannot read %s: %w", name, err)
	}
	return finishDoc(name, d)
}

// cloneDoc is a copy of d that shares no memory a caller could change.
func cloneDoc(d doc) doc {
	var out doc
	deepCopy(reflect.ValueOf(&out).Elem(), reflect.ValueOf(d))
	return out
}

// plainTypes caches whether a type holds anything a copy must not share.
var plainTypes sync.Map // reflect.Type -> bool

// plain is whether a value of t can be copied by assignment: it reaches no
// pointer, slice, map or interface. Strings are immutable and so are plain.
// A struct with unexported fields is copied by assignment too, and the law
// test in doccache_test.go holds that the only such struct a document reaches
// is time.Time, whose one pointer is an immutable location.
func plain(t reflect.Type) bool {
	if v, ok := plainTypes.Load(t); ok {
		return v.(bool)
	}
	p := true
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		p = false
	case reflect.Array:
		p = plain(t.Elem())
	case reflect.Struct:
		if hasUnexported(t) {
			break
		}
		for i := range t.NumField() {
			if !plain(t.Field(i).Type) {
				p = false
				break
			}
		}
	}
	plainTypes.Store(t, p)
	return p
}

func hasUnexported(t reflect.Type) bool {
	for i := range t.NumField() {
		if !t.Field(i).IsExported() {
			return true
		}
	}
	return false
}

// deepCopy sets dst, a settable value of src's type, to a copy of src.
func deepCopy(dst, src reflect.Value) {
	t := src.Type()
	if plain(t) {
		dst.Set(src)
		return
	}
	switch src.Kind() {
	case reflect.Pointer:
		if src.IsNil() {
			return
		}
		n := reflect.New(t.Elem())
		deepCopy(n.Elem(), src.Elem())
		dst.Set(n)
	case reflect.Slice:
		if src.IsNil() {
			return
		}
		n := reflect.MakeSlice(t, src.Len(), src.Len())
		if plain(t.Elem()) {
			reflect.Copy(n, src)
		} else {
			for i := range src.Len() {
				deepCopy(n.Index(i), src.Index(i))
			}
		}
		dst.Set(n)
	case reflect.Map:
		if src.IsNil() {
			return
		}
		n := reflect.MakeMapWithSize(t, src.Len())
		kt, vt := t.Key(), t.Elem()
		for it := src.MapRange(); it.Next(); {
			k, v := reflect.New(kt).Elem(), reflect.New(vt).Elem()
			deepCopy(k, it.Key())
			deepCopy(v, it.Value())
			n.SetMapIndex(k, v)
		}
		dst.Set(n)
	case reflect.Interface:
		if src.IsNil() {
			return
		}
		inner := src.Elem()
		n := reflect.New(inner.Type()).Elem()
		deepCopy(n, inner)
		dst.Set(n)
	case reflect.Array:
		for i := range src.Len() {
			deepCopy(dst.Index(i), src.Index(i))
		}
	case reflect.Struct:
		if hasUnexported(t) {
			dst.Set(src)
			return
		}
		for i := range src.NumField() {
			deepCopy(dst.Field(i), src.Field(i))
		}
	default:
		dst.Set(src)
	}
}

// itemWrites counts, per floor folder, the item documents this process has
// written or removed there. A watcher compares it between looks to learn about
// its own process's writes at once, without asking the disk.
var itemWrites sync.Map // cleaned root -> *atomic.Uint64

func writtenOf(root string) *atomic.Uint64 {
	root = filepath.Clean(root)
	if x, ok := itemWrites.Load(root); ok {
		return x.(*atomic.Uint64)
	}
	x, _ := itemWrites.LoadOrStore(root, new(atomic.Uint64))
	return x.(*atomic.Uint64)
}

// noteWritten counts a write or removal of path when path is an item document.
func noteWritten(path string) {
	name, ok := strings.CutSuffix(filepath.Base(path), ".json")
	if !ok {
		return
	}
	if _, err := strconv.Atoi(name); err != nil {
		return
	}
	writtenOf(filepath.Dir(path)).Add(1)
}

// Written is how many item documents any store in this process has written or
// removed under this floor. It moves on every such write and on nothing else;
// a write made by another process does not move it.
func (st *Store) Written() uint64 {
	if st == nil {
		return 0
	}
	return writtenOf(st.root).Load()
}

// Manager is which conversation one job names as its manager.
type Manager struct {
	ID      int
	Talk    string
	Created time.Time
}

// Managers is every job's [Manager], in the floor's own order (newest first,
// as [Store.List] answers). It costs a listing of the folder and a `stat` per
// job; only a document that changed since the last look is read, and nothing
// is copied, so a watcher may ask it often.
func (st *Store) Managers() ([]Manager, error) {
	entries, err := os.ReadDir(st.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	cache := st.cache()
	out := make([]Manager, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		if _, err := strconv.Atoi(strings.TrimSuffix(name, ".json")); err != nil {
			continue
		}
		path := filepath.Join(st.root, name)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		cache.mu.Lock()
		kept, ok := cache.docs[name]
		cache.mu.Unlock()
		if !ok || !sameFile(kept.file, info) {
			d, err := st.read(path)
			if err != nil {
				continue
			}
			kept.d = d
		}
		out = append(out, Manager{ID: kept.d.Item.ID, Talk: kept.d.Item.Talk, Created: kept.d.Item.Created})
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Created.Equal(out[b].Created) {
			return out[a].ID > out[b].ID
		}
		return out[a].Created.After(out[b].Created)
	})
	return out, nil
}
