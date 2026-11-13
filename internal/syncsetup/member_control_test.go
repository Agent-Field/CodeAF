package syncsetup

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

type unavailableControl struct {
	self uint64
	t    *testing.T
}

func (b unavailableControl) Status(context.Context, string) (clusterctl.Status, error) {
	return clusterctl.Status{Cluster: "native", Self: b.self, LeadershipUnavailable: true}, nil
}
func (b unavailableControl) Lead(context.Context, cs.Command) (cs.LeadershipReceipt, error) {
	b.t.Fatal("R6 an unavailable leadership door received a proposal")
	return cs.LeadershipReceipt{}, clusterctl.ErrUnsupported
}

// R6/R23 refuse before saving a new request when the authenticated relay says
// this home has no durable member or leadership is unavailable. Neither case
// may leave an intent for a later activation to accidentally complete.
func TestR6R23UnavailableLeadershipCannotLeaveNewIntent(t *testing.T) {
	for _, tc := range []struct {
		name string
		self uint64
		want string
	}{
		{"unjoined", 0, "this computer must join the cluster with persistent storage before it can lead"},
		{"joined", 4, clusterctl.ErrUnsupported.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(clusterctl.Handler(func(r *http.Request, b []byte) (string, string, error) { return reqsign.Verify(r, b, time.Now()) }, func(string) (clusterctl.Backend, error) { return unavailableControl{tc.self, t}, nil }))
			defer server.Close()
			home := machine(t, server.URL)
			client, ok, err := Open(home)
			if err != nil || !ok {
				t.Fatal(err)
			}
			_, err = client.LeadCluster(context.Background(), 0)
			if err == nil || err.Error() != tc.want {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(home, "cluster-leadership-"+client.Device.ID()+".json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("R6 refused lead saved an unfinishable request: %v", err)
			}
			t.Setenv(URLVar, "off")
			if _, err := client.ClusterStatus(context.Background()); !errors.Is(err, ErrQuiet) {
				t.Fatalf("R23 retained status handle bypassed quiet: %v", err)
			}
		})
	}
}

// R6/R30 keep the hosted refusal's exact words at the real client boundary.
func TestR6R30AbsentHostedControlKeepsLeadershipSentence(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client, ok, err := Open(machine(t, server.URL))
	if err != nil || !ok {
		t.Fatal(err)
	}
	if _, err := client.ClusterStatus(context.Background()); !errors.Is(err, clusterctl.ErrUnsupported) || err.Error() != clusterctl.ErrUnsupported.Error() {
		t.Fatal(err)
	}
	if _, err := client.LeadCluster(context.Background(), 0); !errors.Is(err, clusterctl.ErrUnsupported) {
		t.Fatal(err)
	}
}
