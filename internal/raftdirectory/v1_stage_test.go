package raftdirectory_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/raftdirectory"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

// R25/R3/R27 require restart and abandoned staging to preserve the old directory
// atomically, with one bounded staging slot even across different requests.
func TestR25V1StagingCrashAndAbandonment(t *testing.T) {
	raw, err := os.ReadFile("testdata/v1-large-requests.json")
	must(t, err)
	var f v1Fixtures
	must(t, json.Unmarshal(raw, &f))
	one := f.Cases[1]
	proof, err := reqsign.Capture(frozenRequest(t, one, "http://localhost"), []byte(one.Body))
	must(t, err)
	c, err := cs.V1Command(cluster, f.Now, proof)
	must(t, err)
	parts, err := cs.V1Commands(c)
	must(t, err)
	if len(parts) < 3 {
		t.Fatal("large request was not paged")
	}
	clock := func() time.Time { return time.UnixMilli(f.Now) }
	path := t.TempDir()
	binary, err := os.Executable()
	must(t, err)
	child := exec.Command(binary, "-test.run=^TestR25V1StagingCrashHelper$", "-test.timeout=15m")
	child.Env = append(os.Environ(), "CODEAF_V1_STAGE_CUT="+path)
	output, err := child.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 86 {
		t.Fatalf("mid-staging process cut: %v\n%s", err, output)
	}
	source, err := directory.OpenSQLite(filepath.Join(path, "source.db"), clock)
	must(t, err)
	defer source.Close()
	manifest, err := source.InventoryLegacy(ctx)
	must(t, err)
	must(t, os.MkdirAll(filepath.Join(path, "members"), 0700))
	host, err := raftdirectory.OpenNative(ctx, filepath.Join(path, "members"), source, manifest)
	must(t, err)
	defer func() {
		if host != nil {
			must(t, host.Close())
		}
	}()
	inspect := func(wantCell bool) {
		t.Helper()
		must(t, host.Inspect(ctx, func(owned context.Context, s *raftstore.SQLite, app raftstore.Application) error {
			rows, _, err := s.Read(owned, app.Applied, []raftstore.Key{{Namespace: raftstore.State, Key: cs.CellKey(one.Name).Key}})
			if err != nil {
				return err
			}
			b := rows[raftstore.Key{Namespace: raftstore.State, Key: cs.CellKey(one.Name).Key}]
			if (b != nil) != wantCell {
				t.Fatal("staging changed directory visibility")
			}
			if wantCell {
				var cell cs.Cell
				var init cs.CellInit
				must(t, cs.Decode(b, &cell))
				must(t, json.Unmarshal([]byte(one.Body), &init))
				if cell.Title != init.Title || cell.Head != init.Head || cell.Lease.Fence != 1 || !reflect.DeepEqual(cell.Frames, init.Frames) || !reflect.DeepEqual(cell.Keys, init.Keys) {
					t.Fatal("committed data changed")
				}
			}
			n, size := 0, 0
			after := ""
			for {
				staged, _, err := s.ScanPage(owned, app.Applied, raftstore.Outbox, after)
				if err != nil {
					return err
				}
				if len(staged) == 0 {
					break
				}
				for _, row := range staged {
					after = row.Key.Key
					if strings.HasPrefix(row.Key.Key, "v1-stage/") {
						n++
						size += len(row.Value)
					}
				}
			}
			if n > cs.V1StagePages+1 || size > cs.MaxV1BodyBytes+cs.MaxCommandBytes {
				t.Fatalf("unbounded staging: %d rows, %d bytes", n, size)
			}
			if wantCell && n != 0 {
				t.Fatal("committed staging was retained")
			}
			if !wantCell && app.VisibleVersion != manifest.Version {
				t.Fatal("partial staging changed visible version")
			}
			return nil
		}))
	}
	cut := len(parts) / 2
	inspect(false)
	// Repeated abandoned prefixes must reuse the same fixed slot after restart.
	for attempt := 0; attempt < 3; attempt++ {
		for _, part := range parts[:cut] {
			r, err := host.CommitV1(ctx, part)
			must(t, err)
			if r.Code != cs.OK {
				t.Fatal(r.Code)
			}
		}
		inspect(false)
	}
	for _, part := range parts[cut:] {
		r, err := host.CommitV1(ctx, part)
		must(t, err)
		if r.Code != cs.OK {
			t.Fatal(r.Code)
		}
	}
	inspect(true)
}

// R25/R3 terminate the process immediately after durable intermediate pages.
func TestR25V1StagingCrashHelper(t *testing.T) {
	path := os.Getenv("CODEAF_V1_STAGE_CUT")
	if path == "" {
		t.Skip("process-cut helper")
	}
	raw, err := os.ReadFile("testdata/v1-large-requests.json")
	must(t, err)
	var f v1Fixtures
	must(t, json.Unmarshal(raw, &f))
	one := f.Cases[1]
	proof, err := reqsign.Capture(frozenRequest(t, one, "http://localhost"), []byte(one.Body))
	must(t, err)
	c, err := cs.V1Command(cluster, f.Now, proof)
	must(t, err)
	parts, err := cs.V1Commands(c)
	must(t, err)
	clock := func() time.Time { return time.UnixMilli(f.Now) }
	source, err := directory.OpenSQLite(filepath.Join(path, "source.db"), clock)
	must(t, err)
	root := key("root").Public().(ed25519.PublicKey)
	cfg := cs.LegacyConfig{Cluster: cluster, Generation: 1, Identity: cs.IdentityID(root), SigningKey: root, EffectsKey: key("effects").Public().(ed25519.PublicKey), Anchors: []uint64{1, 2, 3}, Now: f.Now}
	must(t, source.FreezeLegacy(ctx, cfg))
	manifest, err := source.InventoryLegacy(ctx)
	must(t, err)
	must(t, os.MkdirAll(filepath.Join(path, "members"), 0700))
	host, err := raftdirectory.OpenNative(ctx, filepath.Join(path, "members"), source, manifest)
	must(t, err)
	for _, part := range parts[:len(parts)/2] {
		r, err := host.CommitV1(ctx, part)
		must(t, err)
		if r.Code != cs.OK {
			t.Fatal(r.Code)
		}
	}
	os.Exit(86)
}
