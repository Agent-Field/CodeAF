package store

import (
	"os"
	"reflect"
	"sync"
	"sync/atomic"
)

// ── a document is decoded once per version of its file ─────────────────────
//
// EVERY READER OF THE FLOOR USED TO DECODE EVERY DOCUMENT ON IT, EVERY TIME.
// The runner's capacity read, the server's settle before each request and the
// lease desk each list the floor, and an idle engine with 82 jobs on it read
// 1.7 GB of JSON in six minutes to learn that nothing had changed (the fifth
// live A+B run's E3). A document's file is replaced, never written in place
// ([writeAtomic] renames a fresh file over it), so one stat says whether the
// document read last time is still the one on disk: the same file, the same
// size, the same modification time. Only a document whose file changed is
// read and decoded again, so a listing costs what changed since the last one,
// not what the backlog holds. PERF.md's "Factory floor reads" states the cost.
//
// THE CACHE IS THE PROCESS'S, NOT A STORE'S. Many doors open their own
// [Store] on one root, and a document is the same document whichever of them
// reads it. Every answer is a deep copy, so no caller can change what the
// next caller is given.

// decoded is one document as it was read, and the file it was read from.
type decoded struct {
	file os.FileInfo
	doc  doc
}

// readCacheMost bounds the documents remembered. A process that has seen more
// distinct documents than this (a test binary making a store per test) starts
// again rather than growing without end.
const readCacheMost = 8192

var readCache = struct {
	mu   sync.Mutex
	docs map[string]decoded
}{docs: map[string]decoded{}}

// documentDecodes counts the documents read from disk and decoded, so a test
// can say a listing of an unchanged floor decoded none.
var documentDecodes atomic.Int64

// cachedRead answers the document at path as [Store.read] would, reading and
// decoding it only when its file is not the one last read. The file is stat'd
// before it is read, so a write landing between the two leaves the newer
// document under the older file's identity, and the next read decodes it
// again rather than keeping a stale answer.
func cachedRead(path string, read func(string) (doc, error)) (doc, error) {
	info, statErr := os.Stat(path)
	if statErr == nil {
		readCache.mu.Lock()
		last, ok := readCache.docs[path]
		readCache.mu.Unlock()
		if ok && sameFile(last.file, info) {
			return cloneDoc(last.doc), nil
		}
	}
	d, err := read(path)
	documentDecodes.Add(1)
	readCache.mu.Lock()
	defer readCache.mu.Unlock()
	if err != nil || statErr != nil {
		delete(readCache.docs, path)
		return d, err
	}
	if len(readCache.docs) >= readCacheMost {
		readCache.docs = map[string]decoded{}
	}
	readCache.docs[path] = decoded{file: info, doc: d}
	return cloneDoc(d), nil
}

// sameFile says two looks saw one unchanged file.
func sameFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()) && a.Mode() == b.Mode()
}

func cloneDoc(d doc) doc {
	var out doc
	deepCopy(reflect.ValueOf(&out).Elem(), reflect.ValueOf(d))
	return out
}

// deepCopy copies src into dst, which has src's type, sharing nothing a caller
// could change: every pointer, slice, map and interface is copied in turn. A
// struct is first copied whole, so its unexported fields (a time.Time's
// location among them) are kept as values, and then each exported field that
// can hold a reference is copied again.
func deepCopy(dst, src reflect.Value) {
	switch src.Kind() {
	case reflect.Pointer:
		if src.IsNil() {
			return
		}
		p := reflect.New(src.Elem().Type())
		deepCopy(p.Elem(), src.Elem())
		dst.Set(p)
	case reflect.Interface:
		if src.IsNil() {
			return
		}
		v := reflect.New(src.Elem().Type()).Elem()
		deepCopy(v, src.Elem())
		dst.Set(v)
	case reflect.Slice:
		if src.IsNil() {
			return
		}
		s := reflect.MakeSlice(src.Type(), src.Len(), src.Len())
		if refless(src.Type().Elem()) {
			reflect.Copy(s, src)
		} else {
			for i := 0; i < src.Len(); i++ {
				deepCopy(s.Index(i), src.Index(i))
			}
		}
		dst.Set(s)
	case reflect.Map:
		if src.IsNil() {
			return
		}
		m := reflect.MakeMapWithSize(src.Type(), src.Len())
		iter := src.MapRange()
		for iter.Next() {
			v := reflect.New(src.Type().Elem()).Elem()
			deepCopy(v, iter.Value())
			m.SetMapIndex(iter.Key(), v)
		}
		dst.Set(m)
	case reflect.Array:
		for i := 0; i < src.Len(); i++ {
			deepCopy(dst.Index(i), src.Index(i))
		}
	case reflect.Struct:
		dst.Set(src)
		t := src.Type()
		for i := 0; i < src.NumField(); i++ {
			if t.Field(i).IsExported() && !refless(t.Field(i).Type) {
				deepCopy(dst.Field(i), src.Field(i))
			}
		}
	default:
		dst.Set(src)
	}
}

// reflessTypes remembers [refless] by type: an item has a few hundred fields
// and the answer never changes.
var reflessTypes sync.Map

// refless says a value of t holds nothing shared, so copying it as a value is
// a deep copy. An unexported field is counted as holding nothing, because a
// copy cannot reach it and the store's own types keep nothing there.
func refless(t reflect.Type) bool {
	if v, ok := reflessTypes.Load(t); ok {
		return v.(bool)
	}
	reflessTypes.Store(t, false) // a type that holds itself holds a reference
	out := true
	switch t.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		out = false
	case reflect.Array:
		out = refless(t.Elem())
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if t.Field(i).IsExported() && !refless(t.Field(i).Type) {
				out = false
				break
			}
		}
	}
	reflessTypes.Store(t, out)
	return out
}
