package cas

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPutGetRoundTrip(t *testing.T) {
	store := newTestStore(t)
	want := []byte("an observation too large for the graph")

	ref, err := store.Put(bytes.NewReader(want))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := store.Get(ref)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	if closeErr := reader.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Get() = %q, want %q", got, want)
	}

	size, exists, err := store.Stat(ref)
	if err != nil {
		t.Fatal(err)
	}
	if !exists || size != int64(len(want)) {
		t.Fatalf("Stat() = (%d, %t), want (%d, true)", size, exists, len(want))
	}
	path, err := store.Path(ref)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("Path() = %q, want an absolute path", path)
	}
	if filepath.Base(filepath.Dir(path)) != string(ref[:2]) || filepath.Base(path) != string(ref) {
		t.Fatalf("Path() = %q, want the two-level digest layout", path)
	}
}

func TestPutDeduplicates(t *testing.T) {
	store := newTestStore(t)
	content := []byte("the same immutable transcript")

	first, err := store.PutBytes(content)
	if err != nil {
		t.Fatal(err)
	}
	path, err := store.Path(first)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.PutBytes(content)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if first != second {
		t.Fatalf("second Put() ref = %s, want %s", second, first)
	}
	if !os.SameFile(before, after) {
		t.Fatal("second Put() replaced the existing object")
	}
	if count := countRegularFiles(t, store.root); count != 1 {
		t.Fatalf("store contains %d regular files, want 1", count)
	}
}

func TestConcurrentIdenticalPuts(t *testing.T) {
	store := newTestStore(t)
	content := bytes.Repeat([]byte("concurrent immutable content\n"), 4096)
	const writers = 32

	refs := make(chan Ref, writers)
	errs := make(chan error, writers)
	var wait sync.WaitGroup
	for range writers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			ref, err := store.PutBytes(content)
			refs <- ref
			errs <- err
		}()
	}
	wait.Wait()
	close(refs)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	want := sha256.Sum256(content)
	wantRef := Ref(hex.EncodeToString(want[:]))
	for ref := range refs {
		if ref != wantRef {
			t.Fatalf("Put() ref = %s, want %s", ref, wantRef)
		}
	}
	if count := countRegularFiles(t, store.root); count != 1 {
		t.Fatalf("store contains %d regular files, want 1", count)
	}
	if err := store.Verify(wantRef); err != nil {
		t.Fatal(err)
	}
}

func TestMalformedRefsAreRejected(t *testing.T) {
	base := t.TempDir()
	store, err := New(filepath.Join(base, "store"))
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "outside")
	if err := os.WriteFile(outside, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}

	refs := []Ref{
		"not-hex",
		Ref(strings.Repeat("a", 63)),
		Ref(strings.Repeat("g", 64)),
		"../../etc/passwd",
		Ref("../" + strings.Repeat("a", 61)),
	}
	for _, ref := range refs {
		t.Run(fmt.Sprintf("%q", ref), func(t *testing.T) {
			if reader, err := store.Get(ref); reader != nil || !errors.Is(err, ErrInvalidRef) {
				t.Fatalf("Get() = (%v, %v), want ErrInvalidRef", reader, err)
			}
			if _, _, err := store.Stat(ref); !errors.Is(err, ErrInvalidRef) {
				t.Fatalf("Stat() error = %v, want ErrInvalidRef", err)
			}
			if path, err := store.Path(ref); path != "" || !errors.Is(err, ErrInvalidRef) {
				t.Fatalf("Path() = (%q, %v), want ErrInvalidRef", path, err)
			}
		})
	}
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "untouched" {
		t.Fatalf("outside file = %q, want untouched", got)
	}
}

func TestInterruptedPutNeverPublishesPartialObject(t *testing.T) {
	store := newTestStore(t)
	reader := &interruptingReader{
		blocked: make(chan struct{}),
		release: make(chan struct{}),
	}
	result := make(chan error, 1)
	go func() {
		_, err := store.Put(reader)
		result <- err
	}()
	<-reader.blocked

	foundTemporary := false
	err := filepath.WalkDir(store.root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(store.root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(relative, string(os.PathSeparator))
		if len(parts) == 2 && len(parts[0]) == 2 && len(parts[1]) == sha256.Size*2 {
			t.Fatalf("partial content is visible at final object path %q", relative)
		}
		if len(parts) == 2 && parts[0] == ".tmp" && strings.HasPrefix(parts[1], "put-") {
			foundTemporary = true
			return nil
		}
		t.Fatalf("write used an unexpected staging path %q", relative)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !foundTemporary {
		t.Fatal("Put() did not expose a distinguishable staging file while interrupted")
	}

	close(reader.release)
	if err := <-result; !errors.Is(err, errInterrupted) {
		t.Fatalf("Put() error = %v, want %v", err, errInterrupted)
	}
	if count := countRegularFiles(t, store.root); count != 0 {
		t.Fatalf("store contains %d files after interrupted Put(), want 0", count)
	}
}

func TestPutFileStreamsLargeFile(t *testing.T) {
	store := newTestStore(t)
	sourcePath := filepath.Join(t.TempDir(), "large-source")
	source, err := os.Create(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	hasher := sha256.New()
	chunk := bytes.Repeat([]byte("streamed file content\n"), 1024)
	const chunks = 256
	for range chunks {
		if _, err := io.MultiWriter(source, hasher).Write(chunk); err != nil {
			source.Close()
			t.Fatal(err)
		}
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}

	ref, err := store.PutFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	want := Ref(hex.EncodeToString(hasher.Sum(nil)))
	if ref != want {
		t.Fatalf("PutFile() ref = %s, want %s", ref, want)
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	size, exists, err := store.Stat(ref)
	if err != nil {
		t.Fatal(err)
	}
	if !exists || size != info.Size() {
		t.Fatalf("Stat() = (%d, %t), want (%d, true)", size, exists, info.Size())
	}
	if err := store.Verify(ref); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyDetectsCorruption(t *testing.T) {
	store := newTestStore(t)
	ref, err := store.PutBytes([]byte("original"))
	if err != nil {
		t.Fatal(err)
	}
	path, err := store.Path(ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Verify(ref); !errors.Is(err, ErrContentMismatch) {
		t.Fatalf("Verify() error = %v, want ErrContentMismatch", err)
	}
}

var errInterrupted = errors.New("interrupted read")

type interruptingReader struct {
	blocked chan struct{}
	release chan struct{}
	reads   int
}

func (r *interruptingReader) Read(buffer []byte) (int, error) {
	switch r.reads {
	case 0:
		r.reads++
		return copy(buffer, "partial content that must never be published"), nil
	case 1:
		r.reads++
		close(r.blocked)
		<-r.release
		return 0, errInterrupted
	default:
		return 0, io.EOF
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func countRegularFiles(t *testing.T, root string) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return count
}
