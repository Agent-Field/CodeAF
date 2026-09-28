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

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/pool/index"
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

// seedDay is the day the embedded seed carries. It is read from the seed rather
// than pinned, because the seed is regenerated from the relay and its day moves
// with the pool — an expected figure, not a constant a test should freeze.
func seedDay(t *testing.T) string {
	t.Helper()
	seed, err := index.SeedIndex()
	if err != nil {
		t.Fatalf("the embedded seed does not parse: %v", err)
	}
	return seed.Generated().Format("2006-01-02")
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

// poolDocBothMetrics is the relay's wire shape: the judge's scored cells and
// the graded shares beside them, the shares split by the source that
// produced each. The numbers name no model; it is a fixture, and the field
// it is read for is its shape.
func poolDocBothMetrics() []byte {
	return []byte(`{
		"version": 7,
		"schema": 1,
		"generated": "2026-09-10",
		"min_installs": 1,
		"judges": ["z-ai/glm-5.3"],
		"metrics": {
			"role_quality": {"kind": "gaussian", "unit": "score", "dims": ["role", "model"]},
			"acceptable": {"kind": "bernoulli", "unit": "share", "dims": ["role", "model", "source"]}
		},
		"cells": [
			{"metric": "role_quality", "role": "worker", "model": "z-ai/glm-5.3", "mean": 75, "sd": 7, "n": 30},
			{"metric": "acceptable", "role": "worker", "model": "z-ai/glm-5.3", "mean": 0.9, "sd": 0, "n": 20, "source": "reviewer"},
			{"metric": "acceptable", "role": "planner", "model": "z-ai/glm-5.3", "mean": 0.8, "sd": 0, "n": 30, "source": "reviewer"},
			{"metric": "acceptable", "role": "worker", "model": "z-ai/glm-5.3", "mean": 0.7, "sd": 0, "n": 15, "source": "grader"}
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

// seedDroppedOutbox writes an outbox that holds one dropped marker per reason,
// the way a relay's refusal or the cap leaves them, so status reads a box
// whose measurements were thrown away. An empty reason writes the marker an
// older build left, with no reason at all.
func seedDroppedOutbox(t *testing.T, reasons ...string) string {
	t.Helper()
	dir := t.TempDir()
	poolDir := filepath.Join(dir, "pool")
	if err := os.MkdirAll(poolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for i, reason := range reasons {
		marker := fmt.Sprintf(`{"dropped":"%032x"`, i+1)
		if reason != "" {
			encoded, err := json.Marshal(reason)
			if err != nil {
				t.Fatal(err)
			}
			marker += `,"reason":` + string(encoded)
		}
		body.WriteString(marker + "}")
		body.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(poolDir, "outbox.jsonl"), []byte(body.String()), 0o600); err != nil {
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
		"no index cached yet · built-in seed of " + seedDay(t),
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
	if strings.Contains(out.String(), "dropped") {
		t.Fatalf("status named dropped rows when none were dropped:\n%s", out.String())
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

// When the relay refuses a row by line, or the cap drops one, the outbox
// retires it as dropped and keeps the reason. Status says how many and the
// last one's reason, so a person can tell a working install from one whose
// measurements are being thrown away — and a file that kept no reason says
// the count alone.
func TestPoolStatusSaysWhenTheRelayDroppedRowsAndWhy(t *testing.T) {
	dir := seedDroppedOutbox(t, "a first reason", "role must be worker, high or mastermind")
	var out strings.Builder
	if err := runPoolWith([]string{"status"}, &out, dir, poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	want := "pending 0 · dropped 2 (last: role must be worker, high or mastermind) · can send yes · can read yes"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("status did not say the relay dropped rows and why:\n%s", out.String())
	}

	out.Reset()
	if err := runPoolWith([]string{"status", "--json"}, &out, dir, poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	var answer struct {
		Pending     int    `json:"pending"`
		Dropped     int    `json:"dropped"`
		DroppedLast string `json:"dropped_last"`
	}
	if err := json.Unmarshal([]byte(out.String()), &answer); err != nil {
		t.Fatalf("status --json did not parse: %v\n%s", err, out.String())
	}
	if answer.Pending != 0 || answer.Dropped != 2 || answer.DroppedLast != "role must be worker, high or mastermind" {
		t.Fatalf("the dropped rows did not carry: %+v", answer)
	}

	// A file written before reasons were kept says how many and no more.
	old := seedDroppedOutbox(t, "", "")
	out.Reset()
	if err := runPoolWith([]string{"status"}, &out, old, poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "pending 0 · dropped 2 · can send yes · can read yes") {
		t.Fatalf("a reasonless dropped marker did not read as the count alone:\n%s", out.String())
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

// seedPendingRows writes pending rows the way the doors leave them, by hand
// rather than through writePendingLanding: the writer stamps the moment
// itself, and a status test needs to hold the ages still.
func seedPendingRows(t *testing.T, dir string, rows ...pendingLanding) {
	t.Helper()
	poolDir := filepath.Join(dir, "pool")
	if err := os.MkdirAll(poolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for _, row := range rows {
		data, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		body.Write(data)
		body.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(poolDir, "pending.jsonl"), []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

// seedJudgedMarker lays down the marker the judge leaves for a run it scored,
// so status reads the row behind it as judged.
func seedJudgedMarker(t *testing.T, dir string, id uint64, attempt int) {
	t.Helper()
	poolDir := filepath.Join(dir, "pool")
	if err := os.MkdirAll(judgedDir(poolDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(judgedMarkerPath(poolDir, id, attempt), []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
}

// seedInstallFile writes the install's nonce file where a sending install
// leaves it, so status is read against what stands on disk. The bytes are a
// fixture in the accepted shape; a reading form asks only whether the file is
// there, never what it holds.
func seedInstallFile(t *testing.T, dir string) {
	t.Helper()
	poolDir := filepath.Join(dir, "pool")
	if err := os.MkdirAll(poolDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(poolDir, installFile), []byte(strings.Repeat("a", nonceHexLen)), 0o600); err != nil {
		t.Fatal(err)
	}
}

// seedSweepLast writes the sweep's record the way the sweep leaves it, so
// status is read against what stands on disk.
func seedSweepLast(t *testing.T, dir string, last sweepLast) {
	t.Helper()
	data, err := json.Marshal(last)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "pool"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pool", "sweep-last.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// status says what is waiting for a judge and what the last sweep did: the
// pending file's unjudged rows with the oldest one's door and age, and the
// sweep's own record of what it judged, what its budget left and whether the
// deadline cut it. --json carries both records, and a row written before rows
// carried a moment says an unknown age rather than inventing one.
func TestPoolStatusCountsThePendingJudgeRowsAndSaysWhatTheLastSweepDid(t *testing.T) {
	dir := t.TempDir()
	do := poolTestLanding()
	do.ID, do.Attempt = 7, 1
	exec := poolTestLanding()
	exec.ID, exec.Attempt = 9, 2
	threeHoursAgo := poolClock(t)().Add(-3 * time.Hour)
	seedPendingRows(t, dir,
		pendingLanding{At: threeHoursAgo, Door: "do", Landing: do},
		pendingLanding{Door: "exec", Landing: exec},
	)
	seedJudgedMarker(t, dir, 9, 2)
	seedSweepLast(t, dir, sweepLast{
		At: poolClock(t)().Add(-2 * time.Minute), Judged: 3, Left: 2,
		BudgetUsed: 41, Cut: true,
	})

	var out strings.Builder
	if err := runPoolWith([]string{"status"}, &out, dir, poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	if !strings.Contains(body, "pending judge: 1 · oldest do run 3h") {
		t.Fatalf("status did not count the waiting rows:\n%s", body)
	}
	if !strings.Contains(body, "last sweep: 2m ago · judged 3 · 2 still pending · 41s of 10m") {
		t.Fatalf("status did not say what the last sweep did:\n%s", body)
	}

	out.Reset()
	if err := runPoolWith([]string{"status", "--json"}, &out, dir, poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	var answer struct {
		PendingJudge *struct {
			Count      int        `json:"count"`
			OldestDoor string     `json:"oldest_door"`
			OldestAt   *time.Time `json:"oldest_at"`
		} `json:"pending_judge"`
		LastSweep *struct {
			At         time.Time `json:"at"`
			Judged     int       `json:"judged"`
			Left       int       `json:"left"`
			BudgetUsed int       `json:"budget_used"`
			Cut        bool      `json:"cut"`
		} `json:"last_sweep"`
	}
	if err := json.Unmarshal([]byte(out.String()), &answer); err != nil {
		t.Fatalf("status --json did not parse: %v\n%s", err, out.String())
	}
	if answer.PendingJudge == nil || answer.PendingJudge.Count != 1 || answer.PendingJudge.OldestDoor != "do" {
		t.Fatalf("the waiting rows did not carry: %+v", answer.PendingJudge)
	}
	if got, err := time.Parse(time.RFC3339, "2026-09-17T21:00:00Z"); err != nil ||
		answer.PendingJudge.OldestAt == nil || !answer.PendingJudge.OldestAt.Equal(got) {
		t.Fatalf("the oldest row's moment is %v, want the row's own in RFC 3339", answer.PendingJudge.OldestAt)
	}
	if answer.LastSweep == nil || answer.LastSweep.Judged != 3 || answer.LastSweep.Left != 2 ||
		answer.LastSweep.BudgetUsed != 41 || !answer.LastSweep.Cut {
		t.Fatalf("the sweep's record did not carry: %+v", answer.LastSweep)
	}
	if got, err := time.Parse(time.RFC3339, "2026-09-17T23:58:00Z"); err != nil || !answer.LastSweep.At.Equal(got) {
		t.Fatalf("the sweep's moment is %v, want the record's own in RFC 3339", answer.LastSweep.At)
	}

	// A row written before rows carried a moment is the oldest of them — it
	// predates the stamp — and says so rather than inventing an age.
	unknown := t.TempDir()
	seedPendingRows(t, unknown,
		pendingLanding{At: threeHoursAgo, Door: "do", Landing: do},
		pendingLanding{Door: "exec", Landing: exec},
	)
	out.Reset()
	if err := runPoolWith([]string{"status"}, &out, unknown, poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "pending judge: 2 · oldest exec run age unknown") {
		t.Fatalf("status did not say the oldest row's age is unknown:\n%s", out.String())
	}
}

// An install nothing has reached — no pending file, no sweep record — says so
// in the two sentences a nothing is said in here, and the reading form writes
// nothing while it looks.
func TestPoolStatusSaysWhenNothingWaitsAndNoSweepHasRun(t *testing.T) {
	quiet := t.TempDir()
	var out strings.Builder
	if err := runPoolWith([]string{"status"}, &out, quiet, poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	if !strings.Contains(body, "pending judge: none") {
		t.Fatalf("status did not say nothing waits:\n%s", body)
	}
	if !strings.Contains(body, "last sweep: none yet") {
		t.Fatalf("status did not say no sweep has run:\n%s", body)
	}
	if _, err := os.Stat(filepath.Join(quiet, "pool", "pending.jsonl")); !os.IsNotExist(err) {
		t.Fatal("status created the pending file it was only counting")
	}

	out.Reset()
	if err := runPoolWith([]string{"status", "--json"}, &out, quiet, poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"pending_judge":{"count":0}`) {
		t.Fatalf("an absent pending file did not read as zero:\n%s", out.String())
	}
	if !strings.Contains(out.String(), `"last_sweep":null`) {
		t.Fatalf("a missing sweep record was not said as null:\n%s", out.String())
	}
}

// A fresh install that has minted its nonce says so at the end of the outbox
// line, and one that has not says the line it always did — and looking never
// mints the file, because a reading form writes nothing.
func TestPoolStatusSaysTheInstallIdentityIsSetAndNeverMintsIt(t *testing.T) {
	dir := t.TempDir()
	seedInstallFile(t, dir)
	var out strings.Builder
	if err := runPoolWith([]string{"status"}, &out, dir, poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "can read yes · identity set") {
		t.Fatalf("status did not say the install's identity is set:\n%s", out.String())
	}

	out.Reset()
	if err := runPoolWith([]string{"status", "--json"}, &out, dir, poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	var answer struct {
		Identity *bool `json:"identity"`
	}
	if err := json.Unmarshal([]byte(out.String()), &answer); err != nil {
		t.Fatalf("status --json did not parse: %v\n%s", err, out.String())
	}
	if answer.Identity == nil || !*answer.Identity {
		t.Fatalf("--json did not carry the identity: %+v", answer.Identity)
	}

	// No nonce yet: the line is the line it has always been, and the reading
	// form did not mint one.
	quiet := t.TempDir()
	out.Reset()
	if err := runPoolWith([]string{"status"}, &out, quiet, poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "pending 0 · can send yes · can read yes") {
		t.Fatalf("an install with no nonce did not print the line as it was:\n%s", out.String())
	}
	if strings.Contains(out.String(), "identity set") {
		t.Fatalf("an install with no nonce claimed an identity:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(quiet, "pool", installFile)); !os.IsNotExist(err) {
		t.Fatal("status minted the install nonce it was only reading")
	}

	// The JSON form says false rather than absent, so a script reads a
	// not-yet-sending install from one that is.
	out.Reset()
	if err := runPoolWith([]string{"status", "--json"}, &out, quiet, poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"identity":false`) {
		t.Fatalf("an install with no nonce did not say false:\n%s", out.String())
	}
}

// status counts every landing that has ever been judged, from the markers the
// judge leaves, and folds the total into the sweep line; a line with no marker
// to count reads as it always did, and --json carries the total beside the
// sweep's record.
func TestPoolStatusCountsTheJudgedInAllAndFoldsItIntoTheSweepLine(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []uint64{3, 5, 8} {
		seedJudgedMarker(t, dir, id, 1)
	}
	var out strings.Builder
	if err := runPoolWith([]string{"status"}, &out, dir, poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "last sweep: none yet · judged 3 in all") {
		t.Fatalf("status did not count the judged landings in all:\n%s", out.String())
	}

	out.Reset()
	if err := runPoolWith([]string{"status", "--json"}, &out, dir, poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	var answer struct {
		JudgedTotal *int `json:"judged_total"`
	}
	if err := json.Unmarshal([]byte(out.String()), &answer); err != nil {
		t.Fatalf("status --json did not parse: %v\n%s", err, out.String())
	}
	if answer.JudgedTotal == nil || *answer.JudgedTotal != 3 {
		t.Fatalf("--json carried %v, want 3 judged in all", answer.JudgedTotal)
	}

	// No marker at all: the sweep line is the one it has always been.
	quiet := t.TempDir()
	out.Reset()
	if err := runPoolWith([]string{"status"}, &out, quiet, poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "last sweep: none yet") {
		t.Fatalf("status did not say no sweep has run:\n%s", out.String())
	}
	if strings.Contains(out.String(), "in all") {
		t.Fatalf("status counted landings in all with no marker to count:\n%s", out.String())
	}
}

// With no judge record the judge line says what the absence means — nothing
// has landed to be judged, rather than the pool being off — and --json keeps
// last_judge null.
func TestPoolStatusSaysNoLandingWasJudgedWhenThereIsNoRecord(t *testing.T) {
	quiet := t.TempDir()
	var out strings.Builder
	if err := runPoolWith([]string{"status"}, &out, quiet, poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "last judge: none yet (no landing judged)") {
		t.Fatalf("status did not say no landing was judged:\n%s", out.String())
	}

	out.Reset()
	if err := runPoolWith([]string{"status", "--json"}, &out, quiet, poolClock(t), deadEnv()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"last_judge":null`) {
		t.Fatalf("a missing record was not said as null:\n%s", out.String())
	}
}

// None of the three reaches show: the reading form's words and its --json are
// the ones they always were, so a person reading the config reads no status.
func TestPoolShowIsUnchangedByTheIdentityTheJudgedTotalAndTheLandingSentence(t *testing.T) {
	dir := seedIndex(t)
	seedInstallFile(t, dir)
	for _, id := range []uint64{1, 2, 3} {
		seedJudgedMarker(t, dir, id, 1)
	}
	var out strings.Builder
	if err := runPoolWith([]string{"show"}, &out, dir, poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{"identity set", "in all", "no landing judged"} {
		if strings.Contains(out.String(), unwanted) {
			t.Fatalf("show carried status's %q:\n%s", unwanted, out.String())
		}
	}

	out.Reset()
	if err := runPoolWith([]string{"show", "--json"}, &out, dir, poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{`"identity"`, `"judged_total"`} {
		if strings.Contains(out.String(), unwanted) {
			t.Fatalf("show --json carried status's %s:\n%s", unwanted, out.String())
		}
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
	if !strings.Contains(out.String(), "signature good: version 7, generated 2026-09-10, metrics role_rating") {
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

// verify names the metrics it verified, where it used to count them: the
// count said how many, the names say which, and the second metric is no
// longer invisible. --json carries the array beside the count the way the
// reading forms do.
func TestPoolVerifyNamesTheMetricsItVerified(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	doc := poolDocBothMetrics()
	if err := os.WriteFile(filepath.Join(src, "index.json"), doc, 0o644); err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(priv, doc)
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
	if !strings.Contains(out.String(), "signature good: version 7, generated 2026-09-10, metrics acceptable, role_quality") {
		t.Fatalf("verify did not name both metrics:\n%s", out.String())
	}

	out.Reset()
	if err := runPoolWith([]string{"verify", "--json", "--key", base64.StdEncoding.EncodeToString(pub)},
		&out, dir, poolClock(t), lookup); err != nil {
		t.Fatal(err)
	}
	var answer struct {
		Verified   bool `json:"verified"`
		Metrics    int  `json:"metrics"`
		MetricList []struct {
			Name    string   `json:"name"`
			Kind    string   `json:"kind"`
			Unit    string   `json:"unit"`
			Dims    []string `json:"dims"`
			Cells   int      `json:"cells"`
			Sources []string `json:"sources"`
		} `json:"metric_list"`
	}
	if err := json.Unmarshal([]byte(out.String()), &answer); err != nil {
		t.Fatalf("verify --json did not parse: %v\n%s", err, out.String())
	}
	if !answer.Verified || answer.Metrics != 2 || len(answer.MetricList) != 2 {
		t.Fatalf("verify reported %+v, want both metrics beside the count", answer)
	}
	shares, quality := answer.MetricList[0], answer.MetricList[1]
	if shares.Name != "acceptable" || shares.Kind != "bernoulli" || shares.Unit != "share" ||
		strings.Join(shares.Dims, ",") != "role,model,source" || shares.Cells != 3 ||
		strings.Join(shares.Sources, ",") != "grader,reviewer" {
		t.Fatalf("the graded shares read as %+v", shares)
	}
	if quality.Name != "role_quality" || quality.Kind != "gaussian" || quality.Unit != "score" ||
		strings.Join(quality.Dims, ",") != "role,model" || quality.Cells != 1 || len(quality.Sources) != 0 {
		t.Fatalf("the judged scores read as %+v", quality)
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
	// A CI environment answers the mode when neither the environment word nor
	// the stored setting does: GitHub Actions exports CI=true, the resolver reads
	// that as read-only, the push never starts, and the count below read one on
	// every pull-request run while passing on every laptop. This test is about
	// the DEFAULT mode, so the CI word is emptied for its duration; t.Setenv
	// restores the runner's own value afterwards.
	t.Setenv("CI", "")

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
	if answer.Index == nil || answer.Index.Source != "seed" || answer.Index.Generated != seedDay(t) {
		t.Fatalf("the seed was not reported: %+v", answer.Index)
	}
}

// The reading form counts the held document's cells, so a person can tell
// an empty document from a full one without opening it. The two cells name
// no model; the point is the count, on the index line and in --json alike.
func TestPoolShowSaysHowManyCellsTheCachedIndexHolds(t *testing.T) {
	dir := t.TempDir()
	poolDir := filepath.Join(dir, "pool")
	if err := os.MkdirAll(poolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := []byte(`{
			"version": 7,
			"schema": 1,
			"generated": "2026-09-10",
			"min_installs": 1,
			"judges": ["z-ai/glm-5.3"],
			"metrics": {"role_rating": {"kind": "gaussian", "dims": ["role", "model"]}},
			"cells": [
				{"metric": "role_rating", "role": "planner", "model": "z-ai/glm-5.3", "mean": 1312, "sd": 18, "n": 9},
				{"metric": "role_rating", "role": "checker", "model": "z-ai/glm-5.3", "mean": 1290, "sd": 21, "n": 9}
			]
		}`)
	if err := os.WriteFile(filepath.Join(poolDir, "doc.json"), doc, 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := runPoolWith([]string{"show"}, &out, dir, poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	indexLine := ""
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "index ·") {
			indexLine = line
		}
	}
	if indexLine == "" {
		t.Fatalf("show printed no index line:\n%s", body)
	}
	if !strings.Contains(indexLine, "2 cells") {
		t.Errorf("the index line did not count its cells:\n%s", indexLine)
	}

	out.Reset()
	if err := runPoolWith([]string{"show", "--json"}, &out, dir, poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	var answer struct {
		Index *struct {
			Cells int `json:"cells"`
		} `json:"index"`
	}
	if err := json.Unmarshal([]byte(out.String()), &answer); err != nil {
		t.Fatalf("--json did not parse: %v\n%s", err, out.String())
	}
	if answer.Index == nil || answer.Index.Cells != 2 {
		t.Fatalf("--json carried %+v, want 2 cells", answer.Index)
	}
}

// With no cache the reading form names the seed's own cell count, computed
// here from the seed itself rather than hard-coded, so the sentence follows
// the document the build carries.
func TestPoolShowSaysHowManyCellsTheBuiltInSeedHolds(t *testing.T) {
	seed, err := index.SeedIndex()
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, metric := range seed.Metrics() {
		total += len(seed.Cells(metric))
	}

	var out strings.Builder
	if err := runPoolWith(nil, &out, t.TempDir(), poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	if want := countWord(total, "cell", "cells"); !strings.Contains(out.String(), want) {
		t.Errorf("show did not say the seed holds %q:\n%s", want, out.String())
	}
	// The seed's score metric says itself the way a cached document's do:
	// the kind and unit the document spells, ITS OWN cells — the seed carries
	// the graded shares beside it now, so the metric's count is not the
	// total's — and the dims a cell of it is addressed by.
	if want := "role_quality: gaussian score · " + countWord(len(seed.Cells("role_quality")), "cell", "cells") + " · dims role, model"; !strings.Contains(out.String(), want) {
		t.Errorf("the seed's metric line did not read %q:\n%s", want, out.String())
	}
}

// The index declares two metrics now — the judge's scores and the graded
// shares beside them — and a count says neither which nor what. show prints
// one line per declared metric after the index line: the kind and unit the
// document spells, the cells counted with their noun, the dims a cell of
// the metric is addressed by, and the distinct sources when the cells are
// split by one. The order is the index's own, sorted.
func TestPoolShowPrintsEachMetricAfterTheIndexLine(t *testing.T) {
	dir := t.TempDir()
	writePoolDoc(t, dir, string(poolDocBothMetrics()))
	var out strings.Builder
	if err := runPoolWith([]string{"show"}, &out, dir, poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	indexAt := strings.Index(body, "index · ")
	sharesAt := strings.Index(body, "acceptable: bernoulli share · 3 cells · dims role, model, source · sources grader, reviewer")
	qualityAt := strings.Index(body, "role_quality: gaussian score · 1 cell · dims role, model")
	if indexAt < 0 || sharesAt < 0 || qualityAt < 0 {
		t.Fatalf("show did not print both metrics beside the index line:\n%s", body)
	}
	if sharesAt < indexAt || qualityAt < sharesAt {
		t.Fatalf("the metric lines did not follow the index line in the index's own order:\n%s", body)
	}
}

// A document that declares one metric prints one line, and a metric the
// document spells no unit for says its kind alone.
func TestPoolShowPrintsOneLineForAOneMetricDocument(t *testing.T) {
	var out strings.Builder
	if err := runPoolWith([]string{"show"}, &out, seedIndex(t), poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "role_rating: gaussian · 1 cell · dims role, model") {
		t.Fatalf("the one-metric document did not print its one line:\n%s", out.String())
	}
}

// The --json answer carries the metrics as an array beside the count it
// already carried, so a script written against today's shape still reads
// and a script that wants the split reads it from the array.
func TestPoolShowJSONCarriesTheMetricsBesideTheCount(t *testing.T) {
	dir := t.TempDir()
	writePoolDoc(t, dir, string(poolDocBothMetrics()))
	var out strings.Builder
	if err := runPoolWith([]string{"show", "--json"}, &out, dir, poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	var answer struct {
		Index *struct {
			Metrics    int `json:"metrics"`
			MetricList []struct {
				Name    string   `json:"name"`
				Kind    string   `json:"kind"`
				Unit    string   `json:"unit"`
				Dims    []string `json:"dims"`
				Cells   int      `json:"cells"`
				Sources []string `json:"sources"`
			} `json:"metric_list"`
		} `json:"index"`
	}
	if err := json.Unmarshal([]byte(out.String()), &answer); err != nil {
		t.Fatalf("--json did not parse: %v\n%s", err, out.String())
	}
	held := answer.Index
	if held == nil {
		t.Fatal("a seeded index was not reported")
	}
	if held.Metrics != 2 {
		t.Fatalf("the count did not stay a count: %d", held.Metrics)
	}
	if len(held.MetricList) != 2 {
		t.Fatalf("the array carried %d metric(s), want both: %+v", len(held.MetricList), held.MetricList)
	}
	shares, quality := held.MetricList[0], held.MetricList[1]
	if shares.Name != "acceptable" || shares.Kind != "bernoulli" || shares.Unit != "share" ||
		strings.Join(shares.Dims, ",") != "role,model,source" || shares.Cells != 3 ||
		strings.Join(shares.Sources, ",") != "grader,reviewer" {
		t.Fatalf("the graded shares read as %+v", shares)
	}
	if quality.Name != "role_quality" || quality.Kind != "gaussian" || quality.Unit != "score" ||
		strings.Join(quality.Dims, ",") != "role,model" || quality.Cells != 1 || len(quality.Sources) != 0 {
		t.Fatalf("the judged scores read as %+v", quality)
	}
}

// poolDocThreeCells is a document with two metrics and three cells, every
// cell carrying installs and one carrying the source dim its metric is split
// by — one cell of the graded shares spells no source at all. The numbers
// name no model; it is a fixture, and the field it is read for is its shape.
func poolDocThreeCells() []byte {
	return []byte(`{
		"version": 7,
		"schema": 1,
		"generated": "2026-09-10",
		"min_installs": 1,
		"judges": ["z-ai/glm-5.3"],
		"metrics": {
			"role_quality": {"kind": "gaussian", "unit": "score", "dims": ["role", "model"]},
			"acceptable": {"kind": "bernoulli", "unit": "share", "dims": ["role", "model", "source"]}
		},
		"cells": [
			{"metric": "role_quality", "role": "worker", "model": "z-ai/glm-5.3", "mean": 71.2, "sd": 9.4, "n": 42, "installs": 5},
			{"metric": "acceptable", "role": "worker", "model": "z-ai/glm-5.3", "mean": 0.75, "sd": 0, "n": 20, "installs": 2},
			{"metric": "acceptable", "role": "worker", "model": "z-ai/glm-5.3", "mean": 0.83, "sd": 0, "n": 12, "installs": 4, "source": "grader"}
		]
	}`)
}

// --cells is the per-cell reading form show has lacked: one line per cell
// under a `cells:` header, each line naming its metric, the role and model
// it is addressed by, the dims it spells, the measurement — a share for a
// graded metric — its rows, and the installs behind it, all in the index's
// own order. --json carries the same cells as an array beside the summary,
// and without the flag neither answer moves.
func TestPoolShowCellsListsEachCellWithItsInstallsAndItsDims(t *testing.T) {
	dir := t.TempDir()
	writePoolDoc(t, dir, string(poolDocThreeCells()))

	var out strings.Builder
	if err := runPoolWith([]string{"show", "--cells"}, &out, dir, poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	for _, want := range []string{
		"cells:\n",
		"acceptable · worker · z-ai/glm-5.3 · share 0.75 · n 20 · installs 2",
		"acceptable · worker · z-ai/glm-5.3 · source grader · share 0.83 · n 12 · installs 4",
		"role_quality · worker · z-ai/glm-5.3 · mean 71.2 · sd 9.4 · n 42 · installs 5",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("show --cells is missing %q:\n%s", want, body)
		}
	}
	// The order is the index's own — metric, then role, model, then dims —
	// and the table follows the metric lines it details, ahead of the own
	// sheet, which is a different document.
	header, shares, quality, own := strings.Index(body, "cells:\n"),
		strings.Index(body, "acceptable · worker"), strings.Index(body, "role_quality · worker"),
		strings.Index(body, "own sheet:")
	if header < 0 || shares < 0 || quality < 0 || own < 0 {
		t.Fatalf("show --cells did not print the table:\n%s", body)
	}
	if shares < header || quality < shares || own < quality {
		t.Fatalf("the cells did not follow their metrics in the index's own order:\n%s", body)
	}

	out.Reset()
	if err := runPoolWith([]string{"show", "--json", "--cells"}, &out, dir, poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	var answer struct {
		Index *struct {
			Metrics int `json:"metrics"`
		} `json:"index"`
		Cells []struct {
			Metric   string            `json:"metric"`
			Role     string            `json:"role"`
			Model    string            `json:"model"`
			Dims     map[string]string `json:"dims"`
			Mean     float64           `json:"mean"`
			SD       float64           `json:"sd"`
			N        int               `json:"n"`
			Installs int               `json:"installs"`
		} `json:"cells"`
	}
	if err := json.Unmarshal([]byte(out.String()), &answer); err != nil {
		t.Fatalf("--json --cells did not parse: %v\n%s", err, out.String())
	}
	if answer.Index == nil || answer.Index.Metrics != 2 {
		t.Fatalf("the summary did not stay beside the cells: %+v", answer.Index)
	}
	if len(answer.Cells) != 3 {
		t.Fatalf("--json --cells carried %d cell(s), want three: %+v", len(answer.Cells), answer.Cells)
	}
	plain, graded, scored := answer.Cells[0], answer.Cells[1], answer.Cells[2]
	if plain.Metric != "acceptable" || plain.Role != "worker" || len(plain.Dims) != 0 ||
		plain.Mean != 0.75 || plain.N != 20 || plain.Installs != 2 {
		t.Fatalf("the sourceless share read as %+v", plain)
	}
	if graded.Metric != "acceptable" || graded.Dims["source"] != "grader" ||
		graded.Mean != 0.83 || graded.N != 12 || graded.Installs != 4 {
		t.Fatalf("the graded share read as %+v", graded)
	}
	if scored.Metric != "role_quality" || scored.Mean != 71.2 || scored.SD != 9.4 ||
		scored.N != 42 || scored.Installs != 5 {
		t.Fatalf("the judged score read as %+v", scored)
	}

	// Without the flag the answer is the answer it has always been: no
	// table in the words, no cells key in the object.
	out.Reset()
	if err := runPoolWith([]string{"show"}, &out, dir, poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	plain0 := out.String()
	if strings.Contains(plain0, "installs 5") || strings.Contains(plain0, "cells:\n") {
		t.Fatalf("a show without --cells grew the table:\n%s", plain0)
	}
	out.Reset()
	if err := runPoolWith([]string{"show", "--json"}, &out, dir, poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	var noCells map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out.String()), &noCells); err != nil {
		t.Fatalf("--json did not parse: %v\n%s", err, out.String())
	}
	if _, has := noCells["cells"]; has {
		t.Fatal("a show --json without --cells carried a cells array")
	}
}

// A metric the document declares but holds no cell of — the floor held its
// cells back, or none were ever measured — says none in the table, the way
// every other nothing here is said.
func TestPoolShowCellsSaysNoneForAMetricWithNoCells(t *testing.T) {
	dir := t.TempDir()
	writePoolDoc(t, dir, `{
		"version": 7,
		"schema": 1,
		"generated": "2026-09-10",
		"min_installs": 1,
		"metrics": {"role_rating": {"kind": "gaussian", "dims": ["role", "model"]}},
		"cells": []
	}`)
	var out strings.Builder
	if err := runPoolWith([]string{"show", "--cells"}, &out, dir, poolClock(t), noEnv); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "role_rating · none") {
		t.Fatalf("an empty metric did not say none:\n%s", out.String())
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

// indexLineIn is the index line of a reading form's answer — the line that
// begins with "index ·", or the sentence a form with no cache prints.
func indexLineIn(t *testing.T, body string) string {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "index ·") || strings.HasPrefix(line, "no index cached yet") {
			return line
		}
	}
	t.Fatalf("no index line in:\n%s", body)
	return ""
}

// status FETCHES the index as part of saying it, so the index line must
// describe the document this run now holds and not the one it held before the
// fetch. On a fresh profile the first status says so — the line is the cached
// document's, tailed with what the fetch replaced — and a second status, cache
// already in hand, says the plain cached line.
func TestPoolStatusSaysWhenThisRunCachedTheIndex(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	server := poolServer(t, priv, signedPoolDoc(7))
	lookup := poolEnv(map[string]string{
		"CODEAF_MODEL_POOL_URL":        server.URL + "/index.json",
		"CODEAF_MODEL_POOL_MIRROR_URL": "",
	})
	key := base64.StdEncoding.EncodeToString(pub)
	dir := t.TempDir()

	var out strings.Builder
	if err := runPoolWith([]string{"status", "--key", key}, &out, dir, poolClock(t), lookup); err != nil {
		t.Fatalf("a reachable relay failed status: %v", err)
	}
	first := indexLineIn(t, out.String())
	if !strings.Contains(first, "index · generated 2026-09-10") {
		t.Fatalf("status did not describe the cached document:\n%s", first)
	}
	if !strings.Contains(first, "· cached now (was built-in seed)") {
		t.Fatalf("the index line did not say this run cached the index:\n%s", first)
	}
	if strings.Contains(out.String(), "no index cached yet") {
		t.Fatalf("status said there was no cache while it cached one:\n%s", out.String())
	}

	out.Reset()
	if err := runPoolWith([]string{"status", "--key", key}, &out, dir, poolClock(t), lookup); err != nil {
		t.Fatalf("a second status failed: %v", err)
	}
	second := indexLineIn(t, out.String())
	if strings.Contains(second, "cached now") {
		t.Fatalf("the second status said the cache was written again:\n%s", second)
	}
	if !strings.Contains(second, "index · generated 2026-09-10") {
		t.Fatalf("the second status did not describe the cached document:\n%s", second)
	}
}

// A cache the probe REPLACES is named the same way, tailed with the day of
// the document it displaced rather than the built-in seed.
func TestPoolStatusSaysWhenThisRunReplacedTheCachedIndex(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	server := poolServer(t, priv, signedPoolDoc(7))
	lookup := poolEnv(map[string]string{
		"CODEAF_MODEL_POOL_URL":        server.URL + "/index.json",
		"CODEAF_MODEL_POOL_MIRROR_URL": "",
	})
	dir := t.TempDir()
	writePoolDoc(t, dir, `{"version": 5, "schema": 1, "generated": "2026-08-01", "min_installs": 1, "judges": [], "metrics": {}, "cells": []}`)

	var out strings.Builder
	if err := runPoolWith([]string{"status", "--key", base64.StdEncoding.EncodeToString(pub)}, &out, dir, poolClock(t), lookup); err != nil {
		t.Fatalf("a reachable relay failed status: %v", err)
	}
	line := indexLineIn(t, out.String())
	if !strings.Contains(line, "index · generated 2026-09-10") {
		t.Fatalf("status did not describe the replacing document:\n%s", line)
	}
	if !strings.Contains(line, "· cached now (was 2026-08-01)") {
		t.Fatalf("the index line did not name the document it replaced:\n%s", line)
	}
}

// The --json answer carries what the text says: on the status that cached the
// document, index.cached_now is true beside a source of "cache" — and only on
// that status, since a later status stores nothing.
func TestPoolStatusJSONSaysWhenThisRunCachedTheIndex(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	server := poolServer(t, priv, signedPoolDoc(7))
	lookup := poolEnv(map[string]string{
		"CODEAF_MODEL_POOL_URL":        server.URL + "/index.json",
		"CODEAF_MODEL_POOL_MIRROR_URL": "",
	})
	key := base64.StdEncoding.EncodeToString(pub)
	dir := t.TempDir()

	var out strings.Builder
	if err := runPoolWith([]string{"status", "--json", "--key", key}, &out, dir, poolClock(t), lookup); err != nil {
		t.Fatalf("a reachable relay failed status --json: %v", err)
	}
	var first struct {
		Index *struct {
			Source    string `json:"source"`
			CachedNow bool   `json:"cached_now"`
		} `json:"index"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &first); err != nil {
		t.Fatalf("status --json did not parse: %v\n%s", err, out.String())
	}
	if first.Index == nil || first.Index.Source != "cache" || !first.Index.CachedNow {
		t.Fatalf("the first status did not say it cached the index: %+v", first.Index)
	}

	out.Reset()
	if err := runPoolWith([]string{"status", "--json", "--key", key}, &out, dir, poolClock(t), lookup); err != nil {
		t.Fatalf("a second status --json failed: %v", err)
	}
	var second struct {
		Index *struct {
			Source    string `json:"source"`
			CachedNow bool   `json:"cached_now"`
		} `json:"index"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &second); err != nil {
		t.Fatalf("the second status --json did not parse: %v\n%s", err, out.String())
	}
	if second.Index == nil || second.Index.Source != "cache" || second.Index.CachedNow {
		t.Fatalf("the second status claimed it cached the index: %+v", second.Index)
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
