//go:build linux

package sessionhost

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	helperFailure = 126
	boundingError = "Could not clear the process capability bounding set."
)

func RunHelper(args []string) int {
	var err error
	if len(args) == 0 {
		err = errors.New("A helper command is required.")
	} else {
		switch args[0] {
		case "launch":
			err = launch(args[1:])
		case "reap":
			err = reap(args[1:])
		case "whoami":
			err = whoami()
		default:
			err = errors.New("Unknown helper command.")
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return helperFailure
	}
	return 0
}

func launch(args []string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	fs := flag.NewFlagSet("launch", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	uid := fs.Int("uid", 0, "")
	gid := fs.Int("gid", 0, "")
	dir := fs.String("dir", "", "")
	nproc := fs.Int("nproc", 0, "")
	if fs.Parse(args) != nil || fs.NArg() < 1 || !filepath.IsAbs(fs.Arg(0)) ||
		!filepath.IsAbs(*dir) || !validUIDRange(*uid, 1) || *gid != *uid || *nproc < 1 {
		return errors.New("The launch settings are invalid.")
	}
	if os.Geteuid() != 0 {
		return errors.New("not running as root")
	}
	for _, limit := range []struct {
		resource int
		value    uint64
	}{
		{unix.RLIMIT_NPROC, uint64(*nproc)},
		{unix.RLIMIT_NOFILE, 4096},
		{unix.RLIMIT_FSIZE, 10 << 30},
		{unix.RLIMIT_CORE, 0},
	} {
		if err := unix.Setrlimit(limit.resource, &unix.Rlimit{Cur: limit.value, Max: limit.value}); err != nil {
			return errors.New("Could not set the process limits.")
		}
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return errors.New("Could not block new process privileges.")
	}
	_ = os.WriteFile("/proc/self/oom_score_adj", []byte("500"), 0)
	// A chat's files are its own. /dev/shm and /tmp are shared by every chat on
	// the machine and a file made there with the usual 022 is readable by all of
	// them, so what a chat's processes create is closed to everyone else unless
	// the chat itself opens it up.
	unix.Umask(0o077)

	// Linux requires CAP_SETPCAP to drop a bounding bit. The identity
	// switch clears that capability, so the exec thread must drop the
	// bounding set beforehand and verify it again after the switch.
	if err := dropBoundingSet(); err != nil {
		return err
	}
	if syscall.Setgroups(nil) != nil ||
		syscall.Setresgid(*gid, *gid, *gid) != nil ||
		syscall.Setresuid(*uid, *uid, *uid) != nil ||
		!currentIdentity().matches(*uid) {
		return errors.New("Could not switch to the chat's process identity.")
	}
	if err := verifyUnprivileged(); err != nil {
		return err
	}
	if err := os.Chdir(*dir); err != nil {
		return errors.New("Could not open the chat's working folder.")
	}
	if err := syscall.Exec(fs.Arg(0), fs.Args(), os.Environ()); err != nil {
		return errors.New("Could not launch the chat process.")
	}
	return nil
}

func dropBoundingSet() error {
	// An invalid capability number marks the end of the kernel's set and
	// avoids guessing the largest bit from the headers used to build us.
	for cap := uintptr(0); ; cap++ {
		present, err := unix.PrctlRetInt(unix.PR_CAPBSET_READ, cap, 0, 0, 0)
		if errors.Is(err, unix.EINVAL) {
			return nil
		}
		if err != nil || (present == 1 && unix.Prctl(unix.PR_CAPBSET_DROP, cap, 0, 0, 0) != nil) {
			return errors.New(boundingError)
		}
	}
}

func verifyUnprivileged() error {
	if unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0) != nil {
		return errors.New("Could not clear the process capabilities.")
	}
	// With no effective or permitted bits, capset can still clear the
	// calling thread's inheritable set. It cannot add a capability here.
	var clear [2]unix.CapUserData
	if unix.Capset(&unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}, &clear[0]) != nil {
		return errors.New("Could not clear the process capabilities.")
	}
	status, err := readStatus("/proc/thread-self/status")
	if err != nil {
		return errors.New("Could not verify the process capabilities.")
	}
	if !zeroHex(status["CapEff"]) || !zeroHex(status["CapPrm"]) || !zeroHex(status["CapInh"]) ||
		!zeroHex(status["CapBnd"]) || !zeroHex(status["CapAmb"]) || status["NoNewPrivs"] != "1" {
		return errors.New("Could not verify the process privileges.")
	}
	return nil
}

func zeroHex(value string) bool {
	n, err := strconv.ParseUint(value, 16, 64)
	return err == nil && n == 0
}

type identity struct {
	uid, gid [3]int
	groups   int
}

func currentIdentity() identity {
	var id identity
	id.uid[0], id.uid[1], id.uid[2] = unix.Getresuid()
	id.gid[0], id.gid[1], id.gid[2] = unix.Getresgid()
	groups, err := os.Getgroups()
	id.groups = len(groups)
	if err != nil {
		id.groups = -1
	}
	return id
}

func (id identity) matches(uid int) bool {
	want := [3]int{uid, uid, uid}
	return id.uid == want && id.gid == want && id.groups == 0
}

func whoami() error {
	id := currentIdentity()
	status, err := readStatus("/proc/thread-self/status")
	if err != nil {
		return errors.New("Could not read the process identity.")
	}
	_, err = fmt.Printf("uid=%d,%d,%d gid=%d,%d,%d groups=%d nnp=%s capeff=%s\n",
		id.uid[0], id.uid[1], id.uid[2], id.gid[0], id.gid[1], id.gid[2],
		id.groups, status["NoNewPrivs"], status["CapEff"])
	return err
}

func probeIdentity(output string, uid int) bool {
	fields := strings.Fields(output)
	ids := fmt.Sprintf("%d,%d,%d", uid, uid, uid)
	return len(fields) == 5 && fields[0] == "uid="+ids && fields[1] == "gid="+ids &&
		fields[2] == "groups=0" && fields[3] == "nnp=1" &&
		strings.HasPrefix(fields[4], "capeff=") && zeroHex(strings.TrimPrefix(fields[4], "capeff="))
}
