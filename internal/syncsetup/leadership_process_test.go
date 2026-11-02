package syncsetup

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// R7's helper runs the production member in a separate address space so the
// parent can kill it without any graceful close or sleep notification.
func TestR7PersonalMemberProcessHelper(t *testing.T) {
	home := os.Getenv("CODEAF_TEST_MEMBER_HOME")
	if home == "" {
		return
	}
	celltest.Available(t)
	s, ok, err := Open(home)
	mustNil(t, err)
	if !ok {
		t.Fatal("R7 member home unavailable")
	}
	readyMember(t, s)
	receipt, err := s.LeadCluster(t.Context(), 0)
	mustNil(t, err)
	raw, err := json.Marshal(receipt)
	mustNil(t, err)
	fmt.Println("R7 elected " + string(raw))
	// The parent's SIGKILL is the only exit. The ordinary package deadline still
	// bounds a failed parent; there is no test-created service or daemon.
	select {}
}

// T4/R7 kills the ACTUAL personal process after election and its read receipt.
// Surviving A+B perform a new conditional operation and restore C with P dead.
func TestR7PersonalLeaderProcessDeathRestoresAnchors(t *testing.T) {
	r := newMemberRig(t)
	s := r.home(nil)
	cmd := exec.Command(os.Args[0], "-test.run=^TestR7PersonalMemberProcessHelper$", "-test.v", "-test.timeout=15m")
	cmd.Env = append(os.Environ(), "CODEAF_TEST_MEMBER_HOME="+s.Home)
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
			if strings.HasPrefix(line, "R7 elected ") {
				var receipt cs.LeadershipReceipt
				err := json.Unmarshal([]byte(strings.TrimPrefix(line, "R7 elected ")), &receipt)
				if err != nil {
					failure <- err
					return
				}
				elected <- receipt
				return
			}
		}
		failure <- fmt.Errorf("R7 child exited before election: %v", scanner.Err())
	}()
	var receipt cs.LeadershipReceipt
	select {
	case receipt = <-elected:
	case err := <-failure:
		t.Fatal(err)
	case <-time.After(requestTimeout):
		t.Fatal("R7 child did not finish leadership")
	}
	before, err := s.clusterClient().Status(t.Context())
	mustNil(t, err)
	if before.Leader != receipt.Node || receipt.Barrier == 0 {
		t.Fatal("R7 child was not elected", before, receipt)
	}
	saved := recordOf(t, s)
	mustNil(t, cmd.Process.Kill())
	if err = cmd.Wait(); err == nil {
		t.Fatal("R7 child exited gracefully")
	}
	for tick := 0; tick < 13; tick++ {
		r.clock.Advance(time.Duration(cs.TickEveryMillis) * time.Millisecond)
	}
	status, err := s.clusterClient().Status(t.Context())
	mustNil(t, err)
	if status.Leader == receipt.Node || status.Leader == 0 {
		t.Fatal("R7 anchors did not elect after SIGKILL", status, stderr.String())
	}
	const cell = "01J000000000000000000KILL"
	_, err = s.Dir.Create(t.Context(), cell, directory.CellInit{Head: strings.Repeat("1", 64)})
	mustNil(t, err)
	_, err = s.Dir.Acquire(t.Context(), cell, directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: s.Device.ID(), Fence: 1}})
	mustNil(t, err)
	for tick := 0; tick < 8; tick++ {
		r.clock.Advance(time.Duration(cs.TickEveryMillis) * time.Millisecond)
	}
	status, err = s.clusterClient().Status(t.Context())
	mustNil(t, err)
	if status.Membership.Leadership != nil || !slices.Equal(status.Configuration.Voters, []uint64{1, 2, 3}) {
		t.Fatal("R7 C not restored with dead P", status)
	}
	readyMember(t, s)
	if s.member.sequence < saved.SequenceCeiling {
		t.Fatal("R6/R7 restart reused a transport sequence")
	}
	if nodeOf(s) != receipt.Node {
		t.Fatal("R7 returning durable process received another identity")
	}
	status, err = s.clusterClient().Status(t.Context())
	mustNil(t, err)
	if status.Leader == receipt.Node || !slices.Contains(status.Configuration.Learners, receipt.Node) {
		t.Fatal("R7 dead process regained its vote or leadership", status)
	}
}
