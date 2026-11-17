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
			t.WS, t.Rev = ws, 1
			t.ForYou, t.Run = nil, nil
			doc, err := json.Marshal(t)
			if err != nil {
				return err
			}
			var existing string
			err = tx.QueryRow(`SELECT doc FROM tasks WHERE ws=? AND id=?`, ws, t.ID).Scan(&existing)
			if err == nil {
				if existing != string(doc) {
					return fmt.Errorf("task %s already exists with different work; its source was kept", t.ID)
				}
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if err = tw.Put(t, 0); err != nil {
				return err
			}
		}
		for _, e := range events {
			doc, err := json.Marshal(e)
			if err != nil {
				return err
			}
			var previousWS, previousDoc string
			checkErr := tx.QueryRow(`SELECT ws,doc FROM task_events WHERE id=?`, e.ID).Scan(&previousWS, &previousDoc)
			if checkErr == nil && (previousWS != ws || previousDoc != string(doc)) {
				return fmt.Errorf("this task thread already belongs to another import; its source was kept")
			}
			if checkErr != nil && !errors.Is(checkErr, sql.ErrNoRows) {
				return checkErr
			}
			if _, err = tx.Exec(`INSERT OR IGNORE INTO task_events(ws,task,id,doc) VALUES (?,?,?,?)`, ws, e.Task, e.ID, string(doc)); err != nil {
				return err
			}
		}
		return nil
	})
}
