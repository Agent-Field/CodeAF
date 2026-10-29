//go:build darwin

package darwinproc

import (
	"bytes"
	"encoding/binary"
	"slices"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The raw proc_info trap uses Go's existing per-architecture syscall stubs,
// avoiding assembly, dynamic imports and linkname dependencies that garbling
// could break. Unlike libproc's wrappers, the trap preserves errno directly.
// Apple promises stability for neither the trap numbers nor libproc, but both
// have remained unchanged since macOS 10.5. The private XNU call selectors and
// SDK layouts below are identical on arm64 and amd64; both are little-endian.
const (
	callListPIDs    = 1
	callPIDInfo     = 2
	callPIDFDInfo   = 3
	ppidOnly        = 6
	pidListFDs      = 1
	pidTaskAllInfo  = 2
	pidFDSocketInfo = 3
	fdTypeSocket    = 2
	socketInfoTCP   = 2
	tcpStateListen  = 1
	familyIPv4      = 2
	familyIPv6      = 30

	// StatusZombie marks exited processes, which cannot contribute live work.
	StatusZombie = 5

	pidSize               = 4
	taskAllInfoSize       = 232
	bsdInfoOffset         = 0
	bsdInfoSize           = 136
	bsdStatusOffset       = bsdInfoOffset + 4
	bsdPIDOffset          = bsdInfoOffset + 12
	bsdPPIDOffset         = bsdInfoOffset + 16
	bsdPGIDOffset         = bsdInfoOffset + 100
	bsdStartSecondsOffset = bsdInfoOffset + 120
	bsdStartMicrosOffset  = bsdInfoOffset + 128
	taskInfoOffset        = bsdInfoSize
	taskInfoSize          = 96
	taskResidentOffset    = taskInfoOffset + 8
	taskUserOffset        = taskInfoOffset + 16
	taskSystemOffset      = taskInfoOffset + 24
	fdInfoSize            = 8
	fdNumberOffset        = 0
	fdTypeOffset          = 4
	socketFDInfoSize      = 792
	socketInfoOffset      = 24
	socketInfoSize        = 768
	socketFamilyOffset    = socketInfoOffset + 160
	socketKindOffset      = socketInfoOffset + 232
	socketProtocolOffset  = socketInfoOffset + 240
	socketLocalPortOffset = socketProtocolOffset + 4
	socketTCPStateOffset  = socketProtocolOffset + 80

	childrenInitialPIDs = 64
	childrenGrowth      = 4
	childrenAttempts    = 4

	// procArgsAlign is the boundary the kernel pads the executable path to
	// before the arguments begin: the pointer size of every supported Mac.
	procArgsAlign = 8
)

// procInfo keeps pointer conversion in the syscall expression so the buffer
// remains alive and cannot move while the kernel writes it. A nil buffer is
// used only by interfaces that return a sizing estimate.
func procInfo(call, a1, a2, a3 int, buf []byte) (int, error) {
	if len(buf) > 1<<31-1 {
		return 0, unix.EINVAL
	}
	var n uintptr
	var errno unix.Errno
	if len(buf) == 0 {
		n, _, errno = unix.Syscall6(unix.SYS_PROC_INFO, uintptr(call), uintptr(a1), uintptr(a2), uintptr(a3), 0, 0)
	} else {
		n, _, errno = unix.Syscall6(unix.SYS_PROC_INFO, uintptr(call), uintptr(a1), uintptr(a2), uintptr(a3), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	}
	if errno != 0 {
		return 0, errno
	}
	if n > 1<<31-1 || (len(buf) > 0 && n > uintptr(len(buf))) {
		return 0, unix.EIO
	}
	return int(n), nil
}

// Children lists immediate children, bounding retries when the tree grows
// between reads. When even the last buffer fills, the children it holds are
// returned with ENOMEM: they are a lower bound on the children, never the
// whole list, and a caller counting them must say so.
func Children(ppid int) ([]int, error) {
	if ppid <= 0 {
		return nil, unix.EINVAL
	}
	var initial [childrenInitialPIDs * pidSize]byte
	buf := initial[:]
	for attempt := 0; attempt < childrenAttempts; attempt++ {
		n, err := procInfo(callListPIDs, ppidOnly, ppid, 0, buf)
		if err != nil {
			return nil, err
		}
		if n%pidSize != 0 {
			return nil, unix.EIO
		}
		if n < len(buf) {
			return childPIDs(buf[:n]), nil
		}
		if attempt+1 < childrenAttempts {
			buf = make([]byte, len(buf)*childrenGrowth)
		}
	}
	return childPIDs(buf), unix.ENOMEM
}

func childPIDs(buf []byte) []int {
	pids := make([]int, 0, len(buf)/pidSize)
	for offset := 0; offset+pidSize <= len(buf); offset += pidSize {
		if pid := int(int32(binary.LittleEndian.Uint32(buf[offset:]))); pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids
}

// TaskInfo combines identity and CPU from one kernel snapshot so callers can
// validate ancestry before attributing a process's accumulated CPU time.
type TaskInfo struct {
	PID, PPID, PGID, Status int
	// Start is the process's start time in microseconds. A pid handed out
	// again names a process with another start, so a reader that must not
	// confuse the two compares it.
	Start      uint64
	CPUSeconds float64
	Resident   uint64
}

var (
	frequencyOnce sync.Once
	tickFrequency uint64
)

// Task reads one process. Kernel task times are absolute ticks even under
// Rosetta; hw.tbfrequency converts them correctly on both supported CPUs.
func Task(pid int) (TaskInfo, error) {
	if pid <= 0 {
		return TaskInfo{}, unix.EINVAL
	}
	frequencyOnce.Do(func() { tickFrequency, _ = unix.SysctlUint64("hw.tbfrequency") })
	if tickFrequency == 0 {
		return TaskInfo{}, unix.EIO
	}
	var buf [taskAllInfoSize]byte
	n, err := procInfo(callPIDInfo, pid, pidTaskAllInfo, 0, buf[:])
	if err != nil {
		return TaskInfo{}, err
	}
	if n != taskAllInfoSize {
		return TaskInfo{}, unix.EIO
	}
	return TaskInfo{
		PID:        int(int32(binary.LittleEndian.Uint32(buf[bsdPIDOffset:]))),
		PPID:       int(int32(binary.LittleEndian.Uint32(buf[bsdPPIDOffset:]))),
		PGID:       int(int32(binary.LittleEndian.Uint32(buf[bsdPGIDOffset:]))),
		Status:     int(binary.LittleEndian.Uint32(buf[bsdStatusOffset:])),
		Start:      binary.LittleEndian.Uint64(buf[bsdStartSecondsOffset:])*1_000_000 + binary.LittleEndian.Uint64(buf[bsdStartMicrosOffset:]),
		CPUSeconds: float64(binary.LittleEndian.Uint64(buf[taskUserOffset:]))/float64(tickFrequency) + float64(binary.LittleEndian.Uint64(buf[taskSystemOffset:]))/float64(tickFrequency),
		Resident:   binary.LittleEndian.Uint64(buf[taskResidentOffset:]),
	}, nil
}

// ListenPorts inspects only this process's socket descriptors. Descriptor races
// are skipped individually, and a shared socket buffer avoids one allocation
// per descriptor.
func ListenPorts(pid int) []int {
	if pid <= 0 {
		return nil
	}
	size, err := procInfo(callPIDInfo, pid, pidListFDs, 0, nil)
	if err != nil || size == 0 || size%fdInfoSize != 0 {
		return nil
	}
	fds := make([]byte, size)
	n, err := procInfo(callPIDInfo, pid, pidListFDs, 0, fds)
	if err != nil || n%fdInfoSize != 0 {
		return nil
	}
	var socket [socketFDInfoSize]byte
	var ports []int
	for offset := 0; offset < n; offset += fdInfoSize {
		fd := fds[offset : offset+fdInfoSize]
		if binary.LittleEndian.Uint32(fd[fdTypeOffset:]) != fdTypeSocket {
			continue
		}
		number := int(int32(binary.LittleEndian.Uint32(fd[fdNumberOffset:])))
		count, err := procInfo(callPIDFDInfo, pid, pidFDSocketInfo, number, socket[:])
		if err != nil || count != socketFDInfoSize {
			continue
		}
		family := binary.LittleEndian.Uint32(socket[socketFamilyOffset:])
		if (family != familyIPv4 && family != familyIPv6) || binary.LittleEndian.Uint32(socket[socketKindOffset:]) != socketInfoTCP || binary.LittleEndian.Uint32(socket[socketTCPStateOffset:]) != tcpStateListen {
			continue
		}
		// The low sixteen bits contain network-order bytes on both macOS CPUs.
		port := int(binary.BigEndian.Uint16(socket[socketLocalPortOffset:]))
		ports = append(ports, port)
	}
	slices.Sort(ports)
	return slices.Compact(ports)
}

// Argv discards the raw sysctl response after parsing because it also contains
// environment data that must never enter a service record or diagnostic.
func Argv(pid int) []string {
	if pid <= 0 {
		return nil
	}
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return nil
	}
	return parseProcArgs(raw)
}

func parseProcArgs(raw []byte) []string {
	if len(raw) < pidSize {
		return nil
	}
	argc := int(int32(binary.LittleEndian.Uint32(raw)))
	if argc <= 0 || argc > len(raw)-pidSize {
		return nil
	}
	rest := raw[pidSize:]
	end := bytes.IndexByte(rest, 0)
	if end < 0 {
		return nil
	}
	// The kernel pads the executable path with NULs so the arguments start on
	// a pointer-size boundary of the string area. Skipping every NUL instead
	// would swallow an empty argv[0] and shift the first environment string,
	// which can hold a secret, into the arguments. Padding that is not all NUL
	// is a layout this parser does not know, and it reads as nothing.
	start := (end + 1 + procArgsAlign - 1) &^ (procArgsAlign - 1)
	if start > len(rest) || len(bytes.Trim(rest[end+1:start], "\x00")) != 0 {
		return nil
	}
	rest = rest[start:]
	argv := make([]string, 0, argc)
	for range argc {
		end = bytes.IndexByte(rest, 0)
		if end < 0 {
			return nil
		}
		argv = append(argv, string(rest[:end]))
		rest = rest[end+1:]
	}
	return argv
}
