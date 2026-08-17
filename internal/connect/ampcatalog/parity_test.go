package ampcatalog_test

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	amp "github.com/amp-labs/connectors/providers"

	"github.com/Agent-Field/aforge-v2/internal/connect/ampcatalog"
)

// This is the test that makes the snapshot safe to trust.
//
// The whole point of ampcatalog is that the aforge binary does not link the
// catalog library — 9.6 MB for a table it reads a handful of fields out of. The
// cost of that is a copy, and the risk of a copy is that it drifts from the
// thing it copied without anyone noticing. So the library is still a module
// requirement and still imported HERE, in a test, where it costs nothing that
// ships: this file asserts that regenerating the snapshot right now would
// produce exactly the file that is committed.
//
// When it fails, the fix is one command:
//
//	go generate ./internal/connect/ampcatalog
//
// and then reading the diff, which is what the JSON is for.

func TestSnapshotMatchesTheLibrary(t *testing.T) {
	live := map[ampcatalog.Provider]*ampcatalog.ProviderInfo{}
	for _, name := range amp.AllNames() {
		info, err := amp.ReadInfo(name)
		if err != nil || info == nil {
			continue
		}
		live[ampcatalog.Provider(name)] = narrow(t, info)
	}

	kept := map[ampcatalog.Provider]*ampcatalog.ProviderInfo{}
	for _, name := range ampcatalog.AllNames() {
		info, err := ampcatalog.ReadInfo(name)
		if err != nil {
			t.Fatalf("snapshot lists %q and then will not read it: %v", name, err)
		}
		kept[name] = info
	}

	for _, name := range sortedNames(live) {
		held, ok := kept[name]
		if !ok {
			t.Errorf("the catalog has %q and the snapshot does not — run go generate ./internal/connect/ampcatalog", name)
			continue
		}
		if !reflect.DeepEqual(live[name], held) {
			t.Errorf("%q has drifted:\n snapshot %s\n catalog  %s\nrun go generate ./internal/connect/ampcatalog",
				name, mustJSON(t, held), mustJSON(t, live[name]))
		}
	}
	for _, name := range sortedNames(kept) {
		if _, ok := live[name]; !ok {
			t.Errorf("the snapshot has %q and the catalog no longer does — run go generate ./internal/connect/ampcatalog", name)
		}
	}
}

// TestSnapshotIsNotEmpty guards the failure mode the parity test cannot see: a
// snapshot that decodes to nothing agrees with nothing, and every service would
// simply stop appearing on the accounts menu with no error anywhere.
func TestSnapshotIsNotEmpty(t *testing.T) {
	if got := len(ampcatalog.AllNames()); got < 100 {
		t.Fatalf("the snapshot carries %d providers, which is too few to be real", got)
	}
}

// narrow is gen/main.go's projection, repeated here rather than exported: the
// test has to reach the same answer by the same route, and a shared helper
// would let a bug in the projection agree with itself.
func narrow(t *testing.T, info *amp.ProviderInfo) *ampcatalog.ProviderInfo {
	t.Helper()
	full, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal catalog row: %v", err)
	}
	var cut ampcatalog.ProviderInfo
	if err := json.Unmarshal(full, &cut); err != nil {
		t.Fatalf("narrow catalog row: %v", err)
	}
	return &cut
}

func sortedNames(rows map[ampcatalog.Provider]*ampcatalog.ProviderInfo) []ampcatalog.Provider {
	out := make([]ampcatalog.Provider, 0, len(rows))
	for name := range rows {
		out = append(out, name)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(encoded)
}
