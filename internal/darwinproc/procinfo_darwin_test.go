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

// procArgsFixture lays out a kern.procargs2 answer the way the kernel does:
// argc, the executable path padded with NULs to the next eight-byte boundary,
// then the arguments and the environment.
func procArgsFixture(argc int32, exec string, strings ...string) []byte {
	raw := make([]byte, 4)
	binary.LittleEndian.PutUint32(raw, uint32(argc))
	raw = append(raw, exec...)
	raw = append(raw, 0)
	for (len(raw)-4)%procArgsAlign != 0 {
		raw = append(raw, 0)
	}
	for _, s := range strings {
		raw = append(raw, s...)
		raw = append(raw, 0)
	}
	return raw
}

func TestDarwinProcArgsStopsBeforeEnvironment(t *testing.T) {
	header := func(argc int32, body string) []byte {
		raw := make([]byte, 4, 4+len(body))
		binary.LittleEndian.PutUint32(raw, uint32(argc))
		return append(raw, body...)
	}
	for _, tc := range []struct {
		name string
		raw  []byte
		want []string
	}{
		{"padding and environment", procArgsFixture(4, "/bin/tool", "tool", "", "two words", "last", "PRIVATE=value"), []string{"tool", "", "two words", "last"}},
		{"empty argv[0] keeps the environment out", procArgsFixture(2, "/bin/tool", "", "argument", "PRIVATE=value"), []string{"", "argument"}},
		{"path that fills its boundary", procArgsFixture(1, "/bin/to", "tool", "PRIVATE=value"), []string{"tool"}},
		{"padding that is not NUL", header(1, "/bin/tool\x00\x00tool\x00PRIVATE=value\x00"), nil},
		{"zero", procArgsFixture(0, "/bin/tool", "tool"), nil},
		{"negative", procArgsFixture(-1, "/bin/tool", "tool"), nil},
		{"absurd", procArgsFixture(1<<30, "/bin/tool", "tool"), nil},
		{"missing argument", procArgsFixture(2, "/bin/tool", "tool"), nil},
		{"truncated argument", header(1, "/bin/tool\x00\x00\x00\x00\x00\x00\x00tool"), nil},
		{"truncated padding", header(1, "/bin/tool\x00\x00"), nil},
		{"truncated executable", header(1, "/bin/tool"), nil},
		{"short header", []byte{1, 0, 0}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseProcArgs(tc.raw); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("argv = %q, want %q", got, tc.want)
			}
		})
	}
}

// An empty argv[0] is read in place on a live process, with the environment
// left out, whatever the length of the executable path.
func TestDarwinArgvKeepsAnEmptyFirstArgument(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"s", "sl", "sleeper", "sleeperx"} {
		link := filepath.Join(dir, name)
		if err := os.Symlink("/bin/sleep", link); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(link, "30")
		cmd.Args[0] = ""
		cmd.Env = []string{"PRIVATE=value"}
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		var got []string
		for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
			if got = Argv(cmd.Process.Pid); reflect.DeepEqual(got, []string{"", "30"}) {
				break
			}
		}
		if !reflect.DeepEqual(got, []string{"", "30"}) {
			t.Fatalf("%s: argv = %q, want [\"\" \"30\"]", name, got)
		}
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
