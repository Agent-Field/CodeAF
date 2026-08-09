package msgmodel

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// Replays testdata/fixtures.json, produced by tools/fixtures/gen-msgmodel.ts
// from the real src/session/message-v2.ts (against ai@6.0.168). The gate is
// BYTE equality between jscompat.Stringify(goResult) and the JSON.stringify
// the TS run recorded.

type fixtureLine struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func loadFixtures(t *testing.T, path string) []fixtureLine {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 16<<20)
	var out []fixtureLine
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var fixture fixtureLine
		if err := json.Unmarshal(line, &fixture); err != nil {
			t.Fatalf("decode fixture line: %v", err)
		}
		out = append(out, fixture)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return out
}

func stringify(t *testing.T, v any) string {
	t.Helper()
	raw, err := jscompat.Stringify(v)
	if err != nil {
		t.Fatalf("stringify: %v", err)
	}
	return string(raw)
}

func decodeArgs(t *testing.T, argsJSON string, into any) {
	t.Helper()
	if err := json.Unmarshal([]byte(argsJSON), into); err != nil {
		t.Fatalf("decode args %s: %v", argsJSON, err)
	}
}

// ── argument envelopes (mirrored from gen-msgmodel.ts) ────────────────────

type toModelArgs struct {
	Input   []WithParts     `json:"input"`
	Model   Model           `json:"model"`
	Options *toModelOptions `json:"options"`
}

type toModelOptions struct {
	StripMedia         *bool    `json:"stripMedia"`
	ToolOutputMaxChars *float64 `json:"toolOutputMaxChars"`
}

func (o *toModelOptions) build() *ToModelOptions {
	if o == nil {
		return nil
	}
	return &ToModelOptions{StripMedia: o.StripMedia, ToolOutputMaxChars: o.ToolOutputMaxChars}
}

type filterArgs struct {
	Msgs []WithParts `json:"msgs"`
}

type valueArgs struct {
	Value json.RawMessage `json:"value"`
}

type cursorValueArgs struct {
	Value Cursor `json:"value"`
}

type cursorStringArgs struct {
	Value string `json:"value"`
}

type errorSpec struct {
	Kind            string          `json:"kind"`
	Message         string          `json:"message"`
	Syscall         string          `json:"syscall"`
	Aborted         bool            `json:"aborted"`
	ProviderID      string          `json:"providerID"`
	StatusCode      *uint64         `json:"statusCode"`
	IsRetryable     *bool           `json:"isRetryable"`
	ResponseHeaders RawObject       `json:"responseHeaders"`
	ResponseBody    *string         `json:"responseBody"`
	URL             string          `json:"url"`
	Value           json.RawMessage `json:"value"`
}

type fromErrorArgs struct {
	Spec errorSpec `json:"spec"`
}

func fixtureFromError(t *testing.T, spec errorSpec) AssistantError {
	t.Helper()
	ctx := ErrorContext{ProviderID: spec.ProviderID, Aborted: spec.Aborted}
	if ctx.ProviderID == "" {
		ctx.ProviderID = "openrouter"
	}
	var value any
	var parser ProviderErrorParser
	switch spec.Kind {
	case "abort":
		value = AbortFailure{Message: spec.Message}
	case "output-length":
		value = OutputLengthFailure{}
	case "load-api-key":
		ctx.ProviderID = "anthropic"
		value = LoadAPIKeyFailure{Message: spec.Message}
	case "econnreset":
		value = SystemFailure{Message: spec.Message, Code: "ECONNRESET", Syscall: spec.Syscall}
	case "zlib":
		value = DecompressionFailure{Message: spec.Message, Code: "ZlibError", Errno: -5, Path: "/stream"}
	case "api":
		value = APICallFailure{Cause: errors.New(spec.Message)}
		parser = ProviderErrorParserFunc(func(_ string, _ error) ParsedProviderError {
			kind := "api_error"
			if spec.Message == "prompt is too long" || valueOrZero(spec.StatusCode) == 413 {
				kind = "context_overflow"
			}
			var metadata RawObject
			if spec.URL != "" {
				raw, err := jscompat.Stringify(struct {
					URL string `json:"url"`
				}{URL: spec.URL})
				if err != nil {
					t.Fatal(err)
				}
				metadata = RawObject(raw)
			}
			retryable := false
			if spec.IsRetryable != nil {
				retryable = *spec.IsRetryable
			}
			return ParsedProviderError{
				Type: kind, Message: spec.Message, StatusCode: spec.StatusCode,
				IsRetryable: retryable, ResponseHeaders: spec.ResponseHeaders,
				ResponseBody: spec.ResponseBody, Metadata: metadata,
			}
		})
	case "error":
		value = errors.New(spec.Message)
	case "value":
		value = spec.Value
	default:
		t.Fatalf("unknown error fixture kind %q", spec.Kind)
	}
	return FromError(value, ctx, parser)
}

func valueOrZero(v *uint64) uint64 {
	if v == nil {
		return 0
	}
	return *v
}

func TestFixtures(t *testing.T) {
	fixtures := loadFixtures(t, "testdata/fixtures.json")
	counts := map[string]int{}

	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.Name, func(t *testing.T) {
			var got string
			switch fixture.Fn {
			case "toModelMessages":
				var args toModelArgs
				decodeArgs(t, fixture.ArgsJSON, &args)
				out, err := ToModelMessages(args.Input, args.Model, args.Options.build())
				if err != nil {
					t.Fatalf("ToModelMessages: %v", err)
				}
				got = stringify(t, out)

			case "filterCompacted":
				var args filterArgs
				decodeArgs(t, fixture.ArgsJSON, &args)
				got = stringify(t, FilterCompacted(args.Msgs))

			case "partJSON":
				var args valueArgs
				decodeArgs(t, fixture.ArgsJSON, &args)
				part, err := UnmarshalPart(args.Value)
				if err != nil {
					t.Fatalf("UnmarshalPart: %v", err)
				}
				got = stringify(t, part)

			case "messageJSON":
				var args valueArgs
				decodeArgs(t, fixture.ArgsJSON, &args)
				info, err := UnmarshalInfo(args.Value)
				if err != nil {
					t.Fatalf("UnmarshalInfo: %v", err)
				}
				got = stringify(t, info)

			case "cursorEncode":
				var args cursorValueArgs
				decodeArgs(t, fixture.ArgsJSON, &args)
				out, err := EncodeCursor(args.Value)
				if err != nil {
					t.Fatalf("EncodeCursor: %v", err)
				}
				got = stringify(t, out)

			case "cursorDecode":
				var args cursorStringArgs
				decodeArgs(t, fixture.ArgsJSON, &args)
				out, err := DecodeCursor(args.Value)
				if err != nil {
					t.Fatalf("DecodeCursor: %v", err)
				}
				got = stringify(t, out)

			case "fromError":
				var args fromErrorArgs
				decodeArgs(t, fixture.ArgsJSON, &args)
				got = stringify(t, fixtureFromError(t, args.Spec))

			default:
				t.Fatalf("unknown fn %q", fixture.Fn)
			}

			if got != fixture.OutJSON {
				t.Fatalf("mismatch\n  want %s\n  got  %s", fixture.OutJSON, got)
			}
		})
		counts[fixture.Fn]++
	}

	if len(fixtures) < 90 {
		t.Fatalf("fixture corpus looks truncated: %d cases", len(fixtures))
	}
	for _, fn := range []string{
		"toModelMessages", "filterCompacted", "partJSON", "messageJSON",
		"cursorEncode", "cursorDecode", "fromError",
	} {
		if counts[fn] == 0 {
			t.Fatalf("no fixtures for fn %q", fn)
		}
	}
}
