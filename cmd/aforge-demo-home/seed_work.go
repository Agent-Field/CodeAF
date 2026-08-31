package main

// seed_work.go is what the three projects have RUN: the append-only index each
// project keeps beside its conversations, and the deliverables index that
// remembers what was made for the person.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// demoTask is one row of a project's record of its own work.
type demoTask struct {
	project string
	// talk is the TITLE of the conversation that ran it; the id is looked up,
	// because a row naming a session that is not on the disk is a row nobody can
	// open.
	talk  string
	entry session.TaskIndexEntry
	// ago is when it landed. A row with no ago is still running, and a running
	// row is only believed when the conversation's presence file names the same
	// node (internal/session's world.go), which is why exactly one row here has
	// none and exactly one conversation has a running task.
	ago time.Duration
}

var demoTasks = []demoTask{
	{
		project: firstProjectName, talk: "The Tab Bar's Counts", ago: 2 * time.Hour,
		entry: session.TaskIndexEntry{
			ID: "1", Title: "Count the tabs the bar draws", Status: string(session.TaskDone),
			Outcome:      "One reading of the world, shared by the bar and the list, so the two counts cannot disagree.",
			Files:        []string{"internal/tui3/home.go", "internal/tui3/homebands.go", "internal/session/world.go"},
			FilesChanged: 3, Cost: 0.42, Model: "anthropic/claude-opus-4.1", Tokens: 61_200,
			DurationMS: 11 * 60 * 1000,
		},
	},
	{
		project: firstProjectName, talk: "Porting the Picker", ago: 40 * time.Minute,
		entry: session.TaskIndexEntry{
			ID: "2", Title: "Port the picker onto the new list", Kind: session.TaskKindSubharness,
			Status:       string(session.TaskDone),
			Outcome:      "The picker walks the list's rows and keeps its own cursor; the keys are unchanged.",
			Files:        []string{"internal/tui3/resume.go", "internal/tui3/list.go"},
			FilesChanged: 2, Cost: 0.31, Model: "anthropic/claude-sonnet-4", Tokens: 38_400,
			DurationMS: 6 * 60 * 1000,
		},
	},
	{
		project: firstProjectName, talk: "Why the Frame Jumps", ago: 8 * 24 * time.Hour,
		entry: session.TaskIndexEntry{
			ID: "3", Title: "Rebuild the frame budget report", Status: string(session.TaskFailed),
			Outcome: "The sweep it re-runs needs a build that is not on this machine, so the report could not be regenerated.",
			Cost:    0.18, Model: "anthropic/claude-sonnet-4", Tokens: 22_100,
			DurationMS: 4 * 60 * 1000,
		},
	},
	{
		project: firstProjectName, talk: "Standing Up the Watches", ago: 5 * time.Hour,
		entry: session.TaskIndexEntry{
			ID: "4", Title: "Write the morning sweep down", Status: string(session.TaskDone),
			Outcome:      "The sweep is a standing order now, with a cap of a dollar a day.",
			Files:        []string{"docs/STANDING-ORDERS.md"},
			FilesChanged: 1, Cost: 0.14, Model: "anthropic/claude-sonnet-4", Tokens: 16_800,
			DurationMS: 3 * 60 * 1000,
		},
	},
	// A FAMILY: one root and the three workers it handed the job out to. It is
	// here so the tasks page has a tree to fold — a fixture with none of them
	// draws the same flat list it always drew, which is indistinguishable from
	// the fold being broken (tasksplace.go's [tasksFamilies]).
	{
		project: firstProjectName, talk: "The Tab Bar's Counts", ago: 90 * time.Minute,
		entry: session.TaskIndexEntry{
			ID: "5", Parent: "1", Title: "Port the lexer", Status: string(session.TaskDone),
			Outcome:      "The lexer reads the new list's rows and keeps its own cursor.",
			Files:        []string{"internal/tui3/lex.go"},
			FilesChanged: 1, Cost: 0.11, Model: "anthropic/claude-sonnet-4", Tokens: 14_200,
			DurationMS: 3 * 60 * 1000,
		},
	},
	{
		project: firstProjectName, talk: "The Tab Bar's Counts", ago: 80 * time.Minute,
		entry: session.TaskIndexEntry{
			ID: "6", Parent: "1", Title: "Port the tests", Status: string(session.TaskDone),
			Outcome:      "Every table test moved over; two of them needed the new fixture.",
			Files:        []string{"internal/tui3/lex_test.go", "internal/tui3/fixtures_test.go"},
			FilesChanged: 2, Cost: 0.19, Model: "anthropic/claude-sonnet-4", Tokens: 21_600,
			DurationMS: 5 * 60 * 1000,
		},
	},
	{
		project: firstProjectName, talk: "The Tab Bar's Counts", ago: 70 * time.Minute,
		entry: session.TaskIndexEntry{
			ID: "7", Parent: "1", Title: "Port the docs", Status: string(session.TaskFailed),
			Outcome: "The page it rewrites is generated, so the edit had nowhere to land.",
			Cost:    0.04, Model: "anthropic/claude-sonnet-4", Tokens: 5_900,
			DurationMS: 1 * 60 * 1000,
		},
	},
	{
		project: "infra", talk: "The Certificate Rotation",
		entry: session.TaskIndexEntry{
			ID: "1", Title: "Rotate the wildcard certificate", Status: string(session.TaskRunning),
			Model: "anthropic/claude-opus-4.1",
		},
	},
	{
		project: "infra", talk: "The Backup Window", ago: 2 * 24 * time.Hour,
		entry: session.TaskIndexEntry{
			ID: "2", Title: "Take the backup window down to twenty minutes", Status: string(session.TaskDone),
			Outcome:      "A changed-only pass instead of a full walk; the window is nineteen minutes and a restore was proved end to end.",
			Files:        []string{"runbooks/backup.md", "terraform/main.tf"},
			FilesChanged: 2, Cost: 0.55, Model: "anthropic/claude-opus-4.1", Tokens: 47_300,
			DurationMS: 22 * 60 * 1000,
		},
	},
	{
		project: "pricing-site", talk: "The Annual Toggle", ago: 5 * time.Hour,
		entry: session.TaskIndexEntry{
			ID: "1", Title: "Put the annual toggle on the pricing page", Status: string(session.TaskDone),
			Outcome:      "Annual is the default and the monthly price stays visible beside it.",
			Files:        []string{"index.html", "pricing.json"},
			FilesChanged: 2, Cost: 0.27, Model: "anthropic/claude-sonnet-4", Tokens: 29_700,
			DurationMS: 7 * 60 * 1000,
		},
	},
}

// writeTaskIndex appends every row to its own project's index and answers how
// many it wrote.
//
// The file is tasks.jsonl in the project's BUCKET — one index per project and
// not per conversation, which is what lets a person ask "did anybody land work
// in these files, and when" without opening a transcript
// (internal/session's task_index.go).
func writeTaskIndex(projects map[string]*demoProject, ids map[string]string, now time.Time) (int, error) {
	written := 0
	for _, task := range demoTasks {
		project, ok := projects[task.project]
		if !ok {
			return written, fmt.Errorf("work %q names no project %q", task.entry.Title, task.project)
		}
		id, ok := ids[task.talk]
		if !ok {
			return written, fmt.Errorf("work %q names no conversation %q", task.entry.Title, task.talk)
		}
		entry := task.entry
		// THE THREE SPELLINGS OF A TITLE ARE ALL WRITTEN, because three readers
		// want three different ones and only one of them falls back. Title is the
		// title as it was groomed, Label is what a row draws, and Name is the slug
		// an "@" mention resolves and the spend page joins an id against
		// (internal/tui3's spendNames) — a row with no Name draws a bare `1` in
		// the `what it was for` column. Label is the title itself here because
		// every title in this fixture is inside the engine's own 56-cell cap;
		// [session.TaskSlug] is the engine's own kebab-caser and is asked for
		// rather than imitated.
		entry.Name = session.TaskSlug(entry.Title)
		if entry.Label == "" {
			entry.Label = entry.Title
		}
		entry.SessionID = id
		entry.TranscriptURI = filepath.Join(project.bucket, id, "transcript.jsonl")
		if task.ago > 0 {
			entry.EndedAt = now.Add(-task.ago)
		}
		if err := appendJSONL(filepath.Join(project.bucket, "tasks.jsonl"), entry); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

// appendJSONL writes one record as a whole line, which is the shape every
// append-only index in this codebase keeps: one write per line, so O_APPEND's
// atomic offset covers the record and two writers cannot interleave halves.
func appendJSONL(path string, record any) error {
	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("make %s: %w", filepath.Dir(path), err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return file.Close()
}

// demoArtifact is one thing the machine made for the person.
type demoArtifact struct {
	project, talk, name, kind string
	body                      string
	ago                       time.Duration
}

var demoArtifacts = []demoArtifact{
	{project: firstProjectName, talk: "The Tab Bar's Counts", name: "tab-bar-counts.md", kind: "document",
		body: "# Where the two counts came from\n\nThe bar and the list each read the world.\n", ago: 2 * time.Hour},
	{project: "pricing-site", talk: "Pricing Research", name: "ladder-vs-per-seat.md", kind: "document",
		body: "# The ladder against per-seat\n\nAbove forty users the ladder wins.\n", ago: 3 * time.Hour},
	{project: "infra", talk: "The Backup Window", name: "backup-window.md", kind: "document",
		body: "# Nineteen minutes\n\nChanged-only, and a restore proved end to end.\n", ago: 2 * 24 * time.Hour},
}

// writeArtifacts records what was made for the person, and puts the files where
// the rows say they are — a row whose file is gone is a row a picker declines to
// offer, which would leave the `made for you` band empty on the demo.
func writeArtifacts(index string, projects map[string]*demoProject, ids map[string]string, now time.Time) int {
	written := 0
	for _, made := range demoArtifacts {
		project, ok := projects[made.project]
		if !ok {
			continue
		}
		path := filepath.Join(project.dir, made.name)
		if os.WriteFile(path, []byte(made.body), 0o600) != nil {
			continue
		}
		session.RecordArtifact(index, session.Artifact{
			Path:    path,
			Session: ids[made.talk],
			Title:   made.name,
			Kind:    made.kind,
			Created: now.Add(-made.ago),
		})
		written++
	}
	return written
}
