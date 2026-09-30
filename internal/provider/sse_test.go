package provider

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The decoder replaces the SDK's, so the only thing worth testing is that it
// *is* a replacement: same chunks, in the same order, ending on the same error,
// for every stream shape a provider has been seen to send. The SDK's decoder is
// the oracle, run over the same bytes.

// drain reads a decoder to completion. Both decoders have the same shape, so
// one helper reads either.
func drain(next func() (ai.StreamChunk, error)) ([]ai.StreamChunk, error) {
	var chunks []ai.StreamChunk
	for {
		chunk, err := next()
		if err != nil {
			return chunks, err
		}
		chunks = append(chunks, chunk)
	}
}

// chopped hands out the stream in fixed-size pieces, so an event lands across a
// read boundary wherever the size says it does.
type chopped struct {
	data []byte
	size int
}

func (c *chopped) Read(p []byte) (int, error) {
	if len(c.data) == 0 {
		return 0, io.EOF
	}
	n := min(min(c.size, len(p)), len(c.data))
	copy(p, c.data[:n])
	c.data = c.data[n:]
	return n, nil
}

// trailing returns its last bytes together with io.EOF, which is what a real
// body does and what the SDK's decoder was fixed to handle.
type trailing struct {
	data []byte
}

func (t *trailing) Read(p []byte) (int, error) {
	if len(t.data) == 0 {
		return 0, io.EOF
	}
	n := min(len(p), len(t.data))
	copy(p, t.data[:n])
	t.data = t.data[n:]
	if len(t.data) == 0 {
		return n, io.EOF
	}
	return n, nil
}

func dataEvent(id, text string) string {
	return fmt.Sprintf(`data: {"id":%q,"object":"chat.completion.chunk","created":1,"model":"vendor/model","choices":[{"index":0,"delta":{"content":%q},"finish_reason":null}]}`+"\n\n", id, text)
}

// stream is one synthetic body and the read sizes to cut it at. The sizes are
// per-stream because the oracle is quadratic: handing a four-megabyte message
// to the SDK's decoder one byte at a time would take hours, which is the very
// property under repair.
type stream struct {
	name  string
	body  string
	sizes []int
}

func sseStreams(t *testing.T) []stream {
	t.Helper()

	// A reasoning block delivered whole: one message, several megabytes, which
	// is the case the SDK's decoder is quadratic on.
	huge := strings.Repeat("reasoning about the problem at some length. ", 100_000)

	var ordinary strings.Builder
	for i := range 200 {
		ordinary.WriteString(dataEvent(fmt.Sprintf("chunk-%d", i), fmt.Sprintf("token %d ", i)))
	}
	ordinary.WriteString("data: [DONE]\n\n")

	var withNoise strings.Builder
	withNoise.WriteString(": OPENROUTER PROCESSING\n\n")
	withNoise.WriteString(dataEvent("one", "hello"))
	withNoise.WriteString(":\n\n")
	withNoise.WriteString("event: ping\ndata: {}\n\n")
	withNoise.WriteString(dataEvent("two", " world"))
	withNoise.WriteString("data: not json at all\n\n")
	withNoise.WriteString(`data: {"usage":{"prompt_tokens":11,"completion_tokens":22,"total_tokens":33}}` + "\n\n")
	withNoise.WriteString("data: [DONE]\n\n")

	small := []int{1, 3, 8192, 65536, 1 << 20}
	large := []int{8192, 1 << 20}

	return []stream{
		{"ordinary", ordinary.String(), small},
		{"noise and comments", withNoise.String(), small},
		{"one huge message", dataEvent("big", huge) + "data: [DONE]\n\n", large},
		{"huge among small", dataEvent("first", "a") + dataEvent("big", huge) +
			dataEvent("last", "z") + "data: [DONE]\n\n", large},
		// CRLF has no "\n\n" in it anywhere, so the SDK's decoder finds no
		// message at all and ends on the read error. Replacing that would be a
		// change in behaviour, so it is pinned instead.
		{"crlf", strings.ReplaceAll(dataEvent("one", "hello")+dataEvent("two", "there")+"data: [DONE]\n\n", "\n", "\r\n"), small},
		// No terminator on the last message, and no [DONE]: the stream just
		// stops, which is what a dropped connection looks like.
		{"truncated tail", dataEvent("one", "hello") + `data: {"id":"two"`, small},
		{"empty", "", small},
		{"blank lines", "\n\n\n\n" + dataEvent("one", "hello") + "\n\n" + "data: [DONE]\n\n", small},
		{"no space after data", `data:{"id":"one"}` + "\n\n" + dataEvent("two", "b") + "data: [DONE]\n\n", small},
	}
}

// TestSSEDecoderMatchesTheSDKChunkForChunk is the whole contract. Anything the
// SDK's decoder would have delivered, this one delivers, including where it
// stops and what it stops with.
func TestSSEDecoderMatchesTheSDKChunkForChunk(t *testing.T) {
	for _, sample := range sseStreams(t) {
		for _, size := range sample.sizes {
			t.Run(fmt.Sprintf("%s/reads of %d", sample.name, size), func(t *testing.T) {
				old := ai.NewSSEDecoder(&chopped{data: []byte(sample.body), size: size})
				wantChunks, wantErr := drain(old.Decode)

				fresh := newSSEDecoder(&chopped{data: []byte(sample.body), size: size})
				gotChunks, gotErr := drain(fresh.Decode)

				if !errors.Is(gotErr, wantErr) {
					t.Fatalf("ended with %v, the SDK ended with %v", gotErr, wantErr)
				}
				if len(gotChunks) != len(wantChunks) {
					t.Fatalf("decoded %d chunks, the SDK decoded %d", len(gotChunks), len(wantChunks))
				}
				for i := range wantChunks {
					if !reflect.DeepEqual(gotChunks[i], wantChunks[i]) {
						t.Fatalf("chunk %d differs:\n got %+v\nwant %+v", i, gotChunks[i], wantChunks[i])
					}
				}
			})
		}
	}
}

// A read that returns its last bytes alongside io.EOF must not lose them —
// the terminal usage-accounting chunk arrives that way.
func TestSSEDecoderKeepsBytesDeliveredWithEOF(t *testing.T) {
	stream := dataEvent("one", "hello") +
		`data: {"usage":{"prompt_tokens":7,"completion_tokens":9,"total_tokens":16}}` + "\n\n"

	old := ai.NewSSEDecoder(&trailing{data: []byte(stream)})
	wantChunks, _ := drain(old.Decode)
	fresh := newSSEDecoder(&trailing{data: []byte(stream)})
	gotChunks, _ := drain(fresh.Decode)

	if !reflect.DeepEqual(gotChunks, wantChunks) {
		t.Fatalf("decoded %+v, the SDK decoded %+v", gotChunks, wantChunks)
	}
	if len(gotChunks) != 2 || gotChunks[1].Usage == nil || gotChunks[1].Usage.TotalTokens != 16 {
		t.Fatalf("the usage chunk did not survive the EOF: %+v", gotChunks)
	}
}

// Read boundaries chosen at random, because the ones chosen by hand are the
// ones the implementation already handles.
func TestSSEDecoderMatchesTheSDKOnRandomReadBoundaries(t *testing.T) {
	random := rand.New(rand.NewSource(1))
	var stream strings.Builder
	for i := range 60 {
		stream.WriteString(dataEvent(fmt.Sprintf("c%d", i), strings.Repeat("x", random.Intn(5000))))
		if i%7 == 0 {
			stream.WriteString(": keepalive\n\n")
		}
	}
	stream.WriteString("data: [DONE]\n\n")
	raw := []byte(stream.String())

	old := ai.NewSSEDecoder(bytes.NewReader(raw))
	want, _ := drain(old.Decode)

	for range 25 {
		fresh := newSSEDecoder(&ragged{data: raw, random: random})
		got, _ := drain(fresh.Decode)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("random read boundaries changed the decoding: %d chunks against %d", len(got), len(want))
		}
	}
}

type ragged struct {
	data   []byte
	random *rand.Rand
}

func (r *ragged) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := min(min(1+r.random.Intn(9000), len(p)), len(r.data))
	copy(p, r.data[:n])
	r.data = r.data[n:]
	return n, nil
}

// The reason the decoder was written. The SDK copies its undelivered buffer
// twice per read, so a four-megabyte message costs about a gigabyte of memcpy;
// this allocates the message once. Run with -bench to see the difference.
func BenchmarkSSEDecoderOneHugeMessage(b *testing.B) {
	stream := []byte(dataEvent("big", strings.Repeat("reasoning. ", 400_000)) + "data: [DONE]\n\n")
	b.Run("ours", func(b *testing.B) {
		for range b.N {
			decoder := newSSEDecoder(&chopped{data: stream, size: 8192})
			drain(decoder.Decode)
		}
	})
	b.Run("sdk", func(b *testing.B) {
		for range b.N {
			decoder := ai.NewSSEDecoder(&chopped{data: stream, size: 8192})
			drain(decoder.Decode)
		}
	})
}

// TestKeepaliveCommentsReachTheAliveSeam pins the decoder's one report about
// lines that never become chunks: each SSE comment — the ": OPENROUTER
// PROCESSING" a router sends while an upstream assembles its answer — is
// worth one alive() call, and data lines are worth none, because the stall
// watch already hears those as progress (streamguard.go says what each buys).
func TestKeepaliveCommentsReachTheAliveSeam(t *testing.T) {
	alive := 0
	decoder := newSSEDecoder(strings.NewReader(
		": OPENROUTER PROCESSING\n\n: OPENROUTER PROCESSING\n\n" +
			"data: {\"id\":\"one\"}\n\n" +
			": OPENROUTER PROCESSING\n\n" +
			"data: [DONE]\n\n"))
	decoder.alive = func() { alive++ }
	chunk, err := decoder.DecodeChunk()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if chunk.ID != "one" {
		t.Fatalf("chunk id = %q, the comments must not eat the data", chunk.ID)
	}
	if _, err := decoder.DecodeChunk(); !errors.Is(err, io.EOF) {
		t.Fatalf("end = %v, want io.EOF", err)
	}
	if !decoder.done {
		t.Fatal("the explicit [DONE] marker was not retained by the decoder")
	}
	if alive != 3 {
		t.Fatalf("alive calls = %d, want one per comment line", alive)
	}
}
