package executor

import (
	"context"
	"syscall"
	"testing"
	"time"
)

func TestDataDirFlagTable(t *testing.T) {
	cases := []struct {
		name, root, dir string
		argv            []string
		want            string
	}{
		{"postgres", "/w", "", []string{"postgres", "-D", "pgdata"}, "pgdata"},
		{"postgres", "/w", "db", []string{"postgres", "-D", "data"}, "db/data"},
		{"postgres", "/w", "", []string{"postgres", "-D", "/w/x/pg"}, "x/pg"},
		{"postgres", "/w", "", []string{"postgres", "-D", "/var/lib/pg"}, ""},
		{"postgres", "/w", "", []string{"postgres", "-D", "../pg"}, ""},
		{"redis-server", "/w", "", []string{"redis-server", "--dir", "r"}, "r"},
		{"mysqld", "/w", "", []string{"mysqld", "--datadir=my"}, "my"},
		{"mongod", "/w", "", []string{"mongod", "--dbpath", "m"}, "m"},
		{"postgres", "/w", "", []string{"postgres", "-p", "5432"}, ""},
		{"python3", "/w", "", []string{"python3", "-D", "x"}, ""},
	}
	for _, c := range cases {
		if got := dataDir(c.name, c.argv, c.root, c.dir); got != c.want {
			t.Errorf("%v in %q: got %q want %q", c.argv, c.dir, got, c.want)
		}
	}
}

func TestSnapshotExactIffNoServices(t *testing.T) {
	if !SnapshotExact(ExecResult{}) || SnapshotExact(ExecResult{Services: []Service{{PGID: 1}}}) {
		t.Fatal("exact must mean no services")
	}
}

func TestLocalRecordsAListeningServiceFully(t *testing.T) {
	script := "exec python3 -c 'import socket,time;s=socket.socket();s.bind((\"127.0.0.1\",0));s.listen();print(s.getsockname()[1],flush=True);time.sleep(60)' >port.txt 2>&1 </dev/null &"
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
	// The listener may bind just after the call returned; poll the record.
	deadline := time.Now().Add(5 * time.Second)
	for len(s.Ports) == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		s = describe(sh(script), root, s.PGID, membersOf(s.PGID))
	}
	if len(s.Ports) != 1 || s.Ports[0] == 0 {
		t.Fatalf("port not recorded: %+v", s)
	}
}
