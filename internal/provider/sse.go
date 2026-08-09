package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// sseDecoder reads the provider's event stream one message at a time.
//
// The SDK ships a decoder and this one exists because that one is quadratic in
// the length of a single message: it keeps an undelivered []byte, and every
// call converts the whole of it to a string to look for the blank line and
// converts the remainder back. On ordinary token deltas nobody could measure
// it. On one multi-megabyte message — a reasoning block delivered whole — the
// buffer is re-copied twice per 8 KB read, which is about a gigabyte of memcpy
// to deliver four megabytes. This reads through a bufio.Reader instead, so the
// bytes of a message are copied exactly once, and the SDK is left alone.
//
// It is a *replacement*, not an improvement: the framing below is the SDK's,
// quirks included, because a stream that decoded differently here would change
// answers. Messages are separated by "\n\n" and by nothing else — a CRLF
// stream has no separator at all as far as this is concerned, exactly as
// before — only a message beginning "data: " is looked at, "[DONE]" ends the
// stream with io.EOF, and anything that will not parse as JSON is skipped.
type sseDecoder struct {
	reader  *bufio.Reader
	message []byte
}

// sseReadBuffer is the read size. Larger than the SDK's 8 KB because the read
// is now the only copy: a bigger buffer is fewer syscalls and no more memory
// traffic.
const sseReadBuffer = 64 << 10

func newSSEDecoder(reader io.Reader) *sseDecoder {
	return &sseDecoder{reader: bufio.NewReaderSize(reader, sseReadBuffer)}
}

// Decode returns the next chunk, io.EOF at the end of the stream, and whatever
// the underlying reader failed with otherwise.
func (d *sseDecoder) Decode() (ai.StreamChunk, error) {
	for {
		line, err := d.reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			// One SSE line may be the whole message and the whole message may
			// be megabytes. A piece that does not fit the read buffer carries
			// no '\n' at all, so it is stitched onto the message and cannot be
			// mistaken for the blank line below.
			d.message = append(d.message, line...)
			continue
		}
		if len(line) > 0 {
			// The separator is "\n\n": this line opens with a newline and the
			// message so far ended with one. Everything before that first
			// newline is the message; the rest of the buffer is the next one.
			if line[0] == '\n' && len(d.message) > 0 && d.message[len(d.message)-1] == '\n' {
				chunk, delivered, done := parseSSEMessage(d.message[:len(d.message)-1])
				d.message = d.message[:0]
				switch {
				case done:
					return ai.StreamChunk{}, io.EOF
				case delivered:
					return chunk, nil
				}
				continue
			}
			d.message = append(d.message, line...)
		}
		if err != nil {
			// Per the io.Reader contract a read may return bytes alongside its
			// error, and the bytes are consumed above before the error is
			// surfaced here. A partial trailing message is dropped, which is
			// what a message with no separator has always been.
			return ai.StreamChunk{}, err
		}
	}
}

// parseSSEMessage is the SDK's message handling, byte for byte: delivered says
// the chunk is one to hand back, done says the stream said [DONE].
func parseSSEMessage(message []byte) (chunk ai.StreamChunk, delivered, done bool) {
	const prefix = "data: "
	if !bytes.HasPrefix(message, []byte(prefix)) {
		// A comment, a keepalive, an event: line, a multi-line message — none
		// of them are chunks and none of them stop the stream.
		return ai.StreamChunk{}, false, false
	}
	payload := bytes.TrimSpace(message[len(prefix):])
	if bytes.Equal(payload, []byte("[DONE]")) {
		return ai.StreamChunk{}, false, true
	}
	if err := json.Unmarshal(payload, &chunk); err != nil {
		return ai.StreamChunk{}, false, false
	}
	return chunk, true, false
}
