//go:build linux

package cellstore_test

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/furrowbin"
)

type idleClient struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

func startIdleClient(t *testing.T, socket, binary string, app bool) *idleClient {
	t.Helper()
	fixture := os.Getenv("CODEAF_TEST_IDLE_CLIENT")
	if fixture == "" {
		fixture = filepath.Join(t.TempDir(), "idle-client")
		build := exec.Command("go", "build", "-o", fixture, "./testdata/daemon-lifetime")
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build lifetime client: %v\n%s", err, out)
		}
	}
	cmd := exec.Command(fixture, fmt.Sprintf("-app=%v", app))
	cmd.Env = append(os.Environ(), "CODEAF_TEST_IDLE_SOCKET="+socket, "CODEAF_TEST_IDLE_BINARY="+binary)
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := &idleClient{cmd: cmd, in: in, out: bufio.NewReader(out)}
	t.Cleanup(func() { in.Close(); _ = cmd.Wait() })
	if line, err := c.out.ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("client ready: %q, %v", line, err)
	}
	return c
}

func (c *idleClient) command(t *testing.T, command string) string {
	t.Helper()
	if _, err := fmt.Fprintln(c.in, command); err != nil {
		t.Fatal(err)
	}
	line, err := c.out.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(line)
}

func pinnedIdleBinary(t *testing.T) string {
	t.Helper()
	binary, err := furrowbin.Ensure()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	want := furrowbin.ReadPin().Artifacts[runtime.GOOS+"-"+runtime.GOARCH].SHA256
	if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != want {
		t.Fatalf("furrow checksum %s, want pinned %s", got, want)
	}
	return binary
}

func idleSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "idle-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "e.sock")
}

func assertIdleFlag(t *testing.T, pid, minutes string) {
	t.Helper()
	args, err := os.ReadFile("/proc/" + pid + "/cmdline")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "--idle-minutes\x00"+minutes+"\x00") {
		t.Fatalf("helper arguments %q, want --idle-minutes %s", args, minutes)
	}
	// Only this fixture's verified helper can be killed during failure cleanup.
	processID, err := strconv.Atoi(pid)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		current, _ := os.ReadFile("/proc/" + pid + "/cmdline")
		if string(current) == string(args) {
			p, _ := os.FindProcess(processID)
			_ = p.Kill()
		}
	})
}

func TestDesktopSavingHelperLifetime(t *testing.T) {
	if testing.Short() {
		t.Skip("real one-minute idle exit")
	}
	binary := pinnedIdleBinary(t)
	for _, killed := range []bool{false, true} {
		t.Run(fmt.Sprintf("killed=%v", killed), func(t *testing.T) {
			t.Parallel()
			socket := idleSocket(t)
			client := startIdleClient(t, socket, binary, true)
			pid := client.command(t, "health")
			assertIdleFlag(t, pid, "1")
			time.Sleep(70 * time.Second)
			if got := client.command(t, "health"); got != pid {
				t.Fatalf("quiet app restarted helper: pid %s became %s", pid, got)
			}
			if killed {
				if err := client.cmd.Process.Kill(); err != nil {
					t.Fatal(err)
				}
			} else if got := client.command(t, "quiet"); got != "quiet" {
				t.Fatal(got)
			}
			stopped := time.Now()
			deadline := stopped.Add(75 * time.Second)
			for {
				args, err := os.ReadFile("/proc/" + pid + "/cmdline")
				if os.IsNotExist(err) || (err == nil && len(args) == 0) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if time.Now().After(deadline) {
					t.Fatal("helper outlived touching by 75 seconds without Stop")
				}
				time.Sleep(time.Second)
			}
			if time.Since(stopped) < 45*time.Second {
				t.Fatal("helper closed before its idle limit")
			}
			t.Logf("same pid %s survived quiet app; idle exit after %.1fs without Stop", pid, time.Since(stopped).Seconds())
		})
	}
}

func TestDesktopTouchNeverStartsHelper(t *testing.T) {
	if testing.Short() {
		t.Skip("real twenty-second touch interval")
	}
	binary := pinnedIdleBinary(t)
	socket := idleSocket(t)
	client := startIdleClient(t, socket, binary, true)
	time.Sleep(25 * time.Second)
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("touch started absent helper: %v", err)
	}
	pid := client.command(t, "health")
	assertIdleFlag(t, pid, "1")
	args, err := os.ReadFile("/proc/" + pid + "/cmdline")
	if err != nil || !strings.Contains(string(args), socket) {
		t.Fatalf("helper identity: %q %v", args, err)
	}
	n, _ := strconv.Atoi(pid)
	p, _ := os.FindProcess(n)
	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(25 * time.Second)
	if conn, err := net.DialTimeout("unix", socket, time.Second); err == nil {
		conn.Close()
		t.Fatal("touch restarted a dead helper")
	}
}

func TestTerminalSavingHelperKeepsTenMinuteIdle(t *testing.T) {
	binary := pinnedIdleBinary(t)
	socket := idleSocket(t)
	terminal := startIdleClient(t, socket, binary, false)
	pid := terminal.command(t, "health")
	assertIdleFlag(t, pid, "10")
	app := startIdleClient(t, socket, binary, true)
	if got := app.command(t, "health"); got != pid {
		t.Fatalf("app replaced terminal helper %s with %s", pid, got)
	}
	assertIdleFlag(t, pid, "10")
}
