package shape

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"modernc.org/sqlite"
)

func openDatabase(name, mode string) (*sql.DB, error) {
	u := url.URL{Scheme: "file", Path: name}
	q := u.Query()
	q.Set("mode", mode)
	q.Add("_pragma", "busy_timeout(5000)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err == nil {
		db.SetMaxOpenConns(1)
	}
	return db, err
}

func (d *draft) overlayScope() svc.Scope {
	return svc.Scope{App: d.App, Caller: svc.Caller{Member: d.Member}, Data: svc.DataDraft}
}

func (d *draft) refreshOverlay(ctx context.Context) (bool, error) {
	lock := records.DraftOverlayLock(d.s.cfg.Root, d.overlayScope())
	lock.Lock()
	defer lock.Unlock()
	db, err := openDatabase(filepath.Join(d.dir, "overlay.db"), "rw")
	if err != nil {
		return false, err
	}
	var dirty bool
	err = db.QueryRowContext(ctx, "SELECT dirty FROM _shape_scratch WHERE id=1").Scan(&dirty)
	err = errors.Join(err, db.Close())
	if err != nil || dirty {
		return false, err
	}
	return true, d.copyOverlay(ctx)
}

func (d *draft) copyOverlay(ctx context.Context) error {
	source := filepath.Join(d.s.cfg.Root, d.App, "records.db")
	if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
		// Real databases are lazy. Let Records create its empty schema before
		// taking a snapshot, rather than inventing a second schema here.
		def, decodeErr := spec.Decode(d.BaseFiles)
		if decodeErr != nil {
			return decodeErr
		}
		if d.s.all.Records == nil {
			return bridge.Errorf(bridge.CodeNotFound, "no records database")
		}
		for typ := range def.App.Records {
			scope := d.overlayScope()
			scope.Data = svc.DataReal
			if _, err := d.s.all.Records.Query(ctx, scope, bridge.QueryParams{Type: typ, Limit: 1}); err != nil {
				return err
			}
			break
		}
	}
	db, err := openDatabase(source, "ro")
	if err != nil {
		return err
	}
	defer db.Close()
	tmp, err := os.CreateTemp(d.dir, ".overlay-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if err := tmp.Close(); err != nil {
		return err
	}
	defer os.Remove(name)
	// The online backup API includes committed WAL pages and generated columns.
	if err := backupDatabase(ctx, db, name); err != nil {
		return err
	}
	if err := installScratchFlag(ctx, name); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(d.dir, "overlay.db"))
}

func backupDatabase(ctx context.Context, db *sql.DB, destination string) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.Raw(func(raw any) error {
		provider, ok := raw.(interface {
			NewBackup(string) (*sqlite.Backup, error)
		})
		if !ok {
			return fmt.Errorf("SQLite driver does not support online backup")
		}
		backup, err := provider.NewBackup(destination)
		if err != nil {
			return err
		}
		for {
			if err := ctx.Err(); err != nil {
				return errors.Join(err, backup.Finish())
			}
			more, err := backup.Step(128)
			if err != nil || !more {
				return errors.Join(err, backup.Finish())
			}
		}
	})
}

func installScratchFlag(ctx context.Context, name string) error {
	db, err := openDatabase(name, "rw")
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "CREATE TABLE _shape_scratch (id INTEGER PRIMARY KEY CHECK(id=1), dirty INTEGER NOT NULL); INSERT INTO _shape_scratch VALUES (1,0)"); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name GLOB 'records_*'")
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, table)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, table := range tables {
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			// The flag commits with the row write, including deletions whose
			// history is removed. A failed transaction leaves the copy clean.
			trigger := "_shape_" + table + "_" + op
			statement := "CREATE TRIGGER " + quoteSQL(trigger) + " AFTER " + op + " ON " + quoteSQL(table) + " BEGIN UPDATE _shape_scratch SET dirty=1 WHERE id=1; END"
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func quoteSQL(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }
