package blobstore

import "time"

// The garbage-collection rules for append-only frames (contract §22.9). Frames
// are never deleted today; this file is the design as executable rules, so the
// Worker and the Go relay sweep to the same answer when the sweeps land. What
// the sweeps still need is the multi-location index: `objs` keeps one row per
// rid (first location wins), so deleting a frame could strand an object
// another frame also holds. Until that index exists these rules are only
// tested, never run.
//
// The rules, in one paragraph: a frame is live while any cell record's plan
// names it, or it is younger than a grace window (a publish in flight has
// uploaded frames no record names yet), or it is the vault frame the identity
// record names. A sweep tombstones a collectible frame first and deletes its
// bytes only after a second, longer window, so a take that read a plan just
// before the sweep still finishes: its frame get is answered or 404s into the
// want loop, and both are correct. Rotation (contract §20) is unaffected: the
// retire sweep deletes the whole namespace, and no sweep runs while a rotation
// is frozen or retiring.

// LiveFrames marks the frames a sweep must keep, by the rule above. `frames`
// is every frame the relay holds with its put time; `plans` is the frames
// field of every live cell record; `vault` is the identity's vault frame, ""
// when there is none; `cutoff` is now minus the grace window.
func LiveFrames(frames map[FrameID]time.Time, plans [][]string, vault FrameID, cutoff time.Time) map[FrameID]bool {
	live := make(map[FrameID]bool, len(frames))
	for _, plan := range plans {
		for _, id := range plan {
			live[FrameID(id)] = true
		}
	}
	if vault != "" {
		live[vault] = true
	}
	for id, put := range frames {
		if put.After(cutoff) {
			live[id] = true
		}
	}
	return live
}

// Locations is the multi-location index a sweep requires: every frame that
// holds an object, not only the first one written.
type Locations map[string][]Location

// DropFrame retires one frame from the index: an object it held keeps the
// locations its other frames give it, and an object no other frame holds is
// gone. Gets and locates read any row that is left, so the object stays
// answerable while one live frame carries it.
func DropFrame(locs Locations, frame FrameID) Locations {
	out := make(Locations, len(locs))
	for rid, at := range locs {
		for _, l := range at {
			if l.Frame != frame {
				out[rid] = append(out[rid], l)
			}
		}
		if len(out[rid]) == 0 {
			delete(out, rid)
		}
	}
	return out
}
