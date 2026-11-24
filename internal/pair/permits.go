package pair

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/fsreplace"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// Permit records whether one paired computer may watch and type into chats
// hosted on this computer, who set that choice, and when.
type Permit struct {
	Watch, Drive bool
	By           string
	At           time.Time
}

// PermitClass tells how a device's effective permit was decided.
type PermitClass int

const (
	// ClassExplicit means a row in permits.json names this device directly.
	ClassExplicit PermitClass = iota
	// ClassLegacy means the device was already paired before remote control
	// took its first roster snapshot, or the permit file cannot be trusted yet.
	ClassLegacy
	// ClassNew means the device joined after the host's legacy snapshot was recorded.
	ClassNew
)

// permitPollInterval is the default polling period for PermitBook.Watch so
// tests can shrink it without sleeping on a wall-clock second.
var permitPollInterval = time.Second

// PermitBook is the host-local record of per-device watch and drive permissions.
//
// THIS FILE LIVES ONLY ON THE HOST AND NEVER TRAVELS THROUGH SYNC OR THE
// DIRECTORY. A permission about what another computer may run here is a local
// decision of the machine that runs the tools, separate from the whole-machine
// book that codeaf serve reads.
type PermitBook struct {
	path string

	mu sync.Mutex
}

// Permits is the permit book in the default state directory.
func Permits() *PermitBook {
	return PermitsAt(filepath.Join(Dir(), "permits.json"))
}

// PermitsAt opens the permit book at an explicit file path.
func PermitsAt(path string) *PermitBook {
	return &PermitBook{path: path}
}

type permitDefaults struct {
	Watch bool `json:"watch"`
	Drive bool `json:"drive"`
}

type permitRow struct {
	Watch bool      `json:"watch"`
	Drive bool      `json:"drive"`
	By    string    `json:"by,omitempty"`
	At    time.Time `json:"at,omitempty"`
}

type permitFile struct {
	V        int                  `json:"v"`
	Identity string               `json:"identity"`
	Since    string               `json:"since,omitempty"`
	Legacy   []string             `json:"legacy"`
	New      *permitDefaults      `json:"new,omitempty"`
	Asked    string               `json:"asked,omitempty"`
	Devices  map[string]permitRow `json:"devices,omitempty"`
}

func legacyPermit() (Permit, PermitClass) {
	return Permit{Watch: true, Drive: false}, ClassLegacy
}

func (f permitFile) effectiveFor(identity, device string) (Permit, PermitClass) {
	if identity == "" || device == "" || f.Identity == "" || f.Identity != identity {
		return legacyPermit()
	}
	if row, ok := f.Devices[device]; ok {
		return Permit{Watch: row.Watch, Drive: row.Drive, By: row.By, At: row.At}, ClassExplicit
	}
	if f.Since == "" {
		return legacyPermit()
	}
	if slices.Contains(f.Legacy, device) {
		return legacyPermit()
	}
	watch, drive := true, true
	if f.New != nil {
		watch, drive = f.New.Watch, f.New.Drive
	}
	return Permit{Watch: watch, Drive: drive}, ClassNew
}

// For returns the effective watch and drive permission for device under identity.
// Any missing, unreadable, foreign, or unsnapshotted file fails safe to watch-only
// legacy behavior so an older pairing never gains drive access by accident.
func (b *PermitBook) For(identity, device string) (Permit, PermitClass) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var f permitFile
	if ok, err := b.read(&f); err != nil || !ok {
		return legacyPermit()
	}
	return f.effectiveFor(identity, device)
}

// Set records an explicit permit row for device under identity, changing only
// the fields whose pointer is non-nil and starting from the device's current
// effective permit. Writing for a different identity rebinds the file because
// an identity rotation or replacement changes every device identifier.
func (b *PermitBook) Set(identity, device string, watch, drive *bool, by string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	var f permitFile
	_, _ = b.read(&f)
	if f.Identity != identity {
		f = freshPermitFile(identity)
	}
	cur, _ := f.effectiveFor(identity, device)
	if watch != nil {
		cur.Watch = *watch
	}
	if drive != nil {
		cur.Drive = *drive
	}
	if f.Devices == nil {
		f.Devices = make(map[string]permitRow)
	}
	f.Devices[device] = permitRow{
		Watch: cur.Watch,
		Drive: cur.Drive,
		By:    by,
		At:    time.Now().UTC(),
	}
	return b.write(f)
}

// SnapshotLegacy records the one-time list of devices that were already paired
// when this computer first enabled permits for identity. Once written for an
// identity, a later call is a no-op so newly paired devices stay ClassNew.
func (b *PermitBook) SnapshotLegacy(identity string, roster []string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	var f permitFile
	_, _ = b.read(&f)
	if f.Identity != identity {
		f = freshPermitFile(identity)
	}
	if f.Since != "" {
		return nil
	}
	f.Since = time.Now().UTC().Format(time.RFC3339Nano)
	f.Legacy = make([]string, 0, len(roster))
	for _, dev := range roster {
		if dev != "" && !slices.Contains(f.Legacy, dev) {
			f.Legacy = append(f.Legacy, dev)
		}
	}
	return b.write(f)
}

// NeedsAsk reports whether this host should show the one-time prompt asking
// whether legacy paired computers may type into chats here.
func (b *PermitBook) NeedsAsk(identity string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	var f permitFile
	if ok, err := b.read(&f); err != nil || !ok {
		return false
	}
	if identity == "" || f.Identity != identity || f.Since == "" {
		return false
	}
	return f.Asked == "" && len(f.Legacy) > 0
}

// Asked records that the one-time legacy prompt was answered, and when allow is
// true grants drive permission to each listed device that belongs to the legacy snapshot.
func (b *PermitBook) Asked(identity string, allow bool, devices []string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	var f permitFile
	_, _ = b.read(&f)
	if f.Identity != identity {
		f = freshPermitFile(identity)
	}
	now := time.Now().UTC()
	f.Asked = now.Format(time.RFC3339Nano)
	if allow {
		if f.Devices == nil {
			f.Devices = make(map[string]permitRow)
		}
		for _, dev := range devices {
			if slices.Contains(f.Legacy, dev) {
				f.Devices[dev] = permitRow{
					Watch: true,
					Drive: true,
					By:    "asked",
					At:    now,
				}
			}
		}
	}
	return b.write(f)
}

// Forget removes any explicit row for device so it returns to its snapshot class.
func (b *PermitBook) Forget(device string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	var f permitFile
	ok, err := b.read(&f)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if _, exists := f.Devices[device]; !exists {
		return nil
	}
	delete(f.Devices, device)
	return b.write(f)
}

type permitFileStamp struct {
	exists  bool
	size    int64
	modTime time.Time
}

func statPermitFile(path string) permitFileStamp {
	info, err := os.Stat(path)
	if err != nil {
		return permitFileStamp{}
	}
	return permitFileStamp{
		exists:  true,
		size:    info.Size(),
		modTime: info.ModTime(),
	}
}

func (s permitFileStamp) same(other permitFileStamp) bool {
	return s.exists == other.exists && s.size == other.size && s.modTime.Equal(other.modTime)
}

// Watch polls the permit file's modification time and size until ctx is done,
// calling changed whenever either differs from the previous observation.
func (b *PermitBook) Watch(ctx context.Context, interval time.Duration, changed func()) {
	if interval <= 0 {
		interval = permitPollInterval
	}
	last := statPermitFile(b.path)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cur := statPermitFile(b.path)
			if !cur.same(last) {
				last = cur
				if changed != nil {
					changed()
				}
			}
		}
	}
}

func freshPermitFile(identity string) permitFile {
	return permitFile{
		V:        1,
		Identity: identity,
		Legacy:   []string{},
		New:      &permitDefaults{Watch: true, Drive: true},
		Devices:  make(map[string]permitRow),
	}
}

func (b *PermitBook) read(into *permitFile) (bool, error) {
	var raw []byte
	err := fsreplace.Retry(func() error {
		var err error
		raw, err = os.ReadFile(b.path)
		return err
	})
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return false, fmt.Errorf("%s is not readable any more: %w", b.path, err)
	}
	return true, nil
}

func (b *PermitBook) write(what permitFile) error {
	if what.V == 0 {
		what.V = 1
	}
	if what.Legacy == nil {
		what.Legacy = []string{}
	}
	if what.New == nil {
		what.New = &permitDefaults{Watch: true, Drive: true}
	}
	dir := filepath.Dir(b.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0o700)
	raw, err := json.MarshalIndent(what, "", "  ")
	if err != nil {
		return err
	}
	// The permit book controls which remote devices may drive tools on this
	// computer, so it is owner-only and replaced atomically through a temp file.
	temporary := b.path + ".new"
	if err := os.WriteFile(temporary, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	_ = os.Chmod(temporary, 0o600)
	return fsreplace.Rename(temporary, b.path)
}

// stampJoin records an empty legacy snapshot after a computer joins an
// identity so every device it later meets defaults to ClassNew.
func stampJoin(identity string) {
	if identity == "" {
		return
	}
	_ = Permits().SnapshotLegacy(identity, nil)
}

// EndSoloAt is [identity.EndSolo] for the computer that OFFERS a pairing, and the
// other half of what [stampJoin] does for the one that joins. A computer that was
// alone has no older pairing: every computer it meets from this moment is a new one,
// so it writes the empty legacy snapshot now, before the first of them is on its
// roster. WITHOUT THIS THE FIRST PAIRING OF EVERY PERSON WAS AN OLDER ONE: the
// computer read its roster for the first time after the other had joined, took the
// other for something that was already there, and left it watching and unable to
// type until the person said otherwise.
//
// A computer that was not alone is left as it was, because the computers already on
// its roster ARE older pairings and its first roster read names them. A failure to
// write the snapshot is not a failure to pair: the computer then stays on the safe
// side, watch only.
func EndSoloAt(dir string, book *PermitBook) error {
	alone := identity.Solo(dir)
	if err := identity.EndSolo(dir); err != nil {
		return err
	}
	if !alone || book == nil {
		return nil
	}
	if id, err := identity.Load(dir); err == nil && id.ID() != "" {
		_ = book.SnapshotLegacy(id.ID(), nil)
	}
	return nil
}
