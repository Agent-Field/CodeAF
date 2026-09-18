package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/pool/index"
)

// liveFixture is a small document shaped like the relay's, and deliberately
// carrying a metric (pair_quality) and a dim (partner) the copier has never
// heard of, so the pass-through is proved rather than assumed. Every cell
// meets the floor of 3 on its installs.
const liveFixture = `{
  "schema": 1,
  "version": 1789697843,
  "generated": "2026-09-18",
  "min_installs": 3,
  "judges": ["codeaf/reviewer", "z-ai/glm-5.3-flash"],
  "rubrics": {"role_quality": 1, "acceptable": 1},
  "aliases": {},
  "metrics": {
    "role_quality": {"kind": "gaussian", "unit": "score", "dims": ["role", "model"]},
    "acceptable": {"kind": "bernoulli", "unit": "share", "dims": ["role", "model", "source"]},
    "pair_quality": {"kind": "gaussian", "unit": "score", "dims": ["role", "model", "partner"]}
  },
  "cells": [
    {"metric": "role_quality", "role": "worker", "model": "z-ai/glm-5.3", "mean": 72.5, "sd": 7.0, "n": 38, "installs": 7},
    {"metric": "role_quality", "role": "high", "model": "qwen/qwen3.8-max-0902", "mean": 92.0, "sd": 5.3, "n": 23, "installs": 9},
    {"metric": "acceptable", "role": "worker", "model": "z-ai/glm-5.3", "source": "reviewer", "mean": 0.9, "sd": 0.05, "n": 20, "installs": 4},
    {"metric": "pair_quality", "role": "worker", "model": "z-ai/glm-5.3", "partner": "openai/gpt-5.6-sol", "mean": 88.0, "sd": 3.0, "n": 12, "installs": 5}
  ]
}`

// embeddedFixture is an embedded seed with one alias the live document does not
// carry, so the merge can be observed.
const embeddedFixture = `{
  "schema": 1,
  "generated": "2026-09-17",
  "min_installs": 1,
  "aliases": {"deepseek/deepseek-v4.1-flash": ["ds-v4.1-flash"]},
  "metrics": {"role_quality": {"kind": "gaussian", "unit": "score", "dims": ["role", "model"]}},
  "cells": []
}`

// makeKey makes a fresh ed25519 key pair.
func makeKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

// signDoc signs doc with priv and returns base64 standard encoding, the same
// shape pull's tests sign in.
func signDoc(t *testing.T, priv ed25519.PrivateKey, doc []byte) []byte {
	t.Helper()
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, doc)))
}

// serveRelay serves one document and its signature at <url>/index.json, signed
// by priv.
func serveRelay(t *testing.T, doc []byte, priv ed25519.PrivateKey) *httptest.Server {
	t.Helper()
	sig := signDoc(t, priv, doc)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sig") {
			_, _ = w.Write(sig)
			return
		}
		_, _ = w.Write(doc)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// ── THE COPIER ──────────────────────────────────────────────────────────────

// A metric and a dim the copier has never seen pass through verbatim, and the
// rendered seed is one Parse accepts whole: every cell in the file survives.
func TestRenderCopiesUnknownMetricAndDimVerbatim(t *testing.T) {
	seed, err := render([]byte(liveFixture), []byte(embeddedFixture))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	text := string(seed)
	for _, want := range []string{`"pair_quality"`, `"partner"`, `"openai/gpt-5.6-sol"`} {
		if !strings.Contains(text, want) {
			t.Errorf("the rendered seed does not carry %s", want)
		}
	}
	parsed, err := index.Parse(seed)
	if err != nil {
		t.Fatalf("the rendered seed does not parse: %v", err)
	}
	for metric, want := range map[string]int{"role_quality": 2, "acceptable": 1, "pair_quality": 1} {
		if got := len(parsed.Cells(metric)); got != want {
			t.Errorf("metric %q: Parse kept %d cells, want %d", metric, got, want)
		}
	}
	if kind, ok := parsed.Kind("pair_quality"); !ok || kind != "gaussian" {
		t.Errorf("pair_quality: kind %q, declared %v; the declaration did not survive", kind, ok)
	}
}

// The embedded seed's aliases survive under the live document's, including when
// the live map is empty.
func TestRenderMergesEmbeddedAliasesUnderEmptyLive(t *testing.T) {
	seed, err := render([]byte(liveFixture), []byte(embeddedFixture))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(string(seed), `"deepseek/deepseek-v4.1-flash"`) {
		t.Fatal("the embedded seed's alias did not survive an empty live alias map")
	}
	parsed, err := index.Parse(seed)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := parsed.Canonical("ds-v4.1-flash"); got != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("the alias resolves to %q, want the embedded seed's canonical id", got)
	}
}

// A live alias claims its key: the embedded seed's entry for the same key goes
// under the live one, and an embedded-only key still survives.
func TestRenderLiveAliasesWin(t *testing.T) {
	live := strings.Replace(liveFixture, `"aliases": {}`, `"aliases": {"a/b": ["live-alt"]}`, 1)
	embedded := `{"aliases": {"a/b": ["emb-alt"], "c/d": ["c-alt"]}}`
	seed, err := render([]byte(live), []byte(embedded))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	parsed, err := index.Parse(seed)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := parsed.Canonical("live-alt"); got != "a/b" {
		t.Errorf("the live alias did not win: live-alt resolves to %q", got)
	}
	if got := parsed.Canonical("c-alt"); got != "c/d" {
		t.Errorf("the embedded-only alias did not survive: c-alt resolves to %q", got)
	}
}

// The same two documents always render the same bytes, which is what -check
// rests on.
func TestRenderIsDeterministic(t *testing.T) {
	first, err := render([]byte(liveFixture), []byte(embeddedFixture))
	if err != nil {
		t.Fatal(err)
	}
	second, err := render([]byte(liveFixture), []byte(embeddedFixture))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("two renders of the same documents differ")
	}
}

// The copier refuses a document the reader would not trust whole: a version or
// day that does not read, a cell below the floor, and a cell whose metric the
// document never declared.
func TestRenderRefusesDocumentsItCannotCopy(t *testing.T) {
	cases := []struct {
		name string
		live string
	}{
		{"no version", strings.Replace(liveFixture, `"version": 1789697843,`, "", 1)},
		{"non-integer version", strings.Replace(liveFixture, `"version": 1789697843`, `"version": "five"`, 1)},
		{"unparseable generated", strings.Replace(liveFixture, `"generated": "2026-09-18"`, `"generated": "not-a-day"`, 1)},
		{"cell below the floor", strings.Replace(liveFixture, `"n": 38, "installs": 7`, `"n": 38, "installs": 1`, 1)},
		{"undeclared metric", strings.Replace(liveFixture, `"metric": "pair_quality", "role": "worker"`, `"metric": "ghost_quality", "role": "worker"`, 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := render([]byte(tc.live), []byte(embeddedFixture)); err == nil {
				t.Fatal("render accepted a document it cannot copy")
			}
		})
	}
}

// ── THE FETCH ───────────────────────────────────────────────────────────────

// A signed document is fetched from the relay and handed back as its own bytes.
func TestFetchReadsTheRelay(t *testing.T) {
	pub, priv := makeKey(t)
	srv := serveRelay(t, []byte(liveFixture), priv)
	doc, err := fetch(context.Background(), srv.URL+"/index.json", "", []ed25519.PublicKey{pub}, t.TempDir())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(doc) != liveFixture {
		t.Fatal("fetch did not hand back the relay's own bytes")
	}
}

// The mirror answers when the relay does not.
func TestFetchFallsBackToMirror(t *testing.T) {
	pub, priv := makeKey(t)
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	dead.Close()
	mirror := serveRelay(t, []byte(liveFixture), priv)
	doc, err := fetch(context.Background(), dead.URL+"/index.json", mirror.URL+"/index.json", []ed25519.PublicKey{pub}, t.TempDir())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(doc) != liveFixture {
		t.Fatal("the mirror's bytes are not the relay's")
	}
}

// A document whose signature nobody's key made is refused, and with the mirror
// dead too there is nothing to fall back to.
func TestFetchRefusesABadSignature(t *testing.T) {
	pub, _ := makeKey(t)
	_, otherPriv := makeKey(t)
	srv := serveRelay(t, []byte(liveFixture), otherPriv)
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	dead.Close()
	if _, err := fetch(context.Background(), srv.URL+"/index.json", dead.URL+"/index.json", []ed25519.PublicKey{pub}, t.TempDir()); err == nil {
		t.Fatal("fetch accepted a document signed by a stranger")
	}
}

// ── THE RUN ─────────────────────────────────────────────────────────────────

// run writes the seed and its figure from the relay, and -check reports both as
// current; a drifed file is refused with errDrift.
func TestRunWritesAndThenChecks(t *testing.T) {
	pub, priv := makeKey(t)
	srv := serveRelay(t, []byte(liveFixture), priv)
	dir := t.TempDir()
	out := filepath.Join(dir, "seed.json")
	cells := filepath.Join(dir, "seed-cells.csv")
	ctx := context.Background()
	keys := []ed25519.PublicKey{pub}

	if err := run(ctx, srv.URL+"/index.json", "", out, cells, false, keys); err != nil {
		t.Fatalf("run: %v", err)
	}
	seed, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("the seed was not written: %v", err)
	}
	if _, err := index.Parse(seed); err != nil {
		t.Fatalf("the written seed does not parse: %v", err)
	}
	figure, err := os.ReadFile(cells)
	if err != nil {
		t.Fatalf("the figure was not written: %v", err)
	}
	if !strings.HasPrefix(string(figure), "role,model,mean,sd,n,installs\n") {
		t.Fatalf("the figure does not carry its header:\n%s", figure)
	}
	if strings.Contains(string(figure), "pair_quality") || strings.Contains(string(figure), "reviewer") {
		t.Fatalf("the figure carries a non-role_quality cell:\n%s", figure)
	}

	// A second check against what was just written is clean.
	if err := run(ctx, srv.URL+"/index.json", "", out, cells, true, keys); err != nil {
		t.Fatalf("check of a fresh write: %v", err)
	}
	// Drift the seed and the check reports it, writing nothing.
	var doc map[string]any
	if err := json.Unmarshal(seed, &doc); err != nil {
		t.Fatal(err)
	}
	doc["generated"] = "1999-01-01"
	drifed, _ := json.Marshal(doc)
	if err := os.WriteFile(out, drifed, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, srv.URL+"/index.json", "", out, cells, true, keys); err == nil {
		t.Fatal("a drifed seed passed -check")
	}
}

// The figure is the role_quality cells, with the installs column, in the seed's
// own order.
func TestCellsCSVSelectsRoleQuality(t *testing.T) {
	seed, err := render([]byte(liveFixture), []byte(embeddedFixture))
	if err != nil {
		t.Fatal(err)
	}
	figure, err := cellsCSV(seed)
	if err != nil {
		t.Fatal(err)
	}
	want := "role,model,mean,sd,n,installs\n" +
		"high,qwen/qwen3.8-max-0902,92,5.3,23,9\n" +
		"worker,z-ai/glm-5.3,72.5,7,38,7\n"
	if string(figure) != want {
		t.Fatalf("figure =\n%s\nwant\n%s", figure, want)
	}
}
