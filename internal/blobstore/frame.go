package blobstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// A frame is the unit of upload: many sealed objects behind one header, so one
// request carries what would otherwise be many.
//
//	frame   = magic || u32le(header_len) || header || payload
//	header  = JSON Header
//	payload = the objects' bytes, concatenated in header order
var frameMagic = []byte("AGEF\x01")

// headerVersion is the only header version this package reads or writes.
const headerVersion = 1

// prefixLen is the fixed part in front of the header: the magic and its length.
var prefixLen = len(frameMagic) + 4

// Header is the JSON index at the front of a frame.
type Header struct {
	V         uint16      `json:"V"`
	CellKeyID string      `json:"cell_key_id"`
	Objects   []ObjectRef `json:"objects"`
}

// ObjectRef locates one object inside the payload.
type ObjectRef struct {
	RID string `json:"rid"`
	Off uint64 `json:"off"`
	Len uint32 `json:"len"`
}

// Object is one sealed object and the remote id it is stored under.
type Object struct {
	RID   string
	Bytes []byte
}

// IDOf is the identity of a frame: the hex SHA-256 of all of its bytes. The
// store computes it, so a writer never has to.
func IDOf(frame []byte) FrameID {
	sum := sha256.Sum256(frame)
	return hex.EncodeToString(sum[:])
}

// Encode packs objects into one frame. It runs the result through Decode, so
// this package can never write a frame that it would refuse to read.
func Encode(cellKeyID string, objects []Object) ([]byte, error) {
	refs, err := refsOf(objects)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(Header{V: headerVersion, CellKeyID: cellKeyID, Objects: refs})
	if err != nil {
		return nil, fmt.Errorf("blobstore: encode header: %w", err)
	}
	frame := join(raw, objects)
	if _, _, err := Decode(frame); err != nil {
		return nil, err
	}
	return frame, nil
}

// refsOf lays the objects out back to back, which is the only layout Decode accepts.
func refsOf(objects []Object) ([]ObjectRef, error) {
	refs := make([]ObjectRef, len(objects))
	var off uint64
	for i, o := range objects {
		if len(o.Bytes) > MaxFrame {
			return nil, bad("object is larger than a frame")
		}
		refs[i] = ObjectRef{RID: o.RID, Off: off, Len: uint32(len(o.Bytes))}
		off += uint64(len(o.Bytes))
	}
	return refs, nil
}

// join writes the frame around an already encoded header.
func join(header []byte, objects []Object) []byte {
	var out bytes.Buffer
	out.Write(frameMagic)
	out.Write(binary.LittleEndian.AppendUint32(nil, uint32(len(header))))
	out.Write(header)
	for _, o := range objects {
		out.Write(o.Bytes)
	}
	return out.Bytes()
}

// Decode validates a frame and returns its header and objects. The objects
// alias the frame bytes, so the caller must not change the frame while it
// still uses them. Every rule a frame can break is one check below, and any
// break is ErrBadFrame: a store that accepted a half-valid frame could not
// promise that everything it holds is sealed (L6) and addressable (L5).
func Decode(frame []byte) (Header, []Object, error) {
	if len(frame) > MaxFrame {
		return Header{}, nil, bad("frame is larger than the limit")
	}
	raw, payload, err := split(frame)
	if err != nil {
		return Header{}, nil, err
	}
	h, err := parseHeader(raw)
	if err != nil {
		return Header{}, nil, err
	}
	objects, err := carve(h.Objects, payload)
	if err != nil {
		return Header{}, nil, err
	}
	return h, objects, nil
}

// split cuts a frame into its header bytes and its payload after checking the
// magic and that the declared header length fits the limit and the frame.
func split(frame []byte) (header, payload []byte, err error) {
	if len(frame) < prefixLen || !bytes.HasPrefix(frame, frameMagic) {
		return nil, nil, bad("wrong magic")
	}
	n := binary.LittleEndian.Uint32(frame[len(frameMagic):prefixLen])
	if n > MaxHeader {
		return nil, nil, bad("header is larger than the limit")
	}
	if uint64(prefixLen)+uint64(n) > uint64(len(frame)) {
		return nil, nil, bad("header runs past the frame")
	}
	end := prefixLen + int(n)
	return frame[prefixLen:end], frame[end:], nil
}

// parseHeader reads the header JSON and checks its version and that it names
// at least one object; an empty frame would be a request that does nothing.
func parseHeader(raw []byte) (Header, error) {
	var h Header
	if err := json.Unmarshal(raw, &h); err != nil {
		return Header{}, bad("header is not valid JSON")
	}
	if h.V != headerVersion {
		return Header{}, bad("unknown header version")
	}
	if len(h.Objects) == 0 {
		return Header{}, bad("frame holds no object")
	}
	return h, nil
}

// carve cuts the payload into the objects the header names. Objects must start
// at zero and follow each other with no gap and no overlap, and must cover the
// payload exactly, so no byte of a frame is unaccounted for.
func carve(refs []ObjectRef, payload []byte) ([]Object, error) {
	objects := make([]Object, 0, len(refs))
	seen := make(map[string]bool, len(refs))
	var next uint64
	for _, ref := range refs {
		if err := checkRef(ref, next, seen); err != nil {
			return nil, err
		}
		end := next + uint64(ref.Len)
		if end > uint64(len(payload)) {
			return nil, bad("object runs past the payload")
		}
		// The capacity is cut to the length so an append by a caller cannot
		// overwrite the next object in the frame.
		body := payload[next:end:end]
		if !hasObjectMagic(body) {
			return nil, bad("object is not sealed")
		}
		seen[ref.RID] = true
		objects = append(objects, Object{RID: ref.RID, Bytes: body})
		next = end
	}
	if next != uint64(len(payload)) {
		return nil, bad("payload holds bytes no object names")
	}
	return objects, nil
}

// checkRef checks what a reference can say without looking at the payload.
func checkRef(ref ObjectRef, want uint64, seen map[string]bool) error {
	switch {
	case !ValidRID(ref.RID):
		return bad("object id is not 64 lowercase hex")
	case seen[ref.RID]:
		return bad("object id appears twice")
	case ref.Off != want:
		return bad("objects are not contiguous")
	}
	return nil
}

// bad is the one way this package says a frame is unacceptable.
func bad(why string) error {
	return fmt.Errorf("%w: %s", ErrBadFrame, why)
}
