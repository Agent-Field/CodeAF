package session

import (
	"context"
	"path/filepath"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/inventory"
)

// CarriedProcessLog rebases only a job's exact managed output path. Its
// execution folder stays the source fact; moving a log grants no rerun.
func CarriedProcessLog(root, from string, job JobNotice) JobNotice {
	if root == "" || from == "" || job.ID <= 0 || job.LogPath == "" || filepath.Clean(root) == filepath.Clean(from) {
		return job
	}
	tail := filepath.Join("logs", "jobs", strconv.Itoa(job.ID)+".log")
	for _, base := range []string{from, filepath.Join(from, cell.StateDir)} {
		if filepath.Clean(job.LogPath) == filepath.Join(base, tail) {
			job.LogPath = filepath.Join(root, tail)
			break
		}
	}
	return job
}

// RestoreCarriedProcessHistory admits the older beside-the-chat process store
// into the received journal's store. The arrival gate still blocks new jobs;
// paged history and exact-record reads then agree about each preserved run.
func RestoreCarriedProcessHistory(ctx context.Context, root string) error {
	target := filepath.Dir((Place{Dir: root}).Transcript())
	if target == root {
		return nil
	}
	inv, err := inventory.Open(root)
	if err != nil {
		return err
	}
	from := inv.Snapshot().ChatDir

	// Read only the incoming carrier, never an old local store left beside
	// this chat by a previous visit or its preserved branch.
	source, dst := processStore{dir: filepath.Join(root, cell.StateDir, "files")}, processStore{dir: target}
	ask := ProcessPageAsk{Limit: ProcessPageMax}
	sourcePage, err := source.page(ctx, ask, ProcessPageMax, nil, nil, false)
	if err != nil {
		return err
	}
	targetPage, err := dst.page(ctx, ask, ProcessPageMax, nil, nil, false)
	if err != nil {
		return err
	}
	if len(sourcePage.Rows) == 0 && sourcePage.Next == "" && len(targetPage.Rows) == 0 && targetPage.Next == "" {
		return nil
	}
	processRecordMu.Lock()
	defer processRecordMu.Unlock()
	directory, head, err := dst.open(processFoldWriteTime)
	if err != nil {
		return err
	}
	defer directory.Close()
	carry := func(store processStore, page ProcessPage, onlyPaths bool) error {
		ask := ProcessPageAsk{Limit: ProcessPageMax}
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			for _, row := range page.Rows {
				n := CarriedProcessLog(root, from, row.JobNotice)
				if onlyPaths && n.LogPath == row.LogPath {
					continue
				}
				if _, err := dst.putDeferred(directory, &head, n, true, true); err != nil {
					return err
				}
			}
			if err := writeHead(directory, head); err != nil {
				return err
			}
			if page.Next == "" {
				return nil
			}
			ask.Cursor = page.Next
			var err error
			page, err = store.page(ctx, ask, ProcessPageMax, nil, nil, false)
			if err != nil {
				return err
			}
		}
	}
	if err := carry(source, sourcePage, false); err != nil {
		return err
	}
	// Incoming import can grow the head. Begin the local path pass at its
	// new high-water point; it changes only derived log paths, never commands.
	targetPage, err = dst.page(ctx, ask, ProcessPageMax, nil, nil, false)
	if err != nil {
		return err
	}
	return carry(dst, targetPage, true)
}
