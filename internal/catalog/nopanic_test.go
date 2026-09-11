package catalog

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"
)

func quietCatalogLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buffer := &bytes.Buffer{}
	flags, writer := log.Flags(), log.Writer()
	log.SetOutput(buffer)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(writer)
		log.SetFlags(flags)
	})
	return buffer
}

func faultingCatalogClient() *http.Client {
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		panic("discovery hit a nil header map")
	})}
}

// A fault in discovery is worse than a failed fetch in two ways, and the guard
// has to answer both: it runs on a goroutine nobody waits for, and it lives
// inside a sync.OnceValue, which replays a panic to every later reader. So the
// first capability question would have died on the caller's goroutine — the
// head, long after the fetch was forgotten.
func TestALazyCatalogFaultDegradesToTheKnownDefaults(t *testing.T) {
	logged := quietCatalogLog(t)

	lazy := LoadLazy(context.Background(), Options{
		BaseURL: DefaultBaseURL, Dir: t.TempDir(), HTTPClient: faultingCatalogClient(),
	})

	answered := make(chan bool, 1)
	go func() { answered <- lazy.Supports("hexgrad/kokoro-82m", "output", "speech") }()
	select {
	case ok := <-answered:
		if !ok {
			t.Fatal("a faulted fetch must degrade to the known modality defaults")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the first capability question never returned")
	}

	// The future is asked again, which is where the replayed panic would land.
	// The fallback rows are led by the name internal/config prefers for the
	// video slot, so a cold machine resolves to the same model a warm one does.
	if got := lazy.ModelsWithOutput("video"); len(got) == 0 || got[0].ID != "bytedance/seedance-2.5" {
		t.Fatalf("second question returned %+v, want the fallback video models led by bytedance/seedance-2.5", got)
	}
	if _, ok := lazy.Model("vendor/absent"); ok {
		t.Fatal("an unknown model must still answer false")
	}
	if !strings.Contains(logged.String(), `scope="catalog/load"`) {
		t.Fatalf("the fault was not recorded to the log: %q", logged.String())
	}
}

// The eager path resolves on the caller's own goroutine, where a fault would
// take the caller down instead. It degrades to the same defaults.
func TestAnEagerCatalogFaultDegradesToTheKnownDefaults(t *testing.T) {
	quietCatalogLog(t)
	eager := Load(context.Background(), Options{
		BaseURL: DefaultBaseURL, Dir: t.TempDir(), HTTPClient: faultingCatalogClient(),
	})
	if !eager.Supports("krea/krea-2-medium-turbo", "output", "image") {
		t.Fatal("a faulted eager load must degrade to the known modality defaults")
	}
}
