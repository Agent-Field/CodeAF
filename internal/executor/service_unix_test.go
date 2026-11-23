//go:build linux || darwin

// K6 keeps process-group service inspection on Linux and macOS.
package executor

import (
	"context"
	"syscall"
	"testing"
)

func TestLocalRecordsAListeningServiceFully(t *testing.T) {
	// The launching shell waits (at most five seconds) until the server has
	// written the port it listens on, so the call returns with the server
	// exec'd and listening: the record the call itself returns is asserted,
	// on macOS too, where a backgrounded child can otherwise still be the
	// forked shell when the call ends.
	script := "exec python3 -c 'import socket,time;s=socket.socket();s.bind((\"127.0.0.1\",0));s.listen();print(s.getsockname()[1],flush=True);time.sleep(60)' >port.txt 2>&1 </dev/null & " +
		"i=0; while [ ! -s port.txt ] && [ $i -lt 100 ]; do sleep 0.05; i=$((i+1)); done"
	root := t.TempDir()
	res, err := Local{Root: root}.Exec(context.Background(), sh(script), nil)
	if err != nil || len(res.Services) != 1 {
		t.Fatalf("want one service, got %+v err=%v", res, err)
	}
	s := res.Services[0]
	t.Cleanup(func() { _ = syscall.Kill(-s.PGID, syscall.SIGKILL) })
	if s.Name != "python3" || len(s.Argv) < 3 || s.Argv[1] != "-c" {
		t.Fatalf("name/argv not recorded: %+v", s)
	}
	if len(s.Ports) != 1 || s.Ports[0] == 0 {
		t.Fatalf("port not recorded: %+v", s)
	}
}
