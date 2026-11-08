package syncsetup

import (
	"errors"
	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/wireauth"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type storageTransport func(*http.Request) (*http.Response, error)

func (f storageTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestStorageOptInCacheAndUploadDoors(t *testing.T) {
	home := t.TempDir()
	const id = "id_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, relay := range []string{ProductionRelayURL, StagingRelayURL, DevRelayURL} {
		if !IsHostedRelay(relay) || StorageState(home, relay, id) != "off" {
			t.Fatalf("unknown hosted state open: %s", relay)
		}
	}
	if IsHostedRelay("https://codeaf.agentfield.ai.evil.test/fabric") || StorageState(home, "http://own.test", id) != "on" {
		t.Fatal("self-hosted policy changed")
	}
	calls := 0
	hc := &http.Client{Transport: storageTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `{"frame":"frame","objects":1}`
		if strings.HasSuffix(r.URL.Path, "/has") {
			body = `{"have":[false]}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	c := GateStore(blobstore.NewHTTP(ProductionRelayURL, func(*http.Request, []byte) {}, hc), home, ProductionRelayURL, id)
	if _, err := c.PutFrame(t.Context(), nil); !errors.Is(err, wireauth.ErrStorageOff) || calls != 0 {
		t.Fatalf("write while off: %v calls=%d", err, calls)
	}
	if _, err := c.Has(t.Context(), nil); err != nil || calls != 1 {
		t.Fatalf("read while off: %v calls=%d", err, calls)
	}
	if err := SaveStorage(home, ProductionRelayURL, id, "on"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PutFrame(t.Context(), nil); err != nil || calls != 2 {
		t.Fatalf("approved write: %v calls=%d", err, calls)
	}
	if StorageState(home, DevRelayURL, id) != "off" || StorageState(home, ProductionRelayURL, "id_b") != "off" {
		t.Fatal("permission crossed account or relay")
	}
	if err := SaveStorage(home, ProductionRelayURL, id, "off"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PutFrame(t.Context(), nil); !errors.Is(err, wireauth.ErrStorageOff) || calls != 2 {
		t.Fatal("revoked store made a request")
	}
	_ = SaveStorage(home, ProductionRelayURL, id, "")
	if StorageState(home, ProductionRelayURL, id) != "on" {
		t.Fatal("no-field hosted answer must enable legacy storage")
	}
	_ = os.WriteFile(filepath.Join(home, StorageFile), []byte("{"), 0600)
	if StorageState(home, ProductionRelayURL, id) != "off" {
		t.Fatal("damaged cache enabled storage")
	}
}

func TestStorageOptInDriveStopsBeforeAnyNetworkOrEngineWork(t *testing.T) {
	home := t.TempDir()
	id, err := identity.Ensure(home)
	if err != nil {
		t.Fatal(err)
	}
	s := &Sync{Home: home, Relay: ProductionRelayURL, Identity: id}
	_, err = s.Drive(t.Context(), cellstore.Engine{}, cell.Cell{ID: "local"}, DriveOptions{})
	if !errors.Is(err, wireauth.ErrStorageOff) {
		t.Fatalf("drive started: %v", err)
	}
}

func TestStorageOptInWireRefusalClosesAllLocalUploaders(t *testing.T) {
	const relay = "http://127.0.0.1:18831"
	home := t.TempDir()
	id := "id_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := SaveStorage(home, relay, id, "on"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	hc := &http.Client{Transport: storageTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 403, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"err":"storage_off"}`))}, nil
	})}
	first := GateStore(blobstore.NewHTTP(relay, func(*http.Request, []byte) {}, hc), home, relay, id)
	second := GateStore(blobstore.NewHTTP(relay, func(*http.Request, []byte) {}, hc), home, relay, id)
	_, _ = first.PutFrame(t.Context(), nil)
	if _, err := second.PutFrame(t.Context(), nil); !errors.Is(err, wireauth.ErrStorageOff) || calls != 1 {
		t.Fatalf("refusal was not remembered: %v requests=%d", err, calls)
	}
}

func TestStorageOptInVaultEditsWaitAndUploadAfterApproval(t *testing.T) {
	home := t.TempDir()
	t.Setenv(URLVar, ProductionRelayURL)
	id, err := identity.Ensure(home)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(home)
	if err != nil {
		t.Fatal(err)
	}
	vault, err := keys.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err = vault.Put("project/pending", keys.Entry{Name: "pending", Value: "throwaway-test-value", Scope: "project"}); err != nil {
		t.Fatal(err)
	}
	dir := directory.NewMemory(time.Now)
	defer dir.Close()
	s := &Sync{Home: home, Relay: ProductionRelayURL, Identity: id, Device: dev, Store: blobstore.NewMemory(), Dir: dir.For(dev.ID())}
	if err = s.SyncState(t.Context()); err != nil {
		t.Fatal(err)
	}
	listing, err := s.Dir.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if listing.Identity.Vault != "" || s.stateSent != "" {
		t.Fatal("pending vault was treated as sent while off")
	}
	if err = SaveStorage(home, ProductionRelayURL, id.ID(), "on"); err != nil {
		t.Fatal(err)
	}
	if err = s.SyncState(t.Context()); err != nil {
		t.Fatal(err)
	}
	listing, err = s.Dir.List(t.Context())
	if err != nil || listing.Identity.Vault == "" {
		t.Fatalf("pending vault did not upload after approval: %+v %v", listing.Identity, err)
	}
}

func TestStorageOptInDefaultPortsAndCase(t *testing.T) {
	home := t.TempDir()
	for _, relay := range []string{ProductionRelayURL, StagingRelayURL, DevRelayURL} {
		u, _ := url.Parse(relay)
		u.Host = strings.ToUpper(u.Host) + ":443"
		if !IsHostedRelay(u.String()) || StorageState(home, u.String(), "account") != "off" {
			t.Fatalf("unknown default-port hosted relay is open: %s", u)
		}
		if err := SaveStorage(home, relay, "account", "on"); err != nil {
			t.Fatal(err)
		}
		if StorageState(home, u.String(), "account") != "on" {
			t.Fatalf("cache binding differs for %s", u)
		}
	}
	if !IsHostedRelay("http://CODEAF.AGENTFIELD.AI:80/fabric") || IsHostedRelay("https://codeaf.agentfield.ai:444/fabric") {
		t.Fatal("port normalization")
	}
}

func TestStorageOptInRotationBindingRelay(t *testing.T) {
	for _, relay := range []string{ProductionRelayURL, StagingRelayURL, DevRelayURL} {
		raw := strings.Replace(relay, "https://", "HTTPS://", 1)
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		u.Host = strings.ToUpper(u.Host) + ":443"
		u.Path += "/"
		s := &Sync{Relay: u.String()}
		env := s.Rotation(cellstore.Engine{}, nil, "", nil)
		if env.HostedRelay != relay {
			t.Fatalf("binding relay %q, want %q", env.HostedRelay, relay)
		}
	}
	for _, relay := range []string{"http://own.test:8787", "https://own.test"} {
		s := &Sync{Relay: relay}
		if env := s.Rotation(cellstore.Engine{}, nil, "", nil); env.HostedRelay != "" {
			t.Fatalf("self-hosted binding %q", env.HostedRelay)
		}
	}
}

func TestStorageOptInLegacyCustomOnCacheDoesNotInventCapability(t *testing.T) {
	home := t.TempDir()
	const relay = "http://127.0.0.1:18831"
	const id = "id_account"
	raw := `{"relay":"http://127.0.0.1:18831","identity":"id_account","storage":"on"}`
	if err := os.WriteFile(filepath.Join(home, StorageFile), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if StorageGated(home, relay, id) || StorageState(home, relay, id) != "on" || !StorageNeedsRefresh(home, relay, id) {
		t.Fatal("legacy on cache cannot prove a custom relay reported storage")
	}
	if err := SaveStorage(home, relay, id, "on"); err != nil {
		t.Fatal(err)
	}
	if !StorageGated(home, relay, id) || StorageNeedsRefresh(home, relay, id) {
		t.Fatal("explicit on answer must be gating and idle")
	}
}

func TestStorageOptInRequestLinkUsesPublicWording(t *testing.T) {
	const id = "id_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	link, err := url.Parse(StorageRequestLink(id))
	if err != nil {
		t.Fatal(err)
	}
	query := link.Query()
	if query.Get("title") != "sync storage access: "+id || query.Get("body") != "Please turn on sync storage for my account.\n\nRequest code: "+id {
		t.Fatalf("request wording: %v", query)
	}
}
