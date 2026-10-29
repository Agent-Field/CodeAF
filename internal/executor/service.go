package executor

import (
	"context"
	"os/exec"
	"path/filepath"
	"slices"
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

// Left is the services a finished tool call left running: each process group it
// started that still has a member. A shell tool starts its process through
// Command, so no [ExecResult] carries these; the call's own account of the
// groups it started is the only place they are known.
func Left(call Call) []Service {
	var out []Service
	for _, pgid := range call.Spawns.Groups() {
		if members := processgroup.Members(pgid); len(members) > 0 {
			out = append(out, describe(ExecRequest{Argv: []string{shellTool}}, "", pgid, members))
		}
	}
	return out
}

// describe fills a service record from its surviving members. The member that
// listens names it, because it is the server; with none listening the first
// member does. The ports of all members are its ports.
func describe(req ExecRequest, root string, pgid int, members []int) Service {
	service := describeWith(req, root, pgid, members, pidPorts, cmdline)
	if len(service.Ports) == 0 {
		// An empty query preserves each platform's unknown-versus-empty result
		// without inspecting any member's sockets again.
		service.Ports = listenPorts(nil)
	}
	return service
}

// describeWith uses one socket snapshot for naming and aggregation so a member
// is not inspected twice while a service is recorded.
func describeWith(req ExecRequest, root string, pgid int, members []int, ports func(int) []int, cmdline func(int) []string) Service {
	allPorts := make([]int, 0)
	naming := 0
	for _, member := range members {
		listening := ports(member)
		if naming == 0 && len(listening) > 0 {
			naming = member
		}
		allPorts = append(allPorts, listening...)
	}
	if naming == 0 && len(members) > 0 {
		naming = members[0]
	}
	slices.Sort(allPorts)
	allPorts = slices.Compact(allPorts)
	argv := req.Argv
	if naming > 0 {
		if a := cmdline(naming); len(a) > 0 {
			argv = a
		}
	}
	name := filepath.Base(argv[0])
	return Service{
		Name: name, Argv: argv, PGID: pgid,
		Ports:   allPorts,
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
