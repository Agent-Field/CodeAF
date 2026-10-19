package plandb

import (
	"database/sql"

	"github.com/Agent-Field/codeaf/internal/sqlitefile"
)

// ReadOnly is the committed tasks of an existing store, in the store's order,
// and the id of its root.
type ReadOnly struct {
	RootID string
	Tasks  []*Task
}

// ReadTasks reads the committed tasks of an existing store through a read-only
// connection. Like [ReadRootPreview] it never creates a database, migrates its
// schema, adopts a plan or settles a task a dead process left running, so a
// reader of another conversation's work can look at live stores without being
// able to change them. A store it cannot read is an error, and says nothing
// about the tasks in it.
func ReadTasks(path string) (ReadOnly, error) {
	db, err := sql.Open("sqlite", sqlitefile.URI(path, "mode=ro&_pragma=busy_timeout(2000)"))
	if err != nil {
		return ReadOnly{}, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	tx, err := db.Begin()
	if err != nil {
		return ReadOnly{}, err
	}
	defer tx.Rollback()
	value, err := loadState(tx)
	if err != nil {
		return ReadOnly{}, err
	}
	out := ReadOnly{RootID: value.RootID, Tasks: make([]*Task, 0, len(value.Order))}
	for _, id := range value.Order {
		if task := value.Tasks[id]; task != nil {
			out.Tasks = append(out.Tasks, task)
		}
	}
	return out, nil
}
