package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Agent-Field/codeaf/internal/taskcopy"
)

// ForkTaskHistory retains inherited tasks as result cards, never as a runnable
// frontier. It runs before the independent conversation is opened. The original
// checkpoint remains exact evidence; retry after partial failure is safe, and a
// completed boundary never rewrites new work admitted to the independent chat.
// Returned task IDs select completed output materialization, not active copies.
func ForkTaskHistory(place Place) ([]taskcopy.ResultSelection, error) {
	history := ForkHistoryDir(place)
	document, present, err := forkOriginalTasks(place, history)
	if err != nil || !present {
		return nil, err
	}
	completed := forkCompletedTasks(document)
	marker := filepath.Join(history, "frontier-detached")
	if _, err := os.Stat(marker); err == nil {
		return completed, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	// Resolve portable result-log paths before replacing the operational frontier.
	if codec, sealed := checkpointCodec(place.Tasks()); sealed {
		document = document.mapPaths(codec.resolve)
	}
	for _, node := range document.Nodes {
		if node.Result != nil && node.Result.Whole != "" && isFile(node.Result.Whole) {
			RecordSealedArtifact(place, Artifact{Path: node.Result.Whole, Session: place.ID(), Title: fmt.Sprintf("Task %d · full result", node.ID), Kind: "document", Created: time.Now()})
		}
	}
	document = forkHistoryDocument(document)
	if err := (&taskStore{path: place.Tasks()}).writeLocked(document); err != nil {
		return nil, err
	}
	if err := os.WriteFile(marker, []byte("history-only\n"), 0600); err != nil {
		return nil, err
	}
	return completed, nil
}

func forkOriginalTasks(place Place, history string) (taskDocument, bool, error) {
	original := filepath.Join(history, "tasks.json")
	raw, err := os.ReadFile(original)
	if errors.Is(err, os.ErrNotExist) {
		raw, err = os.ReadFile(place.Tasks())
		if errors.Is(err, os.ErrNotExist) {
			return taskDocument{}, false, nil
		}
		if err != nil {
			return taskDocument{}, false, err
		}
		if err := os.MkdirAll(history, 0700); err != nil {
			return taskDocument{}, false, err
		}
		if err := os.WriteFile(original, raw, 0600); err != nil {
			return taskDocument{}, false, err
		}
	} else if err != nil {
		return taskDocument{}, false, err
	}
	document, err := decodeTasks(raw)
	if err != nil {
		return taskDocument{}, false, fmt.Errorf("read independent conversation history: %w", err)
	}
	return document, true, nil
}

func forkCompletedTasks(document taskDocument) []taskcopy.ResultSelection {
	var completed []taskcopy.ResultSelection
	for _, node := range document.Nodes {
		if node.State.settled() {
			completed = append(completed, taskcopy.ResultSelection{ID: strconv.FormatUint(node.ID, 10), Paths: node.Changed})
		}
	}
	for _, run := range document.Runs {
		if run.State.settled() {
			completed = append(completed, taskcopy.ResultSelection{ID: strconv.FormatUint(run.ID, 10), Paths: run.Changed})
		}
	}
	return completed
}

func forkHistoryDocument(document taskDocument) taskDocument {
	rows := make([]runRecord, 0, len(document.Nodes)+len(document.Runs))
	for _, node := range document.Nodes {
		row := runRecord{ID: node.ID, Parent: node.Parent, Title: node.Title, State: node.State, Report: node.Report, CostUSD: node.CostUSD, ElapsedMS: node.ElapsedMS, StartedAt: node.StartedAt, EndedAt: node.EndedAt, Ending: node.Ending, Changed: node.Changed}
		if node.Result != nil {
			row.Result = node.Result.Text

		}
		rows = append(rows, forkHistoryRow(row))
	}
	for _, row := range document.Runs {
		rows = append(rows, forkHistoryRow(row))
	}
	document.Nodes = nil
	document.Runs = rows
	return document
}

func forkHistoryRow(row runRecord) runRecord {
	if !row.State.settled() {
		row.State, row.Stopped = TaskFailed, true
		row.Report += "\nNot continued in this independent conversation. Start new work here separately."
	}
	// Neither worker context, plan-store IDs, continuation proposals nor landing
	// authority belong to this conversation. Human-readable results remain.
	row.Copy, row.PendingRun, row.CrewState = nil, nil, nil
	row.WorkspaceSet = nil
	row.Run, row.Node, row.PlanTask = "", "", ""
	row.RecoveredCompletion = false
	row.Branch, row.Merge = "", ""
	return row
}

// ForkHistoryDir is the chat-relative historical result area for this fork.
// Namespacing by chat ID keeps a fork of a fork independent and retry-safe.
func ForkHistoryDir(place Place) string {
	return filepath.Join(place.Dir, ".cell", "fork-history", place.ID())
}
