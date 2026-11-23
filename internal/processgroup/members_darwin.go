//go:build darwin

package processgroup

import (
	"encoding/binary"
	"errors"
	"sort"
	"unsafe"

	"github.com/Agent-Field/codeaf/internal/darwinproc"
	"golang.org/x/sys/unix"
)

// Members lists live members in pid order. A growing process table gets only
// three attempts so a completion inspection cannot retry indefinitely.
func Members(pgid int) []int {
	if pgid <= 0 {
		return nil
	}
	for range 3 {
		raw, err := unix.SysctlRaw("kern.proc.pgrp", pgid)
		if errors.Is(err, unix.ENOMEM) {
			continue
		}
		if err != nil || len(raw) == 0 || len(raw)%unix.SizeofKinfoProc != 0 {
			return nil
		}
		return darwinMemberPIDs(pgid, darwinKinfoRecords(raw))
	}
	return nil
}

// Only the three fields membership reads are taken from each kernel record.
// Copying whole records would put the kernel's bytes into the x/sys type's
// pointer-typed fields, which the garbage collector then scans.
const (
	kinfoPIDOffset  = unsafe.Offsetof(unix.KinfoProc{}.Proc) + unsafe.Offsetof(unix.ExternProc{}.P_pid)
	kinfoStatOffset = unsafe.Offsetof(unix.KinfoProc{}.Proc) + unsafe.Offsetof(unix.ExternProc{}.P_stat)
	kinfoPGIDOffset = unsafe.Offsetof(unix.KinfoProc{}.Eproc) + unsafe.Offsetof(unix.Eproc{}.Pgid)
)

func darwinKinfoRecords(raw []byte) []unix.KinfoProc {
	records := make([]unix.KinfoProc, len(raw)/unix.SizeofKinfoProc)
	for i := range records {
		record := raw[i*unix.SizeofKinfoProc : (i+1)*unix.SizeofKinfoProc]
		records[i].Proc.P_pid = int32(binary.LittleEndian.Uint32(record[kinfoPIDOffset:]))
		records[i].Proc.P_stat = int8(record[kinfoStatOffset])
		records[i].Eproc.Pgid = int32(binary.LittleEndian.Uint32(record[kinfoPGIDOffset:]))
	}
	return records
}

func darwinMemberPIDs(pgid int, records []unix.KinfoProc) []int {
	pids := make([]int, 0, len(records))
	for _, record := range records {
		if record.Proc.P_pid > 0 && int(record.Eproc.Pgid) == pgid && record.Proc.P_stat != darwinproc.StatusZombie {
			pids = append(pids, int(record.Proc.P_pid))
		}
	}
	sort.Ints(pids)
	return pids
}

// The shell is reaped before a sweep on macOS (internal/exec/shellwait_other.go),
// so the existing signal probe has no zombie leader to mistake for live work.
func groupHasLiveMember(pgid int) bool {
	return Alive(pgid)
}
