package blobstore

import "bytes"

// objectMagics are the only leading bytes a stored object may carry. A sealed
// object always starts with one of them, so a frame holding anything else, above
// all plaintext, is refused before it can reach a store (L6). AGEO is an
// engine object; AGEV is a vault object.
var objectMagics = [][]byte{
	[]byte("AGEO\x01"),
	[]byte("AGEV\x01"),
}

// hasObjectMagic reports whether b starts with an allowed object magic.
func hasObjectMagic(b []byte) bool {
	for _, m := range objectMagics {
		if bytes.HasPrefix(b, m) {
			return true
		}
	}
	return false
}
