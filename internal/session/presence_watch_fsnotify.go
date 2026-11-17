//go:build !darwin

package session

import "github.com/fsnotify/fsnotify"

type fsnotifyDoorWatcher struct {
	*fsnotify.Watcher
}

func newDoorWatcher() (doorWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &fsnotifyDoorWatcher{watcher}, nil
}

func (w *fsnotifyDoorWatcher) Events() <-chan doorEvent { return w.Watcher.Events }
func (w *fsnotifyDoorWatcher) Errors() <-chan error     { return w.Watcher.Errors }
