package syncsetup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/relayserve"
)

// TestProbeServiceNamesWhatAnOlderServiceLacks pins #2072's release gate: the
// probe finds every promise on the current service, and names the feature that
// waits on a service deployed before conditional takes.
func TestProbeServiceNamesWhatAnOlderServiceLacks(t *testing.T) {
	svc := relayserve.New(relayserve.Config{Store: t.TempDir(), Grace: directory.GraceBounds{Min: time.Second, Max: time.Hour, Default: 10 * time.Second}})
	t.Cleanup(func() { _ = svc.Close() })
	for _, old := range []bool{false, true} {
		var h http.Handler = svc.Handler
		if old {
			h = oldService(h)
		}
		srv := httptest.NewServer(h)
		t.Cleanup(srv.Close)
		missing, err := ProbeService(context.Background(), srv.URL)
		if err != nil {
			t.Fatalf("old=%v: probe: %v", old, err)
		}
		if !old && len(missing) != 0 {
			t.Fatalf("the current service lacks %v", missing)
		}
		if old && (len(missing) != 1 || missing[0].Header != directory.ConditionalTakeHeader) {
			t.Fatalf("the older service lacks %v; want conditional takes", missing)
		}
	}
}
