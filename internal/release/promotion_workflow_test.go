package release

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func workflowDocument(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}
func obj(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("want mapping, got %T: %v", value, value)
	}
	return result
}
func list(t *testing.T, value any) []any {
	t.Helper()
	result, ok := value.([]any)
	if !ok {
		t.Fatalf("want list, got %T: %v", value, value)
	}
	return result
}
func stringField(t *testing.T, value any) string {
	t.Helper()
	result, ok := value.(string)
	if !ok {
		t.Fatalf("want string, got %T: %v", value, value)
	}
	return result
}

// C16: The schedule and the dispatch are the only triggers, and the parsed cron follows the parsed cutoff in both Toronto offsets.
func TestC16PromotionTriggerAndCutoff(t *testing.T) {
	doc := workflowDocument(t, "promote-staging.yml")
	on := obj(t, doc["on"])
	if len(on) != 2 {
		t.Fatalf("triggers = %v", on)
	}
	schedule := list(t, on["schedule"])
	if len(schedule) != 1 {
		t.Fatalf("schedule = %v", schedule)
	}
	cron := strings.Fields(stringField(t, obj(t, schedule[0])["cron"]))
	if len(cron) != 5 {
		t.Fatalf("cron = %v", cron)
	}
	minute, e1 := strconv.Atoi(cron[0])
	hour, e2 := strconv.Atoi(cron[1])
	if e1 != nil || e2 != nil || cron[2] != "*" || cron[3] != "*" {
		t.Fatalf("cron = %v", cron)
	}
	dispatch := obj(t, on["workflow_dispatch"])
	inputs := obj(t, dispatch["inputs"])
	if len(inputs) != 3 {
		t.Fatalf("inputs = %v", inputs)
	}
	for _, name := range []string{"target", "dry_run", "signal"} {
		if _, ok := inputs[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
	env := obj(t, doc["env"])
	zone := stringField(t, env["CUTOFF_ZONE"])
	weekday := stringField(t, env["CUTOFF_WEEKDAY"])
	clock := stringField(t, env["CUTOFF_CLOCK"])
	loc, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	cutParts := strings.Split(clock, ":")
	cutHour, _ := strconv.Atoi(cutParts[0])
	cutMinute, _ := strconv.Atoi(cutParts[1])
	for _, day := range []time.Time{time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)} {
		run := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, time.UTC)
		local := run.In(loc)
		cut := time.Date(local.Year(), local.Month(), local.Day(), cutHour, cutMinute, 0, 0, loc)
		if local.Weekday().String() != weekday || cron[4] != strconv.Itoa(int(local.Weekday())) || !run.After(cut) {
			t.Errorf("cron %v run %s before cutoff %s", cron, run, cut)
		}
	}
	concurrency := obj(t, doc["concurrency"])
	if stringField(t, concurrency["group"]) != "promote-staging-${{ github.event_name }}" || concurrency["cancel-in-progress"] != false {
		t.Fatalf("concurrency = %v", concurrency)
	}
}

// F8: The workflow explains why a late cron is safe and why each job exists.
func TestPromotionWorkflowExplainsItsSafetyLaws(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "promote-staging.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"# THE CUTOFF IS A COMMIT TIME, NOT THE RUN TIME.", "12:59Z and 15:46Z", "PROMOTION_TOKEN", "SLACK_RELEASE_WEBHOOK", "forward-only", "persist-credentials"} {
		if !strings.Contains(text, want) {
			t.Errorf("workflow lacks explanation %q", want)
		}
	}
	for _, name := range []string{"plan", "plan-failed", "signal", "full_check", "finish"} {
		pattern := regexp.MustCompile(`(?m)(?:^  #[^\n]*\n)+  ` + regexp.QuoteMeta(name) + `:`)
		if !pattern.MatchString(text) {
			t.Errorf("%s has no job comment", name)
		}
	}
}

// R5, R6, and R8: Setup and manual copy must describe the token and weekly outcome truthfully.
func TestPromotionDocsDescribeLiveSetupAndConditionalCadence(t *testing.T) {
	root := repositoryRoot(t)
	read := func(path string) string {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	runbook := read("docs/rules/promotion.md")
	for _, want := range []string{"fine-grained\npersonal access token", "Contents: read and", "Workflows: read and write", ".current_user_can_bypass", "always", "expire after one hour", "token-minting step"} {
		if !strings.Contains(runbook, want) {
			t.Errorf("runbook lacks %q", want)
		}
	}
	if strings.Contains(runbook, "installation token with those permissions and bypass\nrights also works") {
		t.Error("runbook presents an expiring App token as a static secret")
	}
	rules := read(".github/rulesets/README.md")
	for _, want := range []string{"repository is public", "live `protection` ruleset", "`main`, `dev`,", "`staging`", "`PROMOTION_TOKEN`", "bypass list"} {
		if !strings.Contains(rules, want) {
			t.Errorf("ruleset README lacks %q", want)
		}
	}
	manual := read("internal/manual/chat/running-from-the-terminal.md")
	section := strings.SplitN(strings.SplitN(manual, "## What is stageaf", 2)[1], "\n## ", 2)[0]
	if len(section) >= 2000 || strings.Contains(section, "Staging moves once a week") || !strings.Contains(section, "The promotion runs once a week") || !strings.Contains(section, "When the check\nfails or there is nothing new, staging stays where it was") {
		t.Fatalf("stageaf manual overpromises or exceeds section limit: %s", section)
	}
}

// C17: The reusable Full check receives the candidate and all permissions its own jobs request.
func TestC17PromotionCallsFullCheckWithPermissions(t *testing.T) {
	jobs := obj(t, workflowDocument(t, "promote-staging.yml")["jobs"])
	full := obj(t, jobs["full_check"])
	if full["uses"] != "./.github/workflows/ci-full.yml" || obj(t, full["with"])["ref"] != "${{ needs.plan.outputs.candidate }}" {
		t.Fatalf("full check = %v", full)
	}
	if obj(t, full["permissions"])["issues"] != "write" {
		t.Fatalf("permissions = %v", full["permissions"])
	}
}

// C18 and R1: A skipped reusable check and a failed plan each keep their own report job runnable.
func TestC18SkippedAncestorsCannotSkipReports(t *testing.T) {
	jobs := obj(t, workflowDocument(t, "promote-staging.yml")["jobs"])
	for _, name := range []string{"finish", "signal", "plan-failed"} {
		job := obj(t, jobs[name])
		needs := list(t, job["needs"])
		if !containsNeed(needs, "plan") {
			t.Errorf("%s does not need plan: %v", name, needs)
		}
		condition := stringField(t, job["if"])
		want := "needs.plan.result == 'success'"
		if name == "plan-failed" {
			want = "needs.plan.result == 'failure'"
		}
		if !strings.HasPrefix(condition, "always() &&") || !strings.Contains(condition, want) {
			t.Errorf("%s: %s", name, condition)
		}
	}
	if !containsNeed(list(t, obj(t, jobs["finish"])["needs"]), "full_check") {
		t.Fatal("finish does not need full_check")
	}
	condition := stringField(t, obj(t, jobs["finish"])["if"])
	for _, status := range []string{"success", "failure", "cancelled", "skipped"} {
		if !strings.Contains(condition, "needs.full_check.result == '"+status+"'") {
			t.Errorf("finish lacks %s", status)
		}
	}
}

func containsNeed(needs []any, name string) bool {
	for _, need := range needs {
		if need == name {
			return true
		}
	}
	return false
}

// R1 and R3: A failed plan and an unexpected finish error still post a summary without calling the Go tool.
func TestPromotionFallbackMessagesSurviveToolFailure(t *testing.T) {
	jobs := obj(t, workflowDocument(t, "promote-staging.yml")["jobs"])
	run := runStep(t, jobs, "plan-failed")
	for _, want := range []string{"scripts/slack-post.sh", "jq -n", "promotion could not choose a dev commit", "DRY_RUN"} {
		if !strings.Contains(run, want) {
			t.Errorf("plan-failed lacks %q", want)
		}
	}
	if strings.Contains(run, "go run") {
		t.Fatal("failed-plan fallback depends on the Go tool")
	}
	finish := obj(t, jobs["finish"])
	finishRun := stringField(t, obj(t, list(t, finish["steps"])[2])["run"])
	for _, want := range []string{"set -Eeuo pipefail", "trap 'report_unexpected_error' ERR", "push_done=true", "promotion step failed before pushing", "promotion could not report its build", "scripts/slack-post.sh fallback.json"} {
		if !strings.Contains(finishRun, want) {
			t.Errorf("finish lacks %q", want)
		}
	}
	for _, want := range []string{"pushed_at=", "--commit \"$CANDIDATE\"", "createdAt,attempt", "--pushed-at \"$pushed_at\""} {
		if !strings.Contains(finishRun, want) {
			t.Errorf("release lookup lacks %q", want)
		}
	}
}

// runStep returns the one shell block a job runs, wherever it sits among the job's steps.
func runStep(t *testing.T, jobs map[string]any, name string) string {
	t.Helper()
	for _, raw := range list(t, obj(t, jobs[name])["steps"]) {
		if run, ok := obj(t, raw)["run"]; ok {
			return stringField(t, run)
		}
	}
	t.Fatalf("%s runs no shell block", name)
	return ""
}

// R4: A job starts in an empty workspace, so every job that runs a repository
// script checks the repository out first. The shell-block test below copies the
// poster into its own directory and so cannot see a job that never had it.
func TestPromotionJobsCheckOutTheScriptsTheyRun(t *testing.T) {
	jobs := obj(t, workflowDocument(t, "promote-staging.yml")["jobs"])
	for name, raw := range jobs {
		job := obj(t, raw)
		if _, called := job["uses"]; called {
			continue
		}
		checkedOut := false
		for i, rawStep := range list(t, job["steps"]) {
			step := obj(t, rawStep)
			if uses, ok := step["uses"].(string); ok && strings.HasPrefix(uses, "actions/checkout@") {
				checkedOut = true
			}
			run, ok := step["run"].(string)
			if ok && (strings.Contains(run, "scripts/") || strings.Contains(run, "./cmd/")) && !checkedOut {
				t.Errorf("%s step %d runs a repository file before any checkout", name, i)
			}
		}
	}
}

// R1 and R3: The real shell blocks send a message when planning fails or the finish tool exits early.
func TestPromotionFallbackShellBlocks(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is not on PATH")
	}
	jobs := obj(t, workflowDocument(t, "promote-staging.yml")["jobs"])
	planFailed := runStep(t, jobs, "plan-failed")
	finish := stringField(t, obj(t, list(t, obj(t, jobs["finish"])["steps"])[2])["run"])
	for _, row := range []struct {
		name, script, want string
		exit               int
		afterPush          bool
	}{
		{"plan failed", planFailed, "promotion could not choose a dev commit", 0, false},
		{"finish tool failed", finish, "promotion step failed before pushing", 27, false},
		{"report failed after push", finish, "promotion could not report its build", 27, true},
	} {
		t.Run(row.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0755); err != nil {
				t.Fatal(err)
			}
			post, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts", "slack-post.sh"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "scripts", "slack-post.sh"), post, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(dir, "bin"), 0755); err != nil {
				t.Fatal(err)
			}
			goStub := "#!/bin/sh\nexit 27\n"
			if row.afterPush {
				goStub = "#!/bin/sh\ncase \"$*\" in\n  *promotion-decision*) echo '{\"action\":\"push\",\"status\":\"success\"}' ;;\n  *promotion-recheck*) echo promote ;;\n  *) exit 27 ;;\nesac\n"
				for _, name := range []string{"git", "gh"} {
					stub := "#!/bin/sh\nexit 0\n"
					if name == "gh" {
						stub = "#!/bin/sh\necho '[]'\n"
					}
					if err := os.WriteFile(filepath.Join(dir, "bin", name), []byte(stub), 0755); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "bin", "go"), []byte(goStub), 0755); err != nil {
				t.Fatal(err)
			}
			summary := filepath.Join(dir, "summary")
			cmd := exec.Command("bash", "-c", row.script)
			cmd.Dir = dir
			promotionToken := ""
			if row.afterPush {
				promotionToken = "test-token"
			}
			cmd.Env = append(os.Environ(),
				"PATH="+filepath.Join(dir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
				"GITHUB_STEP_SUMMARY="+summary, "SLACK_RELEASE_WEBHOOK=", "DRY_RUN=true",
				"GITHUB_SERVER_URL=https://example.test", "GITHUB_REPOSITORY=Agent-Field/codeaf", "GITHUB_RUN_ID=123",
				"PLAN_JSON={}", "CHECK_RESULT=success", "CANDIDATE=abcdef0123456789", "PROMOTION_TOKEN="+promotionToken,
			)
			out, err := cmd.CombinedOutput()
			code := 0
			if err != nil {
				if failed, ok := err.(*exec.ExitError); ok {
					code = failed.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if code != row.exit {
				t.Fatalf("exit = %d, want %d: %s", code, row.exit, out)
			}
			raw, err := os.ReadFile(summary)
			if err != nil || !strings.HasPrefix(string(raw), "[dry run] *") || !strings.Contains(string(raw), row.want) || !strings.Contains(string(raw), "https://example.test/Agent-Field/codeaf/actions/runs/123") {
				t.Fatalf("summary = %q, output = %s, err = %v", raw, out, err)
			}
		})
	}
}

// C19: The push reads its secret through env, discards checkout credentials, and never forces a ref.
func TestC19PromotionPushIsForwardOnly(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "promote-staging.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)
	jobs := obj(t, workflowDocument(t, "promote-staging.yml")["jobs"])
	finish := obj(t, jobs["finish"])
	steps := list(t, finish["steps"])
	checkout := obj(t, steps[0])
	if obj(t, checkout["with"])["persist-credentials"] != false {
		t.Fatalf("checkout = %v", checkout)
	}
	pushStep := obj(t, steps[len(steps)-1])
	env := obj(t, pushStep["env"])
	if env["PROMOTION_TOKEN"] != "${{ secrets.PROMOTION_TOKEN }}" {
		t.Fatalf("token env = %v", env)
	}
	for _, line := range strings.Split(workflow, "\n") {
		if strings.Contains(line, "git push") && (strings.Contains(line, "--force") || regexp.MustCompile(`\s-f(?:\s|$)`).MatchString(line) || regexp.MustCompile(`\s\+[^ ]*:`).MatchString(line)) {
			t.Errorf("forced push: %s", line)
		}
	}
	if !strings.Contains(workflow, "promotion-recheck") || !strings.Contains(workflow, "git fetch --no-tags origin +refs/heads/dev:") {
		t.Fatal("push has no fresh ancestry check")
	}
}

// C20: Full check keeps its existing triggers and checks out the requested commit in every job.
func TestC20FullCheckRunsOnRequestedRef(t *testing.T) {
	doc := workflowDocument(t, "ci-full.yml")
	on := obj(t, doc["on"])
	for _, name := range []string{"push", "pull_request", "schedule", "workflow_dispatch", "workflow_call"} {
		if _, ok := on[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
	if len(on) != 5 {
		t.Fatalf("triggers = %v", on)
	}
	schedule := list(t, on["schedule"])
	if len(schedule) != 1 || obj(t, schedule[0])["cron"] != "0 9 * * *" {
		t.Fatalf("full check schedule changed: %v", schedule)
	}
	for _, name := range []string{"push", "pull_request"} {
		branches := list(t, obj(t, on[name])["branches"])
		if fmt.Sprint(branches) != "[staging main]" {
			t.Errorf("%s branches = %v", name, branches)
		}
	}
	call := obj(t, obj(t, on["workflow_call"])["inputs"])
	ref := obj(t, call["ref"])
	if ref["required"] != true || ref["type"] != "string" {
		t.Fatalf("call ref = %v", ref)
	}
	jobs := obj(t, doc["jobs"])
	noCheckout := map[string]bool{"full-tests": true, "cross-build": true, "page": true}
	for name, value := range jobs {
		job := obj(t, value)
		steps, ok := job["steps"].([]any)
		if !ok {
			continue
		}
		count := 0
		for _, stepValue := range steps {
			step := obj(t, stepValue)
			if step["uses"] == "actions/checkout@v4" {
				count++
				if obj(t, step["with"])["ref"] != "${{ inputs.ref }}" {
					t.Errorf("%s checkout lacks ref", name)
				}
			}
		}
		want := 1
		if noCheckout[name] {
			want = 0
		}
		if count != want {
			t.Errorf("%s has %d checkouts, want %d", name, count, want)
		}
	}
	if !strings.Contains(stringField(t, obj(t, jobs["page"])["if"]), "!inputs.ref") {
		t.Fatal("page condition does not exclude requested refs")
	}
	if obj(t, doc["concurrency"])["group"] != "full-check-${{ inputs.ref || github.ref }}" {
		t.Fatal("concurrency did not follow the requested ref")
	}
}

// C12: Every payload reaches the run summary and a local webhook, while a missing webhook only warns.
func TestC12SlackPostSummaryAndLocalWebhook(t *testing.T) {
	var received string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("payload: %v", err)
		}
		received = payload.Text
	}))
	defer server.Close()
	payload := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payload, []byte(`{"text":"codeaf staging moved"}`), 0600); err != nil {
		t.Fatal(err)
	}
	summary := filepath.Join(t.TempDir(), "summary.md")
	script := filepath.Join(repositoryRoot(t), "scripts", "slack-post.sh")
	scriptBody, err := os.ReadFile(script)
	if err != nil || !strings.Contains(string(scriptBody), "--max-time 20") {
		t.Fatalf("webhook POST has no 20-second bound: %v", err)
	}
	cmd := exec.Command("bash", script, payload)
	cmd.Env = append(os.Environ(), "GITHUB_STEP_SUMMARY="+summary, "SLACK_RELEASE_WEBHOOK="+server.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("post: %s: %v", out, err)
	}
	raw, err := os.ReadFile(summary)
	if err != nil || !strings.Contains(string(raw), "codeaf staging moved") || received != "codeaf staging moved" {
		t.Fatalf("summary %q, received %q: %v", raw, received, err)
	}
	cmd = exec.Command("bash", script, payload)
	cmd.Env = append(os.Environ(), "GITHUB_STEP_SUMMARY="+summary, "SLACK_RELEASE_WEBHOOK=")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "SLACK_RELEASE_WEBHOOK") {
		t.Fatalf("missing webhook: %s: %v", out, err)
	}
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer failing.Close()
	cmd = exec.Command("bash", script, payload)
	cmd.Env = append(os.Environ(), "GITHUB_STEP_SUMMARY="+summary, "SLACK_RELEASE_WEBHOOK="+failing.URL)
	out, err = cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "Slack webhook POST failed") {
		t.Fatalf("failed webhook: %s: %v", out, err)
	}
}
