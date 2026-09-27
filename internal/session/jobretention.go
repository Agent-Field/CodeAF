package session

// Retention bounds completed, managed spools in one jobs directory. Legacy
// logs may have unlocked old writers and are never pruned. Active spools have
// their own size cap and hold an inode lock until the sink closes. This is not
// a machine-wide or active-job aggregate budget.
import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

const (
	jobRetentionLockName      = ".retention.lock"
	jobRetentionCounterName   = ".retention.highwater"
	jobRetentionMarkerSuffix  = ".retention"
	jobRetentionInitialized   = "codeaf job retention v1\n"
	jobRetentionMetadataLimit = 256
)

var errRetentionUnsafe = errors.New("job log retention: unsafe or damaged state; refusing allocation or cleanup")

type jobRetentionBudget struct {
	bytes  int64
	groups int
}

func defaultJobRetentionBudget() jobRetentionBudget { return jobRetentionBudget{128 << 20, 64} }

// Traverse directory handles, rejecting symlinks, including the jobs directory.
// Resolve only the platform's trusted temporary-directory alias first (macOS
// exposes /var as /private/var); user-created links below it remain refused.
func openJobRetentionDir(directory string, create bool) (*os.Root, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	temp := filepath.Clean(os.TempDir())
	if relative, e := filepath.Rel(temp, absolute); e == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		if canonical, e := filepath.EvalSymlinks(temp); e == nil {
			absolute = filepath.Join(canonical, relative)
		}
	}
	volume := filepath.VolumeName(absolute) + string(os.PathSeparator)
	root, err := os.OpenRoot(volume)
	if err != nil {
		return nil, err
	}
	for _, component := range strings.Split(strings.TrimPrefix(absolute, volume), string(os.PathSeparator)) {
		if component == "" {
			continue
		}
		info, e := root.Lstat(component)
		if os.IsNotExist(e) && create {
			if e = root.Mkdir(component, 0o755); e != nil && !os.IsExist(e) {
				root.Close()
				return nil, e
			}
			info, e = root.Lstat(component)
		}
		if e != nil || !info.IsDir() {
			root.Close()
			return nil, errRetentionUnsafe
		}
		next, e := root.OpenRoot(component)
		if e != nil {
			root.Close()
			return nil, e
		}
		opened, e := next.Stat(".")
		root.Close()
		if e != nil || !os.SameFile(info, opened) {
			next.Close()
			return nil, errRetentionUnsafe
		}
		root = next
	}
	return root, nil
}

// Check pathname and opened descriptor before any write. New files use
// O_EXCL, never O_TRUNC. Root confines operations if a parent moves during I/O.
func retentionOpen(root *os.Root, name string, flags int) (*os.File, error) {
	before, err := root.Lstat(name)
	if err != nil || !before.Mode().IsRegular() {
		return nil, errRetentionUnsafe
	}
	file, err := root.OpenFile(name, flags, 0)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	current, pathErr := root.Lstat(name)
	if err != nil || pathErr != nil || !current.Mode().IsRegular() || !os.SameFile(before, after) || !os.SameFile(after, current) || !jobRetentionSingleLink(file) {
		file.Close()
		return nil, errRetentionUnsafe
	}
	return file, nil
}
func retentionRead(root *os.Root, name string) ([]byte, error) {
	file, err := retentionOpen(root, name, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, jobRetentionMetadataLimit+1))
	if err != nil || len(data) > jobRetentionMetadataLimit {
		return nil, errRetentionUnsafe
	}
	return data, nil
}

type jobRetentionDirectory struct {
	root *os.Root
	lock *os.File
}

func (d *jobRetentionDirectory) close() { d.lock.Close(); d.root.Close() }
func lockJobRetention(directory string) (*jobRetentionDirectory, error) {
	root, err := openJobRetentionDir(directory, false)
	if err != nil {
		return nil, err
	}
	file, err := root.OpenFile(jobRetentionLockName, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if os.IsExist(err) {
		file, err = retentionOpen(root, jobRetentionLockName, os.O_RDWR)
	}
	if err != nil {
		root.Close()
		return nil, err
	}
	if err = filelock.Lock(file, true, false); err != nil {
		file.Close()
		root.Close()
		return nil, err
	}
	return &jobRetentionDirectory{root, file}, nil
}
func jobRetentionMarker(base string) string { return "codeaf job log retention v1\n" + base + "\n" }
func jobRetentionMarkerPath(directory string, id int64) string {
	return filepath.Join(directory, fmt.Sprintf("%d.log%s", id, jobRetentionMarkerSuffix))
}
func jobLogBaseID(name string) (int64, bool) {
	name = strings.TrimSuffix(name, jobRetentionMarkerSuffix)
	name = strings.TrimSuffix(name, ".1")
	if !strings.HasSuffix(name, ".log") {
		return 0, false
	}
	digits := strings.TrimSuffix(name, ".log")
	id, err := strconv.ParseInt(digits, 10, 64)
	return id, err == nil && id > 0 && strconv.FormatInt(id, 10) == digits
}
func (d *jobRetentionDirectory) entries() ([]os.DirEntry, error) {
	file, err := d.root.Open(".")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return file.ReadDir(-1)
}
func (d *jobRetentionDirectory) counter() (int64, error) {
	if _, err := d.lock.Seek(0, 0); err != nil {
		return 0, err
	}
	initialized, err := io.ReadAll(io.LimitReader(d.lock, jobRetentionMetadataLimit+1))
	if err != nil || (len(initialized) != 0 && string(initialized) != jobRetentionInitialized) {
		return 0, errRetentionUnsafe
	}
	_, err = d.root.Lstat(jobRetentionCounterName)
	counterExists := err == nil
	var high int64
	if counterExists {
		data, e := retentionRead(d.root, jobRetentionCounterName)
		if e != nil {
			return 0, e
		}
		high, e = strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		if e != nil || high < 0 {
			return 0, errRetentionUnsafe
		}
	} else if !os.IsNotExist(err) || len(initialized) != 0 {
		return 0, errRetentionUnsafe
	}
	entries, err := d.entries()
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		// Markers also prove previous initialization if the lock was removed.
		if strings.HasSuffix(entry.Name(), jobRetentionMarkerSuffix) && !counterExists {
			return 0, errRetentionUnsafe
		}
		if id, ok := jobLogBaseID(entry.Name()); ok && id > high {
			high = id
		}
	}
	return high, nil
}
func (d *jobRetentionDirectory) persist(high int64) error {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temp := fmt.Sprintf(".retention-counter-%x.tmp", nonce)
	file, err := d.root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer d.root.Remove(temp)
	_, err = fmt.Fprintf(file, "%d\n", high)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = d.root.Rename(temp, jobRetentionCounterName); err != nil {
		return err
	}
	if err = jobRetentionSyncDir(d.root); err != nil {
		return err
	}
	// This permanent inode remembers initialization after all payload eviction.
	// A missing counter then fails closed, rather than resetting the id history.
	if _, err = d.lock.WriteAt([]byte(jobRetentionInitialized), 0); err != nil {
		return err
	}
	return d.lock.Sync()
}

// Counter, O_EXCL creation, writer lease and ownership marker publication are
// one transaction under the directory lock. Persist before pruning anything.
func jobRetentionClaim(directory string) (int, string, *os.File, error) {
	return jobRetentionClaimAbove(directory, 0)
}

// A registry may move its legacy jobs directory when the workspace changes;
// its already-issued ids remain reserved even in a fresh destination.
func jobRetentionClaimAbove(directory string, floor int64) (int, string, *os.File, error) {
	d, err := lockJobRetention(directory)
	if err != nil {
		return 0, "", nil, err
	}
	defer d.close()
	high, err := d.counter()
	if err != nil {
		return 0, "", nil, err
	}
	if high < floor {
		high = floor
	}
	if high >= int64(math.MaxInt) {
		return 0, "", nil, errRetentionUnsafe
	}
	id := high + 1
	if err = d.persist(id); err != nil {
		return 0, "", nil, err
	}
	name := fmt.Sprintf("%d.log", id)
	file, err := d.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
	if err != nil {
		return 0, "", nil, err
	}
	if err = filelock.Lock(file, true, true); err != nil {
		file.Close()
		d.root.Remove(name)
		return 0, "", nil, err
	}
	marker, err := d.root.OpenFile(name+jobRetentionMarkerSuffix, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		_, err = io.WriteString(marker, jobRetentionMarker(name))
		closeErr := marker.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			d.root.Remove(name + jobRetentionMarkerSuffix)
		}
	}
	if err != nil {
		file.Close()
		d.root.Remove(name)
		return 0, "", nil, err
	}
	if err = d.sweep(defaultJobRetentionBudget()); err != nil {
		file.Close()
		// Keep the marker if removing the payload failed, so cleanup can retry.
		if e := d.root.Remove(name); e == nil {
			d.root.Remove(name + jobRetentionMarkerSuffix)
		}
		return 0, "", nil, err
	}
	return int(id), filepath.Join(directory, name), file, nil
}

type retainedJob struct {
	id       int64
	base     string
	bytes    int64
	identity os.FileInfo
	newest   time.Time
}

// Acquire an independent lease; the caller holds it throughout deletion.
func (d *jobRetentionDirectory) inspect(base string) (*os.File, int64, error) {
	marker, err := retentionRead(d.root, base+jobRetentionMarkerSuffix)
	if err != nil || string(marker) != jobRetentionMarker(base) {
		return nil, 0, errRetentionUnsafe
	}
	file, err := retentionOpen(d.root, base, os.O_RDWR)
	if err != nil {
		return nil, 0, err
	}
	if err = filelock.Lock(file, true, true); err != nil {
		file.Close()
		return nil, 0, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, 0, err
	}
	size := info.Size()
	backup := base + ".1"
	if _, err = d.root.Lstat(backup); err == nil {
		other, e := retentionOpen(d.root, backup, os.O_RDONLY)
		if e != nil {
			file.Close()
			return nil, 0, e
		}
		stat, e := other.Stat()
		other.Close()
		if e != nil {
			file.Close()
			return nil, 0, e
		}
		if stat.Size() > math.MaxInt64-size {
			file.Close()
			return nil, 0, errRetentionUnsafe
		}
		size += stat.Size()
	} else if !os.IsNotExist(err) {
		file.Close()
		return nil, 0, err
	}
	return file, size, nil
}

// latestPayloadTime keeps a fresh rotated chunk alive even if the base is old.
func (d *jobRetentionDirectory) latestPayloadTime(base string, info os.FileInfo) (time.Time, error) {
	newest := info.ModTime()
	backup, err := d.root.Lstat(base + ".1")
	if err == nil {
		if backup.ModTime().After(newest) {
			newest = backup.ModTime()
		}
	} else if !os.IsNotExist(err) {
		return time.Time{}, err
	}
	return newest, nil
}

func (d *jobRetentionDirectory) sweep(budget jobRetentionBudget, expiredBefore ...time.Time) error {
	var cutoff time.Time
	if len(expiredBefore) > 0 {
		cutoff = expiredBefore[0]
	}
	if budget.bytes < 0 || budget.groups < 0 {
		return errRetentionUnsafe
	}
	entries, err := d.entries()
	if err != nil {
		return err
	}
	var groups []retainedJob
	var total int64
	for _, entry := range entries {
		// A crash after payload unlink may leave only its small ownership
		// marker. Under the directory lock no publisher is in flight.
		if strings.HasSuffix(entry.Name(), jobRetentionMarkerSuffix) {
			base := strings.TrimSuffix(entry.Name(), jobRetentionMarkerSuffix)
			if _, ok := jobLogBaseID(base); ok {
				if _, e := d.root.Lstat(base); os.IsNotExist(e) {
					if _, e := d.root.Lstat(base + ".1"); os.IsNotExist(e) {
						data, e := retentionRead(d.root, entry.Name())
						if e == nil && string(data) == jobRetentionMarker(base) {
							if e := d.root.Remove(entry.Name()); e != nil {
								return e
							}
						}
					}
				}
			}
			continue
		}
		id, ok := jobLogBaseID(entry.Name())
		if !ok || entry.Name() != fmt.Sprintf("%d.log", id) {
			continue
		}
		file, size, err := d.inspect(entry.Name())
		if err != nil {
			continue
		} // live, legacy, unsafe: preserve without deletion
		info, err := file.Stat()
		var newest time.Time
		if err == nil {
			newest, err = d.latestPayloadTime(entry.Name(), info)
		}
		file.Close()
		if err != nil {
			return err
		}
		if size > math.MaxInt64-total {
			return errRetentionUnsafe
		}
		total += size
		groups = append(groups, retainedJob{id: id, base: entry.Name(), bytes: size, identity: info, newest: newest})
	}
	// Expired payloads go first, so they do not displace fresh groups merely
	// because a long-running fresh job has an older allocated id.
	sort.Slice(groups, func(i, j int) bool {
		oldI := !cutoff.IsZero() && groups[i].newest.Before(cutoff)
		oldJ := !cutoff.IsZero() && groups[j].newest.Before(cutoff)
		if oldI != oldJ {
			return oldI
		}
		return groups[i].id < groups[j].id
	})
	count := len(groups)
	for _, group := range groups {
		overBudget := total > budget.bytes || count > budget.groups
		expired := !cutoff.IsZero() && group.newest.Before(cutoff)
		if !overBudget && !expired {
			continue
		}
		file, _, e := d.inspect(group.base)
		if e != nil {
			return fmt.Errorf("job retention could not reclaim %s: %w", group.base, e)
		}
		info, e := file.Stat()
		if e == nil && !os.SameFile(group.identity, info) {
			e = errRetentionUnsafe
		}
		if e == nil && !overBudget {
			newest, ageErr := d.latestPayloadTime(group.base, info)
			if ageErr != nil {
				e = ageErr
			} else if !newest.Before(cutoff) {
				file.Close()
				continue
			}
		}
		if e == nil {
			e = d.remove(group.base)
		}
		file.Close()
		if e != nil {
			return fmt.Errorf("job retention could not reclaim %s: %w", group.base, e)
		}
		total -= group.bytes
		count--
	}
	return nil
}
func (d *jobRetentionDirectory) remove(base string) error {
	// Do not remove ownership if any payload unlink fails: partial cleanup must
	// remain retryable. All operations remain beneath the pinned jobs directory.
	for _, name := range []string{base + ".1", base, base + jobRetentionMarkerSuffix} {
		if _, err := d.root.Lstat(name); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		file, err := retentionOpen(d.root, name, os.O_RDONLY)
		if err != nil {
			return err
		}
		file.Close()
		if err = d.root.Remove(name); err != nil {
			return err
		}
	}
	return nil
}
func jobRetentionSweep(directory string, budget jobRetentionBudget) error {
	return jobRetentionSweepBefore(directory, budget, time.Time{})
}

// Startup retains the existing age policy for proven inactive managed logs,
// without applying age to permanent identity metadata or uncertain legacy logs.
func jobRetentionSweepBefore(directory string, budget jobRetentionBudget, cutoff time.Time) error {
	d, err := lockJobRetention(directory)
	if err != nil {
		return err
	}
	defer d.close()
	high, err := d.counter()
	if err != nil {
		return err
	}
	if err = d.persist(high); err != nil {
		return err
	}
	return d.sweep(budget, cutoff)
}
func jobRetentionFinish(sink *jobSink) {
	if sink == nil || sink.base == "" {
		return
	}
	err := jobRetentionSweep(filepath.Dir(sink.base), defaultJobRetentionBudget())
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if err != nil {
		sink.retentionText = "job log retention deferred: " + err.Error()
	}
}

// A job refused at the registry's join boundary was never published. Remove
// its payload and marker without discarding the directory's durable id history.
func jobRetentionDiscard(path string) error {
	d, err := lockJobRetention(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.close()
	file, _, err := d.inspect(filepath.Base(path))
	if err != nil {
		if _, statErr := d.root.Lstat(filepath.Base(path)); os.IsNotExist(statErr) {
			return nil
		}
		return err
	}
	defer file.Close()
	return d.remove(filepath.Base(path))
}
