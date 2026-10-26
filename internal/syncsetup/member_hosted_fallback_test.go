package syncsetup

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
)

type hostedFallbackState struct {
	Leaders       []string         `json:"leaders"`
	Errors        []string         `json:"errors"`
	Leader        uint64           `json:"leader"`
	Cluster       string           `json:"cluster"`
	Configuration cs.Configuration `json:"configuration"`
	Membership    cs.Membership    `json:"membership"`
	Campaigns     int              `json:"campaigns"`
}

func hostedFallbackTicks(t *testing.T, s *Sync, ticks int, missing string) hostedFallbackState {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"op": "leadership-ticks", "identity": s.Identity.ID(), "ticks": ticks, "missing": missing})
	mustNil(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, hostedLearnerEndpoint(t)+"/fixture", bytes.NewReader(raw))
	mustNil(t, err)
	resp, err := http.DefaultClient.Do(req)
	mustNil(t, err)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("R7 virtual maintenance: %s", resp.Status)
	}
	var out hostedFallbackState
	body, err := io.ReadAll(resp.Body)
	mustNil(t, err)
	mustNil(t, cs.Decode(body, &out))
	return out
}
func hostedLeaderDeparted(t *testing.T, s *Sync) {
	t.Helper()
	deadline := time.NewTimer(requestTimeout)
	defer deadline.Stop()
	retry := time.NewTicker(10 * time.Millisecond)
	defer retry.Stop()
	for {
		state := hostedQuietControl(t, hostedLearnerEndpoint(t), s.Identity.ID(), "quiet-state")
		counts := hostedSocketCounts(t, state)
		if counts["peer"] == 0 && counts["forward"] == 0 {
			return
		}
		select {
		case <-retry.C:
		case <-deadline.C:
			t.Fatal("R7 leader sockets did not close")
		}
	}
}

// R7 requires surviving anchors to elect by their own timers and restore the
// standby without contacting the absent personal voter for permission.
func TestR7HostedPersonalLeaderFallbackAndNoAutomaticRegain(t *testing.T) {
	r := newHostedLeadRig(t)
	p, ok, err := Open(machine(t, r.server.URL))
	mustNil(t, err)
	if !ok {
		t.Fatal("open")
	}
	stop := hostedOwner(t, p)
	receipt := hostedPersonalLeader(t, p)
	saved := recordOf(t, p)
	stop()
	hostedLeaderDeparted(t, p)
	before := hostedQuietControl(t, hostedLearnerEndpoint(t), p.Identity.ID(), "metrics")
	state := hostedFallbackTicks(t, p, 13, "")
	if state.Leader == 0 || state.Leader == receipt.Node || !slices.Equal(state.Configuration.Voters, []uint64{1, 2, 3}) || state.Configuration.Joint() || state.Membership.Leadership != nil || !slices.Contains(state.Configuration.Learners, receipt.Node) {
		t.Fatalf("R7 fallback did not restore anchors within thirteen ticks: %+v", state)
	}
	if uint64(state.Campaigns) != hostedNumber(t, before, "campaigns") {
		t.Fatal("R7 fallback used an explicit campaign")
	}
	const cell = "01J0000000000000000000FAL"
	_, err = p.Dir.Create(t.Context(), cell, directory.CellInit{Head: strings.Repeat("1", 64)})
	mustNil(t, err)
	_, err = p.Dir.Acquire(t.Context(), cell, directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: p.Device.ID(), Fence: 1}})
	mustNil(t, err)
	hostedOwner(t, p)
	hostedReady(t, p)
	if nodeOf(p) != receipt.Node || recordOf(t, p).Store != saved.Store {
		t.Fatal("R7 return replaced durable identity")
	}
	status, err := p.clusterClient().Status(t.Context())
	mustNil(t, err)
	if status.Leader == receipt.Node || !slices.Contains(status.Configuration.Learners, receipt.Node) {
		t.Fatal("R7 returning computer regained leadership", status)
	}
	again := hostedPersonalLeader(t, p)
	if again.Node != receipt.Node {
		t.Fatal("R7 explicit lead changed the returning node")
	}
}

// R8 keeps the existing cluster unavailable when two of its three voters are
// absent, even though a nonvoting anchor retains a complete learning copy.
func TestR8HostedPersonalLeaderAndAnchorLossNeverCreatesSecondCluster(t *testing.T) {
	r := newHostedLeadRig(t)
	p, ok, err := Open(machine(t, r.server.URL))
	mustNil(t, err)
	if !ok {
		t.Fatal("open")
	}
	stop := hostedOwner(t, p)
	receipt := hostedPersonalLeader(t, p)
	before, err := p.clusterClient().Status(t.Context())
	mustNil(t, err)
	stop()
	hostedLeaderDeparted(t, p)
	state := hostedFallbackTicks(t, p, 13, "1")
	if state.Cluster != before.Cluster || !slices.Equal(state.Configuration.Voters, []uint64{1, 2, receipt.Node}) || state.Membership.Leadership == nil || state.Membership.Leadership.Restore {
		t.Fatal("R8 minority rewrote its cluster", state)
	}
	_, err = p.clusterClient().Status(t.Context())
	if err == nil || err.Error() != cs.LeadershipUnavailable {
		t.Fatal("R8 minority status refusal", err)
	}
	_, err = p.Dir.Create(t.Context(), "01J0000000000000000000MIN", directory.CellInit{Head: strings.Repeat("1", 64)})
	if err == nil {
		t.Fatal("R8 minority accepted a mutation")
	}
	after := hostedFallbackTicks(t, p, 1, "")
	if after.Cluster != state.Cluster || !slices.Equal(after.Configuration.Voters, state.Configuration.Voters) {
		t.Fatal("R8 minority manufactured a majority", after)
	}
}

// R7 kills a real personal member process without graceful shutdown. The
// surviving quorum must restore the standby before that process can return.
func TestR7HostedPersonalLeaderProcessDeathRestoresAnchors(t *testing.T) {
	r := newHostedLeadRig(t)
	p, ok, err := Open(machine(t, r.server.URL))
	mustNil(t, err)
	if !ok {
		t.Fatal("open")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestR7HostedPersonalMemberProcessHelper$", "-test.v", "-test.timeout=15m")
	cmd.Env = append(os.Environ(), "CODEAF_TEST_MEMBER_HOME="+p.Home)
	stdout, err := cmd.StdoutPipe()
	mustNil(t, err)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	mustNil(t, cmd.Start())
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	elected := make(chan cs.LeadershipReceipt, 1)
	failure := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			t.Logf("R7 child: %s", line)
			if strings.HasPrefix(line, "R7 elected ") {
				var receipt cs.LeadershipReceipt
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "R7 elected ")), &receipt); err != nil {
					failure <- err
					return
				}
				elected <- receipt
				return
			}
		}
		failure <- fmt.Errorf("R7 process exited: %v", scanner.Err())
	}()
	var receipt cs.LeadershipReceipt
	select {
	case receipt = <-elected:
	case err := <-failure:
		t.Fatal(err, stderr.String())
	case <-time.After(requestTimeout):
		t.Fatal("R7 process never led", stderr.String())
	}
	status, err := p.clusterClient().Status(t.Context())
	mustNil(t, err)
	if status.Leader != receipt.Node || receipt.Barrier == 0 {
		t.Fatal("R7 process was not leader", status)
	}
	saved := recordOf(t, p)
	mustNil(t, cmd.Process.Kill())
	if cmd.Wait() == nil {
		t.Fatal("R7 process exited gracefully")
	}
	hostedLeaderDeparted(t, p)
	before := hostedQuietControl(t, hostedLearnerEndpoint(t), p.Identity.ID(), "metrics")
	state := hostedFallbackTicks(t, p, 13, "")
	if state.Leader == 0 || state.Leader == receipt.Node || !slices.Equal(state.Configuration.Voters, []uint64{1, 2, 3}) || state.Configuration.Joint() || state.Membership.Leadership != nil || uint64(state.Campaigns) != hostedNumber(t, before, "campaigns") {
		t.Fatal("R7 process death did not restore anchors by ordinary ticks", state)
	}
	const cell = "01J000000000000000000KILL"
	_, err = p.Dir.Create(t.Context(), cell, directory.CellInit{Head: strings.Repeat("1", 64)})
	mustNil(t, err)
	_, err = p.Dir.Acquire(t.Context(), cell, directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: p.Device.ID(), Fence: 1}})
	mustNil(t, err)
	hostedOwner(t, p)
	hostedReady(t, p)
	if nodeOf(p) != receipt.Node || recordOf(t, p).Store != saved.Store {
		t.Fatal("R7 process return changed durable identity")
	}
	status, err = p.clusterClient().Status(t.Context())
	mustNil(t, err)
	if status.Leader == receipt.Node || !slices.Contains(status.Configuration.Learners, receipt.Node) {
		t.Fatal("R7 killed process automatically regained leadership", status)
	}
	p.memberMu.Lock()
	m := p.member
	m.mu.Lock()
	sequence := m.sequence
	m.mu.Unlock()
	p.memberMu.Unlock()
	if sequence < saved.SequenceCeiling {
		t.Fatal("R7 process reused a reserved transport sequence")
	}
}

// R7 keeps the child under the same RunState owner as an open terminal. Its
// parent kills this address space after observing the actual elected receipt.
func TestR7HostedPersonalMemberProcessHelper(t *testing.T) {
	home := os.Getenv("CODEAF_TEST_MEMBER_HOME")
	if home == "" {
		return
	}
	p, ok, err := Open(home)
	mustNil(t, err)
	if !ok {
		t.Fatal("open")
	}
	hostedOwner(t, p)
	receipt := hostedPersonalLeader(t, p)
	raw, err := json.Marshal(receipt)
	mustNil(t, err)
	fmt.Println("R7 elected " + string(raw))
	select {}
}

// R26 requires a first cold read to use a real majority without interrupting
// the connected personal leader or discarding its durable routing progress.
func TestR26HostedPersonalLeaderColdReadKeepsQuorum(t *testing.T) {
	r := newHostedLeadRig(t)
	p := r.home(nil)
	receipt := hostedPersonalLeader(t, p)
	id := p.Identity
	b := r.home(&id)
	idle := hostedQuietControl(t, hostedLearnerEndpoint(t), id.ID(), "idle-schedule")
	if string(idle["sleeping"]) != "true" {
		t.Fatal("R26 group did not sleep", idle)
	}
	hostedQuietControl(t, hostedLearnerEndpoint(t), id.ID(), "evict-idle")
	_, err := b.Dir.List(t.Context())
	if err != nil {
		t.Logf("cold routing: %s", hostedQuietControl(t, hostedLearnerEndpoint(t), id.ID(), "quiet-state"))
	}
	mustNil(t, err)
	status, err := b.clusterClient().Status(t.Context())
	mustNil(t, err)
	if status.Leader != receipt.Node || !slices.Equal(status.Configuration.Voters, []uint64{1, 2, receipt.Node}) {
		t.Fatal("R26 cold read interrupted personal leadership", status)
	}
}

// R7/R26 a sleeping leader's departure must wake a cold coordinator's real
// follower clocks even while initialization cannot obtain an authority read.
func TestR7R26HostedColdSleepingLeaderDepartureRestoresAnchors(t *testing.T) {
	r := newHostedLeadRig(t)
	p, ok, err := Open(machine(t, r.server.URL))
	mustNil(t, err)
	if !ok {
		t.Fatal("open")
	}
	tick := hostedTicks(t, p)
	hostedReady(t, p)
	receipt := hostedPersonalLeader(t, p)
	tick()
	hostedSettled(t, p)
	idle := hostedQuietControl(t, hostedLearnerEndpoint(t), p.Identity.ID(), "idle-schedule")
	if string(idle["sleeping"]) != "true" {
		t.Fatal("R26 group did not sleep", idle)
	}
	hostedQuietControl(t, hostedLearnerEndpoint(t), p.Identity.ID(), "evict-idle")
	p.stopMember()
	hostedLeaderDeparted(t, p)
	before := hostedQuietControl(t, hostedLearnerEndpoint(t), p.Identity.ID(), "metrics")
	state := hostedFallbackTicks(t, p, 13, "")
	if state.Leader == 0 || state.Leader == receipt.Node || state.Membership.Leadership != nil || state.Configuration.Joint() || !slices.Equal(state.Configuration.Voters, []uint64{1, 2, 3}) || uint64(state.Campaigns) != hostedNumber(t, before, "campaigns") {
		t.Fatal("R7 R26 cold departure did not restore anchors through election timers", state)
	}
	_, err = p.Dir.Create(t.Context(), "01J00000000000000000COLD", directory.CellInit{Head: strings.Repeat("1", 64)})
	mustNil(t, err)
	_, err = p.Dir.Acquire(t.Context(), "01J00000000000000000COLD", directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: p.Device.ID(), Fence: 1}})
	mustNil(t, err)
}
