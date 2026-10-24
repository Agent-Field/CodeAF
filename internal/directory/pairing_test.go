package directory_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
)

func TestMemoryPairingConforms(t *testing.T) {
	directorytest.RunPairing(t, func(*testing.T) directorytest.PairingRig {
		clock := directorytest.NewFakeClock()
		links := directory.NewLinks(clock.Now, directory.DefaultLinkLimits)
		mem := directory.NewMemory(clock.Now)
		mem.SetLinks(links)
		return directorytest.PairingRig{Clock: clock, Devices: mem.For, Requests: links.From("net")}
	})
}

func TestSQLitePairingConforms(t *testing.T) {
	directorytest.RunPairing(t, func(t *testing.T) directorytest.PairingRig {
		clock := directorytest.NewFakeClock()
		links := directory.NewLinks(clock.Now, directory.DefaultLinkLimits)
		db := openAt(t, filepath.Join(t.TempDir(), "dir.db"), clock)
		db.SetLinks(links)
		return directorytest.PairingRig{Clock: clock, Devices: db.For, Requests: links.From("net")}
	})
}

func TestHTTPPairingConforms(t *testing.T) {
	directorytest.RunPairing(t, func(t *testing.T) directorytest.PairingRig {
		clock := directorytest.NewFakeClock()
		links := directory.NewLinks(clock.Now, directory.DefaultLinkLimits)
		spaces := newNamespaces(clock)
		spaces.links = links
		mux := http.NewServeMux()
		mux.Handle("/v1/dir/", directory.Handler(fakeAuth, spaces.open))
		mux.Handle(directory.LinkPath+"/", directory.LinkHandler(links, func(*http.Request) string { return "net" }))
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)
		as := func(device string) directory.Client {
			return directory.NewHTTP(srv.URL, fakeSign("id_one", device), srv.Client())
		}
		return directorytest.PairingRig{Clock: clock, Devices: as, Requests: directory.NewLinkHTTP(srv.URL, srv.Client())}
	})
}

// The fixtures of docs/testdata/ux-pairing-vectors.json decode into the Go
// records and encode back to the same JSON.
func TestContractVectorsRoundTrip(t *testing.T) {
	raw, err := os.ReadFile("../../docs/testdata/ux-pairing-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors map[string]json.RawMessage
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	for name, into := range map[string]any{
		"request_pending": &directory.Request{}, "create_answer": &directory.Opened{},
		"device_legacy": &directory.Device{}, "device_new": &directory.Device{},
	} {
		if err := json.Unmarshal(vectors[name], into); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		back, _ := json.Marshal(into)
		if !jsonEqual(t, back, vectors[name]) {
			t.Errorf("%s re-encodes as %s, fixture %s", name, back, vectors[name])
		}
	}
}

func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		t.Fatal("not json")
	}
	ja, _ := json.Marshal(x)
	jb, _ := json.Marshal(y)
	return string(ja) == string(jb)
}

// A client that sends no platform, created or last_seen still writes a device,
// and a record that carries them keeps them out of the version count.
func TestLastSeenMovesNoVersion(t *testing.T) {
	clock := directorytest.NewFakeClock()
	mem := directory.NewMemory(clock.Now)
	c := mem.For(tA)
	if err := c.PutDevice(bg, tA, directory.Device{V: 1}); err != nil {
		t.Fatal(err)
	}
	before, _ := c.List(bg)
	if err := c.PutDevice(bg, tA, directory.Device{V: 1, LastSeen: 99}); err != nil {
		t.Fatal(err)
	}
	after, _ := c.List(bg)
	if before.Version != after.Version {
		t.Fatalf("version %d -> %d for an ignored last_seen", before.Version, after.Version)
	}
}
