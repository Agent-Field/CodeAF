package config

import (
	"fmt"
	"sort"
	"sync"
	"testing"
)

// Connecting some services while others are disconnected must leave exactly the
// services the two sides agree on. A list loaded before the other side's write
// and written back whole would bring a disconnected service back or drop a
// connected one; the write reads the list under the profile lock, so it cannot.
func TestConnectAndDisconnectComposeOnTheServiceList(t *testing.T) {
	dir := t.TempDir()
	const count = 12
	var seed []PersistedSource
	for i := 0; i < count; i++ {
		seed = append(seed, PersistedSource{ID: fmt.Sprintf("old%02d", i), Written: "old", Order: i})
	}
	if err := WriteSources(dir, seed); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < count; i++ {
			row := PersistedSource{ID: fmt.Sprintf("new%02d", i), Written: "new", Order: count + i}
			if err := persistConnectedSource(dir, row); err != nil {
				t.Error(err)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < count; i++ {
			if err := DisconnectService(dir, fmt.Sprintf("old%02d", i)); err != nil {
				t.Error(err)
			}
		}
	}()
	wg.Wait()

	var got []string
	for _, row := range PersistedSources(dir) {
		got = append(got, row.ID)
	}
	sort.Strings(got)
	if len(got) != count || got[0] != "new00" || got[count-1] != fmt.Sprintf("new%02d", count-1) {
		t.Fatalf("the service list lost an update: %v", got)
	}
}

// A key stored by another writer is in what a provider-key change is handed, so
// changing one key cannot remove another.
func TestChangeProviderKeysStartsFromWhatIsStored(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAPIKey(dir, "FAKE-model"); err != nil {
		t.Fatal(err)
	}
	err := ChangeProviderKeys(dir, func(held ProviderKeys) ProviderKeys {
		held.Rows = map[string]string{KeyAPIKey: held.Rows[KeyAPIKey], KeyExaKey: "FAKE-search"}
		return held
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := ReadProviderKeys(dir)
	if got.Rows[KeyAPIKey] != "FAKE-model" || got.Rows[KeyExaKey] != "FAKE-search" {
		t.Fatalf("a stored key did not survive: %d rows", len(got.Rows))
	}
}
