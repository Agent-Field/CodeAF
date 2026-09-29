package executor

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// services reports the process group when something in it outlived the leader.
// A call that was killed leaves none.
func services(ctx context.Context, cmd *exec.Cmd, req ExecRequest, root string) []Service {
	pgid := cmd.Process.Pid
	if ctx.Err() != nil || !processgroup.Alive(pgid) {
		return nil
	}
	return []Service{describe(req, root, pgid, processgroup.Members(pgid))}
}

// describe fills a service record from its surviving members. The first member
// names it; the ports of all members are its ports.
func describe(req ExecRequest, root string, pgid int, members []int) Service {
	argv := req.Argv
	if len(members) > 0 {
		if a := cmdline(members[0]); len(a) > 0 {
			argv = a
		}
	}
	name := filepath.Base(argv[0])
	return Service{
		Name: name, Argv: argv, PGID: pgid,
		Ports:   listenPorts(members),
		DataDir: dataDir(name, argv, root, req.Dir),
	}
}

// dataDirFlags is the table of data-directory flags by executable name.
var dataDirFlags = map[string][]string{
	"postgres":     {"-D", "--pgdata"},
	"pg_ctl":       {"-D", "--pgdata"},
	"redis-server": {"--dir"},
	"mysqld":       {"--datadir"},
	"mariadbd":     {"--datadir"},
	"mongod":       {"--dbpath"},
	"etcd":         {"--data-dir"},
	"clickhouse":   {"--path"},
}

// dataDir finds the value of the tool's data-directory flag and returns it
// relative to the workspace root; a directory outside the root is dropped.
func dataDir(name string, argv []string, root, dir string) string {
	for _, flag := range dataDirFlags[name] {
		if v, ok := flagValue(argv, flag); ok {
			return inside(root, dir, v)
		}
	}
	return ""
}

// flagValue reads "flag value" and "flag=value" forms.
func flagValue(argv []string, flag string) (string, bool) {
	for i, a := range argv {
		if a == flag && i+1 < len(argv) {
			return argv[i+1], true
		}
		if v, ok := strings.CutPrefix(a, flag+"="); ok && strings.HasPrefix(flag, "--") {
			return v, true
		}
	}
	return "", false
}

// inside makes path root-relative: a relative path is taken from the call's
// directory, an absolute one from the root. Empty when it leaves the root.
func inside(root, dir, path string) string {
	abs := path
	if !filepath.IsAbs(path) {
		abs = filepath.Join(root, dir, path)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return ""
	}
	return filepath.ToSlash(rel)
}
