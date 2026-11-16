package tokens

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A TRUSTED DEVICE IS KEPT ONLY AS A HASH, for one person at one password
// generation: a change revokes it, it lapses after DeviceLife unused, a person
// keeps at most DevicesPerPerson, and removing the person drops theirs.
func TestADeviceIsAHashBoundToOnePersonAndOnePassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	if err := saveAccounts(path, Accounts{Accounts: []Account{{Handle: "abir", Hash: "$a", Gen: 1}, {Handle: "mia", Hash: "$m", Gen: 1}}}); err != nil {
		t.Fatal(err)
	}
	const secret = "a-device-secret-that-is-long-enough-0001"
	if err := AddDevice(path, "abir", 1, "dev_one", secret, now); err != nil {
		t.Fatal(err)
	}
	if err := AddDevice(path, "abir", 0, "dev_stale", "another secret", now); err != ErrStale {
		t.Fatalf("a device at an old generation = %v", err)
	}
	raw, _ := os.ReadFile(AccountsPath(path))
	if strings.Contains(string(raw), secret) {
		t.Fatal("accounts.json holds the device secret in plain text")
	}
	doc, _ := LoadAccounts(path)
	if d, ok := doc.FindDevice("abir", secret, now); !ok || d.ID != "dev_one" {
		t.Fatalf("find = %+v %v", d, ok)
	}
	for _, c := range []struct {
		handle, secret string
		at             time.Time
	}{{"mia", secret, now}, {"abir", secret + "x", now}, {"abir", "", now}, {"abir", secret, now.Add(DeviceLife)}} {
		if _, ok := doc.FindDevice(c.handle, c.secret, c.at); ok {
			t.Errorf("found for %s %q at %v", c.handle, c.secret, c.at)
		}
	}
	// Use keeps it alive past its first life.
	if err := TouchDevice(path, "dev_one", now.Add(DeviceLife-time.Hour)); err != nil {
		t.Fatal(err)
	}
	doc, _ = LoadAccounts(path)
	if _, ok := doc.FindDevice("abir", secret, now.Add(DeviceLife+time.Hour)); !ok {
		t.Fatal("a device in use lapsed")
	}
	// At most DevicesPerPerson, the latest kept.
	for i := 0; i < DevicesPerPerson+4; i++ {
		if err := AddDevice(path, "mia", 1, fmt.Sprintf("dev_m%02d", i), fmt.Sprintf("mia-secret-%02d-long-enough", i), now.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	doc, _ = LoadAccounts(path)
	mia := 0
	for _, d := range doc.Devices {
		if d.Handle == "mia" {
			mia++
		}
	}
	if _, ok := doc.FindDevice("mia", fmt.Sprintf("mia-secret-%02d-long-enough", DevicesPerPerson+3), now.Add(time.Hour)); mia != DevicesPerPerson || !ok {
		t.Fatalf("mia keeps %d devices (latest found %v)", mia, ok)
	}
	// A change revokes; removal drops.
	if _, err := SetPassword(path, "abir", "$a2", 1, now); err != nil {
		t.Fatal(err)
	}
	doc, _ = LoadAccounts(path)
	if _, ok := doc.FindDevice("abir", secret, now); ok {
		t.Fatal("a device outlived a password change")
	}
	if err := RemoveAccount(path, "mia"); err != nil {
		t.Fatal(err)
	}
	doc, _ = LoadAccounts(path)
	if len(doc.Devices) != 0 {
		t.Fatalf("devices left = %+v", doc.Devices)
	}
}
