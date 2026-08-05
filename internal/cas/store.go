package cas

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var (
	// ErrInvalidRef means a reference is not a hexadecimal SHA-256 digest.
	ErrInvalidRef = errors.New("invalid cas ref")
	// ErrContentMismatch means stored bytes no longer hash to their reference.
	ErrContentMismatch = errors.New("cas content does not match ref")
)

// Ref is the lowercase hexadecimal SHA-256 digest of a blob.
type Ref string

func (r Ref) String() string { return string(r) }

// Store is a content-addressed collection of immutable blobs.
type Store struct {
	root string
}

// New opens a store rooted at root, creating it when necessary.
func New(root string) (*Store, error) {
	trimmed := strings.TrimSpace(root)
	if trimmed == "" {
		return nil, fmt.Errorf("cas root is required")
	}
	absolute, err := filepath.Abs(trimmed)
	if err != nil {
		return nil, fmt.Errorf("resolve cas root: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		return nil, fmt.Errorf("create cas root: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, fmt.Errorf("stat cas root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("cas root %q is not a directory", absolute)
	}
	return &Store{root: absolute}, nil
}

// Put streams a blob into the store and returns its content digest.
func (s *Store) Put(reader io.Reader) (Ref, error) {
	if reader == nil {
		return "", fmt.Errorf("cas reader is required")
	}
	tempDir := filepath.Join(s.root, ".tmp")
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return "", fmt.Errorf("create cas temporary directory: %w", err)
	}
	temporary, err := os.CreateTemp(tempDir, "put-")
	if err != nil {
		return "", fmt.Errorf("create cas temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(temporary, hasher), reader); err != nil {
		temporary.Close()
		return "", fmt.Errorf("write cas temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return "", fmt.Errorf("sync cas temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close cas temporary file: %w", err)
	}

	ref := Ref(hex.EncodeToString(hasher.Sum(nil)))
	target, err := s.Path(ref)
	if err != nil {
		return "", err
	}
	if exists, err := regularFileExists(target); err != nil {
		return "", err
	} else if exists {
		return ref, nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("create cas object directory: %w", err)
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		// Another writer may have installed the same immutable object after our
		// existence check. Platforms differ on whether Rename replaces it.
		if exists, statErr := regularFileExists(target); statErr == nil && exists {
			return ref, nil
		}
		return "", fmt.Errorf("install cas object: %w", err)
	}
	return ref, nil
}

// PutBytes stores data without requiring the caller to construct a reader.
func (s *Store) PutBytes(data []byte) (Ref, error) {
	return s.Put(bytes.NewReader(data))
}

// PutFile streams an existing file into the store.
func (s *Store) PutFile(path string) (Ref, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file for cas: %w", err)
	}
	defer file.Close()
	ref, err := s.Put(file)
	if err != nil {
		return "", fmt.Errorf("put file in cas: %w", err)
	}
	return ref, nil
}

// Get opens a stored blob for reading.
func (s *Store) Get(ref Ref) (io.ReadCloser, error) {
	path, err := s.Path(ref)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open cas object %s: %w", ref, err)
	}
	return file, nil
}

// Stat reports a blob's size and whether it exists.
func (s *Store) Stat(ref Ref) (size int64, exists bool, err error) {
	path, err := s.Path(ref)
	if err != nil {
		return 0, false, err
	}
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("stat cas object %s: %w", ref, err)
	}
	if !info.Mode().IsRegular() {
		return 0, false, fmt.Errorf("cas object %s is not a regular file", ref)
	}
	return info.Size(), true, nil
}

// Path returns the absolute on-disk path for a reference.
func (s *Store) Path(ref Ref) (string, error) {
	digest, err := normalizeRef(ref)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.root, digest[:2], digest), nil
}

// Verify checks that stored content still hashes to ref.
func (s *Store) Verify(ref Ref) error {
	digest, err := normalizeRef(ref)
	if err != nil {
		return err
	}
	reader, err := s.Get(Ref(digest))
	if err != nil {
		return err
	}
	defer reader.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, reader); err != nil {
		return fmt.Errorf("read cas object %s for verification: %w", ref, err)
	}
	actual := hex.EncodeToString(hasher.Sum(nil))
	if actual != digest {
		return fmt.Errorf("%w: ref %s hashes to %s", ErrContentMismatch, digest, actual)
	}
	return nil
}

func normalizeRef(ref Ref) (string, error) {
	raw := string(ref)
	if len(raw) != sha256.Size*2 {
		return "", fmt.Errorf("%w %q: want %d hexadecimal characters", ErrInvalidRef, raw, sha256.Size*2)
	}
	decoded, err := hex.DecodeString(raw)
	if err != nil {
		return "", fmt.Errorf("%w %q: %v", ErrInvalidRef, raw, err)
	}
	return hex.EncodeToString(decoded), nil
}

func regularFileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat cas object: %w", err)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("cas object path %q is not a regular file", path)
	}
	return true, nil
}
