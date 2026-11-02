package relayserve_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

type upgradeFixture struct {
	Now   int64
	Cases []struct {
		Method, URI, Body string
		Headers           map[string]string
	}
}

func loadUpgradeFixture(t *testing.T) upgradeFixture {
	t.Helper()
	raw, err := os.ReadFile("../raftdirectory/testdata/v1-large-requests.json")
	if err != nil {
		t.Fatal(err)
	}
	var f upgradeFixture
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f
}
func upgradeRequest(f upgradeFixture, method, uri string, body []byte) *http.Request {
	r := httptest.NewRequest(method, "http://local"+uri, bytes.NewReader(body))
	for k, v := range f.Cases[1].Headers {
		r.Header.Set(k, v)
	}
	seed := sha256.Sum256([]byte("p17-a"))
	key := ed25519.NewKeyFromSeed(seed[:])
	at := fmt.Sprint(f.Now)
	r.Header.Set(reqsign.HeaderTime, at)
	r.Header.Set(reqsign.HeaderSig, base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, cs.V1SigningMessage(method, uri, at, body))))
	return r
}
func serveUpgrade(t *testing.T, h http.Handler, f upgradeFixture, method, uri string, body []byte, want int) []byte {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, upgradeRequest(f, method, uri, body))
	if w.Code != want {
		t.Fatalf("%s %s: HTTP %d, want %d: %.180s", method, uri, w.Code, want, w.Body.Bytes())
	}
	return w.Body.Bytes()
}
func oldUpgradeStore(t *testing.T, f upgradeFixture) (string, *directory.SQLite, http.Handler) {
	t.Helper()
	store := t.TempDir()
	root, err := base64.RawURLEncoding.DecodeString(f.Cases[1].Headers[reqsign.HeaderIdentity])
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store, "directory")
	if err = os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	old, err := directory.OpenSQLite(filepath.Join(path, reqsign.IDOf(root)+".db"), func() time.Time { return time.UnixMilli(f.Now) })
	if err != nil {
		t.Fatal(err)
	}
	return store, old, directory.Handler(reqsign.AuthenticateAt(func() time.Time { return time.UnixMilli(f.Now) }), func(string) (directory.Directory, error) { return old, nil })
}

// r1.1
func TestR11AcceptedLegacyBodySurvivesUpgrade(t *testing.T) {
	f := loadUpgradeFixture(t)
	store, old, handler := oldUpgradeStore(t, f)
	c := f.Cases[1]
	if len(c.Body) != directory.MaxBody-1 {
		t.Fatal("archived body changed")
	}
	created := serveUpgrade(t, handler, f, c.Method, c.URI, []byte(c.Body), 200)
	before := serveUpgrade(t, handler, f, "GET", c.URI, nil, 200)
	if !bytes.Equal(created, before) {
		t.Fatal("old create/read differ")
	}
	oldListRaw := serveUpgrade(t, handler, f, "GET", "/v1/dir/list", nil, 200)
	var oldList directory.Listing
	if json.Unmarshal(oldListRaw, &oldList) != nil {
		t.Fatal("old list decode")
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	svc := relayserve.New(relayserve.Config{Store: store, Now: func() time.Time { return time.UnixMilli(f.Now) }})
	defer svc.Close()
	after := serveUpgrade(t, svc.Handler, f, "GET", c.URI, nil, 200)
	if !bytes.Equal(after, before) {
		t.Fatal("cell changed during upgrade")
	}
	var list directory.Listing
	raw := serveUpgrade(t, svc.Handler, f, "GET", "/v1/dir/list", nil, 200)
	if json.Unmarshal(raw, &list) != nil {
		t.Fatal("list decode")
	}

	cell, _ := json.Marshal(list.Cells["near-http-limit"])
	want, _ := json.Marshal(oldList.Cells["near-http-limit"])
	if !bytes.Equal(cell, want) {
		t.Fatal("List lost retained fields")
	}
	take := serveUpgrade(t, svc.Handler, f, "POST", c.URI+"/acquire", []byte(`{}`), 200)
	var held directory.CellView
	if json.Unmarshal(take, &held) != nil || held.Cell.Lease.Fence != 2 {
		t.Fatal("take did not commit")
	}
	serveUpgrade(t, svc.Handler, f, "POST", c.URI+"/release", []byte(`{"fence":2}`), 204)
	raw = serveUpgrade(t, svc.Handler, f, "GET", c.URI, nil, 200)
	if json.Unmarshal(raw, &held) != nil || held.Cell.Lease.Expires != 0 || held.Cell.Lease.Fence != 2 {
		t.Fatal("release did not commit")
	}
}

// r1.3
func TestR13ImportedCapacityKeepsDirectoryUsable(t *testing.T) {
	for _, below := range []bool{false, true} {
		t.Run(fmt.Sprint("below=", below), func(t *testing.T) {
			f := loadUpgradeFixture(t)
			store, old, handler := oldUpgradeStore(t, f)
			n := 30
			if below {
				n = 28
			}
			sum := 0
			for i := 0; i < n; i++ {
				title := 300000
				init := directory.CellInit{Head: "head", Title: strings.Repeat("s", title)}
				if below && i == n-1 {
					sample, _ := json.Marshal(cs.Created(init, cs.DeviceIDOfKey(actorKeyForUpgrade()), f.Now))
					title += cs.DefaultApplicationBytes - 2048 - sum - len(sample)
					init.Title = strings.Repeat("s", title)
				}
				body, _ := json.Marshal(init)
				raw := serveUpgrade(t, handler, f, "POST", fmt.Sprintf("/v1/dir/cells/chat-%d", i), body, 200)
				var view directory.CellView
				if json.Unmarshal(raw, &view) != nil {
					t.Fatal("create decode")
				}
				stored, _ := json.Marshal(view.Cell)
				sum += len(stored)
			}
			if below && (sum >= cs.DefaultApplicationBytes || sum < cs.DefaultApplicationBytes-4096) || !below && sum <= cs.DefaultApplicationBytes {
				t.Fatalf("wrong source size %d", sum)
			}
			t.Logf("source cell bytes=%d", sum)
			if err := old.Close(); err != nil {
				t.Fatal(err)
			}
			svc := relayserve.New(relayserve.Config{Store: store, Now: func() time.Time { return time.UnixMilli(f.Now) }})
			defer svc.Close()
			var list directory.Listing
			raw := serveUpgrade(t, svc.Handler, f, "GET", "/v1/dir/list", nil, 200)
			if json.Unmarshal(raw, &list) != nil || len(list.Cells) != n {
				t.Fatal("import list")
			}
			uri := "/v1/dir/cells/chat-0"
			var view directory.CellView
			raw = serveUpgrade(t, svc.Handler, f, "GET", uri, nil, 200)
			if json.Unmarshal(raw, &view) != nil {
				t.Fatal("cell decode")
			}
			growing := directory.CellInit{Head: "growth", Title: upgradeNoise(4096)}
			body, _ := json.Marshal(growing)
			refused := serveUpgrade(t, svc.Handler, f, "POST", "/v1/dir/cells/growth", body, 503)
			if !bytes.Contains(refused, []byte(`"err":"full"`)) {
				t.Fatalf("wrong refusal %.180s", refused)
			}
			publish, _ := json.Marshal(directory.Publish{Fence: 1, OldHead: view.Cell.Head, Head: "next", Title: view.Cell.Title})
			serveUpgrade(t, svc.Handler, f, "POST", uri+"/publish", publish, 200)
			serveUpgrade(t, svc.Handler, f, "POST", uri+"/release", []byte(`{"fence":1}`), 204)
			serveUpgrade(t, svc.Handler, f, "GET", uri, nil, 200)
		})
	}
}
func actorKeyForUpgrade() []byte {
	seed := sha256.Sum256([]byte("p17-a"))
	return ed25519.NewKeyFromSeed(seed[:]).Public().(ed25519.PublicKey)
}
func upgradeNoise(n int) string {
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		sum := sha256.Sum256([]byte(fmt.Sprint(i)))
		b.WriteString(base64.RawURLEncoding.EncodeToString(sum[:]))
	}
	return b.String()[:n]
}
