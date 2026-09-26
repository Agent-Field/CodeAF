//go:build !windows

// Package storage is the flat-file JSON store. Per-resource locks coordinate
// goroutines while advisory file locks make read-modify-write safe across
// processes.
package storage

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/seniordev/jsonutil"
	"golang.org/x/sys/unix"
)

// NotFoundError is the model-visible storage miss.
type NotFoundError struct {
	Message string `json:"message"`
}

func (e *NotFoundError) Error() string { return e.Message }

// Store is a JSON store rooted at Dir.
type Store struct {
	Dir string

	once  sync.Once
	init  error
	mu    sync.Mutex
	locks map[string]*sync.RWMutex
}

// WriteItem is one resource in a WriteBatch operation.
type WriteItem struct {
	Key     []string
	Content any
}

// Option configures a Store.
type Option func(*Store)

// New constructs a store at the exact storage directory passed by the caller.
func New(dir string, options ...Option) *Store {
	s := &Store{
		Dir:   dir,
		locks: make(map[string]*sync.RWMutex),
	}
	for _, option := range options {
		option(s)
	}
	return s
}

// NewFromDataDir roots the store at dataDir/storage.
func NewFromDataDir(dataDir string, options ...Option) *Store {
	return New(filepath.Join(dataDir, "storage"), options...)
}

func (s *Store) initialize() error {
	s.once.Do(func() {
		s.init = os.MkdirAll(s.Dir, 0o755)
	})
	return s.init
}

func (s *Store) target(key []string) string {
	parts := append([]string{s.Dir}, key...)
	return filepath.Join(parts...) + ".json"
}

func (s *Store) lock(target string) *sync.RWMutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	lock := s.locks[target]
	if lock == nil {
		lock = &sync.RWMutex{}
		s.locks[target] = lock
	}
	return lock
}

func (s *Store) withAdvisoryLock(exclusive bool, fn func() error) error {
	return withFileLock(filepath.Join(s.Dir, ".lock"), exclusive, fn)
}

func withFileLock(lockPath string, exclusive bool, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	operation := unix.LOCK_SH
	if exclusive {
		operation = unix.LOCK_EX
	}
	if err := unix.Flock(int(file.Fd()), operation); err != nil {
		return err
	}
	defer unix.Flock(int(file.Fd()), unix.LOCK_UN)
	return fn()
}

func (s *Store) resourceLockPath(target string) string {
	hash := sha256.Sum256([]byte(filepath.Clean(target)))
	return filepath.Join(s.Dir, ".locks", fmt.Sprintf("%x.lock", hash))
}

func (s *Store) withResourceAdvisoryLock(target string, exclusive bool, fn func() error) error {
	return withFileLock(s.resourceLockPath(target), exclusive, fn)
}

func (s *Store) withResourceLocks(targets []string, fn func() error) error {
	unique := make(map[string]struct{}, len(targets))
	ordered := make([]string, 0, len(targets))
	for _, target := range targets {
		if _, exists := unique[target]; exists {
			continue
		}
		unique[target] = struct{}{}
		ordered = append(ordered, target)
	}
	sort.Strings(ordered)
	for _, target := range ordered {
		s.lock(target).Lock()
	}
	defer func() {
		for index := len(ordered) - 1; index >= 0; index-- {
			s.lock(ordered[index]).Unlock()
		}
	}()
	files := make([]*os.File, 0, len(ordered))
	for _, target := range ordered {
		lockPath := s.resourceLockPath(target)
		if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
			closeResourceLocks(files)
			return err
		}
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			closeResourceLocks(files)
			return err
		}
		if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
			_ = file.Close()
			closeResourceLocks(files)
			return err
		}
		files = append(files, file)
	}
	defer closeResourceLocks(files)
	return fn()
}

func closeResourceLocks(files []*os.File) {
	for index := len(files) - 1; index >= 0; index-- {
		_ = unix.Flock(int(files[index].Fd()), unix.LOCK_UN)
		_ = files[index].Close()
	}
}

// Remove deletes a stored resource. Missing resources are ignored.
func (s *Store) Remove(key []string) error {
	if err := s.initialize(); err != nil {
		return err
	}
	target := s.target(key)
	lock := s.lock(target)
	lock.Lock()
	defer lock.Unlock()
	return s.withResourceAdvisoryLock(target, true, func() error {
		err := os.Remove(target)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		return syncDirectory(filepath.Dir(target))
	})
}

// Read decodes a resource into a generic JSON value (maps, slices, float64).
func (s *Store) Read(key []string) (any, error) {
	if err := s.initialize(); err != nil {
		return nil, err
	}
	target := s.target(key)
	lock := s.lock(target)
	lock.RLock()
	defer lock.RUnlock()
	var data []byte
	err := s.withResourceAdvisoryLock(target, false, func() error {
		var readErr error
		data, readErr = os.ReadFile(target)
		return readErr
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, &NotFoundError{Message: "Resource not found: " + target}
	}
	if err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}

// ReadInto decodes a resource into dst using encoding/json.
func (s *Store) ReadInto(key []string, dst any) error {
	if err := s.initialize(); err != nil {
		return err
	}
	target := s.target(key)
	lock := s.lock(target)
	lock.RLock()
	defer lock.RUnlock()
	var data []byte
	err := s.withResourceAdvisoryLock(target, false, func() error {
		var readErr error
		data, readErr = os.ReadFile(target)
		return readErr
	})
	if errors.Is(err, fs.ErrNotExist) {
		return &NotFoundError{Message: "Resource not found: " + target}
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

// ReadAs decodes a resource into a T.
func ReadAs[T any](s *Store, key []string) (T, error) {
	var out T
	err := s.ReadInto(key, &out)
	return out, err
}

// Update holds the resource mutex and cross-process flock across read,
// mutation, and rewrite. mutate must not call a Store method for the same key:
// the callback is deliberately non-reentrant and doing so will deadlock.
func (s *Store) Update(key []string, mutate func(any)) (any, error) {
	if err := s.initialize(); err != nil {
		return nil, err
	}
	target := s.target(key)
	lock := s.lock(target)
	lock.Lock()
	defer lock.Unlock()
	var value any
	err := s.withResourceAdvisoryLock(target, true, func() error {
		data, err := os.ReadFile(target)
		if errors.Is(err, fs.ErrNotExist) {
			return &NotFoundError{Message: "Resource not found: " + target}
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		mutate(value)
		return writeJSON(target, value)
	})
	return value, err
}

// UpdateAs is a typed update helper. It preserves struct field ordering on the
// rewrite, while Update preserves arbitrary parsed-object ordering. mutate has
// the same non-reentrancy requirement as Update.
func UpdateAs[T any](s *Store, key []string, mutate func(*T)) (T, error) {
	var zero T
	if err := s.initialize(); err != nil {
		return zero, err
	}
	target := s.target(key)
	lock := s.lock(target)
	lock.Lock()
	defer lock.Unlock()
	var value T
	err := s.withResourceAdvisoryLock(target, true, func() error {
		data, err := os.ReadFile(target)
		if errors.Is(err, fs.ErrNotExist) {
			return &NotFoundError{Message: "Resource not found: " + target}
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		mutate(&value)
		return writeJSON(target, value)
	})
	if err != nil {
		return zero, err
	}
	return value, nil
}

// Write persists content as two-space-indented JSON without a trailing
// newline.
func (s *Store) Write(key []string, content any) error {
	if err := s.initialize(); err != nil {
		return err
	}
	target := s.target(key)
	lock := s.lock(target)
	lock.Lock()
	defer lock.Unlock()
	return s.withResourceAdvisoryLock(target, true, func() error { return writeJSON(target, content) })
}

// WriteBatch serializes a related group under ordered per-resource locks.
// Callers control item order; prompt persistence writes parts before the
// message that makes the turn visible. Files are synced individually, then
// each touched directory is synced once after all renames.
func (s *Store) WriteBatch(items []WriteItem) error {
	if err := s.initialize(); err != nil {
		return err
	}
	targets := make([]string, len(items))
	for index, item := range items {
		targets[index] = s.target(item.Key)
	}
	return s.withResourceLocks(targets, func() error { return writeJSONBatch(targets, items) })
}

// CreateExclusive writes a resource only when no claimant has created it.
// The store-wide advisory lock makes the check/atomic-rename indivisible
// across cooperating processes; the returned boolean reports the winner.
func (s *Store) CreateExclusive(key []string, content any) (bool, error) {
	if err := s.initialize(); err != nil {
		return false, err
	}
	target := s.target(key)
	lock := s.lock(target)
	lock.Lock()
	defer lock.Unlock()
	created := false
	err := s.withResourceAdvisoryLock(target, true, func() error {
		if _, err := os.Stat(target); err == nil {
			return nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := writeJSON(target, content); err != nil {
			return err
		}
		created = true
		return nil
	})
	return created, err
}

// List returns descendant resource keys in sorted order.
func (s *Store) List(prefix []string) ([][]string, error) {
	if err := s.initialize(); err != nil {
		return nil, err
	}
	cwdParts := append([]string{s.Dir}, prefix...)
	cwd := filepath.Join(cwdParts...)
	result := [][]string{}
	err := s.withAdvisoryLock(false, func() error {
		return filepath.WalkDir(cwd, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if path != cwd && (entry.Name() == ".locks" || entry.Name() == "quarantine") {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Name() == ".lock" || strings.HasPrefix(entry.Name(), ".tmp-") {
				return nil
			}
			rel, err := filepath.Rel(cwd, path)
			if err != nil {
				return err
			}
			// The key is the path minus the five-byte ".json" suffix; the
			// extension is not validated, so a foreign file in the store yields
			// a truncated key rather than an error.
			if len(rel) >= 5 {
				rel = rel[:len(rel)-5]
			} else {
				rel = ""
			}
			key := append(append([]string{}, prefix...), strings.Split(rel, string(filepath.Separator))...)
			result = append(result, key)
			return nil
		})
	})
	if errors.Is(err, fs.ErrNotExist) {
		return [][]string{}, nil
	}
	if err != nil {
		// A walk failure lists nothing rather than failing the caller.
		return [][]string{}, nil
	}
	sort.SliceStable(result, func(i, j int) bool {
		return strings.Join(result[i], "/") < strings.Join(result[j], "/")
	})
	return result, nil
}

func writeJSON(target string, content any) error {
	data, err := jsonutil.MarshalIndent(content)
	if err != nil {
		return err
	}
	return writeBytesAtomic(target, data)
}

func writeJSONBatch(targets []string, items []WriteItem) error {
	directories := map[string]struct{}{}
	for index, item := range items {
		data, err := jsonutil.MarshalIndent(item.Content)
		if err != nil {
			return joinBatchSyncError(err, directories)
		}
		temporary, err := prepareBytesAtomic(targets[index], data)
		if err != nil {
			return joinBatchSyncError(err, directories)
		}
		if err := os.Rename(temporary, targets[index]); err != nil {
			_ = os.Remove(temporary)
			return joinBatchSyncError(err, directories)
		}
		directories[filepath.Dir(targets[index])] = struct{}{}
	}
	return syncBatchDirectories(directories)
}

func joinBatchSyncError(writeErr error, directories map[string]struct{}) error {
	if syncErr := syncBatchDirectories(directories); syncErr != nil {
		return errors.Join(writeErr, syncErr)
	}
	return writeErr
}

func syncBatchDirectories(directories map[string]struct{}) error {
	orderedDirectories := make([]string, 0, len(directories))
	for directory := range directories {
		orderedDirectories = append(orderedDirectories, directory)
	}
	sort.Strings(orderedDirectories)
	for _, directory := range orderedDirectories {
		if err := syncDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

func writeBytesAtomic(target string, data []byte) error {
	temporaryPath, err := prepareBytesAtomic(target, data)
	if err != nil {
		return err
	}
	defer os.Remove(temporaryPath)
	if err := os.Rename(temporaryPath, target); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(target))
}

func prepareBytesAtomic(target string, data []byte) (string, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	directory := filepath.Dir(target)
	temporary, err := os.CreateTemp(directory, ".tmp-*.json")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	failed := true
	defer func() {
		if failed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return "", err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return "", err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	failed = false
	return temporaryPath, nil
}

func syncDirectory(directory string) error {
	file, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
