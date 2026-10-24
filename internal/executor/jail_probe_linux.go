//go:build linux

package executor

import (
	"os"
	"os/exec"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// Capabilities is what this kernel lets an unprivileged process confine with.
type Capabilities struct {
	// UserNS: a private user, mount and network namespace can be created.
	UserNS bool
	// Landlock: the kernel enforces Landlock filesystem rules.
	Landlock bool
	// LandlockABI is the Landlock ABI version, zero when absent.
	LandlockABI int
}

var (
	probeOnce = new(sync.Once)
	probed    Capabilities
)

// Probe reports the confinement capabilities, measured once and cached.
func Probe() Capabilities {
	if namespaceProbeInTests != nil {
		abi := landlockABI()
		return Capabilities{UserNS: namespaceProbeInTests(), Landlock: abi > 0, LandlockABI: abi}
	}
	probeOnce.Do(func() {
		abi := landlockABI()
		probed = Capabilities{UserNS: userNSWorks(), Landlock: abi > 0, LandlockABI: abi}
	})
	return probed
}

func landlockABI() int {
	v, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 {
		return 0
	}
	return int(v)
}

// userNSWorks re-executes this binary with the jail's exact namespace and
// ambient-capability attributes. Running an unrelated program misses hosts
// whose security policy permits clone but refuses the helper exec.
func userNSWorks() bool {
	cmd := exec.Command("/proc/self/exe") //codeaf:plumbing capability probe of the jail itself
	cmd.Args = []string{helperName, "probe"}
	cmd.SysProcAttr = namespaceAttr(true)
	return runNamespaceProbe(cmd) == nil
}

// namespaceAttr builds the clone attributes: a user namespace mapping the
// caller to itself, a private mount namespace, and optionally a network one.
// CAP_SYS_ADMIN survives the exec into the helper as an ambient capability.
func namespaceAttr(newNet bool) *syscall.SysProcAttr {
	flags := uintptr(syscall.CLONE_NEWUSER | syscall.CLONE_NEWNS)
	if newNet {
		flags |= syscall.CLONE_NEWNET
	}
	uid, gid := os.Getuid(), os.Getgid()
	return &syscall.SysProcAttr{
		Cloneflags:  flags,
		UidMappings: []syscall.SysProcIDMap{{ContainerID: uid, HostID: uid, Size: 1}},
		GidMappings: []syscall.SysProcIDMap{{ContainerID: gid, HostID: gid, Size: 1}},
		AmbientCaps: []uintptr{unix.CAP_SYS_ADMIN},
	}
}

// namespaceProbeInTests stands in for hosts that allow clone but deny the
// helper exec. Only test support installs it; production always measures once.
var namespaceProbeInTests func() bool

// UseNamespaceProbeInTests supplies the helper-exec answer for a test and
// returns its restoration, without changing the production probe's cache.
func UseNamespaceProbeInTests(probe func() bool) func() {
	previous := namespaceProbeInTests
	namespaceProbeInTests = probe
	return func() { namespaceProbeInTests = previous }
}

// runNamespaceProbe is the exec boundary tests use to reproduce a denied
// helper after namespace creation, without depending on AppArmor being installed.
var runNamespaceProbe = func(cmd *exec.Cmd) error { return cmd.Run() }
