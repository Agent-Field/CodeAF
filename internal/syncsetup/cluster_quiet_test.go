package syncsetup

import (
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/rafttransport"
)

type clusterWireProbe struct{ calls int }

func (p *clusterWireProbe) RoundTrip(r *http.Request) (*http.Response, error) {
	p.calls++
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"generation":"1","messages":[]}`)), Request: r}, nil
}

// R23 intercepts the actual peer client at the wire, before DNS, including a
// handle opened while solo and one retained after the person turns sync off.
func TestR23ClusterWireChecksQuietAtEveryUse(t *testing.T) {
	home := t.TempDir()
	if _, err := identity.EnsureSolo(home); err != nil {
		t.Fatal(err)
	}
	probe := &clusterWireProbe{}
	s := &Sync{Home: home}
	client := s.ClusterHTTP(&http.Client{Transport: probe})
	peer := rafttransport.Client{URL: "http://sync.invalid", HTTP: client}
	req := rafttransport.Request{Scope: rafttransport.Scope{Identity: "id", Cluster: "cluster", Epoch: 1, Protocol: 1}, Node: 1, Incarnation: 1, Generation: 1}
	if err := req.Sign(ed25519.NewKeyFromSeed(make([]byte, 32))); err != nil {
		t.Fatal(err)
	}
	if _, err := peer.Exchange(context.Background(), req); !errors.Is(err, ErrQuiet) {
		t.Fatal("R23 solo exchange", err)
	}
	for _, path := range []string{"/v2/cluster/enroll", "/v2/cluster/status", "/v2/cluster/socket"} {
		r, _ := http.NewRequest("GET", "http://sync.invalid"+path, nil)
		if _, err := client.Do(r); !errors.Is(err, ErrQuiet) {
			t.Fatal(err)
		}
	}
	if probe.calls != 0 {
		t.Fatal("R23 solo reached transport")
	}
	// The existing paired fixture supplies a real saved personal identity.
	paired := machine(t, "http://sync.invalid")
	s.Home = paired
	existing := s.ClusterHTTP(&http.Client{Transport: probe})
	peer.HTTP = existing
	if _, err := peer.Exchange(context.Background(), req); err != nil {
		t.Fatal("R23 active fixture", err)
	}
	n := probe.calls
	t.Setenv(URLVar, "off")
	if _, err := peer.Exchange(context.Background(), req); !errors.Is(err, ErrQuiet) {
		t.Fatal("R23 retained handle after off", err)
	}
	if probe.calls != n {
		t.Fatal("R23 off reached transport")
	}
}

type clusterStreamProbe struct{ pings, reads, closed int }

func (p *clusterStreamProbe) Next(context.Context) (dirwatch.Frame, error) {
	p.reads++
	return dirwatch.Frame{Pong: true}, nil
}
func (p *clusterStreamProbe) Ping(context.Context) error { p.pings++; return nil }
func (p *clusterStreamProbe) Vouching() bool             { return true }
func (p *clusterStreamProbe) Close()                     { p.closed++ }

func TestR23ExistingWatchStopsSendingAfterOff(t *testing.T) {
	home := machine(t, "http://sync.invalid")
	probe := &clusterStreamProbe{}
	stream := quietStream{Stream: probe, home: home}
	if err := stream.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Setenv(URLVar, "off")
	if err := stream.Ping(context.Background()); !errors.Is(err, ErrQuiet) {
		t.Fatal(err)
	}
	if _, err := stream.Next(context.Background()); !errors.Is(err, ErrQuiet) {
		t.Fatal(err)
	}
	if probe.pings != 1 || probe.reads != 0 || probe.closed == 0 || stream.Vouching() {
		t.Fatal("R23 retained watch bypassed off", probe)
	}
}
