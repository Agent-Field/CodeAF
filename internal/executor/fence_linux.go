//go:build linux

package executor

// fence_linux.go applies the fence (fence.go) with the same two kernel
// features the Linux jail uses, through the same re-executed helper:
//
//   - a private MOUNT namespace, inside which every hidden folder is covered
//     by an empty unreadable tmpfs, every hidden file and container socket by
//     an unreadable empty file, every read-only folder is bound read-only over
//     itself, and the job's own temp folder is bound over /tmp;
//   - LANDLOCK, which then lets the command read anywhere and write only in
//     the checkout, the repository's git folder, the job's temp and cache
//     folders, /dev, and what the person allowed.
//
// There is NO network namespace: the fence keeps the network on. The order is
// the jail's: mounts first, because a landlocked process may not mount.

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// fenceArg is the helper's mode word for a fenced command.
const fenceArg = "fence"

// fenceSpec is what the helper is handed: the fence and how much of it this
// kernel can apply.
type fenceSpec struct {
	Fence Fence     `json:"fence"`
	Mode  fenceMode `json:"mode"`
}

// fenceMode is how much of the fence this kernel applies.
type fenceMode string

const (
	// fenceFull: a mount namespace hides and binds, then Landlock limits.
	fenceFull fenceMode = "full"
	// fenceLandlock: no namespaces (Ubuntu's AppArmor refuses them to an
	// unprivileged binary by default), so Landlock does all of it: writes
	// limited, and the hidden paths left out of what may be read.
	fenceLandlock fenceMode = "landlock"
	// fenceNone: neither; the command runs as it is.
	fenceNone fenceMode = ""
)

// thisFenceMode is what this kernel supports.
func thisFenceMode() fenceMode {
	c := Probe()
	switch {
	case c.UserNS && c.Landlock:
		return fenceFull
	case c.Landlock:
		return fenceLandlock
	}
	return fenceNone
}

// confineFence rewrites cmd to run under the helper. With neither user
// namespaces nor Landlock the command runs as it is, which [FenceLimitLine]
// says once.
func confineFence(cmd *exec.Cmd, f *Fence) error {
	if cmd.Err != nil {
		return cmd.Err
	}
	mode := thisFenceMode()
	if mode == fenceNone {
		return nil
	}
	spec, err := writeFenceSpec(f, mode)
	if err != nil {
		return err
	}
	if mode == fenceFull {
		cmd.SysProcAttr = withNamespaces(cmd.SysProcAttr, false)
	}
	cmd.Args = append([]string{helperName, fenceArg, spec, cmd.Path}, cmd.Args...)
	cmd.Path = "/proc/self/exe"
	return nil
}

// writeFenceSpec keeps the helper's spec in a file beside the job's folders and
// answers its path. It is a file and not an argument because the read grants
// can run to thousands of paths, past what one argument may hold, and it is
// named by its content so a fence written once is read by every command after.
func writeFenceSpec(f *Fence, mode fenceMode) (string, error) {
	data, err := json.Marshal(fenceSpec{Fence: *f, Mode: mode})
	if err != nil {
		return "", err
	}
	path := filepath.Join(filepath.Dir(f.Tmp), "spec-"+fenceDigest(string(data))+".json")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	temp, err := os.CreateTemp(filepath.Dir(f.Tmp), "spec-*.tmp")
	if err != nil {
		return "", err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		os.Remove(temp.Name())
		return "", err
	}
	if err := temp.Close(); err != nil {
		os.Remove(temp.Name())
		return "", err
	}
	return path, os.Rename(temp.Name(), path)
}

// fenceLimit is what this kernel leaves out of the fence, "" when nothing.
func fenceLimit() string {
	switch thisFenceMode() {
	case fenceFull:
		return ""
	case fenceLandlock:
		return FencePartialLine
	}
	return FenceDegradedLine
}

// runFenceHelper runs inside the new namespaces; it only returns on failure.
func runFenceHelper(specPath, path string, argv []string) int {
	var spec fenceSpec
	raw, err := os.ReadFile(specPath)
	if err == nil {
		err = json.Unmarshal(raw, &spec)
	}
	if err != nil {
		os.Stderr.WriteString("jail: " + err.Error() + "\n")
		return 126
	}
	f := spec.Fence
	if spec.Mode == fenceLandlock {
		if err := fenceLandlockOnly(&f); err != nil {
			os.Stderr.WriteString("jail: " + err.Error() + "\n")
			return 126
		}
		err = syscall.Exec(path, argv, os.Environ())
		os.Stderr.WriteString("jail: exec: " + err.Error() + "\n")
		return 127
	}
	steps := []func() error{
		func() error { return unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, "") },
		func() error { return fenceTmp(&f) },
		func() error { return fenceHide(&f) },
		func() error { return fenceReadOnly(&f) },
		func() error { return fenceLandlockRules(&f) },
		dropCaps,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			os.Stderr.WriteString("jail: " + err.Error() + "\n")
			return 126
		}
	}
	err = syscall.Exec(path, argv, os.Environ())
	os.Stderr.WriteString("jail: exec: " + err.Error() + "\n")
	return 127
}

// fenceSharesTmp is whether the job's own folders live under the shared /tmp,
// which then stays as it is: binding over it would hide them.
func fenceSharesTmp(f *Fence) bool {
	for _, p := range []string{f.Root, f.Tmp, f.Cache} {
		if livesInTmp(p) {
			return true
		}
	}
	return false
}

// fenceTmp gives the command the job's own temp folder at /tmp, so a tool that
// writes /tmp by name writes beside the job and finds there what it wrote on
// the last call.
func fenceTmp(f *Fence) error {
	if fenceSharesTmp(f) {
		return nil
	}
	return unix.Mount(f.Tmp, tmpHome, "", unix.MS_BIND|unix.MS_REC, "")
}

// fenceHide covers each hidden path that exists: a folder with an empty
// unreadable tmpfs, a file or a socket with the unreadable blank file.
func fenceHide(f *Fence) error {
	for _, p := range append(append([]string(nil), f.Hidden...), f.Sockets...) {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.IsDir() {
			err = mountTmpfs(p, "mode=000")
		} else {
			err = unix.Mount(f.Blank, p, "", unix.MS_BIND, "")
		}
		if err != nil {
			return errors.New("hide " + p + ": " + err.Error())
		}
	}
	return nil
}

// lockedFlags are the mount flags a user namespace may not drop on a remount,
// so a read-only remount carries whichever of them the mount already has.
const lockedFlags = unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC | unix.MS_NOATIME | unix.MS_NODIRATIME | unix.MS_RELATIME

// fenceReadOnly binds each read-only path that exists over itself, read-only.
func fenceReadOnly(f *Fence) error {
	for _, p := range f.ReadOnly {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := unix.Mount(p, p, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
			return errors.New("read-only " + p + ": " + err.Error())
		}
		var st unix.Statfs_t
		if err := unix.Statfs(p, &st); err != nil {
			return err
		}
		flags := uintptr(unix.MS_BIND|unix.MS_REMOUNT|unix.MS_RDONLY) | (uintptr(st.Flags) & lockedFlags)
		if err := unix.Mount("", p, "", flags, ""); err != nil {
			return errors.New("read-only " + p + ": " + err.Error())
		}
	}
	return nil
}

// fenceLandlockRules reads anywhere and writes only where the fence says.
func fenceLandlockRules(f *Fence) error {
	write := writeAccess(landlockABI())
	fd, err := createRuleset(readAccess | write)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	rules := map[string]uint64{"/": readAccess, "/dev": readAccess | write, f.Root: readAccess | write,
		f.Tmp: readAccess | write, f.Cache: readAccess | write}
	if !fenceSharesTmp(f) {
		rules[tmpHome] = readAccess | write
	}
	for _, p := range f.Writable {
		rules[p] = readAccess | write
	}
	for path, access := range rules {
		if err := allow(fd, path, access); err != nil {
			if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ENOTDIR) {
				continue
			}
			return errors.New("limit " + filepath.Clean(path) + ": " + err.Error())
		}
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	_, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, uintptr(fd), 0, 0)
	return errnoErr(errno)
}

// fileAccess is the part of an access mask Landlock accepts on a file; the
// directory rights are refused there.
const fileAccess = unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_READ_FILE |
	unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_TRUNCATE | unix.LANDLOCK_ACCESS_FS_IOCTL_DEV

// fileRead is what reading a file needs: its bytes, or running it.
const fileRead = unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_READ_FILE

// fenceLandlockOnly is the whole fence from Landlock alone. Landlock only
// grants, so a path is kept from being read by granting everything AROUND it
// ([avoiding]). Listing a folder stays open everywhere, so a hidden folder's
// names can be seen and its files cannot be opened. The repository's hooks
// and config are kept read-only the same way when the git folder is outside
// the checkout (a worktree's); the checkout's own .git, and its .codeaf, are
// written like the rest of the checkout, which [FencePartialLine] says.
func fenceLandlockOnly(f *Fence) error {
	write := writeAccess(landlockABI())
	fd, err := createRuleset(readAccess | write)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	rules := map[string]uint64{"/": unix.LANDLOCK_ACCESS_FS_READ_DIR}
	add := func(p string, access uint64) { rules[p] |= access }
	for _, p := range f.ReadGrants {
		add(p, fileRead)
	}
	for _, p := range []string{"/dev", tmpHome, f.Root, f.Tmp, f.Cache} {
		add(p, readAccess|write)
	}
	for _, w := range f.Writable {
		var keep []string
		for _, ro := range f.ReadOnly {
			if within(ro, w) && !within(ro, f.Root) {
				keep = append(keep, ro)
			}
		}
		if len(keep) == 0 {
			add(w, readAccess|write)
			continue
		}
		for p := range avoiding(w, keep) {
			add(p, readAccess|write)
		}
	}
	for path, access := range rules {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			access &= fileAccess
		}
		if err := allow(fd, path, access); err != nil {
			if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ENOTDIR) || errors.Is(err, unix.EACCES) {
				continue
			}
			return errors.New("limit " + filepath.Clean(path) + ": " + err.Error())
		}
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	_, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, uintptr(fd), 0, 0)
	return errnoErr(errno)
}
