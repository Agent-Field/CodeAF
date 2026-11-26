//go:build linux

package sessionhost

import (
	"errors"

	"golang.org/x/sys/unix"
)

// supervisorCaps are the only capabilities the supervisor needs: CHOWN for the
// one fresh folder it hands to a session uid, KILL for the sweep that ends a
// session, SETUID and SETGID for the helper's identity switch, and SETPCAP
// because the helper must empty the capability bounding set before it gives the
// privilege away. THERE IS NO DAC_OVERRIDE AND NO FOWNER: a root that can ignore
// file modes while it works near folders a session can write is the classic way
// for a session to talk the supervisor into overwriting something it owns.
var supervisorCaps = []uintptr{
	unix.CAP_CHOWN, unix.CAP_KILL, unix.CAP_SETGID, unix.CAP_SETUID, unix.CAP_SETPCAP,
}

// RestrictSupervisor takes every other capability away from this process, in the
// bounding set (so nothing it starts can regain one) and in the effective,
// permitted and inheritable sets. On Docker the container already starts with
// only these five (the Compose file says so), so this is a no-op there; on a
// platform that hands root a larger set, such as a Railway service, it is what
// makes "the supervisor holds five capabilities" true. A process that is not
// root, or that lacks SETPCAP, is left as it is: the identity switch then
// refuses, which is the only thing that matters.
func RestrictSupervisor() error {
	if unix.Geteuid() != 0 {
		return nil
	}
	keep := map[uintptr]bool{}
	for _, c := range supervisorCaps {
		keep[c] = true
	}
	for c := uintptr(0); ; c++ {
		present, err := unix.PrctlRetInt(unix.PR_CAPBSET_READ, c, 0, 0, 0)
		if errors.Is(err, unix.EINVAL) {
			break
		}
		if err != nil {
			return errors.New("Could not read the process capability set.")
		}
		if present == 1 && !keep[c] {
			if unix.Prctl(unix.PR_CAPBSET_DROP, c, 0, 0, 0) != nil {
				return errors.New("Could not drop the extra process capabilities.")
			}
		}
	}
	var mask uint32
	for _, c := range supervisorCaps {
		mask |= 1 << c
	}
	var data [2]unix.CapUserData
	data[0] = unix.CapUserData{Effective: mask, Permitted: mask}
	if unix.Capset(&unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}, &data[0]) != nil {
		return errors.New("Could not narrow the process capabilities.")
	}
	return nil
}
