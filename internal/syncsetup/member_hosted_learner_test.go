package syncsetup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

func hostedLearnerEndpoint(t *testing.T) string {
	t.Helper()
	endpoint := os.Getenv("CODEAF_HOSTED_PROOF_URL")
	if endpoint == "" {
		t.Skip("local hosted proof runner only")
	}
	u, e := url.Parse(endpoint)
	if e != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" {
		t.Fatal("hosted proof requires loopback HTTP")
	}
	return endpoint
}
func hostedOwner(t *testing.T, s *Sync) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { s.RunState(ctx); close(done) }()
	stop := func() {
		cancel()
		select {
		case <-done:
		case <-time.After(requestTimeout):
			t.Error("R9 terminal owner did not stop")
		}
	}
	t.Cleanup(stop)
	deadline := time.Now().Add(requestTimeout)
	for time.Now().Before(deadline) {
		s.memberMu.Lock()
		m := s.member
		var e error
		ready := false
		if m != nil {
			m.mu.Lock()
			e = m.failure
			ready = m.conn != nil && m.forwardConn != nil && m.status.Applied >= m.record.Grant.Barrier
			m.mu.Unlock()
		}
		s.memberMu.Unlock()
		if e != nil {
			t.Fatalf("R9 hosted member failed: %v", e)
		}
		if ready {
			return stop
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("R9 RunState did not join the hosted cluster")
	return stop
}
func hostedReady(t *testing.T, s *Sync) uint64 {
	t.Helper()
	deadline := time.Now().Add(requestTimeout)
	for time.Now().Before(deadline) {
		status, e := s.clusterClient().Status(t.Context())
		mustNil(t, e)
		if len(status.Configuration.Voters) != 3 || !status.LeadershipUnavailable {
			t.Fatalf("R9 hosted voter/leadership policy: %+v", status)
		}
		if status.Self != 0 && slices.Contains(status.Ready, status.Self) && slices.Contains(status.Configuration.Learners, status.Self) {
			return status.Self
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("R9 member was not ready after durable application")
	return 0
}

// R9/R23 exercise the terminal's real lifetime, store reuse and wiped-store
// retirement through the production Worker, without a client-side substitute.
func TestR9R23HostedLearnerRunStateReopenAndWipe(t *testing.T) {
	endpoint := hostedLearnerEndpoint(t)
	home := machine(t, endpoint)
	a, ok, e := Open(home)
	mustNil(t, e)
	if !ok {
		t.Fatal("open")
	}
	stop := hostedOwner(t, a)
	node := hostedReady(t, a)
	record := recordOf(t, a)
	other := machine(t, endpoint)
	_, e = identity.Adopt(other, a.Identity, true)
	mustNil(t, e)
	b, ok, e := Open(other)
	mustNil(t, e)
	if !ok {
		t.Fatal("second home")
	}
	stopB := hostedOwner(t, b)
	otherNode := hostedReady(t, b)
	if node == otherNode {
		t.Fatal("R9 two stores share a node")
	}
	status, e := b.clusterClient().Status(t.Context())
	mustNil(t, e)
	if !slices.Contains(status.Configuration.Learners, node) || !slices.Contains(status.Configuration.Learners, otherNode) {
		t.Fatal("R9 both homes must remain learners")
	}
	_, e = a.LeadCluster(t.Context(), 0)
	if !errors.Is(e, clusterctl.ErrUnsupported) {
		t.Fatalf("R9 leadership refusal: %v", e)
	}
	if _, e = os.Stat(filepath.Join(home, "cluster-leadership-"+a.Device.ID()+".json")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("R9 unsupported lead wrote an intent", e)
	}
	stop()
	reopened, _, e := Open(home)
	mustNil(t, e)
	stopAgain := hostedOwner(t, reopened)
	if hostedReady(t, reopened) != node || recordOf(t, reopened).Store != record.Store {
		t.Fatal("R9 reopen changed durable identity")
	}
	stopAgain()
	mustNil(t, os.RemoveAll(reopened.memberPath()))
	wiped, _, e := Open(home)
	mustNil(t, e)
	hostedOwner(t, wiped)
	replacement := hostedReady(t, wiped)
	if replacement <= node || recordOf(t, wiped).Store == record.Store {
		t.Fatal("R9 wiped store reused authority")
	}
	status, e = wiped.clusterClient().Status(t.Context())
	mustNil(t, e)
	if slices.Contains(status.Configuration.Learners, node) {
		t.Fatal("R9 old learner was not retired")
	}
	stopB()
}

// R23 requires zero attempts for off and never-paired homes, including the
// same owner that starts peer and application sockets on paired homes.
func TestR23HostedQuietHomesMakeNoClusterRequests(t *testing.T) {
	endpoint := hostedLearnerEndpoint(t)
	for _, off := range []bool{false, true} {
		t.Run(map[bool]string{false: "unpaired", true: "off"}[off], func(t *testing.T) {
			home := t.TempDir()
			_, e := identity.EnsureSolo(home)
			mustNil(t, e)
			t.Setenv(URLVar, endpoint)
			if off {
				_, e = identity.Ensure(home)
				mustNil(t, e)
				mustNil(t, identity.EndSolo(home))
			}
			s, ok, e := Open(home)
			mustNil(t, e)
			if !ok {
				t.Fatal("open")
			}
			if off {
				t.Setenv(URLVar, "off")
			}
			for n := 0; n < 3; n++ {
				_, e = s.stateRound(t.Context())
				mustNil(t, e)
			}
			if s.member != nil || s.relayAttempts.Load() != 0 || s.clusterFrames.Load() != 0 {
				t.Fatal("R23 quiet home made network attempts")
			}
			if _, e = os.Stat(s.memberPath()); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("R23 quiet home created a member store")
			}
		})
	}
}

// R9/R23 catch up a real store through multiple bounded chunks, then apply a
// later log entry. Transport credit never substitutes for durable installation.
func TestR9HostedLearnerSnapshotChunksAndLog(t *testing.T) {
	endpoint := hostedLearnerEndpoint(t)
	home := machine(t, endpoint)
	a, ok, e := Open(home)
	mustNil(t, e)
	if !ok {
		t.Fatal("open")
	}
	for n := 0; n < 18; n++ {
		other := machine(t, endpoint)
		_, e = identity.Adopt(other, a.Identity, true)
		mustNil(t, e)
		writer, _, e := Open(other)
		mustNil(t, e)
		mustNil(t, writer.Dir.PutDevice(t.Context(), writer.Device.ID(), directory.Device{V: 1, Name: strings.Repeat("e", 20000)}))
	}
	stop := hostedOwner(t, a)
	node := hostedReady(t, a)
	a.memberMu.Lock()
	m := a.member
	m.mu.Lock()
	recovered, e := m.store.Recover(t.Context())
	m.mu.Unlock()
	a.memberMu.Unlock()
	mustNil(t, e)
	if recovered.Core.Snapshot == nil {
		t.Fatal("R9 joining store received no snapshot")
	}
	manifest, e := raftsnapshot.Decode(recovered.Core.Snapshot.GetData())
	mustNil(t, e)
	if manifest.Chunks < 2 {
		t.Fatalf("R9 expected multiple chunks: %+v", manifest)
	}
	mustNil(t, a.Dir.PutDevice(t.Context(), a.Device.ID(), directory.Device{V: 1, Name: "after snapshot"}))
	waitMember(t, a, func(m *personalMember) bool {
		app, _, err := m.store.ApplicationView(t.Context())
		if err != nil {
			return false
		}
		k := raftstore.Key{Namespace: raftstore.State, Key: cs.DeviceKey(a.Device.ID()).Key}
		rows, _, err := m.store.Read(t.Context(), app.Applied, []raftstore.Key{k})
		var d cs.Device
		return err == nil && cs.Decode(rows[k], &d) == nil && d.Name == "after snapshot"
	})
	if hostedReady(t, a) != node {
		t.Fatal("R9 log catch-up changed node")
	}
	t.Logf("R9 durable hosted snapshot installed: chunks=%d bytes=%d", manifest.Chunks, manifest.Bytes)
	stop()
	// R9/R27 repeatedly overwrite bounded metadata while the learner is absent.
	// The retained log crosses the automatic checkpoint trigger independently
	// of that learner's old progress, without adding application records.
	var latest string
	for n := 0; n < 32; n++ {
		latest = strings.Repeat("p", 20000) + strconv.Itoa(n)
		mustNil(t, a.Dir.PutDevice(t.Context(), a.Device.ID(), directory.Device{V: 1, Name: latest}))
	}
	reopened, _, e := Open(home)
	mustNil(t, e)
	hostedOwner(t, reopened)
	waitMember(t, reopened, func(m *personalMember) bool {
		app, _, err := m.store.ApplicationView(t.Context())
		if err != nil {
			return false
		}
		k := raftstore.Key{Namespace: raftstore.State, Key: cs.DeviceKey(a.Device.ID()).Key}
		rows, _, err := m.store.Read(t.Context(), app.Applied, []raftstore.Key{k})
		var d cs.Device
		return err == nil && cs.Decode(rows[k], &d) == nil && d.Name == latest
	})
	reopened.memberMu.Lock()
	m = reopened.member
	m.mu.Lock()
	returned, e := m.store.Recover(t.Context())
	m.mu.Unlock()
	reopened.memberMu.Unlock()
	mustNil(t, e)
	if returned.Core.Snapshot == nil || returned.Core.Snapshot.GetMetadata().GetIndex() <= manifest.Index {
		t.Fatal("R27 absent learner pinned compaction or did not receive a fresh snapshot")
	}
	if hostedReady(t, reopened) != node {
		t.Fatal("R9 returning learner changed its durable identity")
	}

}

// R9/R27 a legal maximum public command must commit and reach a personal
// learner without increasing the normal peer or chunk byte budgets.
func TestR9R27HostedLearnerMaximumCommand(t *testing.T) {
	endpoint := hostedLearnerEndpoint(t)
	a, ok, e := Open(machine(t, endpoint))
	mustNil(t, e)
	if !ok {
		t.Fatal("open")
	}
	hostedOwner(t, a)
	hostedReady(t, a)
	status, e := a.clusterClient().Status(t.Context())
	mustNil(t, e)
	encodedSize := func(n int) int {
		body, _ := json.Marshal(directory.Device{V: 1, Name: strings.Repeat("x", n)})
		req, e := http.NewRequest(http.MethodPut, a.Relay+"/v1/dir/devices/"+a.Device.ID(), bytes.NewReader(body))
		mustNil(t, e)
		reqsign.Sign(req, body, deviceSigner{a.Identity, a.Device}, a.now())
		proof, e := reqsign.Capture(req, body)
		mustNil(t, e)
		command, e := cs.V1Command(status.Cluster, a.now().UnixMilli(), proof)
		mustNil(t, e)
		raw, e := cs.EncodeCommand(command)
		mustNil(t, e)
		return len(raw)
	}
	low, high := 0, cs.MaxCommandBytes
	for low < high {
		middle := (low + high + 1) / 2
		if encodedSize(middle) <= cs.MaxCommandBytes {
			low = middle
		} else {
			high = middle - 1
		}
	}
	if encodedSize(low) > cs.MaxCommandBytes || encodedSize(low) < cs.MaxCommandBytes-4 {
		t.Fatal("R27 fixture missed the admitted command boundary")
	}
	name := strings.Repeat("x", low)
	mustNil(t, a.Dir.PutDevice(t.Context(), a.Device.ID(), directory.Device{V: 1, Name: name}))
	_, e = a.Dir.List(t.Context())
	mustNil(t, e)
	waitMember(t, a, func(m *personalMember) bool {
		app, _, err := m.store.ApplicationView(t.Context())
		if err != nil {
			return false
		}
		k := raftstore.Key{Namespace: raftstore.State, Key: cs.DeviceKey(a.Device.ID()).Key}
		rows, _, err := m.store.Read(t.Context(), app.Applied, []raftstore.Key{k})
		var d cs.Device
		return err == nil && cs.Decode(rows[k], &d) == nil && d.Name == name
	})
}
