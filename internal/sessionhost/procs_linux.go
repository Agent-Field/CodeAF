//go:build linux

package sessionhost

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type processTable map[int][]int

func readStatus(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	status := make(map[string]string)
	for _, line := range strings.Split(string(b), "\n") {
		name, value, ok := strings.Cut(line, ":")
		if ok {
			status[name] = strings.TrimSpace(value)
		}
	}
	return status, nil
}

func processUIDs(pid int) ([]int, error) {
	status, err := readStatus(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ESRCH) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	state := status["State"]
	// An unreaped exit can remain visible in proc but cannot act or fork.
	if strings.HasPrefix(state, "Z") || strings.HasPrefix(state, "X") {
		return nil, nil
	}
	fields := strings.Fields(status["Uid"])
	if len(fields) != 4 {
		return nil, errors.New("Could not determine which chat owns a running process.")
	}
	uids := make([]int, 0, len(fields))
	for _, field := range fields {
		uid, err := strconv.ParseUint(field, 10, 32)
		if err != nil {
			return nil, err
		}
		if uid < maxSessionUID {
			uids = append(uids, int(uid))
		}
	}
	return uids, nil
}

func scanProcesses() (processTable, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	table := make(processTable)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() || pid <= 0 {
			continue
		}
		uids, err := processUIDs(pid)
		if err != nil {
			return nil, err
		}
		for _, uid := range uids {
			pids := table[uid]
			if len(pids) == 0 || pids[len(pids)-1] != pid {
				table[uid] = append(pids, pid)
			}
		}
	}
	return table, nil
}

// ProcessesOf reports live processes that carry uid in any identity field.
func ProcessesOf(uid int) []int {
	table, _ := scanProcesses()
	return table[uid]
}

type linuxProcesses struct{ cfg Config }

func (p linuxProcesses) snapshot() (processTable, error) { return scanProcesses() }

// KillAll stops the whole observed set before killing it, then looks again
// for processes that were born while the first set was being stopped.
func (p linuxProcesses) KillAll(uid int, grace time.Duration) error {
	if !p.cfg.ownsUID(uid) || uid == os.Getuid() || uid == os.Geteuid() {
		return errors.New("The chat's processes could not be stopped safely.")
	}
	deadline := time.Now().Add(grace)
	var signalErr error
	for round := 0; round < 10; round++ {
		table, err := p.snapshot()
		if err != nil {
			return err
		}
		if len(table[uid]) == 0 {
			return nil
		}
		signalErr = errors.Join(signalErr, killRound(uid, table[uid]))
		nextRound := time.Now().Add(grace / 10)
		for {
			table, err = p.snapshot()
			if err != nil {
				return err
			}
			if len(table[uid]) == 0 {
				return nil
			}
			if time.Now().After(nextRound) || time.Now().After(deadline) {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if time.Now().After(deadline) {
			break
		}
	}
	return errors.Join(errors.New(refusalText[stopFailed]), signalErr)
}

func killRound(uid int, pids []int) error {
	var firstErr error
	var handles []int
	for _, pid := range pids {
		if pid <= 1 || pid == os.Getpid() {
			continue
		}
		fd, err := unix.PidfdOpen(pid, 0)
		if errors.Is(err, unix.ESRCH) {
			continue
		}
		if err != nil {
			firstErr = errors.Join(firstErr, err)
			continue
		}
		// The pidfd names only the opened process. Recheck its identity
		// before sending a signal so an intervening PID reuse is harmless.
		uids, err := processUIDs(pid)
		belongs := false
		for _, candidate := range uids {
			belongs = belongs || candidate == uid
		}
		if !belongs || err != nil {
			unix.Close(fd)
			firstErr = errors.Join(firstErr, err)
			continue
		}
		handles = append(handles, fd)
		err = unix.PidfdSendSignal(fd, unix.SIGSTOP, nil, 0)
		if err != nil && !errors.Is(err, unix.ESRCH) {
			firstErr = errors.Join(firstErr, err)
		}
	}
	for _, fd := range handles {
		err := unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
		unix.Close(fd)
		if err != nil && !errors.Is(err, unix.ESRCH) {
			firstErr = errors.Join(firstErr, err)
		}
	}
	return firstErr
}
