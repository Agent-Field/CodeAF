//go:build linux || darwin

// K6 keeps process-group service inspection on Linux and macOS.
package executor

import (
	"context"
	"syscall"
	"testing"
	"time"
)

func TestLocalRecordsAListeningServiceFully(t *testing.T) {
	script := "exec python3 -c 'import socket,time;s=socket.socket();s.bind((\"127.0.0.1\",0));s.listen();print(s.getsockname()[1],flush=True);time.sleep(60)' >port.txt 2>&1 </dev/null &"
	root := t.TempDir()
	res, err := Local{Root: root}.Exec(context.Background(), sh(script), nil)
	if err != nil || len(res.Services) != 1 {
		t.Fatalf("want one service, got %+v err=%v", res, err)
	}
	s := res.Services[0]
	t.Cleanup(func() { _ = syscall.Kill(-s.PGID, syscall.SIGKILL) })
	// The call can return before the backgrounded shell has exec'd python3
	// (macOS loses that race most times) and before the listener binds, so the
	// record is polled until it names the server and its port.
	deadline := time.Now().Add(5 * time.Second)
	for (s.Name != "python3" || len(s.Ports) == 0) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		s = describe(sh(script), root, s.PGID, membersOf(s.PGID))
	}
	if s.Name != "python3" || len(s.Argv) < 3 || s.Argv[1] != "-c" {
		t.Fatalf("name/argv not recorded: %+v", s)
	}
	if len(s.Ports) != 1 || s.Ports[0] == 0 {
		t.Fatalf("port not recorded: %+v", s)
	}
}
