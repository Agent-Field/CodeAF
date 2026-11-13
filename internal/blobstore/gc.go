package blobstore

import "time"

// Candidate retention rules for append-only frames. No frame sweep runs today.
// These rules retain named, recent and vault frames, but cannot prove that the
// remaining frames are collectible: directory plans are subset hints, and a
// later publish may reuse objects from an unnamed earlier frame. A multi-location
// index alone does not supply the missing reachability proof.
//
// Before any sweep is enabled, clients must provide complete authenticated
// reachability evidence covering retained heads/history, in-flight transfers and
// recovery, with fenced epochs and grace periods. Rotation namespace retirement
// remains a separate operation. Do not turn this candidate set into deletion
// authority or interpret its complement as unreachable storage.

// LiveFrames returns the minimum set retained by the supplied hints and times.
// It does not prove that frames absent from the result are dead. `frames` names
// stored frames and their put times; `plans` contains available frame hints;
// `vault` is the recorded vault frame, or empty; `cutoff` ends the grace window.
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
