package blobstore

import "errors"

// gatherPrefix is GetMany for any store that can answer one object at a time:
// it reads rids in order until a frame's worth of bytes is in hand, and stops
// short at an absent object once it has one. Every implementation answers the
// same prefix this way, so the wire and the fake cannot disagree about it.
func gatherPrefix(rids []string, get func(rid string) ([]byte, error)) ([]Object, error) {
	var out []Object
	size := 0
	for _, rid := range rids {
		if size >= TargetFrame {
			break
		}
		b, err := get(rid)
		if errors.Is(err, ErrNotFound) && len(out) > 0 {
			break
		}
		if err != nil {
			return nil, err
		}
		out = append(out, Object{RID: rid, Bytes: b})
		size += len(b)
	}
	return out, nil
}

// sizeOf is the bytes the objects hold, which is what a Get of each would count.
func sizeOf(objects []Object) int64 {
	var n int64
	for _, o := range objects {
		n += int64(len(o.Bytes))
	}
	return n
}
