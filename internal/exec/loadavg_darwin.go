package exec

import (
	"encoding/binary"

	"golang.org/x/sys/unix"
)

// vm.loadavg is a struct loadavg: three fixed-point averages followed by the
// scale they are expressed in. Reading it is one syscall with no process
// scan, which is what makes it cheap enough for the claim path.
const (
	loadavgFixptBytes = 4
	loadavgScaleAt    = 16
	loadavgScaleBytes = 8
	loadavgSize       = loadavgScaleAt + loadavgScaleBytes
)

func loadAverageOne() (float64, bool) {
	raw, err := unix.SysctlRaw("vm.loadavg")
	if err != nil || len(raw) < loadavgSize {
		return 0, false
	}
	scale := binary.NativeEndian.Uint64(raw[loadavgScaleAt : loadavgScaleAt+loadavgScaleBytes])
	if scale == 0 {
		return 0, false
	}
	return float64(binary.NativeEndian.Uint32(raw[:loadavgFixptBytes])) / float64(scale), true
}
