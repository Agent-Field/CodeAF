//go:build linux

package executor

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// cmdline is a process's argv, or nil when it cannot be read.
func cmdline(pid int) []string {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil || len(raw) == 0 {
		return nil
	}
	return strings.Split(string(bytes.TrimRight(raw, "\x00")), "\x00")
}

// listenPorts maps each member's sockets to LISTEN ports. The table is read
// through the member itself, so a jailed process's own network namespace is
// the one asked.
func listenPorts(pids []int) []int {
	seen := map[int]bool{}
	for _, pid := range pids {
		listening := listeningInodes(pid)
		for inode := range socketInodes(pid) {
			if port, ok := listening[inode]; ok {
				seen[port] = true
			}
		}
	}
	ports := make([]int, 0, len(seen))
	for p := range seen {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	return ports
}

func listeningInodes(pid int) map[uint64]int {
	out := map[uint64]int{}
	for _, table := range []string{"tcp", "tcp6"} {
		raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/net/" + table)
		if err == nil {
			for inode, port := range parseListening(string(raw)) {
				out[inode] = port
			}
		}
	}
	return out
}

func socketInodes(pid int) map[uint64]bool {
	dir := "/proc/" + strconv.Itoa(pid) + "/fd"
	entries, _ := os.ReadDir(dir)
	out := map[uint64]bool{}
	for _, e := range entries {
		if link, err := os.Readlink(filepath.Join(dir, e.Name())); err == nil {
			if inode, ok := parseSocketLink(link); ok {
				out[inode] = true
			}
		}
	}
	return out
}

// parseSocketLink reads "socket:[123]".
func parseSocketLink(link string) (uint64, bool) {
	s, ok := strings.CutPrefix(link, "socket:[")
	if !ok {
		return 0, false
	}
	inode, err := strconv.ParseUint(strings.TrimSuffix(s, "]"), 10, 64)
	return inode, err == nil
}

// tcpListen is the LISTEN state code in /proc/net/tcp.
const tcpListen = "0A"

// parseListening reads /proc/net/tcp{,6} text into inode -> local port for
// sockets in LISTEN state. Columns: sl local rem st ... inode (index 9).
func parseListening(text string) map[uint64]int {
	out := map[uint64]int{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 10 || f[3] != tcpListen {
			continue
		}
		_, hexPort, ok := cutLast(f[1], ":")
		port, perr := strconv.ParseUint(hexPort, 16, 32)
		inode, ierr := strconv.ParseUint(f[9], 10, 64)
		if ok && perr == nil && ierr == nil {
			out[inode] = int(port)
		}
	}
	return out
}

func cutLast(s, sep string) (string, string, bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}
