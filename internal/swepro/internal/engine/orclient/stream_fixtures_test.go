package orclient

// Replays testdata/stream-fixtures.json, produced by
// tools/fixtures/gen-orstream.ts by driving the REAL
// `OpenRouterChatLanguageModel.doStream` from @openrouter/ai-sdk-provider@2.8.1
// over canned SSE bodies.
//
// This is the ENGINE-DESIGN §7.1 "highest-value generator": it pins the SSE
// framing, the zod schemas, the tool-call accumulator (both kept bugs), the
// `!textStarted` reasoning gate, the live reasoning-details reference, the two
// synthetic finish-reason promotions, and the flush — all without a network.
//
// Comparison is BYTE EQUALITY of jscompat.Stringify(goParts) against the
// recorded JSON.stringify, with one documented redaction: the `error` value of
// a zod PARSE FAILURE is a serialised AI_TypeValidationError carrying zod's own
// multi-kilobyte issue text, which this port deliberately does not reproduce
// (see wire.go). The generator and the replay apply the same redaction, so the
// PART SEQUENCE and the error's discriminating `name` are still asserted.

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type fixtureCase struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

type streamArgs struct {
	SSE  string  `json:"sse"`
	Seed float64 `json:"seed"`
}

func loadFixtures(t *testing.T, name string) []fixtureCase {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 16<<20)
	var out []fixtureCase
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var c fixtureCase
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatalf("decode fixture line: %v", err)
		}
		out = append(out, c)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return out
}

// runSSE drives the translator over a raw SSE body exactly the way
// `parseJsonEventStream` → the TransformStream pipeline does: decode a frame,
// drop `[DONE]`, parse, transform; flush at end of stream.
func runSSE(raw string, seed uint32) (parts []StreamPart, thrown error) {
	restore := SetRandomForTesting(jscompat.Mulberry32(seed))
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

// redactErrorParts mirrors the generator's `redact`.
func redactErrorParts(t *testing.T, parts []StreamPart) []json.RawMessage {
	t.Helper()
	out := make([]json.RawMessage, 0, len(parts))
	for _, p := range parts {
		encoded, err := jscompat.Stringify(p)
		if err != nil {
			t.Fatalf("stringify part: %v", err)
		}
		if p.PartType() != PartTypeError {
			out = append(out, encoded)
			continue
		}
		var probe struct {
			Error struct {
				Name string `json:"name"`
			} `json:"error"`
		}
		if err := json.Unmarshal(encoded, &probe); err == nil &&
			(probe.Error.Name == "AI_TypeValidationError" || probe.Error.Name == "AI_JSONParseError") {
			out = append(out, json.RawMessage(`{"type":"error","error":{"__redacted":`+quote(probe.Error.Name)+`}}`))
			continue
		}
		out = append(out, encoded)
	}
	return out
}

func quote(s string) string {
	b, err := jscompat.Stringify(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

func TestStreamFixtures(t *testing.T) {
	cases := loadFixtures(t, "stream-fixtures.json")
	if len(cases) < 60 {
		t.Fatalf("stream fixture corpus looks truncated: %d cases", len(cases))
	}
	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			if c.Fn != "doStream" {
				t.Fatalf("unexpected fn %q", c.Fn)
			}
			var args streamArgs
			if err := json.Unmarshal([]byte(c.ArgsJSON), &args); err != nil {
				t.Fatalf("decode args: %v", err)
			}
			parts, thrown := runSSE(args.SSE, uint32(args.Seed))

			var want struct {
				Ok    bool              `json:"ok"`
				Parts []json.RawMessage `json:"parts"`
				Error string            `json:"error"`
			}
			if err := json.Unmarshal([]byte(c.OutJSON), &want); err != nil {
				t.Fatalf("decode expected: %v", err)
			}

			if want.Ok != (thrown == nil) {
				t.Fatalf("ok mismatch: want %v, got err=%v", want.Ok, thrown)
			}
			if !want.Ok {
				if thrown.Error() != want.Error {
					t.Errorf("throw message mismatch:\n want %q\n  got %q", want.Error, thrown.Error())
				}
			}

			got := redactErrorParts(t, parts)
			if len(got) != len(want.Parts) {
				t.Fatalf("part count mismatch: want %d, got %d\n want %s\n  got %s",
					len(want.Parts), len(got), joinRaw(want.Parts), joinRaw(got))
			}
			for i := range got {
				if string(got[i]) != string(want.Parts[i]) {
					t.Errorf("part %d mismatch:\n want %s\n  got %s", i, want.Parts[i], got[i])
				}
			}
		})
	}
}

func joinRaw(parts []json.RawMessage) string {
	var sb strings.Builder
	sb.WriteByte('[')
	for i, p := range parts {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.Write(p)
	}
	sb.WriteByte(']')
	return sb.String()
}

// TestToolCallResendGuard verifies the CODEAF_GO_FIX_ORCLIENT_TOOLCALL_RESEND
// flag: once a tool-call's accumulated arguments first parse as JSON and the
// burst fires, subsequent deltas for the same id must NOT re-emit
// tool-input-end + tool-call.
func TestToolCallResendGuard(t *testing.T) {
	t.Setenv("CODEAF_GO_FIX_ORCLIENT_TOOLCALL_RESEND", "1")

	// Same SSE that fixture BUGS-KEPT-6 uses — three tool-call deltas for
	// the same id, the first already parsable.
	const sse = "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"t1\",\"type\":\"function\",\"function\":{\"name\":\"f\",\"arguments\":\"{}\"}}]}}]}\n\ndata: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\"}}]}}]}\n\ndata: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\"}}]}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"

	parts, thrown := runSSE(sse, 1234)
	if thrown != nil {
		t.Fatalf("unexpected throw: %v", thrown)
	}

	// Count part types.
	var starts, deltas, ends, calls int
	for _, p := range parts {
		switch p.PartType() {
		case PartTypeToolInputStart:
			starts++
		case PartTypeToolInputDelta:
			deltas++
		case PartTypeToolInputEnd:
			ends++
		case PartTypeToolCall:
			calls++
		}
	}

	if starts != 1 {
		t.Errorf("want 1 tool-input-start, got %d", starts)
	}
	if deltas != 3 {
		t.Errorf("want 3 tool-input-delta, got %d", deltas)
	}
	if ends != 1 {
		t.Errorf("want 1 tool-input-end, got %d", ends)
	}
	if calls != 1 {
		t.Errorf("want 1 tool-call, got %d", calls)
	}
}
