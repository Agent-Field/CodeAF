package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// PromotionPlan keeps the refs read before this run moves staging so the main signal names last week's build.
type PromotionPlan struct {
	Outcome   string            `json:"outcome"`
	Candidate string            `json:"candidate"`
	Staging   string            `json:"staging"`
	Main      string            `json:"main"`
	Coverage  string            `json:"coverage"`
	RunURL    string            `json:"run_url"`
	Reason    string            `json:"reason,omitempty"`
	Changes   []PromotionChange `json:"changes,omitempty"`
}

// PromotionChange retains commit subjects with the plan so the report describes the checked candidate.
type PromotionChange struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
}

// promotionDecision keeps the push gate separate from message rendering so a red check cannot push.
type promotionDecision struct {
	Action string `json:"action"`
	Phase  string `json:"phase,omitempty"`
	Status string `json:"status"`
}

// decidePromotion admits a push only after a successful check and a configured token.
func decidePromotion(plan PromotionPlan, check string, dry, tokenSet bool) promotionDecision {
	if plan.Outcome != "promote" {
		status := "failure"
		if plan.Outcome == "current" {
			status = "success"
		}
		return promotionDecision{"report", "plan", status}
	}
	if check != "success" {
		return promotionDecision{"report", "check", "failure"}
	}
	if dry {
		return promotionDecision{"report", "check", "success"}
	}
	if !tokenSet {
		return promotionDecision{"report", "check", "failure"}
	}
	return promotionDecision{"push", "", "success"}
}

// runPromotionDecision reads the saved plan so the workflow has no policy branches of its own.
func runPromotionDecision(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("promotion-decision", flag.ContinueOnError)
	path := fs.String("plan", "", "")
	check := fs.String("check-result", "", "")
	dry := fs.Bool("dry-run", false, "")
	tokenSet := fs.Bool("token-set", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return usageErr("--plan is required")
	}
	raw, err := os.ReadFile(*path)
	if err != nil {
		return err
	}
	var plan PromotionPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(decidePromotion(plan, *check, *dry, *tokenSet))
}

// gitAt runs read-only git queries in the selected repository fixture or checkout.
func gitAt(repo string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ancestor uses git's ancestry relation because a timestamp cannot prove a safe fast-forward.
func ancestor(repo, older, newer string) bool {
	cmd := exec.Command("git", "-C", repo, "merge-base", "--is-ancestor", older, newer)
	return cmd.Run() == nil
}

// cutoffAt constructs each local Friday anew so daylight changes cannot shift the intended hour.
func cutoffAt(now time.Time, zone, weekday, clock string) (time.Time, error) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.Time{}, err
	}
	var day time.Weekday
	found := false
	for i := time.Sunday; i <= time.Saturday; i++ {
		if i.String() == weekday {
			day = i
			found = true
			break
		}
	}
	if !found {
		return time.Time{}, fmt.Errorf("unknown weekday %q", weekday)
	}
	parts := strings.Split(clock, ":")
	if len(parts) != 2 {
		return time.Time{}, fmt.Errorf("invalid cutoff clock %q", clock)
	}
	hour, e1 := strconv.Atoi(parts[0])
	minute, e2 := strconv.Atoi(parts[1])
	if e1 != nil || e2 != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return time.Time{}, fmt.Errorf("invalid cutoff clock %q", clock)
	}
	local := now.In(loc)
	days := (int(local.Weekday()) - int(day) + 7) % 7
	date := local.AddDate(0, 0, -days)
	cut := time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, loc)
	if cut.After(now) {
		date = date.AddDate(0, 0, -7)
		cut = time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, loc)
	}
	return cut, nil
}

// firstParentCandidate ignores merged side branches because only the dev line marks when work landed.
func firstParentCandidate(repo, ref string, cut time.Time) (string, error) {
	// THE CUTOFF IS A COMMIT TIME, NOT THE RUN TIME. Read the complete first-parent
	// log in one process; consuming it all also avoids a SIGPIPE from early exit.
	line, err := gitAt(repo, "log", "--first-parent", "--format=%H %ct", ref)
	if err != nil {
		return "", err
	}
	for _, entry := range strings.Split(line, "\n") {
		fields := strings.Fields(entry)
		if len(fields) != 2 {
			return "", fmt.Errorf("invalid first-parent log entry %q", entry)
		}
		seconds, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return "", err
		}
		if !time.Unix(seconds, 0).After(cut) {
			return fields[0], nil
		}
	}
	return "", errors.New("no dev commit existed at the cutoff")
}

// changeList follows first parents so the reported count matches promoted dev merges.
func changeList(repo, from, to string) ([]PromotionChange, error) {
	raw, err := gitAt(repo, "log", "--first-parent", "--format=%H%x09%s", from+".."+to)
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, nil
	}
	var changes []PromotionChange
	for _, line := range strings.Split(raw, "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 {
			changes = append(changes, PromotionChange{parts[0], parts[1]})
		}
	}
	return changes, nil
}

// planPromotion freezes pre-push pointers and refuses targets outside dev before choosing an outcome.
func planPromotion(repo string, now time.Time, zone, weekday, clock, target, runURL string) (PromotionPlan, error) {
	// THE SIGNAL READS THE POINTERS FROM BEFORE THIS RUN MOVED ANYTHING.
	plan := PromotionPlan{RunURL: runURL}
	var err error
	if plan.Staging, err = gitAt(repo, "rev-parse", "origin/staging"); err != nil {
		return plan, err
	}
	if plan.Main, err = gitAt(repo, "rev-parse", "origin/main"); err != nil {
		return plan, err
	}
	dev, err := gitAt(repo, "rev-parse", "origin/dev")
	if err != nil {
		return plan, err
	}
	if target == "cutoff" {
		cut, err := cutoffAt(now, zone, weekday, clock)
		if err != nil {
			return plan, err
		}
		plan.Coverage = "dev as of " + cut.In(cut.Location()).Format("Mon Jan 2 15:04 MST")
		plan.Candidate, err = firstParentCandidate(repo, dev, cut)
		if err != nil {
			plan.Outcome = "off-dev"
			plan.Reason = err.Error()
			return plan, nil
		}
	} else {
		plan.Coverage = "dev at " + now.UTC().Format("Mon Jan 2 15:04 MST") + " (chosen by hand)"
		if target == "" {
			plan.Candidate = dev
		} else {
			plan.Candidate, err = gitAt(repo, "rev-parse", "--verify", target+"^{commit}")
			if err != nil {
				plan.Outcome = "off-dev"
				plan.Reason = "target does not name a commit on dev"
				return plan, nil
			}
		}
	}
	if !ancestor(repo, plan.Candidate, dev) {
		plan.Outcome = "off-dev"
		plan.Reason = "target is not on origin/dev"
		return plan, nil
	}
	switch {
	case ancestor(repo, plan.Staging, plan.Candidate):
		if plan.Staging == plan.Candidate {
			plan.Outcome = "current"
		} else {
			plan.Outcome = "promote"
			plan.Changes, err = changeList(repo, plan.Staging, plan.Candidate)
		}
	case ancestor(repo, plan.Candidate, plan.Staging):
		plan.Outcome = "current"
	default:
		plan.Outcome = "diverged"
	}
	return plan, err
}

// runPromotionPlan writes one JSON plan to both stdout and the caller's job outputs.
func runPromotionPlan(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("promotion-plan", flag.ContinueOnError)
	nowText := fs.String("now", "", "planning time in RFC3339")
	zone := fs.String("zone", "", "")
	weekday := fs.String("weekday", "", "")
	clock := fs.String("clock", "", "")
	target := fs.String("target", "", "")
	event := fs.String("event", "workflow_dispatch", "")
	runURL := fs.String("run-url", "", "")
	repo := fs.String("repo", ".", "")
	output := fs.String("github-output", "", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *nowText == "" || *zone == "" || *weekday == "" || *clock == "" {
		return usageErr("--now, --zone, --weekday, and --clock are required")
	}
	now, err := time.Parse(time.RFC3339, *nowText)
	if err != nil {
		return err
	}
	if *event == "schedule" {
		*target = "cutoff"
	}
	plan, err := planPromotion(*repo, now, *zone, *weekday, *clock, *target, *runURL)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, string(raw))
	if *output != "" {
		file, err := os.OpenFile(*output, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = fmt.Fprintf(file, "plan=%s\noutcome=%s\ncandidate=%s\nstaging=%s\n", raw, plan.Outcome, plan.Candidate, plan.Staging)
		return err
	}
	return nil
}

// runPromotionRecheck enforces the same ancestry rule after the long Full check completes.
func runPromotionRecheck(args []string, out io.Writer) error {
	// NOTHING IS EVER FORCED. The workflow fetches both refs immediately before this call.
	fs := flag.NewFlagSet("promotion-recheck", flag.ContinueOnError)
	candidate := fs.String("candidate", "", "")
	repo := fs.String("repo", ".", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *candidate == "" {
		return usageErr("--candidate is required")
	}
	dev, err := gitAt(*repo, "rev-parse", "origin/dev")
	if err != nil {
		return err
	}
	stage, err := gitAt(*repo, "rev-parse", "origin/staging")
	if err != nil {
		return err
	}
	result := "diverged"
	if !ancestor(*repo, *candidate, dev) {
		result = "off-dev"
	} else if ancestor(*repo, stage, *candidate) {
		if stage == *candidate {
			result = "current"
		} else {
			result = "promote"
		}
	} else if ancestor(*repo, *candidate, stage) {
		result = "current"
	}
	fmt.Fprintln(out, result)
	return nil
}

// releaseRun holds the fields needed to distinguish this push from an earlier release of the same SHA.
type releaseRun struct {
	HeadSHA    string `json:"headSha"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url"`
	DatabaseID int64  `json:"databaseId"`
	CreatedAt  string `json:"createdAt"`
	Attempt    int    `json:"attempt"`
}

// publishedRelease supplies the tag and optional publication date for a finished build.
type publishedRelease struct {
	TagName     string `json:"tag_name"`
	PublishedAt string `json:"published_at"`
}

// runPromotionJobs names the failed jobs of the called Full check in a short notification.
func runPromotionJobs(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("promotion-jobs", flag.ContinueOnError)
	path := fs.String("file", "", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return usageErr("--file is required")
	}
	raw, err := os.ReadFile(*path)
	if err != nil {
		return err
	}
	var view struct {
		Jobs []struct {
			Name       string `json:"name"`
			Conclusion string `json:"conclusion"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(raw, &view); err != nil {
		return err
	}
	var names []string
	for _, job := range view.Jobs {
		if job.Conclusion != "success" && job.Conclusion != "skipped" && job.Conclusion != "" {
			names = append(names, job.Name)
		}
	}
	_, err = fmt.Fprintln(out, strings.Join(names, ", "))
	return err
}

// runPromotionPushError keeps the first useful git error so a refused push is actionable.
func runPromotionPushError(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("promotion-push-error", flag.ContinueOnError)
	path := fs.String("file", "", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return usageErr("--file is required")
	}
	raw, err := os.ReadFile(*path)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return errors.New("git push failed without an error line")
	}
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if strings.Contains(line, "error:") || strings.Contains(line, "fatal:") || strings.Contains(line, "remote:") || strings.Contains(line, "[remote rejected]") {
			_, err = fmt.Fprintln(out, line)
			return err
		}
	}
	_, err = fmt.Fprintln(out, lines[0])
	return err
}

// runPromotionPublished requires a staging tag for the candidate before claiming publication.
func runPromotionPublished(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("promotion-published", flag.ContinueOnError)
	releases := fs.String("releases", "", "")
	sha := fs.String("sha", "", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *releases == "" || len(*sha) < 12 {
		return usageErr("--releases and --sha are required")
	}
	raw, err := os.ReadFile(*releases)
	if err != nil {
		return err
	}
	var rows []publishedRelease
	if err := json.Unmarshal(raw, &rows); err != nil {
		var pages [][]publishedRelease
		if nestedErr := json.Unmarshal(raw, &pages); nestedErr != nil {
			return err
		}
		for _, page := range pages {
			rows = append(rows, page...)
		}
	}
	tagPattern := regexp.MustCompile(`^staging-[0-9]{8}-` + regexp.QuoteMeta((*sha)[:12]) + `$`)
	for _, row := range rows {
		if tagPattern.MatchString(row.TagName) {
			return json.NewEncoder(out).Encode(row)
		}
	}
	return json.NewEncoder(out).Encode(publishedRelease{})
}

// releaseState is the small result the workflow polls while waiting for publication.
type releaseState struct {
	Status string `json:"status"`
	URL    string `json:"url,omitempty"`
	ID     int64  `json:"id,omitempty"`
}

// classifyRelease discards old runs so a repeat push cannot inherit an earlier success.
func classifyRelease(raw []byte, sha string, pushedAt time.Time) (releaseState, error) {
	var runs []releaseRun
	if err := json.Unmarshal(raw, &runs); err != nil {
		return releaseState{}, err
	}
	var newest *releaseRun
	var newestAt time.Time
	for _, run := range runs {
		if run.HeadSHA != sha {
			continue
		}
		createdAt, err := time.Parse(time.RFC3339, run.CreatedAt)
		if err != nil {
			return releaseState{}, fmt.Errorf("release run %d createdAt: %w", run.DatabaseID, err)
		}
		if createdAt.Before(pushedAt.Add(-2 * time.Minute)) {
			continue
		}
		if newest == nil || createdAt.After(newestAt) || (createdAt.Equal(newestAt) && run.Attempt > newest.Attempt) {
			copy := run
			newest = &copy
			newestAt = createdAt
		}
	}
	if newest == nil {
		return releaseState{Status: "absent"}, nil
	}
	state := releaseState{Status: "pending", URL: newest.URL, ID: newest.DatabaseID}
	if newest.Status == "completed" {
		state.Status = newest.Conclusion
	}
	return state, nil
}

// runPromotionRelease uses the injected push time rather than a tool-local wall clock.
func runPromotionRelease(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("promotion-release", flag.ContinueOnError)
	runs := fs.String("runs", "", "")
	sha := fs.String("sha", "", "")
	pushedAtText := fs.String("pushed-at", "", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *runs == "" || *sha == "" || *pushedAtText == "" {
		return usageErr("--runs, --sha, and --pushed-at are required")
	}
	pushedAt, err := time.Parse(time.RFC3339, *pushedAtText)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(*runs)
	if err != nil {
		return err
	}
	state, err := classifyRelease(raw, *sha, pushedAt)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(state)
}

// pullSuffix recognizes squash-merge subjects for links without changing their displayed words.
var pullSuffix = regexp.MustCompile(`\s*\(#([0-9]+)\)$`)

// escapeSlack prevents a commit subject from becoming Slack markup.
func escapeSlack(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// short keeps routine status messages scannable while commands retain full SHAs.
func short(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}

// changeLines bounds subject lists so a busy week still fits Slack's message limit.
func changeLines(changes []PromotionChange) string {
	var b strings.Builder
	for i, change := range changes {
		if i >= 20 {
			fmt.Fprintf(&b, "\nand %d more", len(changes)-20)
			break
		}
		subject := change.Subject
		pr := ""
		if match := pullSuffix.FindStringSubmatch(subject); match != nil {
			subject = strings.TrimSuffix(subject, match[0])
			pr = " (<https://github.com/Agent-Field/codeaf/pull/" + match[1] + "|#" + match[1] + ">)"
		}
		runes := []rune(subject)
		if len(runes) > 90 {
			subject = string(runes[:89]) + "…"
		}
		fmt.Fprintf(&b, "\n• %s%s", escapeSlack(subject), pr)
	}
	return b.String()
}

// runLink makes every ordinary outcome traceable to the promotion run.
func runLink(plan PromotionPlan) string {
	if plan.RunURL == "" {
		return ""
	}
	return "\n<" + plan.RunURL + "|Promote to staging run>"
}

// prefix marks dry-run notifications before their headline so nobody mistakes them for a push.
func prefix(dry bool) string {
	if dry {
		return "[dry run] "
	}
	return ""
}

// message composes every normal outcome in one place so workflow branches do not drift in wording.
func message(plan PromotionPlan, phase, checkResult, jobs, pushError, releaseStatus, releaseURL, releaseTag, publishedAt string, dry bool, repo string) (string, error) {
	id := short(plan.Candidate)
	if id == "" {
		id = short(plan.Staging)
	}
	context := fmt.Sprintf("%s (%s)", "`"+id+"`", plan.Coverage)
	var body string
	switch phase {
	case "plan":
		switch plan.Outcome {
		case "current":
			// A plan is current when staging already holds the candidate, which
			// includes staging having moved past it; only the first means dev has
			// nothing new, because dev may carry commits after the cutoff.
			if plan.Candidate == "" || plan.Staging == plan.Candidate {
				body = fmt.Sprintf("*staging is already current* — Nothing new on dev; staging stays on `%s`. %s", short(plan.Staging), context)
			} else {
				body = fmt.Sprintf("*staging is already current* — staging is already past %s; staging stays on `%s`.", context, short(plan.Staging))
			}
		case "diverged":
			body = fmt.Sprintf("*staging did not move* — `%s` and `%s` diverged; nothing was forced. A person has to look. %s", short(plan.Staging), id, context)
		case "off-dev":
			body = fmt.Sprintf("*staging did not move* — %s. %s", escapeSlack(plan.Reason), context)
		default:
			return "", nil
		}
	case "check":
		if checkResult != "success" {
			verb := map[string]string{"failure": "failed", "cancelled": "was cancelled", "skipped": "was skipped"}[checkResult]
			if verb == "" {
				verb = "did not pass"
			}
			body = fmt.Sprintf("*staging did not move* — The full check %s on %s.", verb, context)
			if jobs != "" {
				body += " Failing: " + escapeSlack(jobs) + "."
			}
			body += fmt.Sprintf(" staging stays on `%s`. Fix it on dev, then run Promote to staging from Actions (leave target empty) to try again.", short(plan.Staging))
		} else if dry {
			body = fmt.Sprintf("*staging did not move* — The full check passed. staging would move to %s with %d changes:%s", context, len(plan.Changes), changeLines(plan.Changes))
		} else {
			body = fmt.Sprintf("*staging did not move* — The full check passed on %s, but PROMOTION_TOKEN is not configured. Run `git push origin %s:staging` by hand.", context, plan.Candidate)
		}
	case "push":
		if pushError == "current" {
			body = fmt.Sprintf("*staging is already current* — staging reached %s during the check; nothing moved.", context)
		} else if pushError == "diverged" || pushError == "off-dev" {
			body = fmt.Sprintf("*staging did not move* — The refs changed during the check (%s); nothing was forced. A person has to look. %s", pushError, context)
		} else {
			first := strings.Split(strings.TrimSpace(pushError), "\n")[0]
			if token := os.Getenv("PROMOTION_TOKEN"); token != "" {
				first = strings.ReplaceAll(first, token, "[token]")
			}
			// The workflow passes whatever the error reader printed, and it prints
			// nothing when git left an empty log, so an empty reason is named rather
			// than drawn as a bare colon.
			reason := ": " + escapeSlack(first)
			if first == "" {
				reason = " without a reason from git"
			}
			body = fmt.Sprintf("*staging did not move* — Push of %s was refused%s. Run `git push origin %s:staging` by hand after checking the refs.", context, reason, plan.Candidate)
		}
	case "release":
		if releaseStatus == "success" && releaseTag != "" {
			body = fmt.Sprintf("*staging moved* to %s · %d changes%s\nBuild `%s` is published. Try it beside codeaf: `curl -fsSL https://agentfield.ai/get/stageaf | bash` · already have it: `stageaf update`.", context, len(plan.Changes), changeLines(plan.Changes), releaseTag)
		} else {
			body = fmt.Sprintf("*staging moved* to %s, but the build did not publish", context)
			if releaseStatus == "absent" {
				body += "; no release run appeared within the wait."
			} else {
				body += fmt.Sprintf(" (%s).", releaseStatus)
				if releaseURL != "" {
					body += " <" + releaseURL + "|the release run>"
				}
			}
		}
	case "signal":
		if ancestor(repo, plan.Staging, plan.Main) {
			body = fmt.Sprintf("*main already has everything staging had* (`%s`); nothing to release this week.", short(plan.Staging))
		} else {
			changes, err := changeList(repo, plan.Main, plan.Staging)
			if err != nil {
				return "", err
			}
			since := ""
			if publishedAt != "" {
				if date, err := time.Parse(time.RFC3339, publishedAt); err == nil {
					since = " since " + date.Format("Mon Jan 2")
				}
			}
			body = fmt.Sprintf("*Ready for main* — staging has been on `%s`%s. main is %d changes behind:%s\nWhen you want it released: `git fetch origin && git push origin %s:main` publishes the next rc. Stable is a Release dispatch on main; see docs/rules/promotion.md. Nothing was pushed to main.", short(plan.Staging), since, len(changes), changeLines(changes), plan.Staging)
		}
	default:
		return "", usageErr("unknown message phase %q", phase)
	}
	footer := runLink(plan)
	start := prefix(dry)
	maxBody := 2999 - len([]rune(start)) - len([]rune(footer))
	runes := []rune(body)
	if len(runes) > maxBody {
		runes = append(runes[:maxBody-1], '…')
	}
	return start + string(runes) + footer, nil
}

// runPromotionMessage JSON-encodes the text so quotes and newlines remain safe for the webhook.
func runPromotionMessage(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("promotion-message", flag.ContinueOnError)
	path := fs.String("plan", "", "")
	phase := fs.String("phase", "", "")
	check := fs.String("check-result", "", "")
	jobs := fs.String("jobs", "", "")
	pushErr := fs.String("push-error", "", "")
	releaseStatus := fs.String("release-status", "", "")
	releaseURL := fs.String("release-url", "", "")
	releaseTag := fs.String("release-tag", "", "")
	publishedAt := fs.String("published-at", "", "")
	dry := fs.Bool("dry-run", false, "")
	repo := fs.String("repo", ".", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return usageErr("--plan is required")
	}
	raw, err := os.ReadFile(*path)
	if err != nil {
		return err
	}
	var plan PromotionPlan
	if err = json.Unmarshal(raw, &plan); err != nil {
		return err
	}
	msg, err := message(plan, *phase, *check, *jobs, *pushErr, *releaseStatus, *releaseURL, *releaseTag, *publishedAt, *dry, *repo)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(map[string]string{"text": msg})
}
