package relayserve_test

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

// R9/R30 expose the actual committed anchor topology without manufacturing a
// personal node from a pairing certificate. R6 refuses before proposing an
// unsupported transfer, leaving both registry intent and voters unchanged.
func TestR6R9R30NativeControlReportsCommittedAnchorsAndRefusesLead(t *testing.T) {
	r := newRig(t)
	_, a := newIdentity(t)
	client := clusterctl.Client{Base: r.srv.URL, Sign: reqsign.SignFor(a, r.clock.Now), HTTP: r.srv.Client()}
	status, err := client.Status(bg)
	if err != nil {
		t.Fatal(err)
	}
	if status.Cluster == "" || status.Leader == 0 || status.Self != 0 || status.Membership.Pending != nil || !status.LeadershipUnavailable || !reflect.DeepEqual(status.Configuration.Voters, []uint64{1, 2, 3}) || len(status.Configuration.Learners) != 0 {
		t.Fatalf("R9 status invented a personal member or changed the default: %+v", status)
	}
	c := cs.Command{Schema: cs.SchemaVersion, Cluster: status.Cluster, Actor: a.dev.ID(), Session: "leadership", Sequence: 1, Now: r.clock.Now().UnixMilli(), Change: cs.Mutation{RequestLeadership: &cs.RequestLeadership{Revision: status.Membership.Revision, Node: 4, Device: a.dev.ID()}}}
	c.RequestDigest, err = cs.Digest(c)
	if err != nil {
		t.Fatal(err)
	}
	b, err := cs.SigningBytes(c)
	if err != nil {
		t.Fatal(err)
	}
	c.Signature = a.dev.Sign(b)
	if _, err = client.Lead(bg, c); !errors.Is(err, clusterctl.ErrUnsupported) || err.Error() != "this relay does not offer cluster leadership yet; your saved chats are still here" {
		t.Fatalf("R6 unsupported lead: %v", err)
	}
	after, err := client.Status(bg)
	if err != nil || !reflect.DeepEqual(after, status) {
		t.Fatalf("R6 refusal changed committed membership: before=%+v after=%+v err=%v", status, after, err)
	}
}

// R16 checks the production control door with its real certificate and then
// the same key after committed removal. A cached status cannot vouch it back in.
func TestR16R30NativeControlRequiresCurrentPersonalAuthorization(t *testing.T) {
	r := newRig(t)
	id, a := newIdentity(t)
	b := newDevice(t, id, t.TempDir())
	response, err := http.Get(r.srv.URL + clusterctl.Path)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("unsigned status: %d", response.StatusCode)
	}
	client := clusterctl.Client{Base: r.srv.URL, Sign: reqsign.SignFor(b, r.clock.Now), HTTP: r.srv.Client()}
	if _, err := client.Status(bg); err != nil {
		t.Fatal(err)
	}
	if err := r.dir(b).PutDevice(bg, b.dev.ID(), directory.Device{V: 1, Name: "c2VhbGVk"}); err != nil {
		t.Fatal(err)
	}
	if err := r.dir(a).Revoke(bg, b.dev.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Status(bg); err == nil {
		t.Fatal("R16 revoked certificate read cached cluster status")
	}
}

// R4/R30 require the status door to lose service with quorum, even when its
// previous answer and the remaining anchor's committed registry are intact.
func TestR4R30NativeControlCannotServeCachedStatusWithoutQuorum(t *testing.T) {
	r := newRig(t)
	id, a := newIdentity(t)
	client := func() clusterctl.Client {
		return clusterctl.Client{Base: r.srv.URL, Sign: reqsign.SignFor(a, r.clock.Now), HTTP: r.srv.Client()}
	}
	before, err := client().Status(bg)
	if err != nil {
		t.Fatal(err)
	}
	r.stop()
	path := filepath.Join(r.store, "raft", id.ID())
	for _, node := range []string{"2", "3"} {
		if err := os.Rename(filepath.Join(path, node+".db"), filepath.Join(path, node+".asleep")); err != nil {
			t.Fatal(err)
		}
	}
	r.start()
	if _, err := client().Status(bg); err == nil {
		t.Fatal("R4 cached status answered without a majority")
	}
	r.stop()
	if err := os.Rename(filepath.Join(path, "2.asleep"), filepath.Join(path, "2.db")); err != nil {
		t.Fatal(err)
	}
	r.start()
	after, err := client().Status(bg)
	if err != nil || before.Cluster != after.Cluster || len(after.Configuration.Voters) != 3 {
		t.Fatalf("R4 surviving majority did not recover the original topology: %+v %v", after, err)
	}
}
