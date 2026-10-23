package executor

import (
	"testing"
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
