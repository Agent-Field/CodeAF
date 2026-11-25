//go:build linux

package sessionhost

import (
	"context"
	"crypto/sha256"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const stopGrace = 3 * time.Second

type processes interface {
	snapshot() (processTable, error)
	KillAll(uid int, grace time.Duration) error
}

type supervisorDeps struct {
	launcher  Launcher
	processes processes
	now       func() time.Time
	chown     func(string, int, int) error
	isOwner   func(os.FileInfo, int) bool
	log       *slog.Logger
}

type supervisor struct {
	cfg    Config
	report Report
	secret [sha256.Size]byte
	supervisorDeps

	// The lock covers the registry, credentials and wire reservations.
	// Reservations keep a newly forked helper from racing a stop or a slot
	// check before it has switched to its session identity.
	mu       sync.Mutex
	registry *registry
	creds    map[string]credentials
	wires    map[string]map[*wireAttach]struct{}
	closing  bool
	wireWG   sync.WaitGroup
}

func ListenAndServe(ctx context.Context, cfg Config, report Report) error {
	launcher, err := newLauncher(cfg)
	if err != nil {
		return errors.New("Could not find the chat process executable.")
	}
	s, err := newSupervisor(cfg, report, supervisorDeps{
		launcher: launcher, processes: linuxProcesses{cfg}, now: time.Now,
		chown: os.Chown, isOwner: func(info os.FileInfo, uid int) bool { return ownerUID(info) == uid },
		log: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	})
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return errors.New("Could not listen for hosted chats.")
	}
	srv := &http.Server{
		Handler: s, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, IdleTimeout: time.Minute,
	}
	shutdown := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(shutdown)
		// The detached engines belong to the container and die with it.
		// Only connections and their short-lived bridge children drain here.
		s.closeWires()
		drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if srv.Shutdown(drain) != nil {
			_ = srv.Close()
		}
		done := make(chan struct{})
		go func() {
			s.wireWG.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-drain.Done():
		}
	})
	defer stop()
	err = srv.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		<-shutdown
		return nil
	}
	s.closeWires()
	return errors.New("The hosted chat server stopped listening.")
}

func newSupervisor(cfg Config, report Report, deps supervisorDeps) (*supervisor, error) {
	if !validUIDRange(cfg.UIDBase, cfg.UIDCount) {
		return nil, errors.New("The chat process range is invalid.")
	}
	data, err := filepath.Abs(cfg.Data)
	if err != nil {
		return nil, errLayout
	}
	cfg.Data = data
	if err := prepareData(data); err != nil {
		return nil, errLayout
	}
	if err := ancestorDir(filepath.Join(data, "people")); err != nil {
		return nil, errLayout
	}
	r, err := loadRegistry(cfg, deps.now())
	if err != nil {
		return nil, errors.New("The chat registry could not be opened.")
	}
	s := &supervisor{
		cfg: cfg, report: report, secret: sha256.Sum256([]byte(cfg.Secret)), supervisorDeps: deps,
		registry: r, creds: make(map[string]credentials), wires: make(map[string]map[*wireAttach]struct{}),
	}
	if err := s.reconcile(); err != nil {
		return nil, err
	}
	return s, nil
}

func prepareData(data string) error {
	// None of the configured volume components may redirect the supervisor
	// through a symlink. Ancestors can be mounts owned by the operator.
	path := string(os.PathSeparator)
	for component := range strings.SplitSeq(strings.TrimPrefix(data, path), path) {
		path = filepath.Join(path, component)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			if err := ancestorDir(path); err != nil {
				return err
			}
			info, err = os.Lstat(path)
		}
		if err != nil || !info.IsDir() {
			return errLayout
		}
		if path == data && (ownerUID(info) != os.Geteuid() || info.Mode().Perm()&0o022 != 0) {
			return errLayout
		}
		if path == data && info.Mode().Perm()&0o055 != 0o055 {
			// Every chat's user must be able to open the data folder for reading
			// (ancestorMode says why). The first stage's 0711, and a stricter mode
			// a fresh volume came with, are opened up and nothing else is changed.
			if err := os.Chmod(path, info.Mode().Perm()|0o055); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *supervisor) reconcile() error {
	known := make(map[string]bool, len(s.registry.sessions))
	changed := false
	for id, row := range s.registry.sessions {
		if err := s.processes.KillAll(row.UID, stopGrace); err != nil {
			return errors.New("An earlier chat's processes could not be stopped.")
		}
		folders, err := s.folders(row)
		known[folders.dir] = true
		missing := errors.Is(err, os.ErrNotExist)
		if err != nil && !missing {
			return errLayout
		}
		if row.Missing != missing {
			row.Missing = missing
			s.registry.sessions[id] = row
			changed = true
		}
	}
	if err := s.quarantineFolders(known); err != nil {
		return errors.New("Unused chat folders could not be set aside.")
	}
	if changed && s.registry.store() != nil {
		return errors.New("The chat registry could not be saved.")
	}
	return nil
}

func (s *supervisor) folders(row sessionRow) (layout, error) {
	folders, err := layoutFor(s.cfg.Data, row.Person, row.ID)
	if err != nil {
		return folders, err
	}
	info, err := folders.stat()
	if err == nil && !s.isOwner(info, row.UID) {
		err = errLayout
	}
	return folders, err
}

func (s *supervisor) quarantineFolders(known map[string]bool) error {
	people := filepath.Join(s.cfg.Data, "people")
	parents, err := os.ReadDir(people)
	if err != nil {
		return err
	}
	for _, entry := range parents {
		parent := filepath.Join(people, entry.Name())
		if err := openAncestor(parent); err != nil {
			return err
		}
		folders, err := os.ReadDir(parent)
		if err != nil {
			return err
		}
		for _, entry := range folders {
			path := filepath.Join(parent, entry.Name())
			if known[path] {
				continue
			}
			// Unregistered folders are retained for the operator in this
			// release. Rename the entry itself without opening its contents.
			destination := filepath.Join(s.cfg.Data, "quarantine")
			if err := supervisorDir(destination, 0o700); err != nil {
				return err
			}
			if err := quarantineFolder(path, destination, s.now()); err != nil {
				return err
			}
		}
	}
	return nil
}

func quarantineFolder(path, destination string, now time.Time) error {
	// An earlier recovery can have quarantined the same ID in this second.
	// Keep both entries without replacing either one.
	for second := now.Unix(); ; second++ {
		name := filepath.Base(path) + "-" + strconv.FormatInt(second, 10)
		err := unix.Renameat2(unix.AT_FDCWD, path, unix.AT_FDCWD, filepath.Join(destination, name), unix.RENAME_NOREPLACE)
		if !errors.Is(err, unix.EEXIST) {
			return err
		}
	}
}

func (s *supervisor) buildLayout(row sessionRow) (bool, error) {
	folders, err := layoutFor(s.cfg.Data, row.Person, row.ID)
	if err != nil {
		return false, err
	}
	made, err := folders.makePrivateDir(row.UID, s.chown)
	if err == nil {
		err = s.launcher.reap(row.UID, folders, reapInit)
	}
	return made, err
}

func (s *supervisor) rollbackLayout(row sessionRow) {
	folders, err := layoutFor(s.cfg.Data, row.Person, row.ID)
	if err == nil {
		err = unix.Rmdir(folders.dir)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) && s.removeLayout(row) != nil {
		s.retainDoomed(row)
	}
}

func (s *supervisor) retainDoomed(row sessionRow) {
	row.Doomed = true
	if s.registry.put(row) != nil {
		// Keep the identity reserved in memory even if the volume cannot
		// currently save it. Startup moves an unregistered folder aside.
		s.registry.sessions[row.ID] = row
		s.log.Error("could not save the cleanup record", "id", row.ID)
	}
}

func (s *supervisor) removeLayout(row sessionRow) error {
	folders, err := s.folders(row)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.launcher.reap(row.UID, folders, reapAll); err != nil {
		return err
	}
	return unix.Rmdir(folders.dir)
}

func (s *supervisor) closeWires() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closing = true
	for _, group := range s.wires {
		for wire := range group {
			wire.cancel()
		}
	}
}

func (s *supervisor) stopWires(id string) error {
	group := s.wires[id]
	for wire := range group {
		wire.cancel()
		if wire.child != nil {
			// A just-forked helper may still be root, invisible to a scan
			// of the session identity. This handle names our own child.
			_ = wire.child.signal(os.Kill)
		}
	}
	timer := time.NewTimer(stopGrace)
	defer timer.Stop()
	for wire := range group {
		if wire.child == nil {
			continue
		}
		select {
		case <-wire.child.done:
		case <-timer.C:
			return errors.New(refusalText[stopFailed])
		}
	}
	delete(s.wires, id)
	return nil
}
