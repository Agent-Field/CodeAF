package relayserve_test

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

// R25/R30 pin the real persistent relay door, independently of an adapter or
// fixture backend. The sole production route must commit requests through
// the migrated namespace.
func TestR25R30ProductionRelayRefusesLegacyBlindForce(t *testing.T) {
	raw, err := os.ReadFile("../raftdirectory/testdata/v1-requests.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Now   int64
		Cases []struct {
			Name, Method, URI, Body string
			Headers                 map[string]string
		}
	}
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	store := t.TempDir()
	svc := relayserve.New(relayserve.Config{Store: store, Now: func() time.Time { return time.UnixMilli(f.Now) }})
	defer svc.Close()
	srv := httptest.NewServer(svc.Handler)
	defer srv.Close()
	for _, one := range f.Cases {
		if one.Name != "put-a" && one.Name != "put-b" && one.Name != "create" && one.Name != "blind-force" {
			continue
		}
		r, err := http.NewRequest(one.Method, srv.URL+one.URI, strings.NewReader(one.Body))
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range one.Headers {
			r.Header.Set(k, v)
		}
		resp, err := srv.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if one.Name == "blind-force" {
			if resp.StatusCode != http.StatusConflict || !strings.Contains(string(body), `"err":"lease_held"`) {
				t.Fatalf("R25/R30 persistent production relay accepted blind force or lost the legacy error: status=%d body=%s; production migration/member assembly is required", resp.StatusCode, body)
			}
		} else if resp.StatusCode >= 300 {
			t.Fatalf("%s: status=%d body=%s", one.Name, resp.StatusCode, body)
		}
	}
	// A legacy writer could be taught only the blind-force refusal. The durable
	// source ownership receipt independently requires the actual migration cutover.
	root, err := base64.RawURLEncoding.DecodeString(f.Cases[0].Headers[reqsign.HeaderIdentity])
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(store, "directory", reqsign.IDOf(root)+".db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var gateRaw string
	if err = db.QueryRow("SELECT rec FROM legacy_gate WHERE id=1").Scan(&gateRaw); err != nil {
		t.Fatalf("R30 production namespace has no durable ownership receipt: %v", err)
	}
	var gate struct {
		Stage string `json:"stage"`
	}
	if json.Unmarshal([]byte(gateRaw), &gate) != nil || gate.Stage != "raft-owned" {
		t.Fatalf("R30 production namespace still has no committed owner: %s", gateRaw)
	}
}
