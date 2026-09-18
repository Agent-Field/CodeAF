//go:build linux

package processgroup

import (
	"os"
	"strconv"
	"strings"
)

// groupHasLiveMember reports whether any process that is still RUNNING — not a
// zombie waiting to be reaped — belongs to the process group pgid.
//
// WHY A SIGNAL PROBE IS NOT ENOUGH HERE. kill(-pgid, 0) answers yes while a
// zombie is in the group, and on Linux a job's shell is deliberately left a
// zombie while the detached sweep runs (internal/exec, reapShell): waitid with
// WNOWAIT has seen it exit and cmd.Wait has not reaped it yet. Probed with the
// signal, every finished job read as a group still alive, was sent SIGTERM,
// and was waited on for the whole termination grace before it was reported
// done — two seconds added to the ending of every background job, which is
// what turned a four-second test bound red on every pull request. So the
// members are read from /proc instead: field 5 of /proc/<pid>/stat is a
// process's pgrp and field 3 its state, and a member counts only while its
// state is neither Z (zombie) nor X (dead).
//
// The leader is read first because it answers the common case on its own: a
// running leader is a live member, and the table is walked only once it has
// exited. A /proc that cannot be listed at all falls back to the signal probe,
// which errs towards waiting rather than towards a missed sweep.
func groupHasLiveMember(pgid int) bool {
	if state, pgrp, ok := processStateAndGroup(readStat(pgid)); ok && pgrp == pgid && running(state) {
		return true
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return Alive(pgid)
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 || pid == pgid {
			continue
		}
		if state, pgrp, ok := processStateAndGroup(readStat(pid)); ok && pgrp == pgid && running(state) {
			return true
		}
	}
	return false
}

// running is every state a process can still be signalled out of. Z is a
// zombie — exited, unreaped, holding nothing but its exit status — and X is
// the kernel's dead state; neither can run another instruction.
func running(state byte) bool {
	return state != 'Z' && state != 'X'
}

// readStat reads one /proc/<pid>/stat line, or nothing for a pid that is gone
// or unreadable, which the parser below reports as not ok.
func readStat(pid int) string {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ""
	}
	return string(raw)
}

// processStateAndGroup reads the state (field 3) and the pgrp (field 5) out of
// one /proc/<pid>/stat line, counted from after comm's last ')' the way
// identity_linux.go and usage_linux.go already count: the state is index 0,
// the parent pid index 1 and the pgrp index 2.
func processStateAndGroup(raw string) (state byte, pgrp int, ok bool) {
	close := strings.LastIndexByte(raw, ')')
	if close < 0 {
		return 0, 0, false
	}
	fields := strings.Fields(raw[close+1:])
	if len(fields) <= 2 || len(fields[0]) != 1 {
		return 0, 0, false
	}
	pgrp, err := strconv.Atoi(fields[2])
	if err != nil {
		return 0, 0, false
	}
	return fields[0][0], pgrp, true
}
