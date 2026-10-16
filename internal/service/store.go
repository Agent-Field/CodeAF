package service

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// The store is one small JSON file beside one lock and one logs folder:
//
//	<dir>/services.json   every record and every consent, written whole
//	<dir>/lock            held by the one supervisor of this home
//	<dir>/logs/<id>.log   a service's output, and <id>.log.1 before it
//
// THE FILE IS WRITTEN WHOLE AND RENAMED INTO PLACE, private to the person:
// a crash mid-write leaves the previous file, never half of one, and the exact
// commands in it are readable by nobody else. It is bounded by construction —
// maxServices records and maxConsent consents, each field capped where it is
// made — so it stays a few tens of kilobytes however long codeaf runs.
const (
	storeFile    = "services.json"
	lockFile     = "lock"
	logsDir      = "logs"
	storeVersion = 1
	// maxStoreBytes is the most of the services file that is ever read. A
	// file the bounds below can produce is far smaller; one larger than this
	// is not one this package wrote, and it is refused rather than read.
	maxStoreBytes = 1 << 20
	// logChunk caps a service's live log. Past it the live log is copied to
	// .1 and cut back to empty, so one service keeps at most two chunks.
	logChunk = 2 << 20
)

// ErrBusy is Open's answer when another codeaf already supervises this home.
// The caller leaves the capability off rather than run a second supervisor
// that would restart the first one's services.
var ErrBusy = errors.New("another codeaf is already keeping these services")

// consent is one banked yes: the policy the person chose and the check they
// saw, for one command in one folder.
type consent struct {
	Policy Policy    `json:"policy"`
	Health Health    `json:"health"`
	At     time.Time `json:"at"`
}

type storeImage struct {
	Version  int                `json:"version"`
	Boot     string             `json:"boot,omitempty"`
	Seq      int                `json:"seq"`
	Services []record           `json:"services"`
	Consent  map[string]consent `json:"consent,omitempty"`
}

func lockStore(dir string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Join(dir, logsDir), 0o700); err != nil {
		return nil, err
	}
	_ = os.Chmod(dir, 0o700)
	file, err := os.OpenFile(filepath.Join(dir, lockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := filelock.Lock(file, true, true); err != nil {
		_ = file.Close()
		if filelock.IsBusy(err) {
			return nil, ErrBusy
		}
		return nil, err
	}
	return file, nil
}

// readStore reads the file back, bounded, and refuses one it could not have
// written. A refusal is Open's error, said to the caller: there is no quiet
// fallback to an empty store, which would be a second set of services beside
// the first one's still-running processes.
func readStore(dir string) (storeImage, error) {
	file, err := os.Open(filepath.Join(dir, storeFile))
	if errors.Is(err, os.ErrNotExist) {
		return storeImage{Version: storeVersion}, nil
	}
	if err != nil {
		return storeImage{}, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxStoreBytes+1))
	if err != nil {
		return storeImage{}, err
	}
	if len(raw) > maxStoreBytes {
		return storeImage{}, errors.New("the services file is larger than codeaf ever writes it")
	}
	var image storeImage
	if err := json.Unmarshal(raw, &image); err != nil {
		return storeImage{}, fmt.Errorf("the services file is not readable: %w", err)
	}
	if err := image.check(); err != nil {
		return storeImage{}, fmt.Errorf("the services file is not one codeaf wrote: %w", err)
	}
	return image, nil
}

// check holds a read image to every bound the package keeps when it writes
// one, so nothing on disk can carry a spec that was never validated, a field
// larger than its cap or more records than the store holds.
func (image storeImage) check() error {
	if image.Version != storeVersion {
		return fmt.Errorf("version %d", image.Version)
	}
	if len(image.Services) > maxServices || len(image.Consent) > maxConsent || image.Seq < 0 || len(image.Boot) > 128 {
		return errors.New("more than it holds")
	}
	seen := map[string]bool{}
	for _, rec := range image.Services {
		n, err := strconv.Atoi(strings.TrimPrefix(rec.ID, "s"))
		if !strings.HasPrefix(rec.ID, "s") || err != nil || n <= 0 || n > image.Seq || seen[rec.ID] {
			return fmt.Errorf("service id %q", rec.ID)
		}
		seen[rec.ID] = true
		if err := rec.check(); err != nil {
			return fmt.Errorf("service %s: %w", rec.ID, err)
		}
	}
	for key, c := range image.Consent {
		if _, err := hex.DecodeString(key); err != nil || len(key) != 64 || !c.Policy.Valid() {
			return errors.New("a consent")
		}
		if health, err := c.Health.Check(); err != nil || health != c.Health {
			return errors.New("a consent's check")
		}
	}
	return nil
}

func (r record) check() error {
	spec, err := r.Spec.Check()
	if err != nil {
		return err
	}
	if spec != r.Spec {
		return errors.New("its spec is not in the form codeaf writes")
	}
	if r.Owner == "" || len(r.Owner) > maxOwner || !r.Policy.Valid() || !r.State.valid() {
		return errors.New("its owner, policy or state")
	}
	if len(r.Restarts) > RestartLimit || r.Episode < 0 || len(r.Reason) > 4*maxReason ||
		len(r.From.Label) > 4*maxProvenance || len(r.From.CallID) > 4*maxProvenance ||
		len(r.StoppedBy) > 4*maxName || len(r.StartedHow) > maxName || len(r.EpisodeReason) > 4*maxReason {
		return errors.New("a field larger than its cap")
	}
	if r.Check != nil && len(r.Check.Detail) > 4*maxProbeDetail {
		return errors.New("its check")
	}
	if r.Identity != nil && (r.Identity.PID <= 0 || r.Identity.Start == 0 || len(r.Identity.Boot) > 128) {
		return errors.New("its identity")
	}
	return nil
}

func writeStore(dir string, raw []byte) error {
	if len(raw) > maxStoreBytes {
		return errors.New("kept-running service metadata is full; remove an unused service before adding another")
	}
	tmp, err := os.CreateTemp(dir, ".services-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	_, writeErr := tmp.Write(raw)
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, storeFile)); err != nil {
		return err
	}
	return syncDir(dir)
}

// syncDir makes a rename in dir durable. Without it a machine that loses
// power can come back with the previous file — and with it a previous id
// allocator, which would hand an archived service's output id out again.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Sync(); err != nil && !errors.Is(err, os.ErrInvalid) {
		return err
	}
	return nil
}

func (s *Supervisor) logPath(id string) string {
	return filepath.Join(s.dir, logsDir, id+".log")
}

// rotateLog keeps a live log under logChunk. The service holds its log open
// for appending, so the file is copied aside and cut back IN PLACE — renaming
// it would leave the service writing into the old copy. What the service
// writes in the instant between the copy and the cut is lost, which is the
// price of a bound that needs no cooperation from the service.
//
// THE BOUND HOLDS ONLY WHILE A CODEAF IS RUNNING. The cut happens on each
// check and when a supervisor opens; while no codeaf is open nothing cuts the
// log, and it grows by whatever the service writes until the next one opens
// and cuts it back first. A service that writes fast for days with codeaf
// closed can fill the disk, and this is said rather than promised away: a
// strict bound with codeaf gone needs a relay process of its own between the
// service and the file, which this package does not have.
func rotateLog(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil || info.Size() <= logChunk {
		return false, nil
	}
	live, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return false, err
	}
	defer live.Close()
	backup, err := os.OpenFile(path+".1", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return false, err
	}
	// Only the newest chunk is copied, so the backup is bounded too.
	start := info.Size() - logChunk
	_, copyErr := io.Copy(backup, io.NewSectionReader(live, start, logChunk))
	closeErr := backup.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return false, err
	}
	return true, live.Truncate(0)
}
