package directory

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
)

const legacyWriteRefusal = "codeaf: directory upgraded; use a current relay build"

// LegacyGate is irrevocable. A failed inventory keeps the source frozen, and
// ownership publication never makes the backup writable again.
type LegacyGate struct {
	Stage    string                       `json:"stage"`
	Config   clusterstate.LegacyConfig    `json:"config"`
	Manifest *clusterstate.LegacyManifest `json:"manifest,omitempty"`
}
type LegacyReceipt struct {
	Node     uint64
	Manifest clusterstate.LegacyManifest
	Applied  uint64
}

func gateIn(tx *sql.Tx) (*LegacyGate, error) {
	var raw string
	e := tx.QueryRow(`SELECT rec FROM legacy_gate WHERE id=1`).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var g LegacyGate
	if json.Unmarshal([]byte(raw), &g) != nil || g.Config.Validate() != nil || (g.Stage != "frozen" && g.Stage != "inventoried" && g.Stage != "raft-owned") {
		return nil, clusterstate.ErrMigration
	}
	return &g, nil
}
func keepGate(tx *sql.Tx, g LegacyGate) error {
	b, e := json.Marshal(g)
	if e != nil {
		return e
	}
	_, e = tx.Exec(`INSERT OR REPLACE INTO legacy_gate VALUES(1,?)`, string(b))
	return e
}

// Database guards also stop relay builds that do not know about the gate.
// They share its transaction so a failed freeze leaves the old writer usable.
func freezeLegacyWrites(tx *sql.Tx) error {
	for _, table := range []string{"identity", "devices", "cells", "version"} {
		for _, operation := range []string{"INSERT", "UPDATE", "DELETE"} {
			if _, err := tx.Exec(`CREATE TRIGGER IF NOT EXISTS legacy_frozen_` + table + `_` + operation + ` BEFORE ` + operation + ` ON ` + table + ` BEGIN SELECT RAISE(ABORT, '` + legacyWriteRefusal + `'); END`); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *SQLite) migrationTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = fn(tx); e != nil {
		return e
	}
	s.migrationBoundary("before-commit")
	if e = tx.Commit(); e != nil {
		return e
	}
	s.migrationBoundary("after-commit")
	return nil
}
func (s *SQLite) LegacyGate(ctx context.Context) (g *LegacyGate, e error) {
	e = s.migrationTx(ctx, func(tx *sql.Tx) error { var err error; g, err = gateIn(tx); return err })
	return
}

// FreezeLegacy waits behind admitted SQL operations, captures exact watch
// evidence, and commits the chosen UUID before any export or member bootstrap.
// Closing old watches forces a fresh authoritative listing after cutover.
func (s *SQLite) FreezeLegacy(ctx context.Context, c clusterstate.LegacyConfig) error {
	s.migrationMu.Lock()
	defer s.migrationMu.Unlock()
	if s.pairing.links != nil {
		drain, ok := s.pairing.links.(interface{ WithLegacyGate(func() error) error })
		if !ok {
			return clusterstate.ErrMigration
		}
		return drain.WithLegacyGate(func() error { return s.freezeLegacy(ctx, c) })
	}
	return s.freezeLegacy(ctx, c)
}
func (s *SQLite) freezeLegacy(ctx context.Context, c clusterstate.LegacyConfig) error {
	if c.Validate() != nil {
		return clusterstate.ErrMigration
	}
	e := s.migrationTx(ctx, func(tx *sql.Tx) error {
		g, e := gateIn(tx)
		if e != nil {
			return e
		}
		if g != nil {
			a, _ := clusterstate.Encode(g.Config)
			b, _ := clusterstate.Encode(c)
			if !bytes.Equal(a, b) {
				return clusterstate.ErrMigration
			}
			return freezeLegacyWrites(tx)
		}
		// The source rows are retained byte-for-byte. Only sampled renewal evidence
		// enters this separate migration table and the future committed genesis.
		rows, e := tx.Query(`SELECT id,rec FROM cells ORDER BY id`)
		if e != nil {
			return e
		}
		for rows.Next() {
			var id, raw string
			if e = rows.Scan(&id, &raw); e != nil {
				break
			}
			var cell Cell
			if json.Unmarshal([]byte(raw), &cell) != nil {
				// The gate still freezes a malformed source. Inventory reports
				// its unsupported row without ever reopening the old writer.
				continue
			}
			until := s.feed.Lifted(id, cell).Lease.Expires
			if until > cell.Lease.Expires {
				if _, e = tx.Exec(`INSERT INTO legacy_holds VALUES(?,?)`, id, until); e != nil {
					break
				}
				s.migrationBoundary("after-freeze-hold")
			}
		}
		re := rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if re != nil {
			return re
		}
		if e := freezeLegacyWrites(tx); e != nil {
			return e
		}
		if e := keepGate(tx, LegacyGate{Stage: "frozen", Config: c}); e != nil {
			return e
		}
		s.migrationBoundary("after-freeze-gate")
		return nil
	})
	if e == nil {
		s.feed.Close()
	}
	return e
}

// InventoryLegacy converts one row at a time under the frozen source version.
// Unknown or oversized rows leave the frozen original intact for diagnosis.
func (s *SQLite) InventoryLegacy(ctx context.Context) (m clusterstate.LegacyManifest, e error) {
	e = s.migrationTx(ctx, func(tx *sql.Tx) error {
		g, e := gateIn(tx)
		if e != nil {
			return e
		}
		if g == nil {
			return clusterstate.ErrMigration
		}
		if g.Manifest != nil {
			m = *g.Manifest
			return nil
		}
		insert := func(w clusterstate.RecordWrite) error {
			_, e := tx.Exec(`INSERT INTO legacy_rows VALUES(?,?,?)`, w.Namespace, w.Key, w.Data)
			s.migrationBoundary("after-inventory-row")
			return e
		}
		authorities, e := clusterstate.LegacyAuthorities(g.Config)
		if e != nil {
			return e
		}
		for _, w := range authorities {
			if e = insert(w); e != nil {
				return e
			}
		}
		for _, table := range []string{"identity", "devices", "cells"} {
			rows, e := tx.Query(`SELECT id,rec FROM ` + table + ` ORDER BY id`)
			if e != nil {
				return e
			}
			for rows.Next() {
				var id, raw string
				if e = rows.Scan(&id, &raw); e != nil {
					break
				}
				var until int64
				if table == "cells" {
					err := tx.QueryRow(`SELECT until_ms FROM legacy_holds WHERE id=?`, id).Scan(&until)
					if err != nil && !errors.Is(err, sql.ErrNoRows) {
						e = err
						break
					}
				}
				w, err := clusterstate.LegacyRow(g.Config, table, id, []byte(raw), until)
				if err != nil {
					e = fmt.Errorf("directory: cannot import %s record %q: unsupported fields or row exceeds %d bytes: %w", table, id, clusterstate.MaxEvaluationBytes, err)
					break
				}
				if e = insert(w); e != nil {
					break
				}
			}
			re := rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			if re != nil {
				return re
			}
		}
		m = clusterstate.LegacyManifest{Config: g.Config}
		m.Version, e = versionIn(tx)
		if e != nil {
			return e
		}
		h := sha256.New()
		if e = exportRows(tx, func(w clusterstate.RecordWrite) error {
			b, e := clusterstate.Encode(w)
			if e != nil {
				return e
			}
			h.Write(b)
			h.Write([]byte{'\n'})
			m.Rows++
			m.Bytes += clusterstate.RecordBytes(w.RecordKey, w.Data)
			return nil
		}); e != nil {
			return e
		}
		m.Digest = hex.EncodeToString(h.Sum(nil))
		m, e = clusterstate.SealLegacyManifest(m)
		if e != nil {
			return e
		}
		if m.Validate() != nil {
			return clusterstate.ErrMigration
		}
		g.Stage = "inventoried"
		g.Manifest = &m
		if e := keepGate(tx, *g); e != nil {
			return e
		}
		s.migrationBoundary("after-inventory-gate")
		return nil
	})
	return
}
func exportRows(tx *sql.Tx, visit func(clusterstate.RecordWrite) error) error {
	rows, e := tx.Query(`SELECT namespace,key,data FROM legacy_rows ORDER BY namespace,key`)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var w clusterstate.RecordWrite
		if e = rows.Scan(&w.Namespace, &w.Key, &w.Data); e != nil {
			return e
		}
		if e = visit(w); e != nil {
			return e
		}
	}
	return rows.Err()
}
func (s *SQLite) ExportLegacy(ctx context.Context, visit func(clusterstate.RecordWrite) error) (m clusterstate.LegacyManifest, e error) {
	e = s.migrationTx(ctx, func(tx *sql.Tx) error {
		g, err := gateIn(tx)
		if err != nil {
			return err
		}
		if g == nil || g.Manifest == nil {
			return clusterstate.ErrMigration
		}
		m = *g.Manifest
		return exportRows(tx, visit)
	})
	return
}

// OwnLegacy publishes only after the host observes the SAME durable import
// through a fresh quorum barrier. Receipts are host-internal, never client input.
func (s *SQLite) OwnLegacy(ctx context.Context, receipts []LegacyReceipt) error {
	return s.migrationTx(ctx, func(tx *sql.Tx) error {
		g, e := gateIn(tx)
		if e != nil {
			return e
		}
		if g == nil || g.Manifest == nil {
			return clusterstate.ErrMigration
		}
		want, _ := clusterstate.Encode(*g.Manifest)
		seen := map[uint64]bool{}
		for _, r := range receipts {
			b, _ := clusterstate.Encode(r.Manifest)
			for _, id := range g.Config.Anchors {
				if r.Node == id && r.Applied >= 1 && bytes.Equal(want, b) {
					seen[id] = true
				}
			}
		}
		if len(seen) < 2 {
			return clusterstate.ErrMigration
		}
		g.Stage = "raft-owned"
		if e := keepGate(tx, *g); e != nil {
			return e
		}
		s.migrationBoundary("after-owned-gate")
		return nil
	})
}

// Tests kill the process at actual durable source transitions. Production has
// no callback and no alternate gate or weaker transaction mode.
func (s *SQLite) migrationBoundary(phase string) {
	if s.migrationCut != nil {
		s.migrationCut(phase)
	}
}

// MigrationRoute resolves only a durably published owner. The callback must
// return the consensus backend for that UUID, never a cached legacy directory.
func (s *SQLite) MigrationRoute(ctx context.Context, backend func(clusterstate.LegacyManifest) (RaftBackend, error)) (Directory, error) {
	g, e := s.LegacyGate(ctx)
	if e != nil {
		return nil, e
	}
	if g == nil || g.Stage != "raft-owned" || g.Manifest == nil {
		return nil, clusterstate.ErrMigration
	}
	b, e := backend(*g.Manifest)
	if e != nil {
		return nil, e
	}
	if b == nil {
		return nil, clusterstate.ErrMigration
	}
	return NewRaft(b), nil
}
