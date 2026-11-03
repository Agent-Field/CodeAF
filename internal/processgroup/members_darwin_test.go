//go:build darwin

package processgroup

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/darwinproc"
	"golang.org/x/sys/unix"
)

func TestDarwinMembersExcludeZombies(t *testing.T) {
	records := make([]unix.KinfoProc, 6)
	for i, pid := range []int32{30, 10, 20, 40, 0, -1} {
		records[i].Proc.P_pid = pid
		records[i].Proc.P_stat = 3
		records[i].Eproc.Pgid = 100
	}
	records[0].Proc.P_stat = 4
	records[2].Proc.P_stat = darwinproc.StatusZombie
	records[3].Eproc.Pgid = 101
	if got := darwinMemberPIDs(100, records); !reflect.DeepEqual(got, []int{10, 30}) {
		t.Fatalf("Members = %v, want sleeping and stopped members [10 30]", got)
	}
}

func TestDarwinMembersReadsALiveGroup(t *testing.T) {
	cmd := startDarwinGroup(t, "sleep", "30")
	pid := cmd.Process.Pid
	if got := Members(pid); !reflect.DeepEqual(got, []int{pid}) {
		t.Fatalf("Members(%d) = %v, want [%d]", pid, got, pid)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if got := Members(pid); len(got) != 0 {
		t.Fatalf("Members(%d) after wait = %v, want empty", pid, got)
	}
}

func startDarwinGroup(tb testing.TB, name string, args ...string) *exec.Cmd {
	tb.Helper()
	cmd := exec.Command(name, args...)
	Configure(cmd)
	if err := cmd.Start(); err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = Kill(cmd.Process.Pid)
			_ = cmd.Wait()
		}
	})
	return cmd
}

func BenchmarkMembers(b *testing.B) {
	cmd := startDarwinGroup(b, "sh", "-c", strings.Repeat("sleep 30 & ", 9)+"wait")
	waitForDarwinUsage(b, CaptureGroup(cmd.Process.Pid), 10)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if got := Members(cmd.Process.Pid); len(got) != 10 || got[0] != cmd.Process.Pid {
			b.Fatalf("Members = %v, want the leader and nine sleeping children", got)
		}
	}
}
