package custom

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

const goodSource = `import React from "react";
import { useRecords } from "@codeaf/bridge";
type Props = { title: string };
export default function Summary({ title }: Props) {
  const records = useRecords("deal");
  return <div>{title}: {records.length}</div>;
}`

func TestForbiddenTokens(t *testing.T) {
	cases := []struct{ name, source string }{
		{"fetch", `fetch("/records")`},
		{"XMLHttpRequest", `new XMLHttpRequest()`},
		{"WebSocket", `new WebSocket("wss://example.test")`},
		{"EventSource", `new EventSource("/events")`},
		{"eval", `eval("1 + 1")`},
		{"Function", `new Function("return 1")`},
		{"dynamic import", `import("react")`},
		{"window.top", `window.top.location = "/"`},
		{"parent", `parent.postMessage({}, "*")`},
		{"localStorage", `localStorage.setItem("key", "value")`},
		{"document.cookie", `document.cookie = "key=value"`},
		{"spaced Function", `Function ("return 1")`},
		{"spaced import", `import ("react")`},
		{"commented top", `window /* comment */ . top`},
		{"spaced parent", `parent . location`},
		{"spaced cookie", `document . cookie`},
		{"optional top", `window?.top`},
		{"optional parent", `parent?.location`},
		{"escaped fetch", `f\u0065tch("/records")`},
		{"escaped top", `window.\u{74}op`},
		{"escaped cookie", `document.cook\u0069e`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues := Validate("custom/summary.tsx", []byte("export {};\n"+tc.source))
			if len(issues) == 0 {
				t.Fatal("forbidden source passed")
			}
			if got := issues[0]; got.File != "custom/summary.tsx" || got.Rule != Rule || !strings.HasPrefix(got.Message, "line 2:") {
				t.Fatalf("missing source coordinates or rule: %+v", got)
			}
		})
	}
}

func TestCustomImports(t *testing.T) {
	cases := []struct {
		name, source string
		bad          bool
	}{
		{"react", `import React from "react"; export default React;`, false},
		{"bridge", `import { useRecords } from "@codeaf/bridge"; export { useRecords };`, false},
		{"bare react", `import "react"; export {};`, false},
		{"multiline", "import {\n useRecords\n} from '@codeaf/bridge'; export {useRecords};", false},
		{"package", `import x from "lodash"; export default x;`, true},
		{"unused package", `import x from "lodash"; export {};`, true},
		{"type import", `import type { X } from "other"; export {};`, true},
		{"commented type import", `import /* type */ type { X } from "other"; export {};`, true},
		{"bare package", `import "other"; export {};`, true},
		{"reexport", `export { X } from "other";`, true},
		{"type reexport", `export type { X } from "other";`, true},
		{"local file", `import X from "./other.tsx"; export default X;`, true},
		{"absolute file", `import X from "/tmp/secret.tsx"; export default X;`, true},
		{"URL", `import X from "https://example.test/x.js"; export default X;`, true},
		{"react subpath", `import {jsx} from "react/jsx-runtime"; export {jsx};`, true},
		{"bridge subpath", `import X from "@codeaf/bridge/other"; export default X;`, true},
		{"require", `export const X = require("other");`, true},
		{"TS require import", `import X = require("other"); export {X};`, true},
		{"semicolon comment", `import /* ; */ X from "other"; export default X;`, true},
		{"quoted escape", `import X from "oth\u0065r"; export default X;`, true},
		{"comments", "// fetch() and import 'other' are forbidden\n/* window.top */\nexport {};", false},
		{"identifier substring", `export const fetchCount = 1;`, false},
		{"ordinary from text", `export const message = "from 'deals'";`, false},
		{"syntax", `export default () => <div>`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues := Validate("custom/test.tsx", []byte(tc.source))
			if (len(issues) != 0) != tc.bad {
				t.Fatalf("bad=%v, issues=%+v", tc.bad, issues)
			}
		})
	}
}

func TestCustomSizeLimit(t *testing.T) {
	for _, size := range []int{MaxFileBytes - 1, MaxFileBytes, MaxFileBytes + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			base := "export {};\n//"
			source := []byte(base + strings.Repeat("x", size-len(base)))
			issues := Validate("custom/size.tsx", source)
			if (len(issues) != 0) != (size > MaxFileBytes) {
				t.Fatalf("%d bytes: %+v", size, issues)
			}
		})
	}
}

func TestCustomPaths(t *testing.T) {
	cases := []struct {
		path string
		good bool
	}{
		{"custom/summary.tsx", true},
		{"custom/deal-card.tsx", true},
		{"screens/summary.tsx", false},
		{"custom/summary.ts", false},
		{"custom/summary.TSX", false},
		{"custom/nested/summary.tsx", false},
		{"custom/../summary.tsx", false},
		{"custom/../custom/summary.tsx", false},
		{"/custom/summary.tsx", false},
		{"custom//summary.tsx", false},
		{"custom/.tsx", false},
		{"custom/summary\\other.tsx", false},
		{"custom/summary\x00.tsx", false},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			if got := IsFile(tc.path); got != tc.good {
				t.Fatalf("IsFile=%v, want %v", got, tc.good)
			}
		})
	}
}

type bundleStore struct {
	svc.Store
	detail bridge.AppDetail
	err    error
}

func (s *bundleStore) Get(context.Context, string) (bridge.AppDetail, error) { return s.detail, s.err }

func bundler() (*Service, *bundleStore) {
	store := &bundleStore{detail: bridge.AppDetail{
		AppSummary: bridge.AppSummary{ID: "crm", Version: 1},
		Files:      map[string]string{"custom/summary.tsx": goodSource},
	}}
	return New(svc.Config{}, &svc.Services{Store: store}), store
}

func TestGoodFileBundlesToESModule(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         []string
	}{
		{"typed bridge component", goodSource, []string{`from "react"`, `from "@codeaf/bridge"`, "createElement", "default"}},
		{"named component", `import React from "react"; export const Summary = () => <div>Deals</div>;`, []string{`from "react"`, "createElement", "Summary"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, store := bundler()
			store.detail.Files["custom/summary.tsx"] = tc.source
			result, err := service.Bundle(context.Background(), svc.Scope{App: "crm"}, bridge.CustomBundleParams{File: "custom/summary.tsx"})
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range append(tc.want, "export {") {
				if !strings.Contains(result.JS, want) {
					t.Fatalf("missing %q in compiled module:\n%s", want, result.JS)
				}
			}
			if strings.Contains(result.JS, "type Props") || strings.Contains(result.JS, "<div>") {
				t.Fatalf("TypeScript or JSX was not compiled:\n%s", result.JS)
			}
			if want := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.source))); result.Hash != want {
				t.Fatalf("hash %q, want %q", result.Hash, want)
			}
		})
	}
}

func TestBundleCache(t *testing.T) {
	service, store := bundler()
	ctx, scope, params := context.Background(), svc.Scope{App: "crm"}, bridge.CustomBundleParams{File: "custom/summary.tsx"}
	first, err := service.Bundle(ctx, scope, params)
	if err != nil {
		t.Fatal(err)
	}
	cache := service.cache["crm"]
	// A sentinel distinguishes a cached answer from a fresh deterministic build.
	cached := first
	cached.JS = "cached answer"
	cache.files[params.File] = cached
	if got, err := service.Bundle(ctx, scope, params); err != nil || got != cached {
		t.Fatalf("cache miss: %+v %v", got, err)
	}
	store.detail.Files[params.File] = strings.Replace(goodSource, "deal", "contact", 1)
	changed, err := service.Bundle(ctx, scope, params)
	if err != nil || changed.Hash == first.Hash || !strings.Contains(changed.JS, "contact") {
		t.Fatalf("stale content: %+v %v", changed, err)
	}
	cache.files[params.File] = bridge.CustomBundleResult{JS: "old version", Hash: changed.Hash}
	store.detail.Version++
	if got, err := service.Bundle(ctx, scope, params); err != nil || got != changed || service.cache["crm"] == cache {
		t.Fatalf("stale version: %+v %v", got, err)
	}
	store.detail.Files[params.File] = "fetch('/records')"
	if _, err := service.Bundle(ctx, scope, params); err == nil {
		t.Fatal("changed unsafe content bypassed validation")
	}
	// App identity is independent of source hash and version.
	store.detail.Files[params.File] = goodSource
	if _, err := service.Bundle(ctx, svc.Scope{App: "other"}, params); err != nil || service.cache["other"] == service.cache["crm"] {
		t.Fatalf("apps share a cache: %v", err)
	}
}

func TestBundleErrors(t *testing.T) {
	cases := []struct {
		name, file, source string
		code               bridge.ErrorCode
	}{
		{"path", "../summary.tsx", goodSource, bridge.CodeInvalid},
		{"missing", "custom/missing.tsx", goodSource, bridge.CodeNotFound},
		{"forbidden", "custom/summary.tsx", "fetch('/records')", bridge.CodeCheckFailed},
		{"size", "custom/summary.tsx", strings.Repeat("x", MaxFileBytes+1), bridge.CodeCheckFailed},
		{"syntax", "custom/summary.tsx", "export default <", bridge.CodeCheckFailed},
		{"import", "custom/summary.tsx", `import "other";`, bridge.CodeCheckFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, store := bundler()
			store.detail.Files["custom/summary.tsx"] = tc.source
			_, err := service.Bundle(context.Background(), svc.Scope{App: "crm"}, bridge.CustomBundleParams{File: tc.file})
			var failure *bridge.Error
			if !errors.As(err, &failure) || failure.Code != tc.code {
				t.Fatalf("want %s, got %v", tc.code, err)
			}
			if tc.code == bridge.CodeCheckFailed {
				issues, ok := failure.Details["issues"].([]spec.Issue)
				if !ok || len(issues) == 0 || issues[0].Rule != Rule || issues[0].File != tc.file {
					t.Fatalf("missing check details: %+v", failure)
				}
			}
		})
	}
	service, store := bundler()
	store.err = bridge.Errorf(bridge.CodeDenied, "no access")
	if _, err := service.Bundle(context.Background(), svc.Scope{App: "crm"}, bridge.CustomBundleParams{File: "custom/summary.tsx"}); err != store.err {
		t.Fatalf("store error lost: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Bundle(ctx, svc.Scope{App: "crm"}, bridge.CustomBundleParams{File: "custom/summary.tsx"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestConcurrentBundles(t *testing.T) {
	service, _ := bundler()
	const callers = 8
	results := make([]bridge.CustomBundleResult, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range results {
		wg.Go(func() {
			results[i], errs[i] = service.Bundle(context.Background(), svc.Scope{App: "crm"}, bridge.CustomBundleParams{File: "custom/summary.tsx"})
		})
	}
	wg.Wait()
	for i, result := range results {
		if errs[i] != nil || result.JS == "" || !reflect.DeepEqual(result, results[0]) {
			t.Fatalf("caller %d: %+v %v", i, result, errs[i])
		}
	}
}
