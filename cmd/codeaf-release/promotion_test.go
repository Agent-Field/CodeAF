package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// F7: Every promotion function explains its purpose where a future editor changes the policy.
func TestPromotionFunctionsKeepDocComments(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "promotion.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Doc == nil {
			t.Errorf("%s has no doc comment", fn.Name.Name)
		}
	}
}

func instant(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func fixtureGit(t *testing.T, repo string, date string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=Test", "-c", "user.email=test@example.com"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}
func fixtureCommit(t *testing.T, repo, date, subject string) string {
	t.Helper()
	path := filepath.Join(repo, "item")
	if err := os.WriteFile(path, []byte(date+subject), 0600); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, repo, date, "add", "item")
	fixtureGit(t, repo, date, "commit", "-m", subject)
	return fixtureGit(t, repo, date, "rev-parse", "HEAD")
}
func promotionFixture(t *testing.T) (string, string, string, string) {
	t.Helper()
	repo := t.TempDir()
	date := "2026-09-18T12:00:00Z"
	fixtureGit(t, repo, date, "init", "-b", "dev")
	base := fixtureCommit(t, repo, date, "base")
	old := fixtureCommit(t, repo, "2026-09-25T20:59:00Z", "before cutoff (#1234)")
	at := fixtureCommit(t, repo, "2026-09-25T21:00:00Z", "at cutoff")
	late := fixtureCommit(t, repo, "2026-09-25T21:00:01Z", "after cutoff")
	fixtureGit(t, repo, date, "update-ref", "refs/remotes/origin/dev", late)
	fixtureGit(t, repo, date, "update-ref", "refs/remotes/origin/staging", base)
	fixtureGit(t, repo, date, "update-ref", "refs/remotes/origin/main", base)
	return repo, base, old, at
}

// C1: The Toronto cutoff stays on the preceding Friday until its exact local instant.
func TestC1TorontoCutoff(t *testing.T) {
	for _, row := range []struct{ now, want string }{
		{"2026-09-25T21:00:00Z", "2026-09-25T21:00:00Z"},
		{"2026-09-25T20:59:59Z", "2026-09-18T21:00:00Z"},
		{"2026-09-26T01:30:00Z", "2026-09-25T21:00:00Z"},
		{"2026-10-01T12:00:00Z", "2026-09-25T21:00:00Z"},
	} {
		got, err := cutoffAt(instant(t, row.now), "America/Toronto", "Friday", "17:00")
		if err != nil || !got.Equal(instant(t, row.want)) {
			t.Errorf("%s: %s, %v; want %s", row.now, got, err, row.want)
		}
	}
}

// C2: DST changes alter the UTC cutoff without changing Friday at 17:00 locally.
func TestC2TorontoStandardAndDaylightTime(t *testing.T) {
	for _, row := range []struct{ now, want string }{
		{"2026-11-06T22:00:00Z", "2026-11-06T22:00:00Z"},
		{"2026-11-06T21:30:00Z", "2026-10-30T21:00:00Z"},
		{"2026-10-30T21:00:00Z", "2026-10-30T21:00:00Z"},
		{"2027-03-12T22:00:00Z", "2027-03-12T22:00:00Z"},
		{"2027-03-19T21:00:00Z", "2027-03-19T21:00:00Z"},
	} {
		got, err := cutoffAt(instant(t, row.now), "America/Toronto", "Friday", "17:00")
		if err != nil || !got.Equal(instant(t, row.want)) {
			t.Errorf("%s: %s, %v; want %s", row.now, got, err, row.want)
		}
	}
}

// C3: The latest first-parent commit at or before the cutoff is chosen even after a delayed start.
func TestC3CandidateAtCommitTime(t *testing.T) {
	repo, _, _, at := promotionFixture(t)
	for _, now := range []string{"2026-09-25T21:00:00Z", "2026-09-26T12:00:00Z"} {
		plan, err := planPromotion(repo, instant(t, now), "America/Toronto", "Friday", "17:00", "cutoff", "https://example.test/run")
		if err != nil || plan.Candidate != at {
			t.Fatalf("%s: %+v %v", now, plan, err)
		}
	}
	// A side commit with a newer time is reachable from dev but never on its first-parent line.
	fixtureGit(t, repo, "2026-09-25T20:00:00Z", "checkout", "-b", "side", at)
	if err := os.WriteFile(filepath.Join(repo, "side-file"), []byte("side"), 0600); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, repo, "2026-09-25T20:59:59Z", "add", "side-file")
	fixtureGit(t, repo, "2026-09-25T20:59:59Z", "commit", "-m", "side")
	side := fixtureGit(t, repo, "2026-09-25T20:59:59Z", "rev-parse", "HEAD")
	fixtureGit(t, repo, "2026-09-25T21:01:00Z", "checkout", "dev")
	fixtureGit(t, repo, "2026-09-25T21:01:00Z", "merge", "--no-ff", "side", "-m", "merge side")
	merge := fixtureGit(t, repo, "2026-09-25T21:01:00Z", "rev-parse", "HEAD")
	fixtureGit(t, repo, "2026-09-25T21:01:00Z", "update-ref", "refs/remotes/origin/dev", merge)
	got, err := firstParentCandidate(repo, "origin/dev", instant(t, "2026-09-25T21:00:00Z"))
	if err != nil || got != at || got == side {
		t.Fatalf("first parent = %q, %v", got, err)
	}
}

// C4: Promotion, current, divergence, off-dev, and an empty cutoff line are distinct outcomes.
func TestC4PlanOutcomesAndChanges(t *testing.T) {
	repo, base, old, at := promotionFixture(t)
	now := instant(t, "2026-09-26T12:00:00Z")
	plan, err := planPromotion(repo, now, "America/Toronto", "Friday", "17:00", "cutoff", "run")
	if err != nil || plan.Outcome != "promote" || plan.Candidate != at || len(plan.Changes) != 2 || plan.Changes[0].Subject != "at cutoff" || plan.Changes[1].SHA != old {
		t.Fatalf("promote: %+v %v", plan, err)
	}
	fixtureGit(t, repo, "2026-09-18T12:00:00Z", "update-ref", "refs/remotes/origin/staging", at)
	plan, _ = planPromotion(repo, now, "America/Toronto", "Friday", "17:00", "cutoff", "run")
	if plan.Outcome != "current" {
		t.Fatalf("equal: %+v", plan)
	}
	fixtureGit(t, repo, "2026-09-18T12:00:00Z", "update-ref", "refs/remotes/origin/staging", fixtureGit(t, repo, "2026-09-18T12:00:00Z", "rev-parse", "origin/dev"))
	plan, _ = planPromotion(repo, now, "America/Toronto", "Friday", "17:00", "cutoff", "run")
	if plan.Outcome != "current" {
		t.Fatalf("ahead: %+v", plan)
	}
	fixtureGit(t, repo, "2026-09-18T12:00:00Z", "checkout", "-b", "extra", base)
	extra := fixtureCommit(t, repo, "2026-09-25T20:00:00Z", "extra")
	fixtureGit(t, repo, "2026-09-18T12:00:00Z", "update-ref", "refs/remotes/origin/staging", extra)
	plan, _ = planPromotion(repo, now, "America/Toronto", "Friday", "17:00", "cutoff", "run")
	if plan.Outcome != "diverged" {
		t.Fatalf("diverged: %+v", plan)
	}
	plan, _ = planPromotion(repo, now, "America/Toronto", "Friday", "17:00", extra, "run")
	if plan.Outcome != "off-dev" {
		t.Fatalf("off dev: %+v", plan)
	}
	plan, _ = planPromotion(repo, instant(t, "2026-09-11T12:00:00Z"), "America/Toronto", "Friday", "17:00", "cutoff", "run")
	if plan.Outcome != "off-dev" || plan.Reason == "" {
		t.Fatalf("empty cutoff: %+v", plan)
	}
}

// C5: Empty, cutoff, and explicit dispatch targets resolve to different dev commits.
func TestC5DispatchTargets(t *testing.T) {
	repo, _, _, at := promotionFixture(t)
	now := instant(t, "2026-09-26T12:00:00Z")
	tip, _ := gitAt(repo, "rev-parse", "origin/dev")
	for _, row := range []struct{ target, want string }{{"", tip}, {"cutoff", at}, {at, at}} {
		plan, err := planPromotion(repo, now, "America/Toronto", "Friday", "17:00", row.target, "run")
		if err != nil || plan.Candidate != row.want {
			t.Fatalf("target %q: %+v %v", row.target, plan, err)
		}
	}
}

// C7: A staging pointer moved to another line is refused before any push can run.
func TestC7RecheckRefusesNonFastForward(t *testing.T) {
	repo, base, _, at := promotionFixture(t)
	var out strings.Builder
	if err := runPromotionRecheck([]string{"--repo", repo, "--candidate", at}, &out); err != nil || strings.TrimSpace(out.String()) != "promote" {
		t.Fatalf("initial: %q %v", out.String(), err)
	}
	fixtureGit(t, repo, "2026-09-25T20:00:00Z", "checkout", "-b", "other", base)
	extra := fixtureCommit(t, repo, "2026-09-25T20:00:00Z", "different branch")
	fixtureGit(t, repo, "2026-09-25T20:00:00Z", "update-ref", "refs/remotes/origin/staging", extra)
	out.Reset()
	if err := runPromotionRecheck([]string{"--repo", repo, "--candidate", at}, &out); err != nil || strings.TrimSpace(out.String()) != "diverged" {
		t.Fatalf("moved: %q %v", out.String(), err)
	}
}

// C6-C11: Each check, token, push, publication, and dry-run outcome keeps its message and result distinct.
func TestC6ToC11PromotionMessages(t *testing.T) {
	repo, base, _, at := promotionFixture(t)
	plan, _ := planPromotion(repo, instant(t, "2026-09-26T12:00:00Z"), "America/Toronto", "Friday", "17:00", "cutoff", "https://example.test/run")
	for _, result := range []string{"failure", "cancelled", "skipped"} {
		msg, _ := message(plan, "check", result, "full tests, remote", "", "", "", "", "", false, repo)
		verb := map[string]string{"failure": "failed", "cancelled": "was cancelled", "skipped": "was skipped"}[result]
		if !strings.HasPrefix(msg, "*staging did not move*") || !strings.Contains(msg, "The full check "+verb+" on") || !strings.Contains(msg, "full tests, remote") || !strings.Contains(msg, "https://example.test/run") || !strings.Contains(msg, "leave target empty") {
			t.Fatalf("%s: %s", result, msg)
		}
	}
	withoutJobs, _ := message(plan, "check", "failure", "", "", "", "", "", "", false, repo)
	if strings.Contains(withoutJobs, "Failing:") {
		t.Fatal(withoutJobs)
	}
	noToken, _ := message(plan, "check", "success", "", "", "", "", "", "", false, repo)
	if !strings.Contains(noToken, "PROMOTION_TOKEN") || !strings.Contains(noToken, "git push origin "+at+":staging") {
		t.Fatal(noToken)
	}
	dry, _ := message(plan, "check", "success", "", "", "", "", "", "", true, repo)
	if !strings.HasPrefix(dry, "[dry run] *staging did not move*") || !strings.Contains(dry, "would move") {
		t.Fatal(dry)
	}
	rejected, _ := message(plan, "push", "", "", "remote: denied\nsecond line", "", "", "", "", false, repo)
	if !strings.Contains(rejected, "remote: denied") || strings.Contains(rejected, "second line") || !strings.Contains(rejected, "git push origin "+at+":staging") {
		t.Fatal(rejected)
	}
	good, _ := message(plan, "release", "", "", "", "success", "https://example.test/release", "staging-20260925-"+at[:12], "", false, repo)
	if !strings.HasPrefix(good, "*staging moved*") || !strings.Contains(good, "/get/stageaf") || !strings.Contains(good, "stageaf update") {
		t.Fatal(good)
	}
	bad, _ := message(plan, "release", "", "", "", "failure", "https://example.test/release", "", "", false, repo)
	if !strings.Contains(bad, "did not publish") || !strings.Contains(bad, "<https://example.test/release|the release run>") {
		t.Fatal(bad)
	}
	absent, _ := message(plan, "release", "", "", "", "absent", "", "", "", false, repo)
	if !strings.Contains(absent, "no release run appeared") || strings.Contains(absent, "(absent)") {
		t.Fatal(absent)
	}
	blank, _ := message(plan, "push", "", "", "  \n", "", "", "", "", false, repo)
	if strings.Contains(blank, "refused:") || strings.Contains(blank, ": .") || !strings.Contains(blank, "without a reason from git") {
		t.Fatal(blank)
	}
	candidate := plan.Candidate
	plan.Staging = candidate
	plan.Outcome = "current"
	current, _ := message(plan, "plan", "", "", "", "", "", "", "", false, repo)
	if !strings.HasPrefix(current, "*staging is already current*") || !strings.Contains(current, "Nothing new on dev") || strings.Contains(current, "at the cutoff") {
		t.Fatal(current)
	}
	plan.Staging = base
	plan.Candidate = candidate
	plan.Outcome = "diverged"
	diverged, _ := message(plan, "plan", "", "", "", "", "", "", "", false, repo)
	if !strings.HasPrefix(diverged, "*staging did not move*") || !strings.Contains(diverged, "nothing was forced") {
		t.Fatal(diverged)
	}
}

// C6-C11: Only a successful Full check with a configured token can enter the push road.
func TestC6ToC11PushDecision(t *testing.T) {
	plan := PromotionPlan{Outcome: "promote"}
	for _, check := range []string{"failure", "cancelled", "skipped"} {
		decision := decidePromotion(plan, check, false, true)
		if decision.Action != "report" || decision.Status != "failure" {
			t.Fatalf("%s: %+v", check, decision)
		}
	}
	if decision := decidePromotion(plan, "success", false, false); decision.Action != "report" || decision.Status != "failure" {
		t.Fatalf("missing token: %+v", decision)
	}
	if decision := decidePromotion(plan, "success", true, true); decision.Action != "report" || decision.Status != "success" {
		t.Fatalf("dry run: %+v", decision)
	}
	if decision := decidePromotion(plan, "success", false, true); decision.Action != "push" {
		t.Fatalf("passed: %+v", decision)
	}
	for _, row := range []struct{ outcome, status string }{{"current", "success"}, {"diverged", "failure"}, {"off-dev", "failure"}} {
		plan.Outcome = row.outcome
		decision := decidePromotion(plan, "skipped", false, true)
		if decision.Action != "report" || decision.Phase != "plan" || decision.Status != row.status {
			t.Fatalf("%s: %+v", row.outcome, decision)
		}
	}
}

// C13-C15: Commit subjects are escaped and bounded, and the main signal names the preserved staging SHA.
func TestC13ToC15SlackPayloadAndSignal(t *testing.T) {
	repo, base, _, at := promotionFixture(t)
	plan, _ := planPromotion(repo, instant(t, "2026-09-26T12:00:00Z"), "America/Toronto", "Friday", "17:00", "cutoff", "https://example.test/run")
	plan.Changes = []PromotionChange{{at, "a & <tag> \\\"quote\\\" 😃 (#1234)"}}
	for i := 0; i < 30; i++ {
		plan.Changes = append(plan.Changes, PromotionChange{at, strings.Repeat("a", 100)})
	}
	plan.Changes[1].Subject = "line one\nline two"
	msg, _ := message(plan, "release", "", "", "", "success", "", "staging-20260925-"+at[:12], "", false, repo)
	if !strings.Contains(msg, "&amp; &lt;tag&gt;") || !strings.Contains(msg, "|#1234>") || !strings.Contains(msg, "line one\nline two") || !strings.Contains(msg, "and 11 more") || len([]rune(msg)) >= 3000 {
		t.Fatal(msg)
	}
	raw, err := json.Marshal(map[string]string{"text": msg})
	if err != nil || !json.Valid(raw) {
		t.Fatal(err)
	}
	plan.Staging = at
	plan.Main = base
	signal, _ := message(plan, "signal", "", "", "", "", "", "", "2026-09-22T12:00:00Z", false, repo)
	if !strings.HasPrefix(signal, "*Ready for main*") || strings.Contains(signal, plan.Coverage) || !strings.Contains(signal, "git fetch origin && git push origin "+at+":main") || !strings.Contains(signal, "Nothing was pushed to main") || !strings.Contains(signal, "since Tue Sep 22") {
		t.Fatal(signal)
	}
	plan.Main = at
	signal, _ = message(plan, "signal", "", "", "", "", "", "", "", false, repo)
	if !strings.HasPrefix(signal, "*main already has everything staging had*") || strings.Contains(signal, plan.Coverage) {
		t.Fatal(signal)
	}
}

// C9: Only the matching staging release run can report publication.
func TestC9ReleaseRunClassification(t *testing.T) {
	push := instant(t, "2026-09-25T22:00:00Z")
	for _, row := range []struct {
		name, json, want string
		id               int64
	}{
		{"none", `[]`, "absent", 0},
		{"old success", `[{"headSha":"a","status":"completed","conclusion":"success","createdAt":"2026-09-25T21:57:59Z","databaseId":1}]`, "absent", 0},
		{"old success and new pending", `[{"headSha":"a","status":"completed","conclusion":"success","createdAt":"2026-09-25T21:57:59Z","databaseId":1},{"headSha":"a","status":"in_progress","createdAt":"2026-09-25T22:01:00Z","databaseId":2}]`, "pending", 2},
		{"newest of two", `[{"headSha":"a","status":"completed","conclusion":"success","createdAt":"2026-09-25T22:01:00Z","databaseId":2},{"headSha":"a","status":"completed","conclusion":"failure","createdAt":"2026-09-25T22:02:00Z","databaseId":3}]`, "failure", 3},
		{"latest attempt", `[{"headSha":"a","status":"completed","conclusion":"failure","createdAt":"2026-09-25T22:01:00Z","databaseId":2,"attempt":1},{"headSha":"a","status":"in_progress","createdAt":"2026-09-25T22:01:00Z","databaseId":2,"attempt":2}]`, "pending", 2},
	} {
		t.Run(row.name, func(t *testing.T) {
			got, err := classifyRelease([]byte(row.json), "a", push)
			if err != nil || got.Status != row.want || got.ID != row.id {
				t.Fatalf("%s: %+v %v", row.json, got, err)
			}
		})
	}
}

// R2: When staging has already moved past the cutoff commit, the plan leaves it
// alone and says so, without claiming dev has nothing new — dev can carry commits
// after the cutoff that staging has not seen.
func TestStagingPastTheCutoffIsNotNothingNewOnDev(t *testing.T) {
	repo, _, _, at := promotionFixture(t)
	late := strings.TrimSpace(fixtureGit(t, repo, "2026-09-18T12:00:00Z", "rev-parse", "refs/remotes/origin/dev"))
	fixtureGit(t, repo, "2026-09-18T12:00:00Z", "update-ref", "refs/remotes/origin/staging", late)
	plan, err := planPromotion(repo, instant(t, "2026-09-26T12:00:00Z"), "America/Toronto", "Friday", "17:00", "cutoff", "https://example.test/run")
	if err != nil || plan.Outcome != "current" || plan.Candidate != at || plan.Staging != late {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	msg, _ := message(plan, "plan", "", "", "", "", "", "", "", false, repo)
	if !strings.HasPrefix(msg, "*staging is already current*") || strings.Contains(msg, "Nothing new on dev") || !strings.Contains(msg, "already past `"+short(at)+"`") || !strings.Contains(msg, "stays on `"+short(late)+"`") {
		t.Fatal(msg)
	}
}

// R3: An empty push log is an error from the reader rather than an empty line
// that would be quoted as the reason.
func TestEmptyPushLogIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "push.log")
	if err := os.WriteFile(path, []byte("\n  \n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := runPromotionPushError([]string{"--file", path}, &out); err == nil || out.String() != "" {
		t.Fatalf("empty log printed %q, err %v", out.String(), err)
	}
}

// C7: A rejected push quotes git's first error line rather than its destination preamble.
func TestC7PushErrorSelectsFirstError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "push.log")
	if err := os.WriteFile(path, []byte("To example.test\nremote: permission denied\nerror: failed to push\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := runPromotionPushError([]string{"--file", path}, &out); err != nil || strings.TrimSpace(out.String()) != "remote: permission denied" {
		t.Fatalf("error line = %q, %v", out.String(), err)
	}
}

// F1: A check without failed job records omits the empty failure list from its message.
func TestCheckWithoutFailedJobsHasNoFailureList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	if err := os.WriteFile(path, []byte(`{"jobs":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := runPromotionJobs([]string{"--file", path}, &out); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Fatalf("jobs = %q", out.String())
	}
}

// F9: Selecting the cutoff candidate reads one first-parent log even with many newer commits.
func TestCandidateReadsOneGitLog(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "calls")
	script := filepath.Join(dir, "git")
	content := "#!/bin/sh\nprintf 'call\\n' >> '" + count + "'\nprintf 'new 1790380000\\nold 1000000000\\n'\n"
	if err := os.WriteFile(script, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	got, err := firstParentCandidate(dir, "origin/dev", instant(t, "2026-09-25T21:00:00Z"))
	if err != nil || got != "old" {
		t.Fatalf("candidate = %q, %v", got, err)
	}
	raw, err := os.ReadFile(count)
	if err != nil || strings.Count(string(raw), "call") != 1 {
		t.Fatalf("calls = %q, %v", raw, err)
	}
}

// C9: A published staging tag must carry the candidate's first twelve hex characters.
func TestC9PublishedTagMatchesCandidate(t *testing.T) {
	const sha = "abcdef0123456789abcdef0123456789abcdef01"
	path := filepath.Join(t.TempDir(), "releases.json")
	raw := `[[{"tag_name":"staging-20260925-deadbeef0000","published_at":"2026-09-25T22:00:00Z"}],[{"tag_name":"staging-20260925-abcdef012345","published_at":"2026-09-25T23:00:00Z"}]]`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := runPromotionPublished([]string{"--releases", path, "--sha", sha}, &out); err != nil || !strings.Contains(out.String(), "staging-20260925-abcdef012345") {
		t.Fatalf("published = %s, %v", out.String(), err)
	}
}
