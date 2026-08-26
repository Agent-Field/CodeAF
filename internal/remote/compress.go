package remote

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// frameEncodingGzip is the capability name exchanged at the version door.
const frameEncodingGzip = "gzip"

// compressionThreshold is where gzip starts paying for its own header and CPU.
// It is one source for both halves: a payload below it stays the inspectable JSON
// line the protocol was designed around, while transcript-sized frames cross in
// compressed form when the door selected gzip.
const compressionThreshold = 32 << 10

func supportsEncoding(encodings []string, want string) bool {
	for _, encoding := range encodings {
		if encoding == want {
			return true
		}
	}
	return false
}

// compressFrame replaces only the payload, leaving the envelope readable and
// preserving one JSON line per frame. The receiver restores Payload before any
// method-specific code sees it, so compression cannot change method semantics.
func compressFrame(frame *Frame, encoding string) error {
	if encoding != frameEncodingGzip || len(frame.Payload) < compressionThreshold {
		return nil
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(frame.Payload); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	// Data is base64 inside the JSON envelope. A payload that does not get
	// smaller after that expansion stays plain, because compression is an
	// optimization and must never make an attachment or an already-compressed
	// document heavier on the wire.
	if base64.StdEncoding.EncodedLen(compressed.Len()) >= len(frame.Payload) {
		return nil
	}
	frame.Payload = nil
	frame.Encoding = frameEncodingGzip
	frame.Data = compressed.Bytes()
	return nil
}

// expandFrame restores the ordinary payload and refuses unknown or oversized
// encodings before json.Unmarshal can allocate from an untrusted compressed
// body. frameCap is already the wire's one ceiling, so it governs both forms.
func expandFrame(frame *Frame) error {
	if frame.Encoding == "" {
		return nil
	}
	if frame.Encoding != frameEncodingGzip {
		return fmt.Errorf("remote: unsupported frame encoding %q", frame.Encoding)
	}
	reader, err := gzip.NewReader(bytes.NewReader(frame.Data))
	if err != nil {
		return fmt.Errorf("remote: compressed frame: %w", err)
	}
	defer reader.Close()
	payload, err := io.ReadAll(io.LimitReader(reader, frameCap+1))
	if err != nil {
		return fmt.Errorf("remote: compressed frame: %w", err)
	}
	if len(payload) > frameCap {
		return errors.New("remote: expanded frame exceeds the frame limit")
	}
	frame.Payload = payload
	frame.Encoding = ""
	frame.Data = nil
	return nil
}

// flushFrame makes a frame visible when a caller deliberately supplied a
// buffered writer. Pipes and sockets have nothing to flush, but accepting an
// io.Writer means the protocol must finish the frame for writers that do.
func flushFrame(writer io.Writer) error {
	if flusher, ok := writer.(interface{ Flush() error }); ok {
		return flusher.Flush()
	}
	return nil
}
