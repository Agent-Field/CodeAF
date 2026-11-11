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

// darwinDoor is one kernel handle: a session folder, or the folder's answers
// file while one is waiting to be drained.
type darwinDoor struct {
	dir   string
	fd    int
	token byte
	// answers is a folder's answers-file handle; folder is set on that handle.
	answers *darwinDoor
	folder  *darwinDoor
}

// Only directory entries matter here, plus one file. Watching every file would
// keep a descriptor for each transcript, presence refresh and lock in a held
// chat. Requests are published by create or rename, which change the folder's
// entries. An answer is APPENDED to answers.jsonl, which a folder watch cannot
// see once the file exists, and the doorstep counts an answer only once it is
// written ([presenceDoorstepEvent]), so that one file is watched while it is
// there.
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

// open takes an event-only handle on path and registers it with the kqueue.
func (w *darwinDoorWatcher) open(path string, flags int, fflags uint32) (*darwinDoor, error) {
	fd, err := unix.Open(path, unix.O_EVTONLY|unix.O_CLOEXEC|flags, 0)
	if err != nil {
		return nil, err
	}
	door := &darwinDoor{fd: fd}
	var change unix.Kevent_t
	unix.SetKevent(&change, fd, unix.EVFILT_VNODE, unix.EV_ADD|unix.EV_CLEAR)
	change.Fflags = fflags
	change.Udata = &door.token
	if err := changeDoorKevent(w.kq, change); err != nil {
		unix.Close(fd)
		return nil, err
	}
	w.fds[fd] = door
	return door, nil
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
	door, err := w.open(dir, unix.O_DIRECTORY, unix.NOTE_WRITE|unix.NOTE_DELETE|unix.NOTE_RENAME|unix.NOTE_REVOKE)
	if err != nil {
		return err
	}
	door.dir = dir
	w.dirs[dir] = door
	// An answers file already there is watched from now on. What it holds is
	// not reported: the session looks at its doorstep right after it subscribes.
	if _, err := w.watchAnswers(door); err != nil {
		w.report(err)
	}
	return nil
}

// watchAnswers starts watching folder's answers file if it exists and is not
// watched yet. It says whether the file already holds something, which a write
// made before the watch began would otherwise leave unreported. An empty file
// is not reported: deliverAnswer creates it and only then writes the line.
func (w *darwinDoorWatcher) watchAnswers(folder *darwinDoor) (bool, error) {
	if folder.answers != nil {
		return false, nil
	}
	door, err := w.open(filepath.Join(folder.dir, answersName), 0, unix.NOTE_WRITE|unix.NOTE_EXTEND|unix.NOTE_DELETE|unix.NOTE_RENAME|unix.NOTE_REVOKE)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	door.dir, door.folder, folder.answers = folder.dir, folder, door
	var st unix.Stat_t
	if err := unix.Fstat(door.fd, &st); err != nil {
		return false, err
	}
	return st.Size > 0, nil
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

// remove closes one handle; a folder's answers handle goes with it. Tokens are
// kept until the next batch is read, so an old event cannot name a different
// handle that reused its descriptor or its token's address.
func (w *darwinDoorWatcher) remove(door *darwinDoor) error {
	var err error
	if door.answers != nil {
		err = w.remove(door.answers)
	}
	if door.folder != nil {
		if door.folder.answers == door {
			door.folder.answers = nil
		}
	} else {
		delete(w.dirs, door.dir)
	}
	delete(w.fds, door.fd)
	w.retired = append(w.retired, door)
	return errors.Join(err, unix.Close(door.fd))
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
	gone := uint32(unix.NOTE_DELETE | unix.NOTE_RENAME | unix.NOTE_REVOKE)
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
			if door.folder != nil {
				// The answers file: a write is an answer landing. Drained
				// (renamed away) or deleted, its handle goes; a new file shows
				// up as a folder change.
				if event.Fflags&(unix.NOTE_WRITE|unix.NOTE_EXTEND) != 0 {
					pending = append(pending, doorEvent{Name: filepath.Join(door.dir, answersName), Op: fsnotify.Write})
				}
				if event.Fflags&gone != 0 {
					w.report(w.remove(door))
				}
				continue
			}
			if event.Fflags&gone != 0 {
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
				answers := false
				for _, name := range names {
					if presenceRequest(name.Name()) {
						pending = append(pending, doorEvent{Name: filepath.Join(door.dir, name.Name()), Op: fsnotify.Create})
					}
					answers = answers || name.Name() == answersName
				}
				if answers {
					wrote, err := w.watchAnswers(door)
					w.report(err)
					if wrote {
						pending = append(pending, doorEvent{Name: filepath.Join(door.dir, answersName), Op: fsnotify.Write})
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
