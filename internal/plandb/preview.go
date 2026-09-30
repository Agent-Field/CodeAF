package plandb

import (
	"database/sql"
	"net/url"
	"time"
)

// RootPreview is the original request behind a saved run. Listing a conversation
// needs these words, not a replay or a writable handle to the task graph.
type RootPreview struct {
	Title       string
	Description string
	CreatedAt   time.Time
}

// ReadRootPreview reads only the committed root of an existing store. It never
// creates a database, migrates its schema or settles a task left running by a
// process that exited, so a conversation picker can safely read live work too.
func ReadRootPreview(path string) (RootPreview, error) {
	uri := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return RootPreview{}, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var root RootPreview
	var created string
	err = db.QueryRow(`SELECT tasks.title, tasks.description, tasks.created_at
  FROM tasks JOIN meta ON tasks.id = meta.root_id WHERE meta.id = 1`).Scan(&root.Title, &root.Description, &created)
	if err != nil {
		return RootPreview{}, err
	}
	root.CreatedAt, err = parseTime(created)
	return root, err
}
