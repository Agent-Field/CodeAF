//go:build linux

package executor

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The contract, in the words of a person whose chat shares a private network
// with the organization's other services:
//
//	P1-0c  a command in my chat cannot connect to a service on a TCP port the
//	       operator did not allow, and the refusal says so in one plain
//	       sentence; a port the operator allowed works as it always did. Where
//	       the kernel cannot keep a chat to a list of ports the chat's commands
//	       are refused rather than run with the whole network. A fence nobody
//	       required, or one with no list, keeps the network it always had, and
//	       what is handed to the helper is unchanged.

func TestP1_0c_ParseConnectPorts(t *testing.T) {
	got, err := ParseConnectPorts("443, 80,22  9418,80,\n8080")
	if err != nil || len(got) != 5 || got[0] != 22 || got[1] != 80 || got[2] != 443 || got[3] != 8080 || got[4] != 9418 {
		t.Fatalf("a list as an operator writes it: %v, %v", got, err)
	}
	if got, err := ParseConnectPorts(""); err != nil || len(got) != 0 {
		t.Fatalf("empty text is an empty list: %v, %v", got, err)
	}
	for _, bad := range []string{"0", "65536", "-1", "http", "443;80", "0443", "1.5", "443,,x"} {
		if _, err := ParseConnectPorts(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	long := make([]string, MaxConnectPorts+1)
	for i := range long {
		long[i] = strconv.Itoa(1000 + i)
	}
	if _, err := ParseConnectPorts(strings.Join(long, ",")); err == nil {
		t.Fatal("a list longer than a person could read was accepted")
	}
}

func TestP1_0c_OnlyARequiredFenceWithAListLimitsPorts(t *testing.T) {
	for _, tc := range []struct {
		name        string
		fence       Fence
		abi         int
		wantHandled uint64
		wantOK      bool
	}{
		{"a laptop's fence keeps the network", Fence{ConnectPorts: []int{443}}, 7, 0, true},
		{"a required fence with no list keeps the network", Fence{Require: true}, 7, 0, true},
		{"a required fence with no list on an old kernel is unchanged", Fence{Require: true}, 3, 0, true},
		{"a required fence with a list limits connects", Fence{Require: true, ConnectPorts: []int{443}}, 4, unix.LANDLOCK_ACCESS_NET_CONNECT_TCP, true},
		{"and on every later version", Fence{Require: true, ConnectPorts: []int{443}}, 7, unix.LANDLOCK_ACCESS_NET_CONNECT_TCP, true},
		{"a required fence with a list is refused where the kernel cannot", Fence{Require: true, ConnectPorts: []int{443}}, 3, 0, false},
		{"and where there is no Landlock at all", Fence{Require: true, ConnectPorts: []int{443}}, 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handled, ok := fenceNet(&tc.fence, tc.abi)
			if handled != tc.wantHandled || ok != tc.wantOK {
				t.Fatalf("handled %#x ok %v, want %#x %v", handled, ok, tc.wantHandled, tc.wantOK)
			}
		})
	}
}

func TestP1_0c_SpecIsUnchangedWithoutAList(t *testing.T) {
	raw, err := json.Marshal(fenceSpec{Fence: Fence{Root: "/r", Require: true}, Mode: fenceFull})
	if err != nil || strings.Contains(string(raw), "ConnectPorts") {
		t.Fatalf("a fence with no list changed the helper's spec: %s, %v", raw, err)
	}
	raw, _ = json.Marshal(fenceSpec{Fence: Fence{Root: "/r", Require: true, ConnectPorts: []int{443}}, Mode: fenceFull})
	if !strings.Contains(string(raw), `"ConnectPorts":[443]`) {
		t.Fatalf("a list did not reach the helper: %s", raw)
	}
}

func TestP1_0c_RefusalIsOnePlainSentence(t *testing.T) {
	for _, word := range []string{"uid", "sandbox", "sessionhost", "fence", "landlock", "daemon", "bearer", "token", "proxy"} {
		if strings.Contains(strings.ToLower(FenceConnectLine), word) {
			t.Errorf("the refusal says %q, which a person reading a chat should never meet: %q", word, FenceConnectLine)
		}
	}
	out := WithFenceLine("curl: (7) Failed to connect: Permission denied")
	if !strings.HasSuffix(out, FenceLine) {
		t.Fatalf("a fence with no list explains a refusal as it always did: %q", out)
	}
	if got := (&Fence{Require: true, ConnectPorts: []int{443}}).noteLine(); got != FenceConnectLine {
		t.Fatalf("a chat with a port list is told %q", got)
	}
	if got := (&Fence{Require: true}).noteLine(); got != FenceLine {
		t.Fatalf("a chat with no list is told %q", got)
	}
}

// listenLoopback answers a loopback TCP listener that accepts and closes.
func listenLoopback(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback listener: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	return l.Addr().(*net.TCPAddr).Port
}

// TestP1_0c_CommandsReachOnlyTheAllowedPorts is the contract on the real
// kernel: one command through the fenced seat connects to an allowed port and
// to a port nobody allowed, and only the first gets through.
func TestP1_0c_CommandsReachOnlyTheAllowedPorts(t *testing.T) {
	if landlockABI() < netConnectABI {
		t.Skip("this kernel cannot limit TCP connects by port (needs Landlock ABI 4)")
	}
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("no bash to open a TCP connection with")
	}
	if fenceLimit() == FenceDegradedLine {
		t.Skip("this system cannot apply any of the fence")
	}
	allowed, blocked := listenLoopback(t), listenLoopback(t)
	w := newFenceWorld(t, FenceAllow{})
	w.fence.Require = true
	w.fence.ConnectPorts = []int{allowed}
	runner := FencedSeat(w.fence).In(w.root)
	connect := func(port int) (ExecResult, error) {
		return runner.Exec(context.Background(), ExecRequest{
			Argv: []string{"/bin/bash", "-c", "exec 3<>/dev/tcp/127.0.0.1/" + strconv.Itoa(port) + " && echo connected"},
			Env:  append(os.Environ(), "HOME="+w.home),
		}, nil)
	}
	res, err := connect(allowed)
	if err != nil || res.Exit != 0 || !strings.Contains(string(res.Stdout), "connected") {
		t.Fatalf("a port the operator allowed was refused: err %v exit %d stdout %q stderr %q", err, res.Exit, res.Stdout, res.Stderr)
	}
	res, err = connect(blocked)
	if err != nil || res.Exit == 0 || strings.Contains(string(res.Stdout), "connected") {
		t.Fatalf("a port nobody allowed was reached: err %v exit %d stdout %q stderr %q", err, res.Exit, res.Stdout, res.Stderr)
	}
	if !strings.Contains(string(res.Stderr), FenceConnectLine) {
		t.Fatalf("the refusal was not explained in the one plain sentence: %q", res.Stderr)
	}
}

// TestP1_0c_NoListKeepsTheWholeNetwork is the other half of the contract: the
// same required fence with no list reaches every port, as it always did.
func TestP1_0c_NoListKeepsTheWholeNetwork(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("no bash to open a TCP connection with")
	}
	if fenceLimit() == FenceDegradedLine {
		t.Skip("this system cannot apply any of the fence")
	}
	port := listenLoopback(t)
	w := newFenceWorld(t, FenceAllow{})
	w.fence.Require = true
	res, err := FencedSeat(w.fence).In(w.root).Exec(context.Background(), ExecRequest{
		Argv: []string{"/bin/bash", "-c", "exec 3<>/dev/tcp/127.0.0.1/" + strconv.Itoa(port) + " && echo connected"},
		Env:  append(os.Environ(), "HOME="+w.home),
	}, nil)
	if err != nil || res.Exit != 0 || !strings.Contains(string(res.Stdout), "connected") {
		t.Fatalf("a required fence with no list lost the network: err %v exit %d stderr %q", err, res.Exit, res.Stderr)
	}
}

// TestP1_0c_ARefusedCommandsNoteNamesWhatTheChatMayReach is the sentence a
// person reads in the chat's own transcript: for a chat limited to some ports it
// is the connect sentence, for every other fence it is the old one, and never
// both.
func TestP1_0c_ARefusedCommandsNoteNamesWhatTheChatMayReach(t *testing.T) {
	refused := "/bin/bash: connect: Permission denied"
	limited := &Fence{Require: true, ConnectPorts: []int{443}}
	if got := limited.WithNote(refused); !strings.HasSuffix(got, FenceConnectLine) || strings.Contains(got, FenceLine) {
		t.Errorf("a chat limited to ports was told: %q", got)
	}
	if got := (&Fence{}).WithNote(refused); !strings.HasSuffix(got, FenceLine) || strings.Contains(got, FenceConnectLine) {
		t.Errorf("a fence with no port limit was told: %q", got)
	}
	once := limited.WithNote(refused)
	if limited.WithNote(once) != once {
		t.Error("the note was added twice")
	}
	if got := limited.WithNote("build failed: syntax error"); got != "build failed: syntax error" {
		t.Errorf("a command that was not refused was annotated: %q", got)
	}
	var none *Fence
	if got := none.WithNote(refused); !strings.HasSuffix(got, FenceLine) {
		t.Errorf("no fence at all: %q", got)
	}
}
