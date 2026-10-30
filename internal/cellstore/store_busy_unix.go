//go:build linux || darwin

package cellstore

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/Agent-Field/codeaf/internal/home"
)

func storeProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func pidServesSocket(pid int, socket string) bool {
	uid, argv := storageProcess(context.Background(), pid)
	ownUID := strconv.Itoa(os.Getuid())
	if socket != "" && daemonServesSocket(uid, ownUID, argv, socket) {
		return true
	}
	// Each codeaf home has its own private saving daemon; it is the normal
	// writer path for this home even when a caller inspects without naming a socket.
	served, ok := daemonSocketArg(uid, ownUID, argv)
	if !ok {
		return false
	}
	if filepath.Clean(served) == filepath.Clean(socketFor(argv[0])) {
		return true
	}
	return filepath.Dir(filepath.Clean(served)) == filepath.Clean(home.Join("v3"))
}

func describeProcess(pid int) string {
	_, argv := storageProcess(context.Background(), pid)
	return formatHolderCommand(argv)
}

func lockHolderFromOS(lockPath string) StoreHolder {
	if runtime.GOOS == "linux" {
		return lockHolderLinux(lockPath)
	}
	return lockHolderDarwin(lockPath)
}

func lockHolderLinux(lockPath string) StoreHolder {
	info, err := os.Stat(lockPath)
	if err != nil {
		return StoreHolder{}
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return StoreHolder{}
	}
	pid := pidFromProcLocks(uint64(stat.Dev), stat.Ino)
	if pid <= 0 {
		pid = pidFromProcFDs(info)
	}
	if pid <= 0 {
		return StoreHolder{}
	}
	return resolveUnixHolder(pid)
}

func pidFromProcLocks(dev, ino uint64) int {
	file, err := os.Open("/proc/locks")
	if err != nil {
		return 0
	}
	defer file.Close()

	wantMajor := unix.Major(dev)
	wantMinor := unix.Minor(dev)
	ownPID := os.Getpid()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		// Standard format: "1: FLOCK ADVISORY WRITE <pid> <maj>:<min>:<ino> 0 EOF"
		// Blocked waiters carry "->" after the index ("1: -> FLOCK ..."); skip them.
		if len(fields) < 6 || fields[1] == "->" {
			continue
		}
		pid, err := strconv.Atoi(fields[4])
		if err != nil || pid <= 0 || pid == ownPID {
			continue
		}
		parts := strings.Split(fields[5], ":")
		if len(parts) != 3 {
			continue
		}
		gotIno, err := strconv.ParseUint(parts[2], 10, 64)
		if err != nil || gotIno != ino {
			continue
		}
		maj, err1 := strconv.ParseUint(parts[0], 16, 32)
		min, err2 := strconv.ParseUint(parts[1], 16, 32)
		if err1 != nil || err2 != nil {
			continue
		}
		if uint32(maj) == wantMajor && uint32(min) == wantMinor {
			return pid
		}
	}
	return 0
}

func pidFromProcFDs(target os.FileInfo) int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	ownUID := uint32(os.Getuid())
	ownPID := os.Getpid()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 || pid == ownPID {
			continue
		}
		procDir := filepath.Join("/proc", entry.Name())
		pInfo, err := os.Stat(procDir)
		if err != nil {
			continue
		}
		if st, ok := pInfo.Sys().(*syscall.Stat_t); !ok || st.Uid != ownUID {
			continue
		}
		fds, err := os.ReadDir(filepath.Join(procDir, "fd"))
		if err != nil {
			continue
		}
		for _, fd := range fds {
			fdInfo, err := os.Stat(filepath.Join(procDir, "fd", fd.Name()))
			if err == nil && os.SameFile(target, fdInfo) {
				return pid
			}
		}
	}
	return 0
}

func resolveUnixHolder(pid int) StoreHolder {
	_, argv := storageProcess(context.Background(), pid)
	// A one-shot furrow verb belongs to the codeaf process that launched it; a
	// furrow serve daemon is identified by its own socket so this home's normal
	// writer is never mistaken for a competing codeaf process.
	if len(argv) > 0 && storageProgramName.MatchString(filepath.Base(argv[0])) && (len(argv) < 2 || argv[1] != "serve") {
		if ppid := linuxParentPID(pid); ppid > 1 && ppid != os.Getpid() {
			_, parentArgv := storageProcess(context.Background(), ppid)
			if len(parentArgv) > 0 {
				base := filepath.Base(parentArgv[0])
				if base == "codeaf" || strings.HasPrefix(base, "codeaf-") {
					return StoreHolder{PID: ppid, Command: formatHolderCommand(parentArgv)}
				}
			}
		}
	}
	return StoreHolder{PID: pid, Command: formatHolderCommand(argv)}
}

func linuxParentPID(pid int) int {
	if runtime.GOOS != "linux" {
		return 0
	}
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if after, ok := strings.CutPrefix(line, "PPid:"); ok {
			ppid, _ := strconv.Atoi(strings.TrimSpace(after))
			return ppid
		}
	}
	return 0
}

func lockHolderDarwin(lockPath string) StoreHolder {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	raw, err := exec.CommandContext(ctx, "lsof", "-n", "-P", "-F", "pc", "--", lockPath).Output() //codeaf:plumbing inspects which local process holds an advisory store lock on macOS
	if err != nil {
		return StoreHolder{}
	}
	ownPID := os.Getpid()
	var pid int
	var cmd string
	for _, line := range strings.Split(string(raw), "\n") {
		switch {
		case strings.HasPrefix(line, "p"):
			parsed, err := strconv.Atoi(line[1:])
			if err == nil && parsed > 0 && parsed != ownPID {
				pid = parsed
			} else {
				pid = 0
			}
		case strings.HasPrefix(line, "c") && pid > 0 && cmd == "":
			cmd = strings.TrimSpace(line[1:])
		}
	}
	if pid <= 0 {
		return StoreHolder{}
	}
	holder := resolveUnixHolder(pid)
	if holder.Command == "" {
		holder.Command = cmd
	}
	return holder
}
