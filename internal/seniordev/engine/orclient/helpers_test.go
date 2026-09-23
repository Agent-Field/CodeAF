//go:build !windows

package orclient

import (
	"io"
	"math/rand"
	"strings"
)

// Shared helpers for the tests in this package.

// runSSE drives the translator over a raw SSE body the way Stream.Next does:
// decode a frame, drop `[DONE]`, parse, transform; flush at end of stream.
func runSSE(raw string, seed uint32) (parts []StreamPart, thrown error) {
	restore := SetRandomForTesting(rand.New(rand.NewSource(int64(seed))).Float64)
	defer restore()

	tr := NewTranslator()
	dec := NewSSEDecoder(strings.NewReader(raw))
	for {
		ev, err := dec.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		if ev.Data == DoneSentinel {
			continue
		}
		emitted, err := tr.Transform(ParseChunk(ev.Data))
		parts = append(parts, emitted...)
		if err != nil {
			// A throw out of the transform tears the stream down: no flush.
			return parts, err
		}
	}
	return append(parts, tr.Flush()...), nil
}
