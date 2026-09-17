package main

import (
	"context"
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

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewpick"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/pool/outbox"
	"github.com/Agent-Field/codeaf/internal/pool/poolcfg"
	"github.com/Agent-Field/codeaf/internal/pool/record"
	"github.com/Agent-Field/codeaf/internal/pool/tally"
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
// so in a sentence rather than printing nothing at all. An install that has
// recorded nothing of its own says so too.
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
		"no index cached yet · built-in seed of 2026-09-17",
		"own sheet: none",
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
			Source      string `json:"source"`
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
	if held.Source != "cache" {
		t.Fatalf("a cached index did not name its source: %q", held.Source)
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
	if !strings.Contains(out.String(), "own sheet: none") {
		t.Fatalf("status did not say the install has recorded nothing of its own:\n%s", out.String())
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

// ── THE SEATED INDEX ────────────────────────────────────────────────────────

// writePoolDoc puts a document where poolIndexFor reads the cache: doc.json
// under the profile's pool directory.
func writePoolDoc(t *testing.T, dir, doc string) {
	t.Helper()
	poolDir := filepath.Join(dir, "pool")
	if err := os.MkdirAll(poolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(poolDir, "doc.json"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

// With no cache the reader answers the seed this build carries, and a worker
// cell is in it — so `learn` has numbers on day one.
func TestPoolIndexForAnswersTheSeedWithNoCache(t *testing.T) {
	held := poolIndexFor(t.TempDir(), poolcfg.Resolve("", noEnv), poolClock(t))()
	if held == nil {
		t.Fatal("no cache and no seed: the reader answered nothing")
	}
	worker := false
	for _, cell := range held.Cells("role_quality") {
		if cell.Role == "worker" {
			worker = true
		}
	}
	if !worker {
		t.Fatal("the seed answered no worker cell")
	}
}

// A cached document whose generated day is newer than the seed's wins: the
// reader hands back the cached numbers rather than the embedded ones.
func TestPoolIndexForKeepsANewerCache(t *testing.T) {
	dir := t.TempDir()
	writePoolDoc(t, dir, `{
		"schema": 1,
		"generated": "2026-09-20",
		"min_installs": 1,
		"metrics": {"role_quality": {"kind": "gaussian", "dims": ["role", "model"]}},
		"cells": [{"metric": "role_quality", "role": "worker", "model": "z-ai/glm-5.3", "mean": 75, "sd": 7, "n": 30}]
	}`)
	held := poolIndexFor(dir, poolcfg.Resolve("", noEnv), poolClock(t))()
	if held == nil || held.Generated().Format("2006-01-02") != "2026-09-20" {
		t.Fatalf("a newer cache did not win: %v", held)
	}
}

// A cache that does not parse is not a cache: the seed stands in its place.
func TestPoolIndexForIgnoresAnUnparsableCache(t *testing.T) {
	dir := t.TempDir()
	writePoolDoc(t, dir, "{ this is not a document")
	held := poolIndexFor(dir, poolcfg.Resolve("", noEnv), poolClock(t))()
	if held == nil || held.Generated().Format("2006-01-02") != "2026-09-17" {
		t.Fatalf("an unparsable cache did not fall back to the seed: %v", held)
	}
}

// A mode that forbids reading answers no index at all.
func TestPoolIndexForAnswersNothingWhenTheModeIsOff(t *testing.T) {
	if held := poolIndexFor(t.TempDir(), poolcfg.Resolve("off", noEnv), poolClock(t))(); held != nil {
		t.Fatal("a mode that forbids reading answered an index")
	}
}

// No key in the build means no fetch: the refresh starts no goroutine, which
// is every run on a build with no key compiled in. A key starts the one fetch,
// and the mode still has to allow reading.
func TestPoolRefreshStartsNoGoroutineWithoutAKey(t *testing.T) {
	started := 0
	prev := poolRefreshGo
	poolRefreshGo = func(scope string, fn func()) { started++ }
	t.Cleanup(func() { poolRefreshGo = prev })

	key := []ed25519.PublicKey{make(ed25519.PublicKey, ed25519.PublicKeySize)}
	on := poolcfg.Resolve("", noEnv)
	off := poolcfg.Resolve("off", noEnv)

	startPoolIndexRefresh(context.Background(), t.TempDir(), on, nil)
	if started != 0 {
		t.Fatalf("no key, yet %d goroutine(s) started", started)
	}
	startPoolIndexRefresh(context.Background(), t.TempDir(), off, key)
	if started != 0 {
		t.Fatal("a mode that forbids reading started a goroutine")
	}
	startPoolIndexRefresh(context.Background(), t.TempDir(), on, key)
	if started != 1 {
		t.Fatalf("with a key, %d goroutine(s) started, want 1", started)
	}
}

// With no cache, the --json shape reports the seed the build carries and says
// so under source, so a script sees the index a pick would read.
func TestPoolShowJSONWithNoCacheReportsTheSeed(t *testing.T) {
	var out strings.Builder
	if err := runPoolWith([]string{"show", "--json"}, &out, t.TempDir(), poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	var answer struct {
		Index *struct {
			Generated string `json:"generated"`
			Source    string `json:"source"`
		} `json:"index"`
	}
	if err := json.Unmarshal([]byte(out.String()), &answer); err != nil {
		t.Fatalf("--json did not parse: %v\n%s", err, out.String())
	}
	if answer.Index == nil || answer.Index.Source != "seed" || answer.Index.Generated != "2026-09-17" {
		t.Fatalf("the seed was not reported: %+v", answer.Index)
	}
}

// ── THE OWN SHEET ───────────────────────────────────────────────────────────

// seedOwnSheet records the install's own scores the way a recorder would:
// through the sheet's own observe, saved to the path the pool reads it back
// from.
func seedOwnSheet(t *testing.T, dir string) {
	t.Helper()
	sheet := tally.New()
	sheet.Observe("role_quality", "worker", "a/one", nil, 80)
	sheet.Observe("role_quality", "worker", "a/one", nil, 90)
	sheet.Observe("role_quality", "worker", "b/two", nil, 70)
	if err := record.SaveSheet(filepath.Join(dir, "pool", "own.json"), sheet); err != nil {
		t.Fatal(err)
	}
}

// The install's own judged scores are said with their noun — the cells the
// picker reads beside the index and the observations behind them — on the
// reading forms and in the --json object alike.
func TestPoolShowSaysWhatTheOwnSheetHolds(t *testing.T) {
	dir := t.TempDir()
	seedOwnSheet(t, dir)

	var out strings.Builder
	if err := runPoolWith([]string{"show"}, &out, dir, poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "own sheet: 2 cells, 3 observations") {
		t.Fatalf("show did not count the own sheet's cells:\n%s", out.String())
	}

	out.Reset()
	if err := runPoolWith([]string{"status"}, &out, dir, poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "own sheet: 2 cells, 3 observations") {
		t.Fatalf("status did not count the own sheet's cells:\n%s", out.String())
	}

	out.Reset()
	if err := runPoolWith([]string{"show", "--json"}, &out, dir, poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	var answer struct {
		Own struct {
			Cells        int `json:"cells"`
			Observations int `json:"observations"`
		} `json:"own"`
	}
	if err := json.Unmarshal([]byte(out.String()), &answer); err != nil {
		t.Fatalf("--json did not parse: %v\n%s", err, out.String())
	}
	if answer.Own.Cells != 2 || answer.Own.Observations != 3 {
		t.Fatalf("the own sheet counted as %+v, want 2 cells over 3 observations", answer.Own)
	}
}

// An own sheet that does not parse is a loss, not a fault a reading form
// stops for: it reads as none.
func TestPoolShowReadsAnUnparsableOwnSheetAsNone(t *testing.T) {
	dir := t.TempDir()
	poolDir := filepath.Join(dir, "pool")
	if err := os.MkdirAll(poolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(poolDir, "own.json"), []byte("not a document"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := runPoolWith([]string{"show"}, &out, dir, poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "own sheet: none") {
		t.Fatalf("a broken own sheet did not read as none:\n%s", out.String())
	}
}

// The wired reader answers the own sheet's cells — the install's own evidence,
// read once and parsed once — and an off mode seats nothing at all.
func TestWirePoolIndexSeatsTheOwnSheetsCells(t *testing.T) {
	prevIndex, prevOwn := config.AutoIndex, config.AutoOwnCells
	t.Cleanup(func() { config.AutoIndex, config.AutoOwnCells = prevIndex, prevOwn })

	dir := t.TempDir()
	seedOwnSheet(t, dir)
	wirePoolIndex(dir)
	if config.AutoOwnCells == nil {
		t.Fatal("the own sheet was not seated")
	}
	own := config.AutoOwnCells()
	want := []crewpick.Cell{
		{Role: "worker", Model: "a/one", Mean: 85, N: 2},
		{Role: "worker", Model: "b/two", Mean: 70, N: 1},
	}
	if len(own) != len(want) {
		t.Fatalf("the seated cells are %+v, want %+v", own, want)
	}
	for i := range want {
		if own[i] != want[i] {
			t.Fatalf("cell %d is %+v, want %+v", i, own[i], want[i])
		}
	}
	if config.AutoIndex == nil || config.AutoIndex() == nil {
		t.Fatal("the index was not seated beside the own sheet")
	}
}

// A mode that forbids reading seats nothing: the own sheet is the pool's own
// reading, and off is off for the whole of it.
func TestWirePoolIndexSeatsNothingWhenThePoolIsOff(t *testing.T) {
	prevIndex, prevOwn := config.AutoIndex, config.AutoOwnCells
	t.Cleanup(func() { config.AutoIndex, config.AutoOwnCells = prevIndex, prevOwn })
	t.Setenv("CODEAF_MODEL_POOL", "off")

	dir := t.TempDir()
	seedOwnSheet(t, dir)
	wirePoolIndex(dir)
	if config.AutoOwnCells != nil {
		t.Fatal("a pool that forbids reading seated the own sheet")
	}
	if config.AutoIndex != nil && config.AutoIndex() != nil {
		t.Fatal("a pool that forbids reading seated an index")
	}
}

// An own sheet that does not parse is a loss, not a fault a pick stops for:
// the wired reader answers nothing rather than a broken sheet's half.
func TestWirePoolIndexSeatsNothingForAnUnparsableOwnSheet(t *testing.T) {
	prevOwn := config.AutoOwnCells
	t.Cleanup(func() { config.AutoOwnCells = prevOwn })

	dir := t.TempDir()
	poolDir := filepath.Join(dir, "pool")
	if err := os.MkdirAll(poolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(poolDir, "own.json"), []byte("not a document"), 0o600); err != nil {
		t.Fatal(err)
	}
	wirePoolIndex(dir)
	if got := config.AutoOwnCells; got != nil && got() != nil {
		t.Fatal("a broken own sheet was seated")
	}
}
