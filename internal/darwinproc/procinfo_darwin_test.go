//go:build darwin

package darwinproc

import (
	"encoding/binary"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"syscall"
	"testing"
	"time"
)

func TestDarwinProcArgsStopsBeforeEnvironment(t *testing.T) {
	rawArgs := func(argc int32, body string) []byte {
		raw := make([]byte, 4, 4+len(body))
		binary.LittleEndian.PutUint32(raw, uint32(argc))
		return append(raw, body...)
	}
	for _, tc := range []struct {
		name string
		raw  []byte
		want []string
	}{
		{"padding and environment", rawArgs(4, "/bin/tool\x00\x00\x00tool\x00\x00two words\x00last\x00PRIVATE=value\x00"), []string{"tool", "", "two words", "last"}},
		{"zero", rawArgs(0, "/bin/tool\x00tool\x00"), nil},
		{"negative", rawArgs(-1, "/bin/tool\x00tool\x00"), nil},
		{"absurd", rawArgs(1<<30, "/bin/tool\x00tool\x00"), nil},
		{"missing argument", rawArgs(2, "/bin/tool\x00tool\x00"), nil},
		{"truncated argument", rawArgs(1, "/bin/tool\x00tool"), nil},
		{"truncated executable", rawArgs(1, "/bin/tool"), nil},
		{"short header", []byte{1, 0, 0}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseProcArgs(tc.raw); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("argv = %q, want %q", got, tc.want)
			}
		})
	}
}

func sleepChild(t testing.TB) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return cmd
}

func TestDarwinArgvReadsALiveChild(t *testing.T) {
	cmd := sleepChild(t)
	if got := Argv(cmd.Process.Pid); !reflect.DeepEqual(got, []string{"sleep", "30"}) {
		t.Fatalf("argv = %q", got)
	}
}

func TestDarwinTaskMatchesSelf(t *testing.T) {
	deadline := time.Now().Add(150 * time.Millisecond)
	var work uint64
	for time.Now().Before(deadline) {
		for range 10000 {
			work = work*1664525 + 1013904223
		}
	}
	if work == 0 {
		t.Log("bounded loop finished at zero")
	}
	task, err := Task(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if task.PID != os.Getpid() || task.PPID != os.Getppid() {
		t.Fatalf("identity = %+v", task)
	}
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		t.Fatal(err)
	}
	seconds := float64(usage.Utime.Sec+usage.Stime.Sec) + float64(usage.Utime.Usec+usage.Stime.Usec)/1e6
	if math.Abs(task.CPUSeconds-seconds) > math.Max(seconds*.2, .03) {
		t.Fatalf("CPU seconds = %f, getrusage = %f", task.CPUSeconds, seconds)
	}
	gone := exec.Command("true")
	gone.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	if _, err := Task(gone.Process.Pid); err == nil {
		t.Fatal("gone process returned a task")
	}
}

func TestDarwinChildrenListsASpawnedChild(t *testing.T) {
	cmd := sleepChild(t)
	pids, err := Children(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(pids, cmd.Process.Pid) {
		t.Fatalf("children %v omit %d", pids, cmd.Process.Pid)
	}
}

func TestDarwinListeningPorts(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	want := []int{listener.Addr().(*net.TCPAddr).Port}
	listener6, err := net.Listen("tcp6", "[::1]:0")
	if err == nil {
		t.Cleanup(func() { _ = listener6.Close() })
		want = append(want, listener6.Addr().(*net.TCPAddr).Port)
	} else {
		t.Logf("IPv6 listener unavailable: %v", err)
	}
	conn, err := net.DialTimeout("tcp4", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = listener.(*net.TCPListener).SetDeadline(time.Now().Add(time.Second))
	accepted, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = accepted.Close() })
	udp, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = udp.Close() })
	// A short directory keeps the socket path within macOS's sockaddr_un limit.
	socketDir, err := os.MkdirTemp("", "dp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	local, err := net.Listen("unix", filepath.Join(socketDir, "s"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = local.Close() })
	got := ListenPorts(os.Getpid())
	for _, p := range want {
		if !slices.Contains(got, p) {
			t.Fatalf("ports %v omit listener %d", got, p)
		}
	}
	for _, p := range []int{conn.LocalAddr().(*net.TCPAddr).Port, udp.LocalAddr().(*net.UDPAddr).Port} {
		// A UDP or ephemeral TCP port can coincide with an independently allocated listener.
		if !slices.Contains(want, p) && slices.Contains(got, p) {
			t.Fatalf("ports %v include non-listener %d", got, p)
		}
	}
	if !slices.IsSorted(got) || len(slices.Compact(slices.Clone(got))) != len(got) {
		t.Fatalf("ports not sorted and unique: %v", got)
	}
}

func BenchmarkTask(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Task(os.Getpid()); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkChildren(b *testing.B) {
	_ = sleepChild(b)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Children(os.Getpid()); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkListenPorts(b *testing.B) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = listener.Close() })
	b.ReportAllocs()
	for b.Loop() {
		_ = ListenPorts(os.Getpid())
	}
}
func BenchmarkArgv(b *testing.B) {
	cmd := sleepChild(b)
	b.ReportAllocs()
	for b.Loop() {
		if len(Argv(cmd.Process.Pid)) == 0 {
			b.Fatal("argv unavailable")
		}
	}
}
