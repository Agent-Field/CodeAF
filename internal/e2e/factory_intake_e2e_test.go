//go:build e2e

// A LABELED ISSUE STARTS BY ITSELF; AN ISSUE THAT TOUCHES auth/ ASKS FIRST.
//
// WHAT THIS LANE PROVES. The decision half of automatic intake, against the
// live demo repository's committed `.codeaf/factory.yaml` and two of its real
// issues: a small bug with the start label is inside the rules and starts with
// nobody pressing go, and an issue whose fix must edit auth/token.go meets the
// repo's `never: [auth/**]` and parks as "asked first". Both verdicts come from
// the same code the factory service calls ([intake.Job] then [pipeline.File.Decide]).
//
// WHY THE DECISION AND NOT A FULL REPLAY. A replay needs a server, a chat
// window as worker, a plan from a real model and minutes of waiting; the
// scenario was run that way once by hand (events auto_started and asked_first,
// every call on deepseek/deepseek-v4.1-flash). What can regress cheaply is the
// verdict, so this lane pins the verdict and costs nothing: no model call is
// made. It still skips without a provider key, like every live lane, and
// without `gh` signed in.
//
//	go test -tags e2e -count=1 -run TestLabeledIssueStartsAndAuthIssueAsksFirst ./internal/e2e/
package e2e

import (
	"encoding/base64"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

const intakeDemoRepo = "santoshkumarradha/factory-demo"

// ghJSON runs gh and decodes its JSON answer, skipping the lane when gh is
// absent or cannot reach the repo (not signed in, repo not shared).
func ghJSON(t *testing.T, into any, args ...string) {
	t.Helper()
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skip("gh is not installed")
	}
	out, err := exec.Command("gh", args...).Output()
	if err != nil {
		t.Skipf("gh cannot read %s: %v", intakeDemoRepo, err)
	}
	if err := json.Unmarshal(out, into); err != nil {
		t.Fatalf("gh %v: %v", args, err)
	}
}

// demoIssue is a live issue as the floor would hold it right after the label
// was added and the cheap read stamped a size.
func demoIssue(t *testing.T, num int, size string) factory.Item {
	t.Helper()
	var got struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	ghJSON(t, &got, "issue", "view", strconv.Itoa(num), "-R", intakeDemoRepo, "--json", "title,body")
	return factory.Item{
		ID: num, Kind: factory.KindIssue, Title: got.Title, Body: got.Body,
		Labels: []string{"agent-ready"},
		Triage: factory.Triage{Size: size, TriagedAt: time.Now()},
	}
}

func TestLabeledIssueStartsAndAuthIssueAsksFirst(t *testing.T) {
	liveKey(t)
	var file struct {
		Content string `json:"content"`
	}
	ghJSON(t, &file, "api", "repos/"+intakeDemoRepo+"/contents/.codeaf/factory.yaml")
	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(file.Content, "\n", ""))
	if err != nil {
		t.Fatal(err)
	}
	rules, err := pipeline.Parse(string(raw))
	if err != nil {
		t.Fatalf("the demo repo's factory.yaml does not parse: %v", err)
	}

	plain := rules.Decide(intake.Job(demoIssue(t, 3, "M")))
	if !plain.Start || plain.Ask {
		t.Fatalf("the labeled in-size issue #3 must start by itself, got %+v", plain)
	}

	// The issue is in size too; only the path rule may be what parks it.
	auth := rules.Decide(intake.Job(demoIssue(t, 10, "S")))
	if auth.Start || !auth.Ask {
		t.Fatalf("the issue that edits auth/token.go must ask first, got %+v", auth)
	}
	if !strings.Contains(strings.Join(auth.Reasons, ";"), "auth/") {
		t.Fatalf("the question must name the auth/ path, got %v", auth.Reasons)
	}

	// The same issue without the label is not a candidate at all: it waits.
	unlabeled := demoIssue(t, 3, "M")
	unlabeled.Labels = []string{"bug"}
	if d := rules.Decide(intake.Job(unlabeled)); d.Start || d.Ask {
		t.Fatalf("an unlabeled issue must neither start nor ask, got %+v", d)
	}
}
