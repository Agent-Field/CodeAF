// aforge-embed: D6 — the dispatch envelope's disk floor is capped relative to
// the volume it is measuring. See EMBEDDING.md.
package scheduler

import (
	"math"
	"sync"
)

// diskFloorVolumeFraction is the largest share of a volume the free-space floor
// may claim.
//
// resourceguard's floor is a flat 5GB, ported from upstream where it is a fine
// number for a build host with hundreds of gigabytes. On a small volume it is
// not a floor at all, it is a wall: `isolation.ReadingToStatus` pauses dispatch
// below floor/2, so on a 5GB disk the pause arms at 2.5GB free and *no amount
// of cleanup this engine can do will ever clear it* — the volume cannot hold
// enough free space to satisfy a floor of its own size. A run measured in the
// field spent 12 scheduler cycles and 723 seconds pausing against exactly that,
// and every one of those cycles bought a paid orchestrator turn.
//
// A tenth of the volume is a floor a volume can actually clear: on a 5GB disk
// it asks for 512MB and pauses under 256MB, on a 500GB disk it does not bind at
// all and the ported 5GB stands.
const diskFloorVolumeFraction = 0.1

// volumeTotalGB reports the total size of the volume containing path. The
// second value is false when the volume cannot be measured, in which case the
// floor is left exactly as resourceguard computed it — an unmeasurable volume
// is never an argument for lowering a safety floor.
var volumeTotalGB = cachedVolumeTotalGB

// capDiskFloorGB returns the floor actually in force for path. Zero (the
// guard-disabled floor) and an unmeasurable volume are returned untouched.
func capDiskFloorGB(path string, floorGB float64) float64 {
	if floorGB <= 0 || math.IsNaN(floorGB) || math.IsInf(floorGB, 0) {
		return floorGB
	}
	totalGB, ok := volumeTotalGB(path)
	if !ok || totalGB <= 0 {
		return floorGB
	}
	return math.Min(floorGB, totalGB*diskFloorVolumeFraction)
}

var volumeTotals sync.Map

// cachedVolumeTotalGB memoizes by path: a volume does not change size under a
// run, and the measurement is a syscall on a path that may be slow to stat.
func cachedVolumeTotalGB(path string) (float64, bool) {
	if cached, ok := volumeTotals.Load(path); ok {
		total, isFloat := cached.(float64)
		return total, isFloat && total > 0
	}
	bytes, ok := volumeTotalBytes(path)
	if !ok || bytes <= 0 {
		return 0, false
	}
	total := bytes / math.Pow(1024, 3)
	volumeTotals.Store(path, total)
	return total, true
}
