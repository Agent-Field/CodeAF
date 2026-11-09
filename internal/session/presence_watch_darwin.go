//go:build darwin

package session

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/sys/unix"
)

type darwinDoor struct {
	dir   string
	fd    int
	token byte
}

// Only directory entries matter here. Watching their files would keep a
// descriptor for every transcript, presence refresh and lock in a held chat.
type darwinDoorWatcher struct {
	mu      sync.Mutex
	kq      int
	dirs    map[string]*darwinDoor
	fds     map[int]*darwinDoor
	retired []*darwinDoor
	events  chan doorEvent
	errors  chan error
	stop    chan struct{}
	done    chan struct{}
	err     error
}

func newDoorWatcher() (doorWatcher, error) {
	kq, err := unix.Kqueue()
	if err != nil {
		return nil, err
	}
	unix.CloseOnExec(kq)
	var wake unix.Kevent_t
	unix.SetKevent(&wake, 0, unix.EVFILT_USER, unix.EV_ADD|unix.EV_CLEAR)
	if err := changeDoorKevent(kq, wake); err != nil {
		unix.Close(kq)
		return nil, err
	}
	w := &darwinDoorWatcher{
		kq: kq, dirs: map[string]*darwinDoor{}, fds: map[int]*darwinDoor{},
		events: make(chan doorEvent), errors: make(chan error, 1),
		stop: make(chan struct{}), done: make(chan struct{}),
	}
	go w.read()
	return w, nil
}

func changeDoorKevent(kq int, change unix.Kevent_t) error {
	for {
		_, err := unix.Kevent(kq, []unix.Kevent_t{change}, nil, nil)
		if err != unix.EINTR {
			return err
		}
	}
}

func (w *darwinDoorWatcher) Add(dir string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	select {
	case <-w.stop:
		return os.ErrClosed
	default:
	}
	dir = filepath.Clean(dir)
	if w.dirs[dir] != nil {
		return nil
	}
	fd, err := unix.Open(dir, unix.O_EVTONLY|unix.O_CLOEXEC|unix.O_DIRECTORY, 0)
	if err != nil {
		return err
	}
	door := &darwinDoor{dir: dir, fd: fd}
	var change unix.Kevent_t
	unix.SetKevent(&change, fd, unix.EVFILT_VNODE, unix.EV_ADD|unix.EV_CLEAR)
	change.Fflags = unix.NOTE_WRITE | unix.NOTE_DELETE | unix.NOTE_RENAME | unix.NOTE_REVOKE
	change.Udata = &door.token
	if err := changeDoorKevent(w.kq, change); err != nil {
		unix.Close(fd)
		return err
	}
	w.dirs[dir], w.fds[fd] = door, door
	return nil
}

func (w *darwinDoorWatcher) Remove(dir string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if door := w.dirs[filepath.Clean(dir)]; door != nil {
		err := w.remove(door)
		w.report(w.wake())
		return err
	}
	return nil
}

// Keep tokens until the next batch is read, so an old event cannot name a
// different folder that reused its descriptor or its token's address.
func (w *darwinDoorWatcher) remove(door *darwinDoor) error {
	delete(w.dirs, door.dir)
	delete(w.fds, door.fd)
	w.retired = append(w.retired, door)
	return unix.Close(door.fd)
}

func (w *darwinDoorWatcher) wake() error {
	var wake unix.Kevent_t
	unix.SetKevent(&wake, 0, unix.EVFILT_USER, 0)
	wake.Fflags = unix.NOTE_TRIGGER
	return changeDoorKevent(w.kq, wake)
}

func (w *darwinDoorWatcher) Close() error {
	w.mu.Lock()
	select {
	case <-w.stop:
	default:
		close(w.stop)
		w.err = errors.Join(w.err, w.wake())
	}
	w.mu.Unlock()
	<-w.done
	return w.err
}

func (w *darwinDoorWatcher) Events() <-chan doorEvent { return w.events }
func (w *darwinDoorWatcher) Errors() <-chan error     { return w.errors }

func (w *darwinDoorWatcher) report(err error) {
	if err != nil {
		select {
		case w.errors <- err:
		default:
		}
	}
}

func (w *darwinDoorWatcher) read() {
	defer func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		select {
		case <-w.stop:
		default:
			close(w.stop)
		}
		for _, door := range w.dirs {
			w.err = errors.Join(w.err, w.remove(door))
		}
		w.retired = nil
		w.err = errors.Join(w.err, unix.Close(w.kq))
		close(w.events)
		close(w.errors)
		close(w.done)
	}()
	events := make([]unix.Kevent_t, 32)
	for {
		select {
		case <-w.stop:
			return
		default:
		}
		// No timeout: an idle engine must not wake once a second. Close and
		// Remove trigger the user event, which ends this wait.
		n, err := unix.Kevent(w.kq, nil, events, nil)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			w.report(err)
			return
		}
		w.mu.Lock()
		var pending []doorEvent
		for _, event := range events[:n] {
			if event.Filter != unix.EVFILT_VNODE {
				continue
			}
			door := w.fds[int(event.Ident)]
			if door == nil || event.Udata != &door.token {
				continue
			}
			if event.Flags&unix.EV_ERROR != 0 {
				w.report(unix.Errno(event.Data))
				continue
			}
			if event.Fflags&(unix.NOTE_DELETE|unix.NOTE_RENAME|unix.NOTE_REVOKE) != 0 {
				w.report(w.remove(door))
				pending = append(pending, doorEvent{Name: door.dir, Op: fsnotify.Remove})
				continue
			}
			if event.Fflags&unix.NOTE_WRITE != 0 {
				names, err := os.ReadDir(door.dir)
				if err != nil {
					w.report(err)
					continue
				}
				for _, name := range names {
					if presenceRequest(name.Name()) {
						pending = append(pending, doorEvent{Name: filepath.Join(door.dir, name.Name()), Op: fsnotify.Create})
					}
				}
			}
		}
		w.retired = nil
		w.mu.Unlock()
		for _, event := range pending {
			select {
			case w.events <- event:
			case <-w.stop:
				return
			}
		}
	}
}
