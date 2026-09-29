//go:build linux

package executor

// jail_linux.go confines a child with only kernel features and this binary:
// a private network namespace when the policy denies the network, a private
// /tmp, an unreadable .cell/, and Landlock write limits (workspace, /tmp and
// /dev only). The child is this binary re-executed as a helper, which sets up
// the mounts and Landlock inside the new namespaces and then execs the tool.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// helperName is argv[0] of the re-executed helper.
const helperName = "codeaf-jail-helper"

// LinuxJail is the Linux Jail. The zero value is ready to use.
type LinuxJail struct{}

// Confine rewrites cmd to run under the helper.
func (LinuxJail) Confine(cmd *exec.Cmd, req ExecRequest) error {
	if cmd.Err != nil {
		return cmd.Err
	}
	caps := Probe()
	if !caps.UserNS {
		return refuseWithoutNamespaces(req)
	}
	root, err := filepath.Abs(cmd.Dir)
	if err != nil {
		return err
	}
	// TODO(allowlist): a host allowlist is treated as open until proxying exists.
	cmd.SysProcAttr = withNamespaces(cmd.SysProcAttr, req.Net.Denies())
	cmd.Args = append([]string{helperName, boolArg(caps.Landlock), root, cmd.Path}, cmd.Args...)
	cmd.Path = "/proc/self/exe"
	return nil
}

// Degraded reports that the filesystem limits could not all be applied.
func (LinuxJail) Degraded(ExecRequest) bool {
	c := Probe()
	return !c.Landlock || !c.UserNS
}

// refuseWithoutNamespaces fails closed when the network must be denied and
// runs unconfined (degraded) otherwise.
func refuseWithoutNamespaces(req ExecRequest) error {
	if req.Net.Denies() {
		return errors.New("executor: cannot deny the network, user namespaces are unavailable")
	}
	return nil
}

// withNamespaces merges the namespace attributes into the group attributes.
func withNamespaces(base *syscall.SysProcAttr, newNet bool) *syscall.SysProcAttr {
	ns := namespaceAttr(newNet)
	if base != nil {
		ns.Setpgid, ns.Pgid, ns.Setsid = base.Setpgid, base.Pgid, base.Setsid
	}
	return ns
}

func boolArg(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func init() {
	if len(os.Args) > 4 && os.Args[0] == helperName {
		os.Exit(runHelper(os.Args[1] == "1", os.Args[2], os.Args[3], os.Args[4:]))
	}
}

// runHelper runs inside the new namespaces; it only returns on failure.
func runHelper(landlock bool, root, path string, argv []string) int {
	steps := []func() error{
		func() error { return unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, "") },
		func() error { return privateTmp(root) },
		func() error { return hideCell(root) },
		func() error { return applyLandlock(landlock, root) },
		dropCaps,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			os.Stderr.WriteString("jail: " + err.Error() + "\n")
			return 126
		}
	}
	err := syscall.Exec(path, argv, os.Environ())
	os.Stderr.WriteString("jail: exec: " + err.Error() + "\n")
	return 127
}

// tmpHome is the shared temporary directory the jail replaces with a private one.
const tmpHome = "/tmp"

// livesInTmp reports a workspace that is itself under the shared tmp, which
// then stays as it is: hiding it would hide the workspace.
func livesInTmp(root string) bool {
	rel, err := filepath.Rel(tmpHome, root)
	return err == nil && !strings.HasPrefix(rel, "..")
}

func privateTmp(root string) error {
	if livesInTmp(root) {
		return nil
	}
	return mountTmpfs(tmpHome, "mode=1777")
}

func mountTmpfs(target, opts string) error {
	return unix.Mount("tmpfs", target, "tmpfs", unix.MS_NOSUID|unix.MS_NODEV, opts)
}

// hideCell covers the harness-owned directory with an unreadable empty one.
func hideCell(root string) error {
	cell := filepath.Join(root, ".cell")
	if _, err := os.Stat(cell); err != nil {
		return nil
	}
	return mountTmpfs(cell, "mode=000")
}

func dropCaps() error {
	return unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0)
}

const (
	readAccess = unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_READ_DIR
	// abiTruncate and abiIoctl are the ABI versions that added those rights.
	abiRefer, abiTruncate, abiIoctl = 2, 3, 5
)

// writeAccess is every right beyond reading that this kernel ABI knows.
func writeAccess(abi int) uint64 {
	w := uint64(unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_REMOVE_DIR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_FILE | unix.LANDLOCK_ACCESS_FS_MAKE_CHAR |
		unix.LANDLOCK_ACCESS_FS_MAKE_DIR | unix.LANDLOCK_ACCESS_FS_MAKE_REG |
		unix.LANDLOCK_ACCESS_FS_MAKE_SOCK | unix.LANDLOCK_ACCESS_FS_MAKE_FIFO |
		unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK | unix.LANDLOCK_ACCESS_FS_MAKE_SYM)
	for v, bit := range map[int]uint64{abiRefer: unix.LANDLOCK_ACCESS_FS_REFER,
		abiTruncate: unix.LANDLOCK_ACCESS_FS_TRUNCATE, abiIoctl: unix.LANDLOCK_ACCESS_FS_IOCTL_DEV} {
		if abi >= v {
			w |= bit
		}
	}
	return w
}

// applyLandlock reads anywhere but writes only in the workspace, /tmp and /dev.
func applyLandlock(enabled bool, root string) error {
	if !enabled {
		return nil
	}
	abi := landlockABI()
	write := writeAccess(abi)
	fd, err := createRuleset(readAccess | write)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	rules := map[string]uint64{"/": readAccess, root: readAccess | write, "/dev": readAccess | write}
	if !livesInTmp(root) {
		rules[tmpHome] = readAccess | write
	}
	for path, access := range rules {
		if err := allow(fd, path, access); err != nil {
			return err
		}
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	_, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, uintptr(fd), 0, 0)
	return errnoErr(errno)
}

func createRuleset(handled uint64) (int, error) {
	attr := unix.LandlockRulesetAttr{Access_fs: handled}
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET,
		uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	return int(fd), errnoErr(errno)
}

// allow grants access beneath path; the access is masked to what the ruleset handles.
func allow(ruleset int, path string, access uint64) error {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	rule := unix.LandlockPathBeneathAttr{Allowed_access: access, Parent_fd: int32(fd)}
	_, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, uintptr(ruleset),
		unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&rule)), 0, 0, 0)
	return errnoErr(errno)
}

func errnoErr(e syscall.Errno) error {
	if e == 0 {
		return nil
	}
	return e
}

// DefaultJail is the jail this system provides.
func DefaultJail() Jail { return LinuxJail{} }
