package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/trace"
)

// A KEY THE SCRUB'S SHAPES HAVE NEVER SEEN IS STILL NOT IN THE RECORD. The
// three patterns catch `Bearer …`, `sk-…` and a field named `authorization`;
// they catch nothing at all in a Google `AIza…`, a Groq `gsk_…` or the plain
// token a self-hosted endpoint was handed, and a provider that echoes its own
// error body back is how one of those reaches a folder somebody is about to
// attach to a bug report. The door registers the exact values, so the shape
// does not matter.
func TestTheDoorRegistersConfiguredCredentialsWithTheRecord(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("AFORGE_PROFILE_DIR", profile)
	t.Setenv("AFORGE_HOME", t.TempDir())
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")

	// Neither value looks like a credential to any regex in scrub.go: no
	// scheme, no `sk-` prefix, no field name beside it.
	const key = "AIzaSyD-plain-value-nothing-matches-1"
	const appSecret = "GOCSPX-another-plain-value-22"
	encoded, err := json.Marshal(map[string]string{
		config.KeyAPIKey:            key,
		config.KeyGoogleOAuthSecret: appSecret,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.BudgetConfigPath(profile), encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	openDebugRecord("chat", "deepseek/deepseek-v4-flash", t.TempDir())

	// The body a provider might echo back, with both values in it.
	body := []byte(`{"error":{"message":"invalid key ` + key + ` for app ` + appSecret + `"}}`)
	got := string(trace.Scrub(body))
	for _, secret := range []string{key, appSecret} {
		if strings.Contains(got, secret) {
			t.Fatalf("a configured credential survived the scrub: %s", got)
		}
	}
	if strings.Count(got, "[redacted]") != 2 {
		t.Fatalf("the scrub did not redact both credentials: %s", got)
	}
}

// TestTheDebugFlagNamesTheFolderTheRecordIsActuallyWrittenTo is a promise about
// where evidence lives, which is the one sentence a person reads while they are
// trying to file a bug.
//
// It said `in a folder of its own under the state root`, which names no folder
// — and the state root has two. The developer who turned the switch on went to
// `~/.aforge/runs/aforge-do-<n>/`, found a `graph.db` and nothing else, and
// concluded no record had been written. One had: it goes beside the model-call
// log, under `logs/trace/<run>/`. The sentence was the half that was wrong.
//
// So this OPENS A REAL RECORD and asserts the help names the folder that
// appeared, rather than asserting one string against another.
func TestTheDebugFlagNamesTheFolderTheRecordIsActuallyWrittenTo(t *testing.T) {
	house := t.TempDir()
	t.Setenv("AFORGE_HOME", house)
	t.Setenv("AFORGE_PROFILE_DIR", t.TempDir())

	// The door's own three steps, in the door's own order (openDebugRecord).
	// The switch is turned on for THIS RUN and not for the process:
	// [trace.Enable] is what `--debug` calls and it is deliberately never
	// turned off again, so a test that flipped it would leave every test after
	// it in this package running as though somebody had asked for a record.
	ctx := trace.Begin(context.Background())
	trace.EnableRun(ctx)
	trace.OpenRun(ctx, trace.RunHeader{Command: "do", Started: time.Now()})
	written := trace.Dir(trace.RunFrom(ctx))
	if _, err := os.Stat(written); err != nil {
		t.Fatalf("--debug wrote no folder at all, so the flag is promising a record nothing keeps: %v", err)
	}

	// `--debug` is one sentence on three doors, and every one of them has to
	// point at the folder that just appeared.
	help := debugFlagHelp()
	folder := written
	if house != "" {
		// The help names the ROOT the run folders sit in; the run's own id is
		// minted per invocation and cannot be in a static string.
		folder = filepath.Dir(written)
	}
	if !strings.Contains(help, folder) {
		t.Errorf("--debug says the record is kept\n\t%s\nand it was written to\n\t%s\n"+
			"a person hunting for evidence reads this sentence and goes to the wrong folder", help, written)
	}
	if strings.Contains(help, "under the state root") {
		t.Errorf("--debug still points at `the state root`, which holds two folders and names neither: %s", help)
	}
}
