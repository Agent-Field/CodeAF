package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

type personalStatusFixture struct {
	clusterFixture
	status clusterctl.Status
}

func (f personalStatusFixture) ClusterStatus(context.Context) (clusterctl.Status, error) {
	return f.status, nil
}

// H1/R9 makes the real status command expose this home's committed learner
// and readiness, with the three anchors still the only voting computers.
func TestR9ClusterStatusShowsPersonalLearner(t *testing.T) {
	fixture := personalStatusFixture{status: clusterctl.Status{Self: 4, Leader: 1, Configuration: cs.Configuration{Voters: []uint64{1, 2, 3}, Learners: []uint64{4, 5}}, Ready: []uint64{4}}}
	var out bytes.Buffer
	if err := clusterCommand(t.Context(), fixture, "status", &out); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"coordinator 1\n", "this computer 4\n", "voting computers [1 2 3]\n", "learning computers [4 5]\n", "ready computers [4]\n"} {
		if !strings.Contains(out.String(), line) {
			t.Errorf("R9 status omitted %q: %s", line, out.String())
		}
	}
	out.Reset()
	fixture.status = clusterctl.Status{}
	if err := clusterCommand(t.Context(), fixture, "status", &out); err != nil || out.Len() != 0 {
		t.Fatal("R9 unknown status did not remain absent", err, out.String())
	}
}
