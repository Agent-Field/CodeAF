package syncsetup

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// StorageFile keeps the last permission beside the saved relay, bound to both
// account and service so pairing or switching relays cannot reuse an approval.
const StorageFile = "sync-storage"

// StoragePollEvery notices a permission refreshed by another local process.
const StoragePollEvery = time.Minute

// StorageRequestURL is the one public door for requesting hosted storage.
const StorageRequestURL = "https://github.com/Agent-Field/CodeAF/issues/new"

type storageReading struct {
	Relay          string `json:"relay"`
	Identity       string `json:"identity"`
	Storage        string `json:"storage"`
	ReportsStorage *bool  `json:"reports_storage,omitempty"`
}

// IsHostedRelay recognizes all hosted channels, including an explicitly saved URL.
func IsHostedRelay(raw string) bool {
	u := storageURL(raw)
	for _, host := range HostedLinkHosts() {
		if strings.EqualFold(u.Host, host) {
			return true
		}
	}
	return false
}

// storagePolicy distinguishes a cached answer without storage from no answer.
// A legacy on reading at a custom address could have come from the old
// no-field default; it needs one probe before claiming a permission row.
func storagePolicy(home, relay, identity string) (state string, gating, answered bool) {
	raw, err := os.ReadFile(filepath.Join(home, StorageFile))
	var reading storageReading
	if err == nil && json.Unmarshal(raw, &reading) == nil &&
		normalizeStorageURL(reading.Relay) == normalizeStorageURL(relay) && reading.Identity == identity {
		switch reading.Storage {
		case "on", "off":
			if reading.ReportsStorage != nil {
				if !*reading.ReportsStorage {
					return "on", false, true
				}
				return reading.Storage, true, true
			}
			if reading.Storage == "off" || IsHostedRelay(relay) {
				return reading.Storage, true, true
			}
			return "on", false, false
		case "":
			return "on", false, true
		}
	}
	if IsHostedRelay(relay) {
		return "off", true, false
	}
	return "on", false, false
}

// StorageState reads only local state. Before an answer, hosted services are
// closed and other relays are open; afterwards the relay's answer decides.
func StorageState(home, relay, identity string) string {
	state, _, _ := storagePolicy(home, relay, identity)
	return state
}

// StorageGated reports whether this relay exposes account storage permission.
func StorageGated(home, relay, identity string) bool {
	_, gating, _ := storagePolicy(home, relay, identity)
	return gating
}

// SaveStorage remembers a successful stats answer atomically. An empty state
// records a relay without storage permission, so it need not be probed again.
func SaveStorage(home, relay, identity, state string) error {
	reports := state != ""
	if reports && state != "on" {
		state = "off"
	}
	if err := os.MkdirAll(home, 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(storageReading{Relay: normalizeStorageURL(relay), Identity: identity, Storage: state, ReportsStorage: &reports})
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(home, ".sync-storage-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(raw, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), filepath.Join(home, StorageFile)); err != nil {
		return err
	}
	effective := state
	if effective == "" {
		effective = "on"
	}
	storageSaved(home, relay, identity, effective)
	return nil
}

// GateStore keeps every relay upload door on the same local permission check.
func GateStore(store *blobstore.HTTP, home, relay, identity string) *blobstore.HTTP {
	store.CanStore = func() bool { return StorageState(home, relay, identity) == "on" }
	store.OnStorageOff = func() { _ = SaveStorage(home, relay, identity, "off") }
	return store
}

func (s *Sync) StorageAllowed() bool {
	return StorageState(s.Home, s.Relay, s.Identity.ID()) == "on"
}

// StorageRequestLink includes only the public account identifier, never a key.
func StorageRequestLink(identity string) string {
	query := url.Values{"title": {"sync storage access: " + identity}, "body": {"Please turn on sync storage for my account.\n\nRequest code: " + identity}}
	return StorageRequestURL + "?" + query.Encode()
}

// StorageForHome does not create an identity or make a network request.
func StorageForHome(home string) string {
	relay := Resolve(home)
	if !relay.On() {
		return ""
	}
	id, err := identity.Load(home)
	if err != nil {
		return ""
	}
	return StorageState(home, relay.URL, id.ID())
}

// Default ports and host case identify the same service and permission file.
func storageURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		return &url.URL{}
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if u.Scheme == "https" && u.Port() == "443" || u.Scheme == "http" && u.Port() == "80" {
		u.Host = u.Hostname()
	}
	return u
}
func normalizeStorageURL(raw string) string { return strings.TrimRight(storageURL(raw).String(), "/") }
