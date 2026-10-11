// Package vectors is the organiser's embedding cache and the stage that fills
// it. Store implements placeorg.Vectors (an append-only file per space) and
// Vectorizer implements placeorg.Vectorizer (cache first, then the primary
// Embedder, then the local one when the primary fails).
//
// A vector is fetched once and never refetched: the reference run showed that
// two fetches of the same text differ enough to move leaves, so the cache is
// keyed by the hash of the embedded text and only a changed text is embedded
// again.
package vectors

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// magic opens every file so a foreign or older-format file is set aside
// rather than misread as vectors.
var magic = [4]byte{'P', 'V', 'E', '1'}

// headerLen is the magic plus the dimension count (uint32).
const headerLen = 8

// HashText is the cache key for a chat's embedded text. Any change to the text,
// a retitle included, changes the key and so re-embeds exactly that chat.
func HashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:16])
}

// FileName is where a space's cache lives beside the organiser's other files.
// The space id holds slashes and an at-sign, so it is flattened.
func FileName(space placeorg.SpaceID) string {
	r := strings.NewReplacer("/", "-", "@", "-", "\\", "-", ":", "-")
	return "places-vectors-" + r.Replace(string(space)) + ".bin"
}

type entry struct {
	hash string
	vec  []float32
}

// Store implements placeorg.Vectors for one space. It is loaded whole and
// safe for concurrent use.
type Store struct {
	space placeorg.SpaceID
	dims  int
	path  string

	mu      sync.Mutex
	live    map[string]entry
	records int // records on disk, live or stale
	f       *os.File
}

var _ placeorg.Vectors = (*Store)(nil)

// Open loads the cache for a space from dir, creating it when absent. A file
// that is damaged is set aside as .damaged and a fresh one started, because a
// cache can always be refilled; a torn final record (a crash mid-append) is
// cut off and everything before it is kept.
func Open(dir string, space placeorg.SpaceID, dims int) (*Store, error) {
	if dims <= 0 {
		return nil, errors.New("vectors: dims must be positive")
	}
	s := &Store{space: space, dims: dims, path: filepath.Join(dir, FileName(space)), live: map[string]entry{}}
	good, err := s.load()
	if err != nil {
		_ = os.Rename(s.path, s.path+".damaged")
		s.live, s.records, good = map[string]entry{}, 0, 0
	}
	if err := s.openAppend(good); err != nil {
		return nil, err
	}
	return s, nil
}

// load reads the whole file and returns the byte length of the intact prefix.
func (s *Store) load() (int64, error) {
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	var head [headerLen]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return 0, nil // empty or torn header: start over
	}
	if [4]byte(head[:4]) != magic || int(binary.LittleEndian.Uint32(head[4:])) != s.dims {
		return 0, fmt.Errorf("vectors: %s is not a %d-dim cache", s.path, s.dims)
	}
	good := int64(headerLen)
	for {
		id, hash, vec, n, err := s.readRecord(r)
		if err != nil {
			return good, nil // EOF or a torn tail: keep the intact prefix
		}
		s.live[id] = entry{hash, vec}
		s.records++
		good += n
	}
}

func (s *Store) readRecord(r *bufio.Reader) (id, hash string, vec []float32, n int64, err error) {
	var lens [4]byte
	if _, err = io.ReadFull(r, lens[:]); err != nil {
		return
	}
	idLen, hashLen := int(binary.LittleEndian.Uint16(lens[:2])), int(binary.LittleEndian.Uint16(lens[2:]))
	buf := make([]byte, idLen+hashLen+4*s.dims)
	if _, err = io.ReadFull(r, buf); err != nil {
		return
	}
	id, hash = string(buf[:idLen]), string(buf[idLen:idLen+hashLen])
	raw := buf[idLen+hashLen:]
	vec = make([]float32, s.dims)
	for i := range vec {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[4*i:]))
	}
	return id, hash, vec, int64(4 + len(buf)), nil
}

// openAppend opens the file for appending after cutting it to its intact
// prefix, writing the header when the file is new.
func (s *Store) openAppend(good int64) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	if good == 0 {
		var head [headerLen]byte
		copy(head[:], magic[:])
		binary.LittleEndian.PutUint32(head[4:], uint32(s.dims))
		if err := f.Truncate(0); err == nil {
			_, err = f.WriteAt(head[:], 0)
		}
		if err != nil {
			f.Close()
			return err
		}
		good = headerLen
	} else if err := f.Truncate(good); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Seek(good, io.SeekStart); err != nil {
		f.Close()
		return err
	}
	s.f = f
	return nil
}

// Space names the space this cache belongs to.
func (s *Store) Space() placeorg.SpaceID { return s.space }

// Len is the number of chats with a live vector.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.live)
}

// Get returns the vector for a chat when its text hash matches. The slice is
// shared with the cache and must not be modified.
func (s *Store) Get(chatID, textHash string) ([]float32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.live[chatID]
	if !ok || e.hash != textHash {
		return nil, false
	}
	return e.vec, true
}

// All returns the cached vector for each id whose hash matches, with false for
// the rest, in the order given.
func (s *Store) All(ids, hashes []string) ([][]float32, []bool) {
	out, ok := make([][]float32, len(ids)), make([]bool, len(ids))
	for i, id := range ids {
		if i < len(hashes) {
			out[i], ok[i] = s.Get(id, hashes[i])
		}
	}
	return out, ok
}

// Put appends a vector. The record is written whole in one call so a crash
// leaves at most one torn tail, which the next load cuts off.
func (s *Store) Put(chatID, textHash string, v []float32) error {
	if len(v) != s.dims {
		return fmt.Errorf("vectors: got %d dims, space %s has %d", len(v), s.space, s.dims)
	}
	if len(chatID) > math.MaxUint16 || len(textHash) > math.MaxUint16 {
		return errors.New("vectors: id or hash too long")
	}
	e := entry{textHash, append([]float32(nil), v...)}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeRecord(chatID, e); err != nil {
		return err
	}
	s.live[chatID] = e
	s.records++
	return nil
}

// Compact rewrites the file with live records only when at least half of what
// is on disk is stale. It reports whether it rewrote.
func (s *Store) Compact() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.records == 0 || 2*(s.records-len(s.live)) < s.records {
		return false, nil
	}
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return false, err
	}
	fresh := &Store{space: s.space, dims: s.dims, f: f}
	var head [headerLen]byte
	copy(head[:], magic[:])
	binary.LittleEndian.PutUint32(head[4:], uint32(s.dims))
	_, err = f.Write(head[:])
	for id, e := range s.live {
		if err != nil {
			break
		}
		err = fresh.writeRecord(id, e)
	}
	if err == nil {
		err = f.Sync()
	}
	if err == nil {
		err = os.Rename(tmp, s.path)
	}
	if err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return false, err
	}
	_ = s.f.Close()
	s.f, s.records = f, len(s.live)
	return true, nil
}

// writeRecord appends one record without touching the in-memory index.
func (s *Store) writeRecord(id string, e entry) error {
	buf := make([]byte, 4+len(id)+len(e.hash)+4*s.dims)
	binary.LittleEndian.PutUint16(buf, uint16(len(id)))
	binary.LittleEndian.PutUint16(buf[2:], uint16(len(e.hash)))
	off := 4 + copy(buf[4:], id)
	off += copy(buf[off:], e.hash)
	for i, x := range e.vec {
		binary.LittleEndian.PutUint32(buf[off+4*i:], math.Float32bits(x))
	}
	_, err := s.f.Write(buf)
	return err
}

// Close releases the file.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.f.Close()
}
