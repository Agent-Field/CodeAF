package pull

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// cache is the on-disk record of the last good fetch: the document, its
// signature, its ETag and when it was fetched. It is written so that a run cut
// in half never leaves a half-written cache: each file goes to a temporary name
// in CacheDir and is then renamed into place, and the files it leaves behind
// have mode 0600.
//
// A cache that does not read is no cache. Bytes that do not parse, or whose
// signature no longer verifies under [Puller.Keys], are treated as if nothing were
// cached: no error of their own, no panic, and the next good fetch replaces
// them.
type cache struct {
	doc       []byte
	sig       []byte
	etag      string
	version   int64
	fetchedAt time.Time
	ok        bool
}

// loadCache reads the cache directory and returns what it finds, or a cache
// with ok false. A cache that fails to read is not an error: it is treated as
// no cache at all.
func (p *Puller) loadCache() (cache, bool) {
	var c cache
	doc, err := readLimitedFile(filepath.Join(p.CacheDir, "doc.json"))
	if err != nil {
		return c, false
	}
	sig, err := readLimitedFile(filepath.Join(p.CacheDir, "doc.sig"))
	if err != nil {
		return c, false
	}
	meta, err := os.ReadFile(filepath.Join(p.CacheDir, "meta.json"))
	if err != nil {
		return c, false
	}
	var m struct {
		ETag      string    `json:"etag"`
		Version   int64     `json:"version"`
		FetchedAt time.Time `json:"fetched_at"`
	}
	if err := json.Unmarshal(meta, &m); err != nil {
		return c, false
	}
	// THE CACHE IS NOT A TRUSTED STORE. A signature that no longer verifies
	// under [Puller.Keys] means the cache is stale, not that the caller is owed an
	// error.
	if !Verify(doc, sig, p.Keys) {
		return c, false
	}
	// AND THE VERSION COMES OFF THE DOCUMENT, not off the file beside it. The
	// three files are committed by three renames, so a run cut between them
	// leaves a doc and a signature that verify as a matching pair while
	// meta.json still describes the copy before. Trusting that number would
	// hand a caller one document's bytes under another's version, and — worse
	// — would check the next fetch's version against the older one, which is
	// the rollback the version floor exists to catch. A cache whose metadata
	// does not describe the bytes it sits beside is a cache that does not read.
	version, err := docVersion(doc)
	if err != nil || version != m.Version {
		return c, false
	}
	c.doc = doc
	c.sig = sig
	c.etag = m.ETag
	c.version = version
	c.fetchedAt = m.FetchedAt
	c.ok = true
	return c, true
}

// storeCache writes the document, its signature, its ETag and when it was
// fetched to CacheDir. Each file goes to a temporary name and is then renamed
// into place, so a run cut in half never leaves a half-written cache. The
// files it leaves behind have mode 0600. CacheDir is made if it is missing.
func (p *Puller) storeCache(doc, sig []byte, etag string, version int64, fetchedAt time.Time) {
	if err := os.MkdirAll(p.CacheDir, 0o700); err != nil {
		// A cache that cannot be written is not fatal: the caller still gets
		// its document, and the next run simply fetches again.
		return
	}
	meta, err := json.Marshal(struct {
		ETag      string    `json:"etag"`
		Version   int64     `json:"version"`
		FetchedAt time.Time `json:"fetched_at"`
	}{etag, version, fetchedAt})
	if err != nil {
		return
	}
	files := []struct {
		name string
		data []byte
	}{
		{"doc.json", doc},
		{"doc.sig", sig},
		{"meta.json", meta},
	}
	for _, f := range files {
		if err := atomicWrite(p.CacheDir, f.name, f.data); err != nil {
			return
		}
	}
}

// atomicWrite writes data to a temporary file in dir and renames it into
// place. The file is created with mode 0600, so the files the cache leaves
// behind are owner-readable only.
func atomicWrite(dir, name string, data []byte) error {
	tmp, err := os.CreateTemp(dir, "."+name+".*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // a rename that succeeded leaves no temp behind
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, filepath.Join(dir, name))
}
