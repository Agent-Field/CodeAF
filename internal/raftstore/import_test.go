package raftstore

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
)

func importFixture(t *testing.T) (clusterstate.LegacyManifest, []clusterstate.RecordWrite) {
	t.Helper()
	root := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, 32)).Public().(ed25519.PublicKey)
	effect := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{2}, 32)).Public().(ed25519.PublicKey)
	c := clusterstate.LegacyConfig{Cluster: "r24-import", Generation: 1, Identity: clusterstate.IdentityID(root), SigningKey: root, EffectsKey: effect, Anchors: []uint64{1, 2, 3}, Now: 12345}
	id, e := clusterstate.LegacyRow(c, "identity", "", []byte(`{"V":1,"vault":"sealed-vault","rotation":{"state":"retired","by":"device","at":100,"retire_at":999999}}`), 0)
	must(t, e)
	cell, e := clusterstate.LegacyRow(c, "cells", "chat", []byte(`{"V":1,"head":"head","class":"host-bound","size":91,"keys":{},"lease":{"device":"device","fence":9007199254740991,"expires":0,"pending":3,"released_at":44},"frames":["frame"]}`), 0)
	must(t, e)
	rows := []clusterstate.RecordWrite{clusterstate.LegacyEffects(c), id, cell}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		return a.Namespace < b.Namespace || a.Namespace == b.Namespace && a.Key < b.Key
	})
	m := clusterstate.LegacyManifest{Config: c, Version: 71, Rows: uint64(len(rows))}
	h := sha256.New()
	for _, r := range rows {
		b, e := clusterstate.Encode(r)
		must(t, e)
		h.Write(b)
		h.Write([]byte{'\n'})
		m.Bytes += clusterstate.RecordBytes(r.RecordKey, r.Data)
	}
	m.Digest = hex.EncodeToString(h.Sum(nil))
	m, e = clusterstate.SealLegacyManifest(m)
	must(t, e)
	return m, rows
}
func fixtureStream(rows []clusterstate.RecordWrite) func(func(clusterstate.RecordWrite) error) error {
	return func(visit func(clusterstate.RecordWrite) error) error {
		for _, r := range rows {
			if e := visit(r); e != nil {
				return e
			}
		}
		return nil
	}
}
func TestR24ImportCrashHelper(t *testing.T) {
	path := os.Getenv("P11_TARGET")
	if path == "" {
		return
	}
	m, rows := importFixture(t)
	s, e := Open(path, Identity{ClusterUUID: m.Config.Cluster, NodeID: 1, Protocol: 1})
	must(t, e)
	n, e := strconv.Atoi(os.Getenv("P11_NUMBER"))
	must(t, e)
	count := 0
	s.cut = func(phase string) {
		if phase == os.Getenv("P11_PHASE") {
			count++
			if count == n {
				os.Exit(82)
			}
		}
	}
	must(t, s.ImportLegacy(context.Background(), m, fixtureStream(rows)))
	t.Fatal("import crash cut not reached")
}

// R24/R3 cuts every atomic genesis row and both sides of its FULL commit,
// followed by the streamed checkpoint's source/chunk/pointer boundaries.
func TestR24ImportCrashesNeverExposePartialGenesis(t *testing.T) {
	m, rows := importFixture(t)
	cuts := map[string]int{"after-begin": 2, "after-import-row": 4, "after-import-genesis": 1, "before-commit": 2, "after-commit": 2, "after-snapshot-source-row": 4, "after-snapshot-created-chunk": 1, "after-snapshot-pointer": 1}
	phases := make([]string, 0, len(cuts))
	for p := range cuts {
		phases = append(phases, p)
	}
	sort.Strings(phases)
	for _, phase := range phases {
		for number := 1; number <= cuts[phase]; number++ {
			t.Run(fmt.Sprintf("%s/%d", phase, number), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "member.db")
				s, e := Open(path, Identity{ClusterUUID: m.Config.Cluster, NodeID: 1, Protocol: 1})
				must(t, e)
				must(t, s.Close())
				cmd := exec.Command(os.Args[0], "-test.run=^TestR24ImportCrashHelper$", "-test.count=1")
				cmd.Env = append(os.Environ(), "P11_TARGET="+path, "P11_PHASE="+phase, "P11_NUMBER="+strconv.Itoa(number))
				out, e := cmd.CombinedOutput()
				var exit *exec.ExitError
				if !errors.As(e, &exit) || exit.ExitCode() != 82 {
					t.Fatalf("cut: %v %s", e, out)
				}
				s, e = Open(path, Identity{ClusterUUID: m.Config.Cluster, NodeID: 1, Protocol: 1})
				must(t, e)
				defer s.Close()
				r, e := s.Recover(context.Background())
				must(t, e)
				if r.Application.RecordCount != 0 && r.Application.RecordCount != 4 {
					t.Fatal("partial import became authoritative")
				}
				must(t, s.ImportLegacy(context.Background(), m, fixtureStream(rows)))
				r, e = s.Recover(context.Background())
				must(t, e)
				if r.Application.RecordCount != 4 || r.Application.VisibleVersion != 71 || r.Core.Snapshot.GetMetadata().GetIndex() != 1 || len(r.Application.ConfState.Voters) != 3 {
					t.Fatal("resume lost genesis")
				}
				changed := m
				changed.Version++
				changed, e = clusterstate.SealLegacyManifest(changed)
				must(t, e)
				if e = s.ImportLegacy(context.Background(), changed, fixtureStream(rows)); !errors.Is(e, ErrBatch) {
					t.Fatal("changed import accepted", e)
				}
			})
		}
	}
}

// R24/R28 refuses missing, reordered and altered streams atomically. A failed
// import cannot become an excuse to truncate source data or bootstrap a voter.
func TestR24ImportCorruptionAndConflictingStreams(t *testing.T) {
	m, rows := importFixture(t)
	for _, kind := range []string{"missing", "reordered", "changed", "identity"} {
		t.Run(kind, func(t *testing.T) {
			s, e := Open(filepath.Join(t.TempDir(), "member.db"), Identity{ClusterUUID: m.Config.Cluster, NodeID: 1, Protocol: 1})
			must(t, e)
			defer s.Close()
			bad := append([]clusterstate.RecordWrite(nil), rows...)
			candidate := m
			switch kind {
			case "missing":
				bad = bad[:2]
			case "reordered":
				bad[0], bad[1] = bad[1], bad[0]
			case "changed":
				bad[1].Data = []byte("changed")
			case "identity":
				candidate.Config.Cluster = "another"
			}
			if e = s.ImportLegacy(context.Background(), candidate, fixtureStream(bad)); e == nil {
				t.Fatal("invalid source imported")
			}
			r, e := s.Recover(context.Background())
			must(t, e)
			if !r.Fresh || r.Application.RecordCount != 0 {
				t.Fatal("failed import changed authority")
			}
		})
	}
}
