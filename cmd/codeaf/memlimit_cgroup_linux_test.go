//go:build linux

package main

// memlimit_cgroup_linux_test.go holds the cgroup reader down against FAKE FILES,
// never the real hierarchy. On the machine this runs on every level of the
// process's cgroup reads `max`, so the real /sys/fs/cgroup cannot show a bound
// at all; a t.TempDir() standing in for /sys/fs/cgroup and a written stand-in
// for /proc/self/cgroup are the only way the derivation can be seen to work.

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeCgroup writes a stand-in for /proc/self/cgroup holding body, with the
// hierarchy in its own directory, and returns the pair the reader takes: the
// stand-in's path and the hierarchy root.
func fakeCgroup(t *testing.T, body string) (procSelfCgroup, root string) {
	t.Helper()
	procSelfCgroup = filepath.Join(t.TempDir(), "self-cgroup")
	if err := os.WriteFile(procSelfCgroup, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return procSelfCgroup, t.TempDir()
}

// writeBound writes value at root/rel, making the parents the way a cgroup
// hierarchy has them.
func writeBound(t *testing.T, root, rel, value string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}

// No cgroup files at all is not a bound: the reader answers nothing rather than
// a zero-means-limit, so the surface falls back to physical memory untouched.
func TestCgroupMemoryLimitWithoutAnyFilesIsNoBound(t *testing.T) {
	proc, root := fakeCgroup(t, "0::/user.slice/session.scope\n")
	if got := cgroupMemoryLimit(proc, root); got != 0 {
		t.Fatalf("cgroupMemoryLimit with no memory.max = %d, want no bound (0)", got)
	}

	// And a /proc/self/cgroup that cannot be read is no bound either.
	missing := filepath.Join(t.TempDir(), "absent")
	if got := cgroupMemoryLimit(missing, root); got != 0 {
		t.Fatalf("cgroupMemoryLimit with no /proc/self/cgroup = %d, want no bound (0)", got)
	}
}

// The value can be the literal string `max`, meaning no limit. It is not a
// bound, and it must not become a limit.
func TestCgroupMemoryLimitIgnoresMax(t *testing.T) {
	proc, root := fakeCgroup(t, "0::/user.slice/session.scope\n")
	writeBound(t, root, "user.slice/session.scope/memory.max", "max\n")
	if got := cgroupMemoryLimit(proc, root); got != 0 {
		t.Fatalf("cgroupMemoryLimit where memory.max is `max` = %d, want no bound (0)", got)
	}
}

// Under cgroup v1 "no limit" is the huge sentinel, not a word. It is not a bound
// either.
func TestCgroupMemoryLimitIgnoresTheV1Sentinel(t *testing.T) {
	proc, root := fakeCgroup(t, "10:memory:/user.slice\n")
	writeBound(t, root, "memory/user.slice/memory.limit_in_bytes", "9223372036854771712\n")
	if got := cgroupMemoryLimit(proc, root); got != 0 {
		t.Fatalf("cgroupMemoryLimit where the v1 sentinel is the limit = %d, want no bound (0)", got)
	}
}

// A value that will not parse is not a bound, and neither is a value at or below
// zero.
func TestCgroupMemoryLimitIgnoresUnparsableValues(t *testing.T) {
	for _, value := range []string{"not-a-number\n", "0\n", "-1\n", "\n"} {
		proc, root := fakeCgroup(t, "0::/user.slice/session.scope\n")
		writeBound(t, root, "user.slice/session.scope/memory.max", value)
		if got := cgroupMemoryLimit(proc, root); got != 0 {
			t.Errorf("cgroupMemoryLimit where memory.max is %q = %d, want no bound (0)", value, got)
		}
	}
}

// A real number on the process's own cgroup is the bound the reader answers.
func TestCgroupMemoryLimitReadsTheProcessOwnV2Bound(t *testing.T) {
	proc, root := fakeCgroup(t, "0::/user.slice/user-1001.slice/session.scope\n")
	writeBound(t, root, "user.slice/user-1001.slice/session.scope/memory.max", "1073741824\n")
	if got := cgroupMemoryLimit(proc, root); got != 1<<30 {
		t.Fatalf("cgroupMemoryLimit = %d, want the process's own %d", got, 1<<30)
	}
}

// The process's own level can be `max` while a parent slice is bounded, so the
// whole path is walked and the tightest finite bound wins.
func TestCgroupMemoryLimitWalksUpToTheTightestAncestor(t *testing.T) {
	proc, root := fakeCgroup(t, "0::/user.slice/user-1001.slice/session.scope\n")
	writeBound(t, root, "user.slice/user-1001.slice/session.scope/memory.max", "max\n")
	writeBound(t, root, "user.slice/user-1001.slice/memory.max", "4294967296\n") // 4 GiB
	writeBound(t, root, "user.slice/memory.max", "2147483648\n")                 // 2 GiB, the tightest
	if got := cgroupMemoryLimit(proc, root); got != 2<<30 {
		t.Fatalf("cgroupMemoryLimit = %d, want the tightest ancestor %d", got, 2<<30)
	}
}

// A cgroup v1 hierarchy keeps its bound in memory.limit_in_bytes on the memory
// controller's path, and it is read the same way.
func TestCgroupMemoryLimitReadsCgroupV1(t *testing.T) {
	proc, root := fakeCgroup(t, "11:name=systemd:/\n10:memory:/user.slice\n")
	writeBound(t, root, "memory/user.slice/memory.limit_in_bytes", "2147483648\n")
	if got := cgroupMemoryLimit(proc, root); got != 2<<30 {
		t.Fatalf("cgroupMemoryLimit (v1) = %d, want %d", got, 2<<30)
	}
}

// The root cgroup's own memory.max is read too, for a process left at `/`, and
// on a host where it is absent that simply does not participate.
func TestCgroupMemoryLimitReadsTheRootLevel(t *testing.T) {
	proc, root := fakeCgroup(t, "0::/\n")
	writeBound(t, root, "memory.max", "1073741824\n")
	if got := cgroupMemoryLimit(proc, root); got != 1<<30 {
		t.Fatalf("cgroupMemoryLimit at the root = %d, want %d", got, 1<<30)
	}
}
