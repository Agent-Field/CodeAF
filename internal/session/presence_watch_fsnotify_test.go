//go:build !darwin

package session

import (
	"path/filepath"
	"testing"
)

func TestPresenceDoorC7KeepsFsnotifyDelivery(t *testing.T) {
	for _, name := range []string{takeoverName, conversationDeleteRequest, ".task-delete-request-123"} {
		t.Run(name, func(t *testing.T) {
			var watch presenceDoorWatch
			dir := t.TempDir()
			requests, _, _, cancel := watch.subscribe(dir)
			t.Cleanup(cancel)
			watch.mu.Lock()
			watcher, ok := watch.run.watcher.(*fsnotifyDoorWatcher)
			watch.mu.Unlock()
			if !ok {
				t.Fatal("non-darwin subscription did not use fsnotify")
			}
			if watcher.Events() != watcher.Watcher.Events || watcher.Errors() != watcher.Watcher.Errors {
				t.Fatal("fsnotify channels were replaced by another delivery path")
			}
			if err := writePresenceRequest(filepath.Join(dir, name), []byte("request")); err != nil {
				t.Fatal(err)
			}
			waitDoorRequest(t, requests)
		})
	}
}
