package syncsetup

import (
	"bytes"
	"context"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStorageOptInFreshServingHomeDiscoversApproval(t *testing.T) {
	celltest.Available(t)
	server := relay(t)
	first := machine(t, server.URL)
	source, ok, err := Open(first)
	if err != nil || !ok {
		t.Fatal(err)
	}
	fresh := t.TempDir()
	if err := identity.Install(fresh, source.Identity, source.Device); err != nil {
		t.Fatal(err)
	}
	t.Setenv(URLVar, ProductionRelayURL)
	t.Setenv(IntervalVar, "20")
	_ = SaveStorage(first, ProductionRelayURL, source.Identity.ID(), "on")
	if StorageForHome(fresh) != "off" {
		t.Fatal("fresh home started open")
	}
	vault, err := keys.Open(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Put("p/result", keys.Entry{Name: "result", Scope: "p", Value: "serving-home-upload"}); err != nil {
		t.Fatal(err)
	}
	original := http.DefaultTransport
	var stats, writes atomic.Int32
	approved := make(chan struct{}, 1)
	http.DefaultTransport = storageTransport(func(r *http.Request) (*http.Response, error) {
		if IsHostedRelay(r.URL.Scheme + "://" + r.URL.Host) {
			var raw []byte
			if r.Body != nil {
				raw, _ = io.ReadAll(r.Body)
				r.Body = io.NopCloser(bytes.NewReader(raw))
			}
			if _, _, err := reqsign.AuthenticateAt(time.Now)(r, raw); err != nil {
				t.Errorf("unsigned request: %v", err)
			}
			if strings.HasSuffix(r.URL.Path, "/v1/store/stats") {
				stats.Add(1)
				select {
				case approved <- struct{}{}:
				default:
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"storage":"on"}`))}, nil
			}
			if strings.HasSuffix(r.URL.Path, "/v1/store/frames") {
				writes.Add(1)
			}
			clone := r.Clone(r.Context())
			u, _ := url.Parse(server.URL)
			clone.URL.Scheme = u.Scheme
			clone.URL.Host = u.Host
			clone.URL.Path = strings.TrimPrefix(clone.URL.Path, "/fabric")
			clone.RequestURI = ""
			reqsign.SignFor(deviceSigner{source.Identity, source.Device}, time.Now)(clone, raw)
			return original.RoundTrip(clone)
		}
		return original.RoundTrip(r)
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); RunHomeState(ctx, fresh, func(err error) { t.Errorf("serving: %v", err) }) }()
	defer func() { cancel(); <-done }()
	select {
	case <-approved:
	case <-time.After(5 * time.Second):
		t.Fatal("headless startup never read approval")
	}
	waitServeState(t, func() bool { return writes.Load() > 0 })
	if stats.Load() != 1 || StorageForHome(fresh) != "on" {
		t.Fatalf("stats=%d permission=%s", stats.Load(), StorageForHome(fresh))
	}
}
