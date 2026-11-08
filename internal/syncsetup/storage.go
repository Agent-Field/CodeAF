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
	Relay    string `json:"relay"`
	Identity string `json:"identity"`
	Storage  string `json:"storage"`
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

// StorageState reads only local state. An unknown hosted permission is closed;
// older and self-hosted relays retain their existing enabled storage policy.
func StorageState(home, relay, identity string) string {
	raw, err := os.ReadFile(filepath.Join(home, StorageFile))
	var reading storageReading
	if err == nil && json.Unmarshal(raw, &reading) == nil &&
		normalizeStorageURL(reading.Relay) == normalizeStorageURL(relay) && reading.Identity == identity &&
		(reading.Storage == "on" || reading.Storage == "off") {
		return reading.Storage
	}
	if IsHostedRelay(relay) {
		return "off"
	}
	return "on"
}

// SaveStorage remembers a successful stats answer atomically. Missing state
// means closed on our hosts and enabled on a relay that predates this field.
func SaveStorage(home, relay, identity, state string) error {
	if state != "on" && state != "off" {
		state = "on"
		if IsHostedRelay(relay) {
			state = "off"
		}
	}
	if err := os.MkdirAll(home, 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(storageReading{normalizeStorageURL(relay), identity, state})
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
	storageSaved(home, relay, identity, state)
	return nil
}

// GateStore keeps every hosted upload door on the same local permission check.
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
	query := url.Values{"title": {"relay storage access: " + identity}, "body": {"Please turn on relay storage for my account.\n\nRequest code: " + identity}}
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
