package syncsetup

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"reflect"
	"testing"
	"time"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

func legacyLargeLeaderRequests(t *testing.T, base string, hc *http.Client, p *Sync, clock func() time.Time) {
	t.Helper()
	t.Logf("leader identity %s", p.Identity.ID())
	raw, err := os.ReadFile("../raftdirectory/testdata/v1-large-requests.json")
	mustNil(t, err)
	var f struct {
		Cases []struct{ Name, Method, URI, Body string }
	}
	mustNil(t, json.Unmarshal(raw, &f))
	for _, one := range f.Cases {
		request, err := http.NewRequestWithContext(t.Context(), one.Method, base+one.URI, bytes.NewBufferString(one.Body))
		mustNil(t, err)
		reqsign.Sign(request, []byte(one.Body), deviceSigner{p.Identity, p.Device}, clock())
		started := time.Now()
		response, err := hc.Do(request)
		mustNil(t, err)
		t.Logf("large create %s completed in %s", one.Name, time.Since(started))
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		mustNil(t, err)
		if response.StatusCode != 200 {
			p.member.mu.Lock()
			app, _, _ := p.member.store.ApplicationView(t.Context())
			rows, _, _ := p.member.store.Read(t.Context(), app.Applied, []raftstore.Key{{Namespace: raftstore.State, Key: cs.CellKey(one.Name).Key}, {Namespace: raftstore.Outbox, Key: cs.V1StageKey().Key}})
			t.Logf("leader response state: failure=%v status=%+v cellBytes=%d stageHeader=%s pending=%d", p.member.failure, p.member.status, len(rows[raftstore.Key{Namespace: raftstore.State, Key: cs.CellKey(one.Name).Key}]), rows[raftstore.Key{Namespace: raftstore.Outbox, Key: cs.V1StageKey().Key}], len(p.member.forwardPending))
			p.member.mu.Unlock()

			t.Fatalf("large request through personal leader: %s status %d: %.200s", one.Name, response.StatusCode, body)
		}
		var init directory.CellInit
		var view directory.CellView
		mustNil(t, json.Unmarshal([]byte(one.Body), &init))
		mustNil(t, json.Unmarshal(body, &view))
		if view.Cell.Head != init.Head || view.Cell.Title != init.Title || !reflect.DeepEqual(view.Cell.Frames, init.Frames) || !reflect.DeepEqual(view.Cell.Keys, init.Keys) || view.Cell.Lease.Fence != 1 {
			t.Fatal("forwarded result lost original fields")
		}
		// A small resume also returns the retained large view through the same wire.
		started = time.Now()
		resumed, err := p.Dir.Acquire(t.Context(), one.Name, directory.AcquireOpts{})
		t.Logf("large resume %s completed in %s: %v", one.Name, time.Since(started), err)
		if err != nil {
			if p.member.mu.TryLock() {
				t.Logf("resume member state: failure=%v status=%+v pending=%d sent=%d", p.member.failure, p.member.status, len(p.member.forwardPending), len(p.member.sent))
				for _, work := range p.member.forwardPending {
					t.Logf("pending action=%s barrier=%d applied=%d commandBytes=%d", work.Action, work.Barrier, work.Applied, len(work.Command))
				}
				p.member.mu.Unlock()
			} else {
				t.Log("resume member serialization is busy")
			}
			t.Fatal(err)
		}
		if resumed.Cell.Lease.Fence != 2 || !reflect.DeepEqual(resumed.Cell.Frames, init.Frames) {
			t.Fatal("large retained result was not returned")
		}
		mustNil(t, p.Dir.Release(t.Context(), one.Name, 2))
	}
}

// R25/R6 keep legacy body and reply limits usable when an external core leads.
func TestR25PersonalLeaderLargeLegacyRequests(t *testing.T) {
	r := newMemberRig(t)
	p := r.home(nil)
	personalLeader(t, r, p)
	legacyLargeLeaderRequests(t, r.server.URL, r.server.Client(), p, r.clock.Now)
}

// R25/R6 use the same body and reply assertions through public workerd ingress.
func TestR25HostedPersonalLeaderLargeLegacyRequests(t *testing.T) {
	r := newHostedLeadRig(t)
	p := r.home(nil)
	hostedPersonalLeader(t, p)
	// R6/R25 keep routing through the authenticated personal leader while cold
	// followers have forgotten their volatile leader hint.
	payload, _ := json.Marshal(map[string]string{"op": "forget-follower-hints", "identity": p.Identity.ID()})
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, r.server.URL+"/fixture", bytes.NewReader(payload))
	mustNil(t, err)
	response, err := r.server.Client().Do(request)
	mustNil(t, err)
	var route struct{ Before, After string }
	mustNil(t, json.NewDecoder(response.Body).Decode(&route))
	response.Body.Close()
	if response.StatusCode != 200 || route.Before == "0" || route.After != route.Before {
		t.Fatalf("cold follower lost its authenticated route: %+v", route)
	}
	legacyLargeLeaderRequests(t, r.server.URL, r.server.Client(), p, time.Now)
}
