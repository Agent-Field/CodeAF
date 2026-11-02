package directory

import (
	"bytes"
	"net/http/httptest"
	"testing"
)

// Aliased records must retain the legacy presentation projections for both
// values and pointers without putting HTTP/presentation code into the core.
func TestR25SharedRecordProjectionPreservesLegacyVersions(t *testing.T) {
	d := Device{LastSeen: 10}
	seen := d
	seen.LastSeen = 20
	if !bytes.Equal(fingerprint(d, 100), fingerprint(&seen, 100)) {
		t.Fatal("device presence changed the visible record")
	}
	c := Cell{Lease: Lease{Expires: 200}}
	later := c
	later.Lease.Expires = 300
	if !bytes.Equal(fingerprint(c, 100), fingerprint(&later, 100)) {
		t.Fatal("expiry-only heartbeat changed the visible record")
	}
	for _, value := range []any{Listing{Version: 42}, &Listing{Version: 42}, PresenceView{Version: 42}} {
		w := httptest.NewRecorder()
		reply(w, value)
		if w.Header().Get(VersionHeader) != "42" {
			t.Fatalf("%T lost the directory version header", value)
		}
	}
}
