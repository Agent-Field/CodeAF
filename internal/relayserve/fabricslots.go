package relayserve

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/clientproto"

	"github.com/Agent-Field/codeaf/internal/fsreplace"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// Slot limits are the same for every client and implementation.
const (
	MaxSlotBytes   = clientproto.MaxSlotBytes
	MaxClientSlots = clientproto.MaxClientSlots
	FabricMaxAge   = clientproto.MaxAge
)

type fabricSlot struct {
	Sealed  []byte `json:"sealed"`
	Writer  string `json:"writer"`
	At      int64  `json:"at"`
	Version uint64 `json:"version"`
}

// clientState is replaced durably as one file; readers never see half a write.
// Slots and queue share the same lifetime and revocation boundary.
type clientState struct {
	Version    uint64                `json:"version"`
	Slots      map[string]fabricSlot `json:"slots"`
	Queue      []queueEntry          `json:"queue"`
	LastNumber int64                 `json:"last_number"`
}

func (c fabricCall) statePath() string {
	return filepath.Join(c.f.ns.root, "fabric", c.id, c.r.PathValue("device")+".json")
}

func (c fabricCall) load() (clientState, error) {
	s := clientState{Slots: map[string]fabricSlot{}}
	raw, err := os.ReadFile(c.statePath())
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, err
	}
	now := c.f.ns.now().UnixMilli()
	for name, slot := range s.Slots {
		if now-slot.At >= FabricMaxAge.Milliseconds() {
			delete(s.Slots, name)
		}
	}
	s.Queue = slices.DeleteFunc(s.Queue, func(e queueEntry) bool { return e.At <= now-FabricMaxAge.Milliseconds() })
	return s, nil
}

func (c fabricCall) save(s clientState) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	path := c.statePath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".client-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, werr := file.Write(raw)
	err = errors.Join(werr, file.Sync(), file.Close())
	if err != nil {
		return err
	}
	if err := fsreplace.Rename(file.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

func (c fabricCall) target(client bool) error {
	callerClient := c.listing.Devices[c.dev].Kind == "client"
	if callerClient != client || (client && c.dev != c.r.PathValue("device")) {
		return wireauth.ErrUnauthorized
	}
	target, ok := c.listing.Devices[c.r.PathValue("device")]
	if !ok || target.Kind != "client" || target.Revoked {
		return badFabric(404, "not_found")
	}
	if c.r.Method != "GET" && c.listing.Identity.Rotation != nil {
		return wireauth.ErrRotated
	}
	return nil
}

func putSlot(c fabricCall) (any, error) {
	if err := c.target(false); err != nil {
		return nil, err
	}
	name := c.r.PathValue("name")
	if !fabricName.MatchString(name) {
		return nil, badFabric(400, "bad_request")
	}
	var in struct {
		Sealed []byte `json:"sealed"`
	}
	if err := decodeFabric(c.body, &in); err != nil {
		return nil, err
	}
	if len(in.Sealed) > MaxSlotBytes {
		return nil, badFabric(413, "too_large")
	}
	if len(in.Sealed) == 0 {
		return nil, badFabric(400, "bad_request")
	}
	s, err := c.load()
	if err != nil {
		return nil, err
	}
	if _, exists := s.Slots[name]; !exists && len(s.Slots) >= MaxClientSlots {
		return nil, badFabric(429, "full")
	}
	s.Version++
	s.Slots[name] = fabricSlot{Sealed: in.Sealed, Writer: c.dev, At: c.f.ns.now().UnixMilli(), Version: s.Version}
	if err := c.save(s); err != nil {
		return nil, err
	}
	c.f.wakeSlots(c.statePath())
	return nil, nil
}

func getSlot(c fabricCall) (any, error) {
	if err := c.target(true); err != nil {
		return nil, err
	}
	if len(c.body) != 0 || !fabricName.MatchString(c.r.PathValue("name")) {
		return nil, badFabric(400, "bad_request")
	}
	s, err := c.load()
	if err != nil {
		return nil, err
	}
	slot, ok := s.Slots[c.r.PathValue("name")]
	if !ok {
		return nil, badFabric(404, "not_found")
	}
	return slot, nil
}

func revokeClient(c fabricCall) (any, error) {
	if c.listing.Devices[c.dev].Kind == "client" {
		return nil, wireauth.ErrUnauthorized
	}
	if len(c.body) != 0 {
		return nil, badFabric(400, "bad_request")
	}
	if err := c.dir.For(c.dev).Revoke(c.r.Context(), c.r.PathValue("device")); err != nil {
		return nil, err
	}
	c.f.wakeSlots(c.statePath())
	err := os.Remove(c.statePath())
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	return nil, err
}

// cleanRevoked completes deletion if the process stopped between committing a
// revoke and removing its client file. The sweep already holds the fabric lock.
func (f *fabric) cleanRevoked() error {
	ids, err := f.ns.known()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if f.ns.gone(id) {
			continue
		}
		d, err := f.ns.sqlite(id)
		if err != nil {
			return err
		}
		entries, err := os.ReadDir(filepath.Join(f.ns.root, "fabric", id))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			dev, ok := strings.CutSuffix(entry.Name(), ".json")
			if ok && fabricDevice.MatchString(dev) && d.Revoked(dev) {
				if err := os.Remove(filepath.Join(f.ns.root, "fabric", id, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
		}
	}
	return nil
}
