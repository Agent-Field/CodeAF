package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/pool/outbox"
)

// poolClock is a stopped now, so an index's age is a fact rather than a race
// against the test runner.
func poolClock(t *testing.T) func() time.Time {
	t.Helper()
	moment, err := time.Parse(time.RFC3339, "2026-09-18T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	return func() time.Time { return moment }
}

// noEnv is an environment in which nothing is set, handed in the way the verb
// reads the world, so a test's answer cannot depend on the machine's shell.
func noEnv(string) (string, bool) {
	return "", false
}

// oneEnv is an environment with one name set — the injected stand-in for
// `CODEAF_MODEL_POOL=off codeaf pool show`.
func oneEnv(name, value string) func(string) (string, bool) {
	return func(asked string) (string, bool) {
		if asked == name {
			return value, true
		}
		return "", false
	}
}

// poolDoc is a document the reader accepts whole: one metric with dims, one
// cell that clears its min installs, one judge. The numbers name no model;
// it is a fixture, and the field it is read for is its shape.
func poolDoc() []byte {
	return []byte(`{
		"version": 7,
		"schema": 1,
		"generated": "2026-09-10",
		"min_installs": 1,
		"judges": ["z-ai/glm-5.3"],
		"metrics": {"role_rating": {"kind": "gaussian", "dims": ["role", "model"]}},
		"cells": [
			{"metric": "role_rating", "role": "planner", "model": "z-ai/glm-5.3", "mean": 1312, "sd": 18, "n": 9}
		]
	}`)
}

// seedIndex writes poolDoc where show reads the cache, and nothing else:
// show takes the document as it stands and does not ask its signature —
// that is verify's question, not the reading form's.
func seedIndex(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	poolDir := filepath.Join(dir, "pool")
	if err := os.MkdirAll(poolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(poolDir, "doc.json"), poolDoc(), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// seedOutbox records two rows the way a run would: through the outbox's own
// open and append, closed again, so status reads what stands on disk.
func seedOutbox(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	box, err := outbox.Open(filepath.Join(dir, "pool", "outbox.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []string{`{"n":1}`, `{"n":2}`} {
		if err := box.Append([]byte(row)); err != nil {
			t.Fatal(err)
		}
	}
	if err := box.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The reading form answers over the whole config, every value beside the word
// saying where it came from — and a machine that has never read an index says
// so in a sentence rather than printing nothing at all.
func TestPoolShowPrintsTheConfigAndSaysWhenNoIndexIsCached(t *testing.T) {
	var out strings.Builder
	if err := runPoolWith(nil, &out, t.TempDir(), poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	for _, want := range []string{
		"mode on · default",
		"index https://pool.invalid/index.json · default",
		"submit https://pool.invalid/submit · default",
		"ttl 1d · default",
		"no index cached yet",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("show is missing %q:\n%s", want, body)
		}
	}
}

// The pin is a source the reader can check, and the word it holds is the mode
// the machine answers to — the same three words the row and CI spell.
func TestPoolShowNamesTheEnvPinAsTheModeSource(t *testing.T) {
	var out strings.Builder
	if err := runPoolWith([]string{"show"}, &out, t.TempDir(), poolClock(t),
		oneEnv("CODEAF_MODEL_POOL", "off")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "mode off · env") {
		t.Fatalf("the env pin did not name itself:\n%s", out.String())
	}
}

// --json is one parseable object, the cached index under index or null — and,
// this is the point of the absent fields, status's three are absent from a
// show, so a script reads absence as not-asked rather than as zero.
func TestPoolShowJSONWithASeededIndexReportsTheDocument(t *testing.T) {
	var out strings.Builder
	if err := runPoolWith([]string{"show", "--json"}, &out, seedIndex(t), poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	var answer struct {
		Mode       string `json:"mode"`
		ModeSource string `json:"mode_source"`
		IndexURL   string `json:"index_url"`
		SubmitURL  string `json:"submit_url"`
		TTLSeconds int    `json:"ttl_seconds"`
		Pending    *int   `json:"pending"`
		Index      *struct {
			Generated   string `json:"generated"`
			AgeSeconds  int    `json:"age_seconds"`
			Schema      int    `json:"schema"`
			Metrics     int    `json:"metrics"`
			Judges      int    `json:"judges"`
			MinInstalls int    `json:"min_installs"`
		} `json:"index"`
	}
	if err := json.Unmarshal([]byte(out.String()), &answer); err != nil {
		t.Fatalf("--json did not parse: %v\n%s", err, out.String())
	}
	if answer.Mode != "on" || answer.ModeSource != "default" {
		t.Fatalf("mode = %q from %q", answer.Mode, answer.ModeSource)
	}
	if answer.IndexURL == "" || answer.SubmitURL == "" || answer.TTLSeconds != 86400 {
		t.Fatalf("the config is not whole: %+v", answer)
	}
	if answer.Pending != nil {
		t.Fatal("a show carried status's pending count")
	}
	held := answer.Index
	if held == nil {
		t.Fatal("a seeded index was not reported")
	}
	// 2026-09-18 midnight less 2026-09-10 midnight: eight days, to the second.
	if held.Generated != "2026-09-10" || held.AgeSeconds != 8*24*60*60 {
		t.Fatalf("generated = %q, age = %d", held.Generated, held.AgeSeconds)
	}
	if held.Schema != 1 || held.Metrics != 1 || held.Judges != 1 || held.MinInstalls != 1 {
		t.Fatalf("the document's counts moved: %+v", held)
	}
}

// status is show plus the outbox and the two doors the mode opens, said in
// words a person reads and carried as fields a script reads.
func TestPoolStatusCountsPendingRowsAndNamesItsDoors(t *testing.T) {
	dir := seedOutbox(t)
	var out strings.Builder
	if err := runPoolWith([]string{"status"}, &out, dir, poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "pending 2 · can send yes · can read yes") {
		t.Fatalf("status did not count the seeded rows:\n%s", out.String())
	}

	out.Reset()
	if err := runPoolWith([]string{"status", "--json"}, &out, dir, poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	var answer struct {
		Pending int  `json:"pending"`
		CanSend bool `json:"can_send"`
		CanRead bool `json:"can_read"`
	}
	if err := json.Unmarshal([]byte(out.String()), &answer); err != nil {
		t.Fatalf("status --json did not parse: %v\n%s", err, out.String())
	}
	if answer.Pending != 2 || !answer.CanSend || !answer.CanRead {
		t.Fatalf("the doors moved: %+v", answer)
	}

	// A machine that has recorded no measurement says zero rather than
	// inventing a file to count — and the reading form WRITES NOTHING: status
	// reads the outbox by count, because opening one would create it.
	quiet := t.TempDir()
	out.Reset()
	if err := runPoolWith([]string{"status"}, &out, quiet, poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "pending 0") {
		t.Fatalf("an absent outbox did not read as zero:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(quiet, "pool", "outbox.jsonl")); !os.IsNotExist(err) {
		t.Fatal("status created the outbox it was only counting")
	}
}

// An empty profile is the state root's own profile, the way every other file
// under the profile resolves — never a directory called "pool" beside wherever
// the command happened to run.
func TestPoolWithNoProfileDirReadsTheStateRoots(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	poolDir := filepath.Join(root, "pool")
	if err := os.MkdirAll(poolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(poolDir, "doc.json"), poolDoc(), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := runPoolWith([]string{"show", "--json"}, &out, "", poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"generated":"2026-09-10"`) {
		t.Fatalf("an empty profile did not read the state root's pool:\n%s", out.String())
	}
}

// The key gate stands in front of the fetch: with no key in hand verify
// refuses at the door — exit 2, the remedy on the line — and nothing is
// fetched from the address the config names, and nothing is cached.
func TestPoolVerifyWithoutAKeyRefusesAtTheDoorAndFetchesNothing(t *testing.T) {
	dir := t.TempDir()
	// A dead port is the fetch that would have happened: if the door let one
	// through, the failure below would be a fetch error, not the sentence.
	lookup := oneEnv("CODEAF_MODEL_POOL_URL", "http://127.0.0.1:1/index.json")
	var out strings.Builder
	err := runPoolWith([]string{"verify"}, &out, dir, poolClock(t), lookup)
	if err == nil {
		t.Fatal("a keyless verify fetched")
	}
	if !errors.Is(err, exitIncomplete) {
		t.Fatalf("the door refusal is not exit 2: %v", err)
	}
	if !strings.Contains(out.String(), "no public key built into this build; pass --key") {
		t.Fatalf("the refusal did not name the remedy:\n%s", out.String())
	}
	if _, statErr := os.Stat(filepath.Join(dir, "pool", "doc.json")); !os.IsNotExist(statErr) {
		t.Fatal("the refused verify wrote a cache")
	}
}

// OFF is a refusal at the door even with a key in hand, because the setting
// is the word the machine answers to.
func TestPoolVerifyRefusesWhenThePoolIsOff(t *testing.T) {
	var out strings.Builder
	err := runPoolWith([]string{"verify"}, &out, t.TempDir(), poolClock(t),
		oneEnv("CODEAF_MODEL_POOL", "off"))
	if !errors.Is(err, exitIncomplete) {
		t.Fatalf("an off pool did not refuse on exit 2: %v", err)
	}
	if !strings.Contains(out.String(), "the Model Pool is off in settings") {
		t.Fatalf("the refusal did not say so:\n%s", out.String())
	}
}

// A key the flag cannot read — not base64, or not the length of an ed25519
// public key — is refused where it was typed, by the flag set, on exit 1,
// because nothing was attempted.
func TestPoolVerifyRefusesAKeyItCannotRead(t *testing.T) {
	for _, bad := range []string{"not base64!!", "abcd"} {
		var out strings.Builder
		err := runPoolWith([]string{"verify", "--key", bad}, &out, t.TempDir(), poolClock(t), noEnv)
		if !errors.Is(err, exitCannotRun) {
			t.Fatalf("a key %q is a door refusal, not a fetch: %v", bad, err)
		}
	}
}

// The happy path, over no network at all: an index and its signature on disk,
// fetched through the same Puller a real verify uses, checked under the key
// --key hands in, and read back as the sentence a person came for. The fetch
// is a pull, so what verified is the cache show reads next.
func TestPoolVerifyFetchesAndChecksASignedIndex(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "index.json"), poolDoc(), 0o644); err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(priv, poolDoc())
	if err := os.WriteFile(filepath.Join(src, "index.json.sig"),
		[]byte(base64.StdEncoding.EncodeToString(sig)), 0o644); err != nil {
		t.Fatal(err)
	}
	lookup := oneEnv("CODEAF_MODEL_POOL_URL", filepath.Join(src, "index.json"))
	dir := t.TempDir()
	var out strings.Builder
	if err := runPoolWith([]string{"verify", "--key", base64.StdEncoding.EncodeToString(pub)},
		&out, dir, poolClock(t), lookup); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "signature good: version 7, generated 2026-09-10, 1 metric") {
		t.Fatalf("verify did not read the fetched document:\n%s", out.String())
	}
	out.Reset()
	if err := runPoolWith([]string{"show"}, &out, dir, poolClock(t), lookup); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "generated 2026-09-10") {
		t.Fatalf("a verified index did not land in the cache show reads:\n%s", out.String())
	}
}

// A document the key does not trust is not verified: the failure says so in
// the puller's words on exit 1, and — this is the puller's own promise, read
// through the door — nothing wrong is cached in its place.
func TestPoolVerifyRefusesADocumentItsKeyDoesNotTrust(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	doc := append([]byte(nil), poolDoc()...)
	// One bit of the generated date, so the bytes no longer match the
	// signature beside them and nothing else about the document moved.
	doc[46] ^= 0x01
	if err := os.WriteFile(filepath.Join(src, "index.json"), doc, 0o644); err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(priv, poolDoc())
	if err := os.WriteFile(filepath.Join(src, "index.json.sig"),
		[]byte(base64.StdEncoding.EncodeToString(sig)), 0o644); err != nil {
		t.Fatal(err)
	}
	lookup := oneEnv("CODEAF_MODEL_POOL_URL", filepath.Join(src, "index.json"))
	dir := t.TempDir()
	var out strings.Builder
	err = runPoolWith([]string{"verify", "--key", base64.StdEncoding.EncodeToString(pub)},
		&out, dir, poolClock(t), lookup)
	if err == nil || !strings.Contains(err.Error(), "pull: signature does not verify") {
		t.Fatalf("a tampered document did not refuse: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "pool", "doc.json")); !os.IsNotExist(statErr) {
		t.Fatal("a refused document was cached")
	}
}
