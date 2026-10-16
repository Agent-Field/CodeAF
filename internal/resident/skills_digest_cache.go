package resident

import (
	"crypto/sha256"
	"encoding/binary"
	"io/fs"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// THE IMPORT PASS RUNS ON EVERY LAUNCH, AND A LAUNCH MUST NOT READ EVERY BYTE
// OF EVERY FOREIGN SKILL TO LEARN THAT NOTHING CHANGED. [contentDigest] is the
// digest of record and stays the only thing that makes one; this file only
// decides when its answer for a folder is still the answer, by walking the
// folder's stat data (no file is opened) and comparing a signature of it with
// the one taken when the digest was last made.
//
// THE SIGNATURE covers, for every regular file: relative path, size, mtime,
// ctime, inode, device and mode. ctime is the field that makes an edit with a
// restored mtime (touch -r, cp -p, rsync -t) visible, because the kernel sets
// it on every write and no tool can set it back. A file replaced by rename
// changes inode.
//
// ANYTHING UNCERTAIN READS THE CONTENT. A walk error, a platform without
// change times, and a folder holding a file touched within [digestRacyWindow]
// of the walk (a write in the same timestamp tick would leave the signature
// unchanged — git's "racily clean" case) are never cached, and a miss always
// falls through to [contentDigest]. The cache can therefore cost a read but
// never hide an edit. There is no refresh TTL: nothing here is ever trusted
// for a period, only for as long as the stat data is identical.
//
// THE CACHE IS BOUNDED: [digestCacheMax] folders, least recently used out
// first. It is keyed by resolved folder and holds a signature and a digest,
// nothing about workspaces, so one workspace's launch can never change what
// another one's pass concludes — the alive set is rebuilt from disk every pass.
const (
	digestCacheMax   = 512
	digestRacyWindow = 2 * time.Second
)

type cachedDigest struct {
	signature [sha256.Size]byte
	digest    string
	used      uint64
}

type digestCache struct {
	mu   sync.Mutex
	now  func() time.Time
	max  int
	tick uint64
	m    map[string]*cachedDigest
	// reading holds one channel per folder being read right now, closed when
	// that read ends. It is bounded by the reads in progress and is the whole
	// of the coalescing: other folders never wait on it.
	reading map[string]chan struct{}

	// hits and reads count answers served from the signature and full content
	// digests made, so a test or benchmark can prove what a pass cost.
	hits, reads atomic.Int64
}

func newDigestCache(max int, now func() time.Time) *digestCache {
	return &digestCache{now: now, max: max, m: make(map[string]*cachedDigest), reading: make(map[string]chan struct{})}
}

// skillDigests is the process's one cache; tests build their own.
var skillDigests = newDigestCache(digestCacheMax, time.Now)

// digest returns contentDigest(dir), reading file content only when the
// folder's signature differs from the remembered one or cannot be trusted.
func (c *digestCache) digest(dir string) (string, error) {
	signature, cacheable, err := treeSignature(dir, c.now())
	if err != nil || !cacheable {
		c.reads.Add(1)
		return contentDigest(dir)
	}
	for {
		value, done, hit, owner := c.claimRead(dir, signature)
		if hit {
			c.hits.Add(1)
			return value, nil
		}
		if !owner {
			<-done
			continue
		}
		return c.read(dir, signature, done)
	}
}

// claimRead holds the mutex only while choosing a cached result, an existing
// reader to wait for, or ownership of a new read. No file I/O waits under it.
func (c *digestCache) claimRead(dir string, signature [sha256.Size]byte) (value string, done chan struct{}, hit, owner bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.m[dir]; ok && entry.signature == signature {
		c.tick++
		entry.used = c.tick
		return entry.digest, nil, true, false
	}
	if pending, busy := c.reading[dir]; busy {
		return "", pending, false, false
	}
	done = make(chan struct{})
	c.reading[dir] = done
	return "", done, false, true
}

func (c *digestCache) finishRead(dir string, done chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.reading, dir)
	close(done)
}

// read is the one reader for dir; it publishes its result and releases the
// folder even when contentDigest fails.
func (c *digestCache) read(dir string, signature [sha256.Size]byte, done chan struct{}) (string, error) {
	defer c.finishRead(dir, done)
	// The signature was taken BEFORE the read, so an edit landing mid-read
	// leaves an old signature beside newer content and the next walk misses.
	c.reads.Add(1)
	digest, err := contentDigest(dir)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		delete(c.m, dir)
		return "", err
	}
	if _, ok := c.m[dir]; !ok && len(c.m) >= c.max {
		var oldest string
		var least uint64
		first := true
		for key, entry := range c.m {
			if first || entry.used < least {
				oldest, least, first = key, entry.used, false
			}
		}
		delete(c.m, oldest)
	}
	c.tick++
	c.m[dir] = &cachedDigest{signature: signature, digest: digest, used: c.tick}
	return digest, nil
}

func (c *digestCache) size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.m)
}

// treeSignature hashes the stat data of every regular file under dir, in
// WalkDir's lexical order, without opening any of them. cacheable is false
// when the signature must not be trusted to stand in for the content.
func treeSignature(dir string, now time.Time) (signature [sha256.Size]byte, cacheable bool, err error) {
	h := sha256.New()
	cacheable = true
	racy := now.Add(-digestRacyWindow).UnixNano()
	var buf [48]byte
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		ctime, inode, device, ok := fileStamp(info)
		if !ok {
			cacheable = false
		}
		mtime := info.ModTime().UnixNano()
		if mtime > racy || ctime > racy {
			cacheable = false
		}
		relative := path[len(dir):]
		binary.LittleEndian.PutUint64(buf[:8], uint64(len(relative)))
		_, _ = h.Write(buf[:8])
		_, _ = h.Write([]byte(relative))
		binary.LittleEndian.PutUint64(buf[0:], uint64(info.Size()))
		binary.LittleEndian.PutUint64(buf[8:], uint64(mtime))
		binary.LittleEndian.PutUint64(buf[16:], uint64(ctime))
		binary.LittleEndian.PutUint64(buf[24:], inode)
		binary.LittleEndian.PutUint64(buf[32:], device)
		binary.LittleEndian.PutUint64(buf[40:], uint64(info.Mode()))
		_, _ = h.Write(buf[:])
		return nil
	})
	h.Sum(signature[:0])
	return signature, cacheable, err
}
