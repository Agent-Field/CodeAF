//go:build !windows

package storage

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestStoreReadWriteUpdateListRemove(t *testing.T) {
	root := filepath.Join(t.TempDir(), "storage")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	store := New(root)
	type record struct {
		Z float64 `json:"z"`
		A string  `json:"a"`
	}
	if err := store.Write([]string{"session", "s1"}, record{Z: 1, A: "before"}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(root, "session", "s1.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"z\": 1,\n  \"a\": \"before\"\n}"
	if string(raw) != want {
		t.Fatalf("write bytes:\n got %q\nwant %q", raw, want)
	}

	updated, err := store.Update([]string{"session", "s1"}, func(value any) {
		object := value.(map[string]any)
		object["a"] = "after"
		object["new"] = true
	})
	if err != nil {
		t.Fatal(err)
	}
	wantUpdated := map[string]any{"z": float64(1), "a": "after", "new": true}
	if !reflect.DeepEqual(updated, wantUpdated) {
		t.Fatalf("updated value: %#v", updated)
	}
	if got, err := ReadAs[record](store, []string{"session", "s1"}); err != nil || got.A != "after" {
		t.Fatalf("read after update: %+v, %v", got, err)
	}

	keys, err := store.List([]string{"session"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(keys, [][]string{{"session", "s1"}}) {
		t.Fatalf("list: %#v", keys)
	}

	if err := store.Remove([]string{"session", "s1"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove([]string{"session", "s1"}); err != nil {
		t.Fatal(err)
	}
	_, err = store.Read([]string{"session", "s1"})
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected NotFoundError, got %v", err)
	}
	if notFound.Message != "Resource not found: "+filepath.Join(root, "session", "s1.json") {
		t.Fatalf("message: %q", notFound.Message)
	}
}

func TestStoreUpdateSerializesConcurrentMutations(t *testing.T) {
	root := filepath.Join(t.TempDir(), "storage")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	store := New(root)
	type counter struct {
		N int `json:"n"`
	}
	if err := store.Write([]string{"counter"}, counter{}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := UpdateAs(store, []string{"counter"}, func(value *counter) {
				value.N++
			}); err != nil {
				t.Errorf("update: %v", err)
			}
		}()
	}
	wg.Wait()
	got, err := ReadAs[counter](store, []string{"counter"})
	if err != nil {
		t.Fatal(err)
	}
	if got.N != 30 {
		t.Fatalf("counter = %d, want 30", got.N)
	}
}

func TestStoreUpdatesDifferentResourcesConcurrently(t *testing.T) {
	// Unrelated resources do not queue behind a store-wide exclusive lock.
	root := filepath.Join(t.TempDir(), "storage")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	store := New(root)
	for _, key := range []string{"first", "second"} {
		if err := store.Write([]string{key}, map[string]any{"value": 0}); err != nil {
			t.Fatal(err)
		}
	}
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		_, err := store.Update([]string{"first"}, func(any) {
			close(firstEntered)
			<-releaseFirst
		})
		firstDone <- err
	}()
	<-firstEntered
	secondEntered := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		_, err := store.Update([]string{"second"}, func(any) { close(secondEntered) })
		secondDone <- err
	}()
	select {
	case <-secondEntered:
	case <-time.After(2 * time.Second):
		close(releaseFirst)
		t.Fatal("different-resource update blocked behind the first resource")
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
}

func TestStoreUpdateCallbackOwnsResourceLock(t *testing.T) {
	// Update's callback runs while holding the resource lock, so same-key
	// callers serialize around the non-reentrant callback.
	root := filepath.Join(t.TempDir(), "storage")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	store := New(root)
	if err := store.Write([]string{"shared"}, map[string]any{"value": 0}); err != nil {
		t.Fatal(err)
	}
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		_, err := store.Update([]string{"shared"}, func(any) {
			close(firstEntered)
			<-releaseFirst
		})
		firstDone <- err
	}()
	<-firstEntered
	secondEntered := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		_, err := store.Update([]string{"shared"}, func(any) { close(secondEntered) })
		secondDone <- err
	}()
	select {
	case <-secondEntered:
		close(releaseFirst)
		t.Fatal("same-resource callback ran without owning the resource lock")
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-secondEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("same-resource waiter did not resume after callback returned")
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
}

func TestStoreCrossProcessLockAndAtomicReplacement(t *testing.T) {
	// Independent store instances cannot lose a read-modify-write, and each
	// durable rewrite is an atomic inode replacement.
	root := filepath.Join(t.TempDir(), "storage")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	type counter struct {
		N int `json:"n"`
	}
	first, second := New(root), New(root)
	if err := first.Write([]string{"counter"}, counter{}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "counter.json")
	commands := make([]*exec.Cmd, 2)
	for index := range commands {
		commands[index] = exec.Command(os.Args[0], "-test.run=^TestStoreProcessUpdateHelper$", "-test.count=1")
		commands[index].Env = append(os.Environ(), "SENIOR_DEV_STORAGE_HELPER_ROOT="+root)
		if err := commands[index].Start(); err != nil {
			t.Fatal(err)
		}
	}
	for _, command := range commands {
		if err := command.Wait(); err != nil {
			t.Fatalf("storage helper: %v", err)
		}
	}
	got, err := ReadAs[counter](second, []string{"counter"})
	if err != nil || got.N != 40 {
		t.Fatalf("cross-store counter = %+v, %v; want 40", got, err)
	}
	before, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Write([]string{"counter"}, got); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Fatal("durable rewrite mutated the JSON inode in place")
	}
	matches, err := filepath.Glob(filepath.Join(root, ".tmp-*.json"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("atomic rewrite leftovers = %v, %v", matches, err)
	}
}

func TestStoreProcessUpdateHelper(t *testing.T) {
	root := os.Getenv("SENIOR_DEV_STORAGE_HELPER_ROOT")
	if root == "" {
		t.Skip("subprocess helper")
	}
	type counter struct {
		N int `json:"n"`
	}
	store := New(root)
	for index := 0; index < 20; index++ {
		if _, err := UpdateAs(store, []string{"counter"}, func(value *counter) { value.N++ }); err != nil {
			t.Fatal(err)
		}
	}
}
