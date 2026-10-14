package automation

// presence.go answers the one question the clock may never get wrong: is any
// codeaf window open on this machine right now?
//
// A WINDOW IS A LOCK, NOT A HEARTBEAT. Each open window holds an operating-
// system lock on a file of its own for as long as it is open; the kernel drops
// the lock the moment its process ends, however it ends. So "is it still open?"
// is answered by trying to take the lock — a file whose lock can be taken is a
// window that is gone — and there is no clock to drift, no timeout to tune and
// no stale entry that looks alive. The same shape already keeps one clock at a
// time ([Clock]).

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// windowsDir is the folder, inside the store's root, that holds one lock file
// per open window.
const windowsDir = "windows"

// Presence is the set of windows open on this machine for one codeaf home.
type Presence struct {
	dir string
}

// NewPresence is the presence folder under root, the store's own folder.
func NewPresence(root string) *Presence {
	return &Presence{dir: filepath.Join(root, windowsDir)}
}

// Window is what one open window says about itself. Liveness is the lock,
// never these fields.
type Window struct {
	PID     int       `json:"pid"`
	Label   string    `json:"label,omitempty"`
	Started time.Time `json:"started"`
	// Env is a salted fingerprint of the environment the window was started
	// in — the provider keys and codeaf's own variables, never their values —
	// so the clock can tell that the newest window would hand a run a
	// different key than the one it was started with ([Clock.Env]). Empty says
	// nothing, and a window that says nothing is never followed.
	Env string `json:"env,omitempty"`
}

// Hold registers one open window until release is called or the process ends.
// label says what it is ("window", "attached from laptop.local") for anybody
// reading the folder.
func (p *Presence) Hold(label string) (release func(), err error) {
	return p.HoldAs(Window{Label: label})
}

// HoldAs is [Presence.Hold] with everything the window says about itself.
func (p *Presence) HoldAs(w Window) (release func(), err error) {
	if p == nil {
		return func() {}, errors.New("automations: no presence folder")
	}
	if err := os.MkdirAll(p.dir, 0o700); err != nil {
		return func() {}, fmt.Errorf("automations: %w", err)
	}
	var raw [6]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return func() {}, fmt.Errorf("automations: %w", err)
	}
	// THE FILE IS LOCKED AND WRITTEN UNDER A NAME NOBODY COUNTS, and only then
	// renamed into place. Created straight under its `.lock` name, it stood
	// unlocked for an instant, and a counter that looked in that instant took
	// the lock, read a window that was gone, and removed it — leaving an open
	// window invisible, and no clock running for it, for as long as it stayed
	// open. A lock is on the file, not the name, so it survives the rename.
	name := fmt.Sprintf("%d-%s", os.Getpid(), hex.EncodeToString(raw[:]))
	staging := filepath.Join(p.dir, "."+name+".opening")
	path := filepath.Join(p.dir, name+".lock")
	file, err := os.OpenFile(staging, os.O_CREATE|os.O_RDWR|os.O_EXCL, 0o600)
	if err != nil {
		return func() {}, fmt.Errorf("automations: %w", err)
	}
	if err := filelock.Lock(file, true, true); err != nil {
		_ = file.Close()
		_ = os.Remove(staging)
		return func() {}, fmt.Errorf("automations: hold a window: %w", err)
	}
	body, _ := json.Marshal(Window{PID: os.Getpid(), Label: strings.TrimSpace(w.Label), Started: time.Now(), Env: strings.TrimSpace(w.Env)})
	_, _ = file.Write(body)
	if err := os.Rename(staging, path); err != nil {
		_ = os.Remove(staging)
		_ = filelock.Unlock(file)
		_ = file.Close()
		return func() {}, fmt.Errorf("automations: hold a window: %w", err)
	}
	done := false
	return func() {
		if done {
			return
		}
		done = true
		// REMOVED BEFORE IT IS UNLOCKED, so a counter never takes the lock of a
		// file that is still going to be removed by its owner.
		_ = os.Remove(path)
		_ = filelock.Unlock(file)
		_ = file.Close()
	}, nil
}

// Count is how many windows are open now. A file whose lock can be taken
// belongs to a window that is gone, and it is removed as it is found.
func (p *Presence) Count() (int, error) {
	if p == nil {
		return 0, nil
	}
	entries, err := os.ReadDir(p.dir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	open := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".lock") {
			continue
		}
		if p.alive(filepath.Join(p.dir, entry.Name())) {
			open++
		}
	}
	return open, nil
}

// alive reports whether a window file's owner still holds it, and removes the
// file when it does not.
func (p *Presence) alive(path string) bool {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		// Gone between the listing and the open: its owner released it.
		return false
	}
	defer file.Close()
	err = filelock.Lock(file, true, true)
	if err == nil {
		_ = os.Remove(path)
		_ = filelock.Unlock(file)
		return false
	}
	return filelock.IsBusy(err)
}

// Newest is the most recently opened window that said what environment it was
// started in, and false when no open window did. The clock follows it
// ([Clock.Env]), and a window starts a clock only when it is that window or
// shares its environment, so two windows from two shells never take turns.
func (p *Presence) Newest() (Window, bool) {
	if p == nil {
		return Window{}, false
	}
	entries, err := os.ReadDir(p.dir)
	if err != nil {
		return Window{}, false
	}
	var newest Window
	found := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".lock") {
			continue
		}
		path := filepath.Join(p.dir, entry.Name())
		if !p.alive(path) {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var w Window
		if json.Unmarshal(body, &w) != nil || strings.TrimSpace(w.Env) == "" {
			continue
		}
		if !found || w.Started.After(newest.Started) {
			newest, found = w, true
		}
	}
	return newest, found
}
