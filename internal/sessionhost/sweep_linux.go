//go:build linux

package sessionhost

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// sharedAreas are the places every chat on the machine can write to and none of
// them owns. A chat's own folder goes with the chat; what it left in these did
// not, and a chat that is given the same Linux user later (after the day's
// quarantine, or when the range wraps) would find it, owned by "itself".
var sharedAreas = []string{"/dev/shm", "/tmp", "/var/tmp"}

// sysvDir is where the kernel lists the System V objects. It is a variable so a test can point the sweep at a
// folder of its own instead of sweeping the objects of whoever runs the test.
var sysvDir = "/proc/sysvipc"

// sweepShared removes, from each area, everything owned by uid, and every
// System V segment, semaphore set and message queue that uid owns or made. It
// runs as the chat's own Linux user (the reap helper has dropped to it), so it
// can only ever remove what that user could remove anyway, and it never
// follows a link: an entry is unlinked, and a directory is walked by file
// descriptor. The first failure is returned after every area has been tried.
func sweepShared(uid int, areas []string, sysvDir string) error {
	var first error
	keep := func(err error) {
		if err != nil && first == nil {
			first = err
		}
	}
	for _, area := range areas {
		keep(sweepArea(uid, area))
	}
	keep(sweepSysV(uid, sysvDir))
	return first
}

func sweepArea(uid int, area string) error {
	entries, err := os.ReadDir(area)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	parent, err := unix.Open(area, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	var first error
	for _, entry := range entries {
		var st unix.Stat_t
		if err := unix.Fstatat(parent, entry.Name(), &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			continue
		}
		if int(st.Uid) != uid {
			continue
		}
		if err := removeAt(parent, entry.Name()); err != nil && first == nil {
			first = err
		}
	}
	return first
}

type sysvTable struct {
	file, id string
	remove   func(id int) error
}

// sysvTables are the three kernel tables of System V objects and how each kind
// is removed.
var sysvTables = []sysvTable{
	{"shm", "shmid", func(id int) error {
		_, _, e := unix.Syscall(unix.SYS_SHMCTL, uintptr(id), unix.IPC_RMID, 0)
		return errnoOrNil(e)
	}},
	{"sem", "semid", func(id int) error {
		_, _, e := unix.Syscall6(unix.SYS_SEMCTL, uintptr(id), 0, unix.IPC_RMID, 0, 0, 0)
		return errnoOrNil(e)
	}},
	{"msg", "msqid", func(id int) error {
		_, _, e := unix.Syscall(unix.SYS_MSGCTL, uintptr(id), unix.IPC_RMID, 0)
		return errnoOrNil(e)
	}},
}

// sweepSysV walks the three kernel tables under dir (normally /proc/sysvipc).
func sweepSysV(uid int, dir string) error {
	var first error
	for _, table := range sysvTables {
		ids, err := ownedSysV(filepath.Join(dir, table.file), table.id, uid)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) && first == nil {
				first = err
			}
			continue
		}
		for _, id := range ids {
			if err := table.remove(id); err != nil && !errors.Is(err, unix.EINVAL) && !errors.Is(err, unix.EIDRM) && first == nil {
				first = err
			}
		}
	}
	return first
}

func errnoOrNil(e unix.Errno) error {
	if e == 0 {
		return nil
	}
	return e
}

// ownedSysV reads one /proc/sysvipc table and answers the ids whose owner or
// creator is uid. The columns are found by their header names, because the
// tables differ in width and a kernel may add one.
func ownedSysV(path, idColumn string, uid int) ([]int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	if !scan.Scan() {
		return nil, scan.Err()
	}
	column := map[string]int{}
	for i, name := range strings.Fields(scan.Text()) {
		column[name] = i
	}
	idAt, haveID := column[idColumn]
	uidAt, haveUID := column["uid"]
	cuidAt, haveCUID := column["cuid"]
	if !haveID || !haveUID || !haveCUID {
		return nil, errors.New("unreadable table")
	}
	var ids []int
	for scan.Scan() {
		fields := strings.Fields(scan.Text())
		if len(fields) <= max(idAt, uidAt, cuidAt) {
			continue
		}
		owner, _ := strconv.Atoi(fields[uidAt])
		creator, _ := strconv.Atoi(fields[cuidAt])
		id, err := strconv.Atoi(fields[idAt])
		if err == nil && (owner == uid || creator == uid) {
			ids = append(ids, id)
		}
	}
	return ids, scan.Err()
}
