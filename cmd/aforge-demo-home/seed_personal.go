package main

// seed_personal.go is the PERSONAL FIXTURE: the few records the folders place
// needs in order to be looked at — one project on the disk, three conversations,
// one finished piece of work — written into a state root the caller names, and
// NOTHING about folders, rules or ongoing work.
//
// WHY SO LITTLE OF IT IS HERE. Folders, placements, rules and watches all have a
// supported door on the shipped binary (`aforge collections …`, `aforge standing
// add …`), so the script that builds this fixture (scripts/demo-personal.sh)
// makes them through those doors and this program does not spell them a second
// time. What is left is what has no door because it is the record of something
// that already happened: a conversation's journal and the project's task index.
// Those are written the way the whole demo home writes them ([writeConversation],
// [writeTaskIndex]'s shape), and every word in them says it is a fixture.
//
// WHY THE ROOT IS AFORGE_HOME AND NOT HOME. The demo home proper is handed to the
// binary as HOME; this one is handed to it as AFORGE_HOME, so the person's own
// HOME — their shell, their ssh keys, their provider configuration — stays what
// it is, and only aforge's state moves. The project therefore lives INSIDE the
// state root, under `fixture/`, which no reader in aforge walks.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// personalProjectName is the fixture's one workspace, under the state root's
// `fixture/` directory.
const personalProjectName = "startup"

// personalManifest is what the seeding answers on stdout, so the script can file
// the records it made by their real ids rather than by guessing them.
type personalManifest struct {
	State    string `json:"state"`
	Project  string `json:"project"`
	Shared   string `json:"shared"`
	Roadmap  string `json:"roadmap"`
	Unfiled  string `json:"unfiled"`
	TaskID   string `json:"taskId"`
	TaskChat string `json:"taskSession"`
	Spec     string `json:"spec"`
	Report   string `json:"report"`
	// Reports is stage 2's conversation for setting up Product's reports.
	Reports string `json:"reports,omitempty"`
}

var personalFiles = map[string]string{
	"README.md":                 "# Startup (aforge demo fixture)\n\nEverything in this directory was written by aforge-demo-home --personal. None of it is real.\n",
	"product/spec.md":           "# Product spec (fixture)\n\n- Offline support: supported on desktop.\n- Export: CSV and PDF.\n- Sharing: read-only links, no editing by guests.\n",
	"product/onboarding-faq.md": "# Onboarding FAQ (fixture draft)\n\n**Does it work offline?** On desktop, yes.\n\n**Can I export?** CSV and PDF.\n",
	"marketing/launch-copy.md":  "# Launch copy (fixture)\n\nWorks offline, anywhere. Export to CSV, PDF and Excel in one click.\n",
	"inbox/2026-09-12.md":       "# Inbox note (fixture)\n\nDecision: pricing page ships with the annual toggle on.\nRequest: marketing wants the export list confirmed.\n",
	"reports/product-digest.md": "# Product digest (fixture)\n\nThis report was written by the fixture, not by a run.\n\n- Spec: offline on desktop; export CSV and PDF.\n- Open: launch copy still claims Excel export.\n",
}

var personalConversations = []demoTalk{
	{
		project: personalProjectName, title: "Pricing and positioning", ago: 3 * time.Hour,
		model: "fixture/no-model",
		turns: []demoTurn{
			{"(fixture) how should we position the annual plan against monthly?", "(fixture) Lead with two months free; keep the monthly price visible beside it."},
			{"(fixture) and what does marketing need from product for launch?", "(fixture) A confirmed export list — the launch copy still says Excel, and the spec says CSV and PDF."},
		},
	},
	{
		project: personalProjectName, title: "Roadmap notes", ago: 26 * time.Hour,
		model: "fixture/no-model",
		turns: []demoTurn{
			{"(fixture) draft an onboarding FAQ from the spec", "(fixture) Drafted product/onboarding-faq.md from the spec's offline and export lines."},
		},
	},
	{
		project: personalProjectName, title: "Quick question about tar flags", ago: 50 * time.Minute,
		model: "fixture/no-model",
		turns: []demoTurn{
			{"(fixture) what does tar -xzf do?", "(fixture) Extract (x) a gzip-compressed (z) archive from the file (f) named next."},
		},
	},
}

// personalReportsTalk is stage 2's fourth conversation: the one a person uses to
// set Product's recurring reports up. Its only history is one fixture exchange
// saying what it is for, and it carries no model of its own, so a real turn in
// it runs on the profile's model.
var personalReportsTalk = demoTalk{
	project: personalProjectName, title: "Product reports", ago: 20 * time.Minute,
	turns: []demoTurn{
		{"(fixture) this is the conversation for Product's recurring reports", "(fixture) Noted. Ask here when you want a report kept current."},
	},
}

// personalStageTwoSpec is the spec as checkpoint 2's fixture writes it: the
// same lines plus one contact detail, so the Product folder's rule (reports
// never quote customer contact details) has something real to hold a report to.
const personalStageTwoSpec = "# Product spec (fixture)\n\n- Offline support: supported on desktop.\n- Export: CSV and PDF.\n" +
	"- Sharing: read-only links, no editing by guests.\n- Escalations (fixture customer): dana.lee@example.com, +1 555 0100.\n"

// personalFilesFor is the fixture's files at a stage. STAGE 2 WRITES NO REPORT
// FILE: its report is set up through a chat and first published by aforge, and a
// file already at that path would be one aforge never put there.
func personalFilesFor(stage int) map[string]string {
	files := make(map[string]string, len(personalFiles))
	for name, body := range personalFiles {
		files[name] = body
	}
	if stage >= 2 {
		delete(files, "reports/product-digest.md")
		files["product/spec.md"] = personalStageTwoSpec
	}
	return files
}

// seedPersonal writes the personal fixture into the state root and answers the
// ids it minted. state must be empty or missing; it is AFORGE_HOME.
func seedPersonal(state string, now time.Time) (personalManifest, error) {
	return seedPersonalStage(state, now, 1)
}

// seedPersonalStage is [seedPersonal] at a checkpoint's stage of the fixture.
func seedPersonalStage(state string, now time.Time, stage int) (personalManifest, error) {
	manifest := personalManifest{State: state}
	project := &demoProject{name: personalProjectName, files: personalFilesFor(stage)}
	project.dir = filepath.Join(state, "fixture", personalProjectName)
	project.bucket = filepath.Join(state, "v3", "projects", encodeWorkspace(project.dir))
	if err := os.MkdirAll(project.bucket, 0o700); err != nil {
		return manifest, fmt.Errorf("make %s: %w", project.bucket, err)
	}
	if err := writeTree(project.dir, project.files); err != nil {
		return manifest, err
	}
	manifest.Project = project.dir
	manifest.Spec = filepath.Join(project.dir, "product", "spec.md")
	manifest.Report = filepath.Join(project.dir, "reports", "product-digest.md")

	brain, err := store.Open(filepath.Join(state, "graph.db"))
	if err != nil {
		return manifest, fmt.Errorf("open the search index: %w", err)
	}
	defer brain.Close()
	ids := make([]string, 0, len(personalConversations))
	for _, talk := range personalConversations {
		id, err := writeConversation(project, talk, now, brain)
		if err != nil {
			return manifest, err
		}
		ids = append(ids, id)
	}
	manifest.Shared, manifest.Roadmap, manifest.Unfiled = ids[0], ids[1], ids[2]
	if stage >= 2 {
		if manifest.Reports, err = writeConversation(project, personalReportsTalk, now, brain); err != nil {
			return manifest, err
		}
	}

	// THE ONE FINITE PIECE OF WORK, in the roadmap conversation's project index,
	// in the shape [writeTaskIndex] writes.
	title := "Draft the onboarding FAQ"
	entry := session.TaskIndexEntry{
		ID: "1", Title: title, Status: string(session.TaskDone),
		Outcome:      "Fixture record: a first draft was written to product/onboarding-faq.md. No model ran.",
		Files:        []string{"product/onboarding-faq.md"},
		FilesChanged: 1, Model: "fixture/no-model",
		Name: session.TaskSlug(title), Label: demoTaskLabel(title),
		SessionID:     manifest.Roadmap,
		TranscriptURI: filepath.Join(project.bucket, manifest.Roadmap, "transcript.jsonl"),
		EndedAt:       now.Add(-25 * time.Hour),
	}
	if err := appendJSONL(filepath.Join(project.bucket, "tasks.jsonl"), entry); err != nil {
		return manifest, err
	}
	manifest.TaskID, manifest.TaskChat = entry.ID, entry.SessionID
	return manifest, nil
}

// runPersonal is the --personal door: seed, then print the manifest as one JSON
// line on stdout and nothing else there.
func runPersonal(state string, now time.Time) error { return runPersonalStage(state, now, 1) }

// runPersonalStage is the --personal door at a stage (--stage).
func runPersonalStage(state string, now time.Time, stage int) error {
	if state == "" {
		return fmt.Errorf("--personal needs --into, the directory aforge will be given as AFORGE_HOME")
	}
	dir, fresh, err := demoDir(state, false)
	if err != nil {
		return err
	}
	if !fresh {
		return fmt.Errorf("%s already holds something; the personal fixture only writes into an empty directory", dir)
	}
	manifest, err := seedPersonalStage(dir, now, stage)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(manifest)
}
