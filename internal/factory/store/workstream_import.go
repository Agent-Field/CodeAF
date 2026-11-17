package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// ImportWorkstreamContents completes a retryable import without replacing
// anything already imported. Files and tasks commit together, with stable ids.
func (st *Store) ImportWorkstreamContents(ws string, files []factory.WorkstreamFile, tasks []factory.WorkstreamTask, events []factory.WorkstreamTaskEvent) error {
	if _, err := st.filesDB(true); err != nil {
		return err
	}
	return st.wsTx(func(tx *sql.Tx) error {
		for _, f := range files {
			f.WS = ws
			f.Rev = 1
			doc, err := json.Marshal(f)
			if err != nil {
				return err
			}
			var existing string
			checkErr := tx.QueryRow(`SELECT doc FROM workstream_files WHERE ws=? AND id=?`, ws, f.ID).Scan(&existing)
			if checkErr == nil && existing != string(doc) {
				return fmt.Errorf("file %s already exists with different contents; its source was kept", f.ID)
			}
			if checkErr != nil && !errors.Is(checkErr, sql.ErrNoRows) {
				return checkErr
			}
			if _, err = tx.Exec(`INSERT OR IGNORE INTO workstream_files(ws,id,rev,doc) VALUES (?,?,?,?)`, ws, f.ID, f.Rev, string(doc)); err != nil {
				return err
			}
		}
		tw := TaskWrite{tx: tx}
		for _, t := range tasks {
			var found int
			err := tx.QueryRow(`SELECT rev FROM tasks WHERE ws=? AND id=?`, ws, t.ID).Scan(&found)
			if err == nil {
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			t.WS = ws
			t.Rev = 1
			t.ForYou = nil
			t.Run = nil
			if err = tw.Put(t, 0); err != nil {
				return err
			}
		}
		for _, e := range events {
			doc, err := json.Marshal(e)
			if err != nil {
				return err
			}
			if _, err = tx.Exec(`INSERT OR IGNORE INTO task_events(ws,task,id,doc) VALUES (?,?,?,?)`, ws, e.Task, e.ID, string(doc)); err != nil {
				return err
			}
		}
		return nil
	})
}
