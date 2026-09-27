package bare

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/home"
)

const (
	bashSpillBytes       = 8 << 20
	bashSpillBudgetBytes = 128 << 20
	bashSpillBudgetFiles = 64
	bashSpillTTL         = 7 * 24 * time.Hour
)

// Foreground output keeps a bounded initial snapshot plus the accumulator's
// latest in-memory tail. Unlike a job, it has no durable numeric identity.
// Random exclusive names and independent file leases suffice for retention.
type bashSpill struct {
	root     *os.Root
	file     *os.File
	identity os.FileInfo
	path     string
	written  int64
	limited  bool
	problem  error
	closed   bool
}

// The selected state root may be an alias; owned descendants may not be links.
func bashSpillRoot() (*os.Root, string, error) {
	base := home.Dir()
	if absolute, err := filepath.Abs(base); err == nil {
		base = absolute
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return nil, "", err
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, "", err
	}
	for _, part := range []string{"logs", "bash"} {
		if err := root.Mkdir(part, 0o700); err != nil && !os.IsExist(err) {
			root.Close()
			return nil, "", err
		}
		before, err := root.Lstat(part)
		if err != nil || !before.IsDir() {
			root.Close()
			return nil, "", errors.New("linked or invalid bash output directory")
		}
		next, err := root.OpenRoot(part)
		if err != nil {
			root.Close()
			return nil, "", err
		}
		after, err := next.Stat(".")
		root.Close()
		if err != nil || !os.SameFile(before, after) {
			next.Close()
			return nil, "", errors.New("bash output directory changed")
		}
		root = next
		base = filepath.Join(base, part)
	}
	return root, base, nil
}

func bashSpillOpen(root *os.Root, name string, flags int) (*os.File, error) {
	before, err := root.Lstat(name)
	if err != nil || !before.Mode().IsRegular() {
		return nil, errors.New("unsafe bash output file")
	}
	file, err := root.OpenFile(name, flags, 0)
	if err != nil {
		return nil, err
	}
	after, statErr := file.Stat()
	current, pathErr := root.Lstat(name)
	if statErr != nil || pathErr != nil || !current.Mode().IsRegular() || !os.SameFile(before, after) || !os.SameFile(after, current) || !bashSpillSingleLink(file) {
		file.Close()
		return nil, errors.New("bash output identity changed")
	}
	return file, nil
}

func bashSpillLock(root *os.Root) (*os.File, error) {
	file, err := root.OpenFile(".retention.lock", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if os.IsExist(err) {
		file, err = bashSpillOpen(root, ".retention.lock", os.O_RDWR)
	}
	if err != nil {
		return nil, err
	}
	if err := filelock.Lock(file, true, false); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func newBashSpill() *bashSpill {
	s := &bashSpill{}
	root, base, err := bashSpillRoot()
	if err != nil {
		s.problem = err
		return s
	}
	defer func() {
		if s.root == nil {
			root.Close()
		}
	}()
	lock, err := bashSpillLock(root)
	if err != nil {
		s.problem = err
		return s
	}
	defer lock.Close()
	if err := sweepBashSpills(root, bashSpillBudgetBytes, bashSpillBudgetFiles, time.Now()); err != nil {
		s.problem = err
		return s
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		s.problem = err
		return s
	}
	name := "spill-" + hex.EncodeToString(id[:]) + ".log"
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		s.problem = err
		return s
	}
	if err := filelock.Lock(file, true, true); err != nil {
		file.Close()
		root.Remove(name)
		s.problem = err
		return s
	}
	s.identity, err = file.Stat()
	if err != nil {
		file.Close()
		root.Remove(name)
		s.problem = err
		return s
	}
	s.root, s.file, s.path = root, file, filepath.Join(base, name)
	return s
}

func (s *bashSpill) write(data []byte) {
	if s == nil || s.closed || s.problem != nil || s.file == nil {
		return
	}
	room := int64(bashSpillBytes) - s.written
	if int64(len(data)) > room {
		s.limited = true
		data = data[:room]
	}
	if len(data) == 0 {
		return
	}
	n, err := s.file.Write(data)
	s.written += int64(n)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		s.problem = err
	}
}

func (s *bashSpill) close() {
	if s == nil || s.closed {
		return
	}
	s.closed = true
	if s.file != nil {
		s.problem = errors.Join(s.problem, s.file.Close())
	}
	root := s.root
	if root == nil {
		return
	}
	defer root.Close()
	lock, err := bashSpillLock(root)
	if err == nil {
		defer lock.Close()
		err = sweepBashSpills(root, bashSpillBudgetBytes, bashSpillBudgetFiles, time.Now())
	}
	s.problem = errors.Join(s.problem, err)
}

func (s *bashSpill) description() string {
	if s == nil {
		return "Output snapshot unavailable"
	}
	if s.problem != nil {
		return fmt.Sprintf("Output snapshot incomplete (%v): %s", s.problem, s.path)
	}
	if s.path == "" {
		return "Output snapshot unavailable"
	}
	info, err := os.Stat(s.path)
	if os.IsNotExist(err) {
		return "Output snapshot evicted by retention; latest output shown above"
	}
	if err != nil || !os.SameFile(s.identity, info) {
		return "Output snapshot unavailable: file identity changed; latest output shown above"
	}
	if s.limited {
		return fmt.Sprintf("Output snapshot: %s (first %d MiB only; latest output shown above)", s.path, bashSpillBytes>>20)
	}
	return "Full output: " + s.path
}

func bashSpillName(name string) bool {
	if !strings.HasPrefix(name, "spill-") || !strings.HasSuffix(name, ".log") {
		return false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(name, "spill-"), ".log")
	_, err := hex.DecodeString(id)
	return len(id) == 32 && err == nil
}

// Called under the directory lock. Unknown files and active/unsafe writers
// are preserved. Reacquiring the same inode's lease covers the unlink itself.
func sweepBashSpills(root *os.Root, bytesLimit int64, countLimit int, now time.Time) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	dir.Close()
	if err != nil {
		return err
	}
	type candidate struct {
		name string
		info os.FileInfo
	}
	var eligible []candidate
	var total int64
	for _, entry := range entries {
		if !bashSpillName(entry.Name()) {
			continue
		}
		file, err := bashSpillOpen(root, entry.Name(), os.O_RDWR)
		if err != nil {
			continue
		}
		if err := filelock.Lock(file, true, true); err != nil {
			file.Close()
			continue
		}
		info, err := file.Stat()
		file.Close()
		if err != nil {
			return err
		}
		eligible = append(eligible, candidate{entry.Name(), info})
		if info.Size() > math.MaxInt64-total {
			return errors.New("bash output retention size overflow")
		}
		total += info.Size()
	}
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].info.ModTime().Before(eligible[j].info.ModTime()) })
	count := len(eligible)
	for _, one := range eligible {
		if total <= bytesLimit && count <= countLimit && !one.info.ModTime().Before(now.Add(-bashSpillTTL)) {
			break
		}
		file, err := bashSpillOpen(root, one.name, os.O_RDWR)
		if err != nil {
			return err
		}
		if err := filelock.Lock(file, true, true); err != nil {
			file.Close()
			return err
		}
		info, err := file.Stat()
		if err == nil && !os.SameFile(one.info, info) {
			err = errors.New("bash output identity changed before cleanup")
		}
		if err == nil {
			err = root.Remove(one.name)
		}
		file.Close()
		if err != nil {
			return err
		}
		total -= one.info.Size()
		count--
	}
	return nil
}
