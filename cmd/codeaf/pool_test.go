package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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
		"relay https://codeaf.agentfield.ai/pool · default",
		"index https://codeaf.agentfield.ai/pool/index.json · default",
		"mirror https://raw.githubusercontent.com/Agent-Field/CodeAF/model-pool/pool/index.json · default",
		"submit https://codeaf.agentfield.ai/pool/v1/rows · default",
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
	if err := runPoolWith([]string{"status"}, &out, dir, poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "pending 2 · can send yes · can read yes") {
		t.Fatalf("status did not count the seeded rows:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "own sheet: none") {
		t.Fatalf("status did not say the install has recorded nothing of its own:\n%s", out.String())
	}

	out.Reset()
	if err := runPoolWith([]string{"status", "--json"}, &out, dir, poolClock(t), deadEnv()); err != nil {
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
	if err := runPoolWith([]string{"status"}, &out, quiet, poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "pending 0") {
		t.Fatalf("an absent outbox did not read as zero:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(quiet, "pool", "outbox.jsonl")); !os.IsNotExist(err) {
		t.Fatal("status created the outbox it was only counting")
	}
}

// seedJudgeLast writes the hook's record the way the hook leaves it, so
// status is read against what stands on disk.
func seedJudgeLast(t *testing.T, dir string, last judgeLast) {
	t.Helper()
	data, err := json.Marshal(last)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "pool"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pool", "judge-last.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// status says what the last judge did: which model, which seats it scored —
// or, when none answered, how many were asked and the reason the last one
// failed. With no record at all it says none yet, and a file that does not
// parse reads the same way, still on exit 0.
func TestPoolStatusSaysWhatTheLastJudgeDid(t *testing.T) {
	dir := t.TempDir()
	moment := time.Date(2026, 9, 17, 14, 5, 0, 0, time.Local)

	seedJudgeLast(t, dir, judgeLast{
		At: moment, Task: 7, Judge: "other/judge",
		Seats:  []string{"crew/worker", "crew/high"},
		Scored: []string{"crew/worker", "crew/high"},
	})
	var out strings.Builder
	if err := runPoolWith([]string{"status"}, &out, dir, poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "last judge: 14:05 · other/judge · scored crew/worker, crew/high") {
		t.Fatalf("status did not say what the last judge did:\n%s", out.String())
	}

	out.Reset()
	seedJudgeLast(t, dir, judgeLast{
		At: moment, Task: 7,
		Tried:  []string{"other/flaky", "other/steady"},
		Seats:  []string{"crew/worker", "crew/high"},
		Reason: "the model answered with a 429",
	})
	if err := runPoolWith([]string{"status"}, &out, dir, poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "last judge: 14:05 · failed after 2 candidates (other/flaky, …) · the model answered with a 429") {
		t.Fatalf("status did not say how the judging failed:\n%s", out.String())
	}

	// No record, and a record that does not parse: the same sentence, and the
	// reading form still exits 0.
	for name, seed := range map[string]func(t *testing.T, dir string){
		"absent": func(t *testing.T, dir string) {},
		"malformed": func(t *testing.T, dir string) {
			if err := os.MkdirAll(filepath.Join(dir, "pool"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "pool", "judge-last.json"), []byte("{"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("CODEAF_HOME", t.TempDir())
			quiet := t.TempDir()
			seed(t, quiet)
			var out strings.Builder
			if err := runPoolWith([]string{"status"}, &out, quiet, poolClock(t), deadEnv()); err != nil {
				t.Fatalf("a reading form failed over a judge record: %v", err)
			}
			if !strings.Contains(out.String(), "last judge: none yet") {
				t.Fatalf("status did not say none yet:\n%s", out.String())
			}
		})
	}
}

// The JSON form carries the record whole — the moment in RFC 3339, the judge,
// the seats — and null when there is none.
func TestPoolStatusJSONCarriesTheLastJudge(t *testing.T) {
	moment := time.Date(2026, 9, 17, 14, 5, 0, 0, time.UTC)
	dir := t.TempDir()
	seedJudgeLast(t, dir, judgeLast{
		At: moment, Task: 7, Judge: "other/judge",
		Seats:  []string{"crew/worker"},
		Scored: []string{"crew/worker"},
	})
	var out strings.Builder
	if err := runPoolWith([]string{"status", "--json"}, &out, dir, poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	var answer struct {
		LastJudge *judgeLast `json:"last_judge"`
	}
	if err := json.Unmarshal([]byte(out.String()), &answer); err != nil {
		t.Fatalf("status --json did not parse: %v\n%s", err, out.String())
	}
	if answer.LastJudge == nil {
		t.Fatal("a written record read as none")
	}
	if answer.LastJudge.Judge != "other/judge" || answer.LastJudge.Task != 7 ||
		strings.Join(answer.LastJudge.Scored, ", ") != "crew/worker" || answer.LastJudge.Reason != "" {
		t.Fatalf("the record moved: %+v", answer.LastJudge)
	}
	if got, err := time.Parse(time.RFC3339, "2026-09-17T14:05:00Z"); err != nil || !answer.LastJudge.At.Equal(got) {
		t.Fatalf("the moment is %v, want the record's own in RFC 3339", answer.LastJudge.At)
	}

	// No record: the field is null and not absent, so a script can tell the
	// two apart.
	out.Reset()
	if err := runPoolWith([]string{"status", "--json"}, &out, t.TempDir(), poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"last_judge":null`) {
		t.Fatalf("a missing record was not said as null:\n%s", out.String())
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

// The build's one key is the index signer's: it decoded at init, it is an
// ed25519 public key's length, and it starts the start-up refresh under a
// mode that reads.
func TestPoolPublicKeysCarryTheIndexSignersKey(t *testing.T) {
	if len(poolPublicKeys) != 1 {
		t.Fatalf("the build carries %d public key(s), want the index signer's one", len(poolPublicKeys))
	}
	if len(poolPublicKeys[0]) != ed25519.PublicKeySize {
		t.Fatalf("the built-in key is %d bytes, want an ed25519 public key's %d", len(poolPublicKeys[0]), ed25519.PublicKeySize)
	}

	started := 0
	prev := poolRefreshGo
	poolRefreshGo = func(scope string, fn func()) { started++ }
	t.Cleanup(func() { poolRefreshGo = prev })
	startPoolIndexRefresh(context.Background(), t.TempDir(), poolcfg.Resolve("", "", noEnv), poolPublicKeys)
	if started != 1 {
		t.Fatalf("the build's key started %d refresh(es) under mode on, want 1", started)
	}
}

// The stored key, when one is set, is the only key the puller is handed: a
// word that does not decode is a key nobody can vouch for, so the answer is
// no keys rather than the build's own — and nothing stored hands back the
// build's. poolTrustedKeysErr says which: a word set but not decodable is a
// reason, not an emptiness.
func TestPoolTrustedKeysFollowTheStoredKey(t *testing.T) {
	good := base64.StdEncoding.EncodeToString(make(ed25519.PublicKey, ed25519.PublicKeySize))
	got, err := poolTrustedKeysErr(poolcfg.Resolve("", good, noEnv))
	if err != nil || len(got) != 1 || len(got[0]) != ed25519.PublicKeySize {
		t.Fatalf("a stored key gave %d key(s) with err %v, want its one and no error", len(got), err)
	}

	for name, bad := range map[string]string{
		"not base64": "not a key at all",
		"too short":  base64.StdEncoding.EncodeToString([]byte("short")),
	} {
		keys, keyErr := poolTrustedKeysErr(poolcfg.Resolve("", bad, noEnv))
		if keyErr == nil || len(keys) != 0 {
			t.Fatalf("%s: a key that does not decode left %d key(s) in hand and no reason", name, len(keys))
		}
	}

	built, err := poolTrustedKeysErr(poolcfg.Resolve("", "", noEnv))
	if err != nil || len(built) != 1 || !bytes.Equal(built[0], poolPublicKeys[0]) {
		t.Fatalf("with nothing stored, the build's own key is the key (err %v)", err)
	}
}

// The key ships in the binary, so verify fetches under it: a dead address is
// a fetch that failed — exit 1, in the puller's words — and not a refusal at
// the door, and nothing was cached in its place.
func TestPoolVerifyFetchesUnderTheBuiltInKeyAndNamesWhatFailed(t *testing.T) {
	dir := t.TempDir()
	// A dead port is the fetch that would have happened: if the door let one
	// through, the failure below would be a fetch error, not the sentence.
	lookup := oneEnv("CODEAF_MODEL_POOL_URL", "http://127.0.0.1:1/index.json")
	var out strings.Builder
	err := runPoolWith([]string{"verify"}, &out, dir, poolClock(t), lookup)
	if err == nil {
		t.Fatal("a dead relay verified")
	}
	if errors.Is(err, exitIncomplete) {
		t.Fatalf("verify refused at the door though the build carries a key: %v", err)
	}
	if !strings.Contains(err.Error(), "pull") {
		t.Fatalf("the failure did not name the fetch: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "pool", "doc.json")); !os.IsNotExist(statErr) {
		t.Fatal("the failed verify wrote a cache")
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

// --key replaces the keys the build resolves, it does not add to them: the
// document below is signed under the stored key, which is the key the verb
// would trust with no flag at all — the stand-in for the build's own, whose
// private half no test holds. A fresh unrelated key must be able to prove a
// document does not verify under it, which is the whole reason the flag
// exists; were the flag's keys only added to the resolved ones, the stored
// key would still be in the list and the document would verify. The control
// run without the flag verifies, so the refusal is the flag's doing and not
// the signature's.
func TestPoolVerifyChecksOnlyUnderTheKeyItIsGiven(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	server := poolServer(t, priv, signedPoolDoc(7))
	other, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	lookup := poolEnv(map[string]string{
		"CODEAF_MODEL_POOL_URL":        server.URL + "/index.json",
		"CODEAF_MODEL_POOL_MIRROR_URL": "",
	})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"models.pool.public_key": "`+base64.StdEncoding.EncodeToString(pub)+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	err = runPoolWith([]string{"verify", "--key", base64.StdEncoding.EncodeToString(other)},
		&out, dir, poolClock(t), lookup)
	if err == nil || !strings.Contains(err.Error(), "pull: signature does not verify") {
		t.Fatalf("a document the --key does not trust verified: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "pool", "doc.json")); !os.IsNotExist(statErr) {
		t.Fatal("a refused document was cached")
	}

	// The control: no flag, the stored key answers for the same document.
	out.Reset()
	if err := runPoolWith([]string{"verify"}, &out, dir, poolClock(t), lookup); err != nil {
		t.Fatalf("the stored key did not verify its own document: %v", err)
	}
	if !strings.Contains(out.String(), "signature good") {
		t.Fatalf("the control run did not verify:\n%s", out.String())
	}
}

// A stored key that does not decode is not the ordinary nothing: verify
// refuses at the door with the row's name on it, and nothing is fetched.
func TestPoolVerifyNamesAStoredKeyThatDoesNotDecode(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"models.pool.public_key": "not-a-key"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	err := runPoolWith([]string{"verify"}, &out, dir, poolClock(t), deadEnv())
	if !errors.Is(err, exitIncomplete) {
		t.Fatalf("a stored key that does not decode is a door refusal, not a fetch: %v", err)
	}
	if !strings.Contains(out.String(), "models.pool.public_key does not decode") {
		t.Fatalf("the refusal did not name the row:\n%s", out.String())
	}
}

// The same refusal names the environment pin when the broken word came in as
// one — the remedy belongs to whichever word is in force.
func TestPoolVerifyNamesTheEnvPinWhenItsKeyDoesNotDecode(t *testing.T) {
	var out strings.Builder
	err := runPoolWith([]string{"verify"}, &out, t.TempDir(), poolClock(t),
		oneEnv("CODEAF_MODEL_POOL_PUBLIC_KEY", "not-a-key"))
	if !errors.Is(err, exitIncomplete) {
		t.Fatalf("a pin that does not decode is a door refusal, not a fetch: %v", err)
	}
	if !strings.Contains(out.String(), "CODEAF_MODEL_POOL_PUBLIC_KEY does not decode") {
		t.Fatalf("the refusal did not name the pin:\n%s", out.String())
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
	held := poolIndexFor(t.TempDir(), poolcfg.Resolve("", "", noEnv), poolClock(t))()
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
	held := poolIndexFor(dir, poolcfg.Resolve("", "", noEnv), poolClock(t))()
	if held == nil || held.Generated().Format("2006-01-02") != "2026-09-20" {
		t.Fatalf("a newer cache did not win: %v", held)
	}
}

// A cache that does not parse is not a cache: the seed stands in its place.
func TestPoolIndexForIgnoresAnUnparsableCache(t *testing.T) {
	dir := t.TempDir()
	writePoolDoc(t, dir, "{ this is not a document")
	held := poolIndexFor(dir, poolcfg.Resolve("", "", noEnv), poolClock(t))()
	if held == nil || held.Generated().Format("2006-01-02") != "2026-09-17" {
		t.Fatalf("an unparsable cache did not fall back to the seed: %v", held)
	}
}

// A mode that forbids reading answers no index at all.
func TestPoolIndexForAnswersNothingWhenTheModeIsOff(t *testing.T) {
	if held := poolIndexFor(t.TempDir(), poolcfg.Resolve("off", "", noEnv), poolClock(t))(); held != nil {
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
	on := poolcfg.Resolve("", "", noEnv)
	off := poolcfg.Resolve("off", "", noEnv)

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

// stubPoolRefresh stands the goroutine guard in for the length of one test
// and answers the counter it increments, so a test of what wirePoolIndex
// seats runs no fetch and no push. The tests that call it read seats, not
// errands; the errands have their own tests.
func stubPoolRefresh(t *testing.T) *int {
	t.Helper()
	started := 0
	prev := poolRefreshGo
	poolRefreshGo = func(scope string, fn func()) { started++ }
	t.Cleanup(func() { poolRefreshGo = prev })
	return &started
}

// The start-up errands are one push and one refresh behind the same guard:
// a mode that sends and reads starts both, a mode that only reads starts
// only the refresh (its push would send nowhere), and off starts neither.
func TestWirePoolIndexStartsTheRefreshAndThePush(t *testing.T) {
	started := stubPoolRefresh(t)
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", "http://127.0.0.1:1/v1/rows")

	// The default mode is on: the refresh and the push both start.
	wirePoolIndex(t.TempDir())
	if *started != 2 {
		t.Fatalf("a sending pool started %d errand(s) at start-up, want the refresh and the push", *started)
	}

	// A mode that does not send starts the refresh alone.
	*started = 0
	t.Setenv("CODEAF_MODEL_POOL", "read")
	wirePoolIndex(t.TempDir())
	if *started != 1 {
		t.Fatalf("a read-only pool started %d errand(s), want the refresh alone", *started)
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
	if err := runPoolWith([]string{"status"}, &out, dir, poolClock(t), deadEnv()); err != nil {
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
	stubPoolRefresh(t)

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
	stubPoolRefresh(t)

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
	stubPoolRefresh(t)

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

// ── THE MIRROR ──────────────────────────────────────────────────────────────

// poolEnv is an environment holding exactly the names given, so a test pins
// the pool's addresses without touching the process.
func poolEnv(pairs map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, set := pairs[name]
		return value, set
	}
}

// deadEnv pins both index addresses at a closed local port, so a probe fails
// at once and reaches no network.
func deadEnv() func(string) (string, bool) {
	return poolEnv(map[string]string{
		"CODEAF_MODEL_POOL_URL":        "http://127.0.0.1:1/index.json",
		"CODEAF_MODEL_POOL_MIRROR_URL": "http://127.0.0.1:1/index.json",
	})
}

// signedPoolDoc is a document the puller accepts: one integer version and
// nothing else it reads. The version is where a test finds which address was
// the one that answered.
func signedPoolDoc(version int) []byte {
	return []byte(fmt.Sprintf(`{"version": %d, "schema": 1, "generated": "2026-09-10", "min_installs": 1, "judges": [], "metrics": {}, "cells": []}`, version))
}

// poolServer serves a signed index over http the way a relay does: the
// document at /index.json and its base64 signature beside it at
// /index.json.sig, both signed under priv.
func poolServer(t *testing.T, priv ed25519.PrivateKey, doc []byte) *httptest.Server {
	t.Helper()
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, doc))
	mux := http.NewServeMux()
	mux.HandleFunc("/index.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(doc)
	})
	mux.HandleFunc("/index.json.sig", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sig))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// cachedVersion reads the "version" of the document under the profile's pool
// directory, and whether one is there at all.
func cachedVersion(t *testing.T, dir string) (int64, bool) {
	t.Helper()
	doc, err := os.ReadFile(filepath.Join(dir, "pool", "doc.json"))
	if err != nil {
		return 0, false
	}
	var obj struct {
		Version int64 `json:"version"`
	}
	if err := json.Unmarshal(doc, &obj); err != nil {
		t.Fatalf("the cached document does not parse: %v", err)
	}
	return obj.Version, true
}

// A relay that does not answer is not the end of the fetch: the mirror is
// asked with the same keys and the same cache directory, and the document it
// serves lands where the next start reads it.
func TestPoolRefreshFallsToTheMirrorWhenTheRelayIsDown(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	mirror := poolServer(t, priv, signedPoolDoc(9))
	dir := t.TempDir()
	cfg := poolcfg.Config{Mode: poolcfg.On, IndexURL: "http://127.0.0.1:1/index.json", MirrorURL: mirror.URL + "/index.json"}
	refreshPoolIndex(context.Background(), dir, cfg, []ed25519.PublicKey{pub})
	version, ok := cachedVersion(t, dir)
	if !ok || version != 9 {
		t.Fatalf("the mirror's document was not kept: version %d, cached %v", version, ok)
	}
}

// Both addresses down leaves the cache exactly where it was: the fetch falls
// back to the copy already on disk and writes nothing.
func TestPoolRefreshKeepsTheCacheWhenBothAddressesAreDown(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	relay := poolServer(t, priv, signedPoolDoc(7))
	dir := t.TempDir()
	live := poolcfg.Config{Mode: poolcfg.On, IndexURL: relay.URL + "/index.json", MirrorURL: relay.URL + "/index.json"}
	refreshPoolIndex(context.Background(), dir, live, []ed25519.PublicKey{pub})
	if version, ok := cachedVersion(t, dir); !ok || version != 7 {
		t.Fatalf("the relay's document was not cached: version %d, cached %v", version, ok)
	}
	relay.Close()
	dead := poolcfg.Config{Mode: poolcfg.On, IndexURL: "http://127.0.0.1:1/index.json", MirrorURL: "http://127.0.0.1:1/index.json"}
	refreshPoolIndex(context.Background(), dir, dead, []ed25519.PublicKey{pub})
	if version, ok := cachedVersion(t, dir); !ok || version != 7 {
		t.Fatalf("a failed refresh moved the cache: version %d, cached %v", version, ok)
	}
}

// A signature failure is a statement about the primary's bytes, not a dead
// source: the mirror is not asked, so its good document is never cached in
// place of the one whose signature just failed.
func TestPoolRefreshDoesNotFallToTheMirrorOnABadSignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, otherPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// The primary serves a document signed under a key nobody trusts; the
	// mirror serves a good one that must not be reached.
	relay := poolServer(t, otherPriv, signedPoolDoc(7))
	mirror := poolServer(t, priv, signedPoolDoc(9))
	dir := t.TempDir()
	cfg := poolcfg.Config{Mode: poolcfg.On, IndexURL: relay.URL + "/index.json", MirrorURL: mirror.URL + "/index.json"}
	refreshPoolIndex(context.Background(), dir, cfg, []ed25519.PublicKey{pub})
	if _, ok := cachedVersion(t, dir); ok {
		t.Fatal("a document with a bad signature fell through to the mirror, or was cached")
	}
}

// ── THE RELAY LINE ──────────────────────────────────────────────────────────

// status asks the relay whether it answers, over a signed document served by
// an httptest relay and checked under the key --key hands in.
func TestPoolStatusSaysTheRelayAnswered(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	server := poolServer(t, priv, signedPoolDoc(7))
	lookup := poolEnv(map[string]string{
		"CODEAF_MODEL_POOL_URL":        server.URL + "/index.json",
		"CODEAF_MODEL_POOL_MIRROR_URL": "",
	})
	var out strings.Builder
	if err := runPoolWith([]string{"status", "--key", base64.StdEncoding.EncodeToString(pub)}, &out, t.TempDir(), poolClock(t), lookup); err != nil {
		t.Fatalf("a reachable relay failed status: %v", err)
	}
	if !strings.Contains(out.String(), "relay: reachable · index version 7") {
		t.Fatalf("status did not say the relay answered:\n%s", out.String())
	}
}

// A closed relay and a closed mirror are a reading, not a failure: status says
// both are unreachable, says what it read instead, and still exits 0.
func TestPoolStatusSaysUnreachableAndStillExitsZero(t *testing.T) {
	var out strings.Builder
	if err := runPoolWith([]string{"status"}, &out, t.TempDir(), poolClock(t), deadEnv()); err != nil {
		t.Fatalf("an unreachable relay failed status: %v", err)
	}
	body := out.String()
	if !strings.Contains(body, "relay: unreachable (") {
		t.Fatalf("status did not say the relay was unreachable:\n%s", body)
	}
	if !strings.Contains(body, "mirror: unreachable (") {
		t.Fatalf("status did not say the mirror was unreachable:\n%s", body)
	}
	if !strings.Contains(body, "reading built-in seed") {
		t.Fatalf("status did not say what it read instead:\n%s", body)
	}
}

// The mirror is asked when the relay does not answer, and the line says so.
func TestPoolStatusFallsToTheMirrorWhenTheRelayIsDown(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	mirror := poolServer(t, priv, signedPoolDoc(7))
	lookup := poolEnv(map[string]string{
		"CODEAF_MODEL_POOL_URL":        "http://127.0.0.1:1/index.json",
		"CODEAF_MODEL_POOL_MIRROR_URL": mirror.URL + "/index.json",
	})
	var out strings.Builder
	if err := runPoolWith([]string{"status", "--key", base64.StdEncoding.EncodeToString(pub)}, &out, t.TempDir(), poolClock(t), lookup); err != nil {
		t.Fatalf("a mirror that answered still failed status: %v", err)
	}
	body := out.String()
	if !strings.Contains(body, "relay: unreachable (") || !strings.Contains(body, "mirror: reachable · index version 7") {
		t.Fatalf("status did not fall to the mirror:\n%s", body)
	}
}

// status checks under --key alone too: the relay's document is signed under
// the stored key — the one the probe would trust with no flag at all — and a
// fresh unrelated key must be able to prove it does not answer. Were the
// flag's keys only added to the resolved ones, the stored key would still be
// in the list and the relay would answer; the control run without the flag
// does answer, so the refusal is the flag's doing.
func TestPoolStatusChecksOnlyUnderTheKeyItIsGiven(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	server := poolServer(t, priv, signedPoolDoc(7))
	other, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	lookup := poolEnv(map[string]string{
		"CODEAF_MODEL_POOL_URL":        server.URL + "/index.json",
		"CODEAF_MODEL_POOL_MIRROR_URL": "",
	})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"models.pool.public_key": "`+base64.StdEncoding.EncodeToString(pub)+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := runPoolWith([]string{"status", "--json", "--key", base64.StdEncoding.EncodeToString(other)}, &out, dir, poolClock(t), lookup); err != nil {
		t.Fatalf("a reading form failed over a signature that did not check: %v", err)
	}
	var answer struct {
		Relay *probeSummary `json:"relay"`
	}
	if err := json.Unmarshal([]byte(out.String()), &answer); err != nil {
		t.Fatalf("status --json did not parse: %v\n%s", err, out.String())
	}
	if answer.Relay == nil || answer.Relay.Reachable || !strings.Contains(answer.Relay.Reason, "signature does not verify") {
		t.Fatalf("the relay was not checked under the flag's key alone: %+v", answer.Relay)
	}

	// The control: no flag, the same relay answers under the stored key.
	out.Reset()
	if err := runPoolWith([]string{"status", "--json"}, &out, dir, poolClock(t), lookup); err != nil {
		t.Fatalf("the stored key did not answer for its own document: %v", err)
	}
	if err := json.Unmarshal([]byte(out.String()), &answer); err != nil {
		t.Fatalf("status --json did not parse: %v\n%s", err, out.String())
	}
	if answer.Relay == nil || !answer.Relay.Reachable {
		t.Fatalf("the control run did not reach the relay under the stored key: %+v", answer.Relay)
	}
}

// A stored key that does not decode does not fail a reading: status says why
// the relay was not asked, in the line and in the JSON reason, and exits 0.
func TestPoolStatusSaysWhyAStoredKeyDoesNotDecode(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"models.pool.public_key": "not-a-key"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := runPoolWith([]string{"status"}, &out, dir, poolClock(t), deadEnv()); err != nil {
		t.Fatalf("a broken stored key failed status: %v", err)
	}
	if !strings.Contains(out.String(), "relay: unreachable (models.pool.public_key does not decode") {
		t.Fatalf("status did not name the broken row:\n%s", out.String())
	}

	out.Reset()
	if err := runPoolWith([]string{"status", "--json"}, &out, dir, poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	var answer struct {
		Relay *probeSummary `json:"relay"`
	}
	if err := json.Unmarshal([]byte(out.String()), &answer); err != nil {
		t.Fatalf("status --json did not parse: %v\n%s", err, out.String())
	}
	if answer.Relay == nil || !strings.Contains(answer.Relay.Reason, "models.pool.public_key does not decode") {
		t.Fatalf("the reason did not name the broken row: %+v", answer.Relay)
	}
}

// A mode that forbids reading asks the network nothing and says so, whatever
// the addresses in force are.
func TestPoolStatusDoesNotReadWhenOff(t *testing.T) {
	var out strings.Builder
	lookup := poolEnv(map[string]string{"CODEAF_MODEL_POOL": "off"})
	if err := runPoolWith([]string{"status"}, &out, t.TempDir(), poolClock(t), lookup); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "relay: not read (model_pool off)") {
		t.Fatalf("an off pool did not say it asked nothing:\n%s", out.String())
	}
}

// status --json carries the relay and the mirror as objects — whether each
// answered, the version it served, and the reason when it did not.
func TestPoolStatusJSONCarriesTheRelayAndMirror(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	server := poolServer(t, priv, signedPoolDoc(7))
	lookup := poolEnv(map[string]string{
		"CODEAF_MODEL_POOL_URL":        server.URL + "/index.json",
		"CODEAF_MODEL_POOL_MIRROR_URL": "",
	})
	var out strings.Builder
	if err := runPoolWith([]string{"status", "--json", "--key", base64.StdEncoding.EncodeToString(pub)}, &out, t.TempDir(), poolClock(t), lookup); err != nil {
		t.Fatal(err)
	}
	var answer struct {
		Relay  *probeSummary `json:"relay"`
		Mirror *probeSummary `json:"mirror"`
	}
	if err := json.Unmarshal([]byte(out.String()), &answer); err != nil {
		t.Fatalf("status --json did not parse: %v\n%s", err, out.String())
	}
	if answer.Relay == nil || !answer.Relay.Reachable || answer.Relay.Version != 7 {
		t.Fatalf("the relay was not reported: %+v", answer.Relay)
	}
	if answer.Mirror == nil || answer.Mirror.Reachable {
		t.Fatalf("the mirror was reported as answering though the relay did: %+v", answer.Mirror)
	}
}

// A show asks nothing, so its object carries neither a relay nor a mirror.
func TestPoolShowJSONCarriesNoRelayAnswer(t *testing.T) {
	var out strings.Builder
	if err := runPoolWith([]string{"show", "--json"}, &out, t.TempDir(), poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	var answer map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out.String()), &answer); err != nil {
		t.Fatalf("show --json did not parse: %v\n%s", err, out.String())
	}
	if _, has := answer["relay"]; has {
		t.Fatal("a show carried a relay field")
	}
	if _, has := answer["mirror"]; has {
		t.Fatal("a show carried a mirror field")
	}
}
