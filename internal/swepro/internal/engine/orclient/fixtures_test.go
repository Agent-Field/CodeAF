package orclient

// Replays testdata/fixtures.json, produced by tools/fixtures/gen-orclient.ts
// from the REAL @openrouter/ai-sdk-provider@2.8.1 and the REAL
// src/provider/transform.ts.
//
// Every case asserts BYTE EQUALITY of jscompat.Stringify(goResult) against the
// recorded JSON.stringify — not a tolerance compare, and not a structural one:
// key order in the request body decides prompt-cache hit rate, and key order
// inside a tool call's `arguments` decides whether an Anthropic signature
// survives the round trip (ENGINE-DESIGN R0).

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

func stringify(t *testing.T, v any) string {
	t.Helper()
	b, err := jscompat.Stringify(v)
	if err != nil {
		t.Fatalf("stringify: %v", err)
	}
	return string(b)
}

func decodeArgs(t *testing.T, argsJSON string, into any) {
	t.Helper()
	if err := json.Unmarshal([]byte(argsJSON), into); err != nil {
		t.Fatalf("decode args: %v", err)
	}
}

func TestFixtures(t *testing.T) {
	cases := loadFixtures(t, "fixtures.json")
	if len(cases) < 100 {
		t.Fatalf("fixture corpus looks truncated: %d cases", len(cases))
	}
	counts := map[string]int{}
	for _, c := range cases {
		c := c
		counts[c.Fn]++
		t.Run(c.Name, func(t *testing.T) { replay(t, c) })
	}
	for _, fn := range []string{
		"deterministicStringify", "requestBody", "headers", "temperature", "topP", "topK",
		"maxOutputTokens", "options", "providerOptions", "sanitizeSurrogates", "message",
	} {
		if counts[fn] == 0 {
			t.Errorf("no fixture cases for fn %q — the generator is out of sync", fn)
		}
	}
}

func replay(t *testing.T, c fixtureCase) {
	t.Helper()
	switch c.Fn {
	case "deterministicStringify":
		var args struct {
			Input json.RawMessage `json:"input"`
		}
		decodeArgs(t, c.ArgsJSON, &args)
		got, err := DeterministicStringify(args.Input)
		if err != nil {
			t.Fatalf("DeterministicStringify: %v", err)
		}
		assertEqual(t, stringify(t, string(got)), c.OutJSON)

	case "requestBody":
		replayRequestBody(t, c)

	case "headers":
		replayHeaders(t, c)

	case "temperature", "topP", "topK":
		var args struct {
			Model Model `json:"model"`
		}
		decodeArgs(t, c.ArgsJSON, &args)
		var got *float64
		switch c.Fn {
		case "temperature":
			got = Temperature(args.Model)
		case "topP":
			got = TopP(args.Model)
		case "topK":
			got = TopK(args.Model)
		}
		// The generator records `?? null`, so an undefined knob is JSON null.
		if got == nil {
			assertEqual(t, "null", c.OutJSON)
			return
		}
		assertEqual(t, stringify(t, jscompat.JSNumber(*got)), c.OutJSON)

	case "maxOutputTokens":
		var args struct {
			Model Model `json:"model"`
		}
		decodeArgs(t, c.ArgsJSON, &args)
		assertEqual(t, stringify(t, jscompat.JSNumber(MaxOutputTokens(args.Model))), c.OutJSON)

	case "options":
		var args struct {
			Model     Model  `json:"model"`
			SessionID string `json:"sessionID"`
		}
		decodeArgs(t, c.ArgsJSON, &args)
		got := Options(OptionsInput{Model: args.Model, SessionID: args.SessionID})
		assertEqual(t, stringify(t, got), c.OutJSON)

	case "providerOptions":
		var args struct {
			Model   Model           `json:"model"`
			Options json.RawMessage `json:"options"`
		}
		decodeArgs(t, c.ArgsJSON, &args)
		options, err := ParseObject(args.Options)
		if err != nil {
			t.Fatalf("parse options: %v", err)
		}
		assertEqual(t, stringify(t, ProviderOptions(args.Model, options)), c.OutJSON)

	case "sanitizeSurrogates":
		var args struct {
			Input string `json:"input"`
		}
		decodeArgs(t, c.ArgsJSON, &args)
		assertEqual(t, stringify(t, SanitizeSurrogates(args.Input)), c.OutJSON)

	case "message":
		var args struct {
			Msgs  json.RawMessage `json:"msgs"`
			Model Model           `json:"model"`
		}
		decodeArgs(t, c.ArgsJSON, &args)
		msgs := decodeModelMessages(t, args.Msgs)
		assertEqual(t, stringify(t, Message(msgs, args.Model)), c.OutJSON)

	default:
		t.Fatalf("unknown fn %q", c.Fn)
	}
}

// requestOpts mirrors the `LanguageModelV3CallOptions` subset the generator
// records.
type requestOpts struct {
	Prompt          json.RawMessage `json:"prompt"`
	MaxOutputTokens *float64        `json:"maxOutputTokens"`
	Temperature     *float64        `json:"temperature"`
	TopP            *float64        `json:"topP"`
	TopK            *float64        `json:"topK"`
	Tools           []struct {
		Type            string          `json:"type"`
		Name            string          `json:"name"`
		Description     string          `json:"description"`
		InputSchema     json.RawMessage `json:"inputSchema"`
		ProviderOptions json.RawMessage `json:"providerOptions"`
	} `json:"tools"`
	ToolChoice      *ToolChoice     `json:"toolChoice"`
	ProviderOptions json.RawMessage `json:"providerOptions"`
}

func replayRequestBody(t *testing.T, c fixtureCase) {
	t.Helper()
	var args struct {
		ModelID       string      `json:"modelId"`
		Opts          requestOpts `json:"opts"`
		Compatibility string      `json:"compatibility"`
	}
	decodeArgs(t, c.ArgsJSON, &args)

	params := RequestParams{
		ModelID:         args.ModelID,
		Prompt:          decodeModelMessages(t, args.Opts.Prompt),
		MaxOutputTokens: args.Opts.MaxOutputTokens,
		Temperature:     args.Opts.Temperature,
		TopP:            args.Opts.TopP,
		TopK:            args.Opts.TopK,
		ToolChoice:      args.Opts.ToolChoice,
		Compatibility:   args.Compatibility,
	}
	for _, tool := range args.Opts.Tools {
		params.Tools = append(params.Tools, Tool{
			Type:            tool.Type,
			Name:            tool.Name,
			Description:     tool.Description,
			InputSchema:     tool.InputSchema,
			ProviderOptions: tool.ProviderOptions,
		})
	}
	if len(args.Opts.ProviderOptions) > 0 {
		bag, err := ParseObject(args.Opts.ProviderOptions)
		if err != nil {
			t.Fatalf("parse providerOptions: %v", err)
		}
		if ns, ok := bag.Get("openrouter"); ok {
			params.OpenRouterOptions, err = ParseObject(ns)
			if err != nil {
				t.Fatalf("parse openrouter namespace: %v", err)
			}
		}
	}

	got, err := BuildRequestBody(params)
	if err != nil {
		t.Fatalf("BuildRequestBody: %v", err)
	}
	assertEqual(t, string(got), c.OutJSON)
}

func replayHeaders(t *testing.T, c fixtureCase) {
	t.Helper()
	var args struct {
		CallHeaders     map[string]string `json:"callHeaders"`
		ProviderHeaders map[string]string `json:"providerHeaders"`
	}
	decodeArgs(t, c.ArgsJSON, &args)

	// The generator's default provider header set, in `createOpenRouter`'s
	// literal order (`dist/index.mjs:5270-5281`).
	provider := []HeaderPair{
		{Name: "Authorization", Value: "Bearer KEY"},
		{Name: "X-OpenRouter-Title", Value: "codeaf"},
		{Name: "HTTP-Referer", Value: "https://codeaf.local/"},
		{Name: "X-Title", Value: "codeaf"},
		{Name: "user-agent", Value: "ai-sdk/openrouter/2.8.1"},
	}
	if args.ProviderHeaders != nil {
		provider = orderedPairs(t, c.ArgsJSON, "providerHeaders")
	}

	got := BuildHeaders(HeaderInputs{
		Provider: provider,
		// The default provider set already carries the suffix, so it is not
		// applied twice.
		Call:                   orderedPairs(t, c.ArgsJSON, "callHeaders"),
		UtilsUserAgentSuffix:   "ai-sdk/provider-utils/4.0.23",
		RuntimeUserAgentSuffix: "runtime/bun/1.2.23",
	})

	out := NewObject()
	for _, h := range got {
		out.SetString(h.Name, h.Value)
	}
	assertEqual(t, stringify(t, out), c.OutJSON)
}

// orderedPairs re-reads one header object out of the raw args JSON so that its
// KEY ORDER survives — a map[string]string would lose it, and the object-spread
// precedence depends on it.
func orderedPairs(t *testing.T, argsJSON, key string) []HeaderPair {
	t.Helper()
	root, err := ParseObject([]byte(argsJSON))
	if err != nil {
		t.Fatalf("parse args object: %v", err)
	}
	raw, ok := root.Get(key)
	if !ok || string(raw) == "null" {
		return nil
	}
	obj, err := ParseObject(raw)
	if err != nil {
		t.Fatalf("parse %s: %v", key, err)
	}
	out := make([]HeaderPair, 0, obj.Len())
	for _, name := range obj.Keys() {
		v, _ := obj.Get(name)
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			t.Fatalf("header %s value: %v", name, err)
		}
		out = append(out, HeaderPair{Name: name, Value: s})
	}
	return out
}

func assertEqual(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("mismatch:\n want %s\n  got %s", want, got)
	}
}

var _ = msgmodel.ModelMessage{}
