package connect

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	ampsdk "github.com/amp-labs/connectors/providers"

	"github.com/Agent-Field/aforge-v2/internal/connect/ampcatalog"
)

// This file is the proof that replacing the amp-labs import with a committed
// snapshot took nothing away.
//
// ampcatalog's own parity test compares the two catalogs row by row. That is
// necessary and not sufficient: what a person actually sees is not a catalog
// row, it is a plug — an id, a name, a category, a blurb, an address with its
// blank still in it, where the key rides, and the health check. So this test
// builds the whole plug list twice, once from the snapshot the binary ships
// and once from the library at its pinned commit, and requires them to be
// equal in every field including the unexported ones.
//
// The library is imported HERE and nowhere the binary can reach, which is the
// entire arrangement: a test-only dependency costs nothing at runtime and
// nothing in the binary, and it is the only thing that can tell us the copy is
// still true.

func TestSnapshotPlugsAreTheLibrarysPlugs(t *testing.T) {
	fromSnapshot := catalogPlugs()
	fromLibrary := libraryPlugs(t)

	if len(fromSnapshot) != len(fromLibrary) {
		t.Fatalf("the snapshot yields %d plugs and the library yields %d — a service has been lost or gained",
			len(fromSnapshot), len(fromLibrary))
	}

	for index := range fromSnapshot {
		mine, theirs := fromSnapshot[index], fromLibrary[index]
		if !reflect.DeepEqual(mine, theirs) {
			t.Errorf("plug %d differs:\n snapshot %+v\n library  %+v", index, mine, theirs)
		}
	}
}

// TestSnapshotKeepsEveryKeyedService states the count out loud. A projection
// that silently dropped a field could still produce the same number of plugs;
// a projection that dropped a PROVIDER could not. Both tests are wanted.
func TestSnapshotKeepsEveryKeyedService(t *testing.T) {
	snapshot := len(catalogPlugs())
	library := len(libraryPlugs(t))
	if snapshot != library {
		t.Fatalf("keyed services: snapshot %d, library %d", snapshot, library)
	}
	if snapshot == 0 {
		t.Fatal("no keyed services at all, which cannot be right")
	}
	t.Logf("keyed services carried by the snapshot: %d", snapshot)
}

// libraryPlugs is catalogPlugs, driven by the live library instead of the
// snapshot.
//
// It runs the SAME catalogPlug over the SAME projection gen/main.go performs,
// so what it isolates is exactly the one thing under test: whether the bytes
// the snapshot carries are the bytes the library would have given. Everything
// downstream of catalogPlug — the blurb, the category, the blank filling — is
// shared code and is therefore not a variable here.
func libraryPlugs(t *testing.T) []Plug {
	t.Helper()
	names := ampsdk.AllNames()
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	plugs := make([]Plug, 0, len(names))
	for _, name := range names {
		info, err := ampsdk.ReadInfo(name)
		if err != nil || info == nil {
			continue
		}
		if plug, ok := catalogPlug(string(name), project(t, info)); ok {
			plugs = append(plugs, plug)
		}
	}
	return plugs
}

// project is gen/main.go's narrow, spelled again here on purpose: a shared
// helper would let a bug in the projection agree with itself.
func project(t *testing.T, info *ampsdk.ProviderInfo) *ampcatalog.ProviderInfo {
	t.Helper()
	full, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal library row: %v", err)
	}
	var cut ampcatalog.ProviderInfo
	if err := json.Unmarshal(full, &cut); err != nil {
		t.Fatalf("project library row: %v", err)
	}
	return &cut
}

// TestRegistryStillHoldsEveryService counts what the whole registry holds, not
// just the catalog's share of it.
//
// The snapshot changed where the keyed services' data comes from and nothing
// else, so the browser-connected services — Google and the five tool servers,
// registered by google.go and mcp_catalog.go and never near the catalog — must
// still be there, by name. This is the assertion that would catch a change to
// init ordering or to Register that a catalog-only test would sail past.
//
// The numbers are the census taken at this wave's merge base and are meant to
// be hard to change by accident: 99 keyed services from the catalog, six
// browser services, 105 in all.
func TestRegistryStillHoldsEveryService(t *testing.T) {
	byAuth := map[string]int{}
	browser := []string{}
	for _, plug := range Registered() {
		service := plug.Service()
		byAuth[service.Auth]++
		if service.Auth == AuthBrowser {
			browser = append(browser, service.ID)
		}
	}
	sort.Strings(browser)

	if got := byAuth[AuthKey]; got != 99 {
		t.Errorf("keyed services registered: %d, want 99", got)
	}
	wantBrowser := []string{"atlassian", "google", "linear", "notion", "sentry", "slack"}
	if !reflect.DeepEqual(browser, wantBrowser) {
		t.Errorf("browser services registered: %v, want %v", browser, wantBrowser)
	}
	if got := len(Registered()); got != 105 {
		t.Errorf("services registered: %d, want 105", got)
	}
}
