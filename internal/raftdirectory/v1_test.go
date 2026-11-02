package raftdirectory_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftdirectory"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

func (g *group) CommitV1(_ context.Context, c cs.Command) (cs.Result, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.submit(c)
}
func (g *group) ReadV1(ctx context.Context, c cs.Command, committed bool, inspect func(context.Context, *raftstore.SQLite, raftstore.Application) error) error {
	ctx = context.WithoutCancel(ctx)
	g.mu.Lock()
	defer g.mu.Unlock()
	if !committed {
		if c.Change.AdvanceTime == nil {
			return directory.ErrUnauthorized
		}
		r, e := g.submit(c)
		if e != nil {
			return e
		}
		if r.Code == cs.Revoked {
			return directory.ErrRevoked
		}
		if r.Code != cs.OK {
			return directory.ErrUnauthorized
		}
	}
	i := g.leader()
	if i < 0 {
		return directory.ErrUnreachable
	}
	g.readID++
	id := "v1-read-" + time.Unix(int64(g.readID), 0).Format(time.RFC3339Nano)
	g.drive(i, raftcore.Read{RequestID: id})
	g.drain()
	if !g.reads[id] {
		return directory.ErrUnreachable
	}
	app, _, e := g.stores[i].ApplicationView(ctx)
	if e != nil {
		return e
	}
	return inspect(ctx, g.stores[i], app)
}

type v1Fixture struct {
	Name, Method, URI, Body string
	Headers                 map[string]string
}
type v1Fixtures struct {
	Now    int64
	ActorA string `json:"actor_a"`
	ActorB string `json:"actor_b"`
	Cases  []v1Fixture
}

func frozenV1(t *testing.T) v1Fixtures {
	t.Helper()
	raw, e := os.ReadFile("testdata/v1-requests.json")
	must(t, e)
	var f v1Fixtures
	must(t, json.Unmarshal(raw, &f))
	return f
}
func frozenRequest(t *testing.T, f v1Fixture, base string) *http.Request {
	t.Helper()
	r, e := http.NewRequest(f.Method, base+f.URI, strings.NewReader(f.Body))
	must(t, e)
	for k, v := range f.Headers {
		r.Header.Set(k, v)
	}
	return r
}

// R25 uses frozen independently signed v1 requests at the actual HTTP door.
// No test-only device signing or synthetic expectation replaces these bytes.
func TestR25V1FrozenRequestsThroughCommittedHTTP(t *testing.T) {
	f := frozenV1(t)
	clock := func() time.Time { return time.UnixMilli(f.Now) }
	var extra []raftstore.Write
	for _, code := range []string{"1234", "5678"} {
		k := cs.JoinKey(code)
		extra = append(extra, raftstore.Write{Key: raftstore.Key{Namespace: raftstore.Outbox, Key: k.Key}, Value: encoded(t, cs.JoinRequest{Device: f.ActorB, PublicKey: key("p17-b").Public().(ed25519.PublicKey), Expires: f.Now + 90000})})
	}
	g := newGroup(t, clock, extra...)
	d := &raftdirectory.V1Directory{Host: g, Cluster: cluster, Now: clock}
	srv := httptest.NewServer(directory.Handler(reqsign.AuthenticateAt(clock), func(string) (directory.Directory, error) { return d, nil }))
	defer srv.Close()
	for _, one := range f.Cases {
		t.Run(one.Name, func(t *testing.T) {
			r := frozenRequest(t, one, srv.URL)
			resp, e := srv.Client().Do(r)
			must(t, e)
			defer resp.Body.Close()
			raw, e := io.ReadAll(resp.Body)
			must(t, e)
			want := http.StatusOK
			if one.Name == "blind-force" {
				want = http.StatusConflict
			}
			if resp.StatusCode >= 300 && resp.StatusCode != want {
				t.Fatalf("%s: status %d: %s", one.Name, resp.StatusCode, raw)
			}
			if one.Name == "blind-force" && (resp.StatusCode != want || !bytes.Contains(raw, []byte(`"err":"lease_held"`))) {
				t.Fatalf("legacy refusal %d %s", resp.StatusCode, raw)
			}
			if one.Name == "conditional" || one.Name == "publish" || one.Name == "resume" {
				var v directory.CellView
				must(t, json.Unmarshal(raw, &v))
				if one.Name == "resume" && v.Cell.Lease.Fence != 3 {
					t.Fatal(v)
				}
				if one.Name == "conditional" && v.Cell.Lease.Fence != 2 {
					t.Fatal(v)
				}
				if one.Name == "publish" && v.Cell.Head != "head-two" {
					t.Fatal(v)
				}
			}
			if resp.Header.Get(directory.ConditionalTakeHeader) != "1" && one.Name != "blind-force" {
				t.Fatal("conditional promise absent")
			}
		})
	}
	// An old successful request arriving after revoke cannot regain authority.
	for _, one := range f.Cases {
		if one.Name == "cell" {
			resp, e := srv.Client().Do(frozenRequest(t, one, srv.URL))
			must(t, e)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("revoked replay status %d", resp.StatusCode)
			}
		}
	}
	for i := range 3 {
		app, _, e := g.stores[i].ApplicationView(ctx)
		must(t, e)
		rows, _, e := g.stores[i].ScanPage(ctx, app.Applied, raftstore.Sessions, "")
		must(t, e)
		if len(rows) != 0 {
			t.Fatal("v1 request consumed durable v2 sessions")
		}
	}
}

// R16/R25 prove that neither the relay nor another device can rewrite an old
// authenticated proof into a different command after admission.
func TestR16R25V1OriginalProofCannotBeRewritten(t *testing.T) {
	f := frozenV1(t)
	var create v1Fixture
	for _, one := range f.Cases {
		if one.Name == "create" {
			create = one
		}
	}
	p, e := reqsign.Capture(frozenRequest(t, create, "http://localhost"), []byte(create.Body))
	must(t, e)
	c, e := cs.V1Command(cluster, f.Now, p)
	must(t, e)
	id := cs.IdentityRec{V: 1, Identity: cs.IdentityID(key("root").Public().(ed25519.PublicKey)), SigningKey: key("root").Public().(ed25519.PublicKey)}
	if _, ok := cs.V1Authority(c, id); !ok {
		t.Fatal("original request refused")
	}
	if _, ok := cs.V1AuthorityAt(c, id, f.Now+cs.V1SkewMillis+1); ok {
		t.Fatal("stale forwarded timestamp became fresh")
	}
	for _, name := range []string{"mutation", "actor", "identity", "signature", "body", "route", "time", "session"} {
		t.Run(name, func(t *testing.T) {
			raw := encoded(t, c)
			var bad cs.Command
			must(t, cs.Decode(raw, &bad))
			switch name {
			case "mutation":
				bad.Change.Create.Init.Head = "forged"
			case "actor":
				bad.Actor = f.ActorB
			case "identity":
				bad.V1.IdentityKey = key("other").Public().(ed25519.PublicKey)
			case "signature":
				bad.V1.Signature[0] ^= 1
			case "body":
				bad.V1.Body = []byte(`{"head":"forged"}`)
			case "route":
				bad.V1.URI = "/v1/dir/cells/other"
			case "time":
				bad.Now += cs.V1SkewMillis + 1
			case "session":
				bad.Session = "v2"
			}
			if _, ok := cs.V1Authority(bad, id); ok {
				t.Fatal("rewritten proof authorized")
			}
		})
	}
	if cs.ClientCommand(c) == nil {
		t.Fatal("v2 accepted v1 proof")
	}
	for _, m := range []cs.Mutation{{Create: &cs.CreateCell{V1: true}}, {Publish: &cs.PublishCell{V1: true}}, {Acquire: &cs.AcquireCell{Legacy: true}}, {Acquire: &cs.AcquireCell{V1: true}}} {
		if cs.ClientCommand(cs.Command{Change: m}) == nil {
			t.Fatal("v2 compatibility marker accepted")
		}
	}
	for _, body := range []string{`{"head":"h","private_path":"/home/private"}`, `null`, `{"head":"h"} {}`, `{"head":"private","head":"h"}`} {
		p.Body = []byte(body)
		if _, e := p.Mutation(); e == nil {
			t.Fatal("nonrouting body admitted")
		}
	}
}

type fixtureSigner struct {
	private     ed25519.PrivateKey
	certificate identity.Cert
}

func (s fixtureSigner) IdentityKey() ed25519.PublicKey {
	return key("root").Public().(ed25519.PublicKey)
}
func (s fixtureSigner) Cert() identity.Cert    { return s.certificate }
func (s fixtureSigner) Sign(raw []byte) []byte { return ed25519.Sign(s.private, raw) }

// R1/R25 check concurrent current HTTP clients against the same frozen holder
// observation. The losing wire error is the existing sentinel consumed by the
// real continuation screen, whose sentence is pinned by its focused gate.
func TestR1R25V1ConcurrentOriginalObservationsHaveOneWinner(t *testing.T) {
	f := frozenV1(t)
	clock := func() time.Time { return time.UnixMilli(f.Now) }
	g := newGroup(t, clock)
	d := &raftdirectory.V1Directory{Host: g, Cluster: cluster, Now: clock}
	srv := httptest.NewServer(directory.Handler(reqsign.AuthenticateAt(clock), func(string) (directory.Directory, error) { return d, nil }))
	defer srv.Close()
	makeClient := func(name, label string) *directory.HTTP {
		var fixture v1Fixture
		for _, one := range f.Cases {
			if one.Name == name {
				fixture = one
			}
		}
		p, e := reqsign.Capture(frozenRequest(t, fixture, srv.URL), []byte(fixture.Body))
		must(t, e)
		raw, e := base64.RawURLEncoding.DecodeString(p.Certificate)
		must(t, e)
		var cert identity.Cert
		must(t, json.Unmarshal(raw, &cert))
		return directory.NewHTTP(srv.URL, reqsign.SignFor(fixtureSigner{key(label), cert}, clock), srv.Client())
	}
	a, b := makeClient("put-a", "p17-a"), makeClient("put-b", "p17-b")
	_, e := a.Create(ctx, "chat", directory.CellInit{Head: "saved"})
	must(t, e)
	seen := directory.LeaseExpectation{Device: f.ActorA, Fence: 1}
	start := make(chan struct{})
	out := make(chan error, 2)
	var wg sync.WaitGroup
	for _, cl := range []directory.Client{a, b} {
		wg.Add(1)
		go func(cl directory.Client) {
			defer wg.Done()
			<-start
			_, e := cl.Acquire(ctx, "chat", directory.AcquireOpts{Force: true, Expect: &seen})
			out <- e
		}(cl)
	}
	close(start)
	wg.Wait()
	close(out)
	won, lost := 0, 0
	for err := range out {
		if err == nil {
			won++
		} else if errors.Is(err, directory.ErrLeaseHeld) {
			lost++
		} else {
			t.Fatal(err)
		}
	}
	if won != 1 || lost != 1 {
		t.Fatalf("winners %d losers %d", won, lost)
	}
}
