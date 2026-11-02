package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestReleaseWorkflowKeepsTheChannelContract(t *testing.T) {
	path := filepath.Join(repositoryRoot(t), ".github", "workflows", "release.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)

	push := regexp.MustCompile(`(?ms)^  push:\n    branches:\n((?:      - [^\n]+\n)+)`).FindStringSubmatch(workflow)
	if push == nil || push[1] != "      - dev\n      - staging\n      - main\n" {
		t.Fatalf("push branches are not exactly dev, staging, main:\n%s", workflow)
	}
	if regexp.MustCompile(`(?m)^\s+tags:`).MatchString(workflow) {
		t.Fatal("a pushed tag must not trigger the release workflow")
	}
	for _, input := range []string{"channel:", "component:", "existing_version:"} {
		if !strings.Contains(workflow, "      "+input) {
			t.Errorf("workflow_dispatch input %s is missing", input)
		}
	}
	wantCancellation := "  cancel-in-progress: ${{ (github.event_name == 'push' && github.ref_name == 'dev') || (github.event_name == 'workflow_dispatch' && inputs.channel == 'dev') }}"
	cancellation := regexp.MustCompile(`(?m)^  cancel-in-progress: .+$`).FindString(workflow)
	if cancellation != wantCancellation {
		t.Fatalf("cancel-in-progress must name only dev:\n%s", cancellation)
	}
	for _, command := range []string{
		"go run ./cmd/codeaf-release next",
		"go run ./cmd/codeaf-release prune",
		// The stable notes are rendered for the page and never copied whole:
		// a rolled-up section is bigger than a release body may be.
		`go run ./cmd/codeaf-changes notes "$TAG"`,
	} {
		if !strings.Contains(workflow, command) {
			t.Errorf("workflow does not run %q", command)
		}
	}
	// THE NOTICE TRAVELS WITH THE BINARIES. Its licence obligation belongs to
	// the distribution, and its checksum makes the copied document accountable.
	for _, source := range []string{
		"cp THIRD-PARTY-NOTICES.md dist/",
		"sha256sum codeaf-* THIRD-PARTY-NOTICES.md > checksums.txt",
	} {
		if !strings.Contains(workflow, source) {
			t.Errorf("a release does not carry the third-party notices beside its binaries: missing %q", source)
		}
	}
	if strings.Contains(workflow, "awk -v tag=") {
		t.Fatal("the stable notes are copied straight out of CHANGELOG.md, which GitHub refuses once the section outgrows a release body")
	}
	if strings.Contains(workflow, "40") {
		t.Fatal("the retention count was copied into the workflow instead of read from codeaf-release")
	}
	// `test` is skipped on every dev build, and GitHub skips any job whose
	// dependency chain holds a skipped job unless that job's own condition says
	// always(). The first live dev run built for eight minutes and published
	// nothing because publish had no condition; both jobs below `test` must
	// carry one, and it must still refuse a red predecessor.
	for job, want := range map[string]string{
		"build":   "    if: always() && needs.prepare.result == 'success' && (needs.test.result == 'success' || needs.test.result == 'skipped')",
		"publish": "    if: always() && needs.prepare.result == 'success' && needs.build.result == 'success' && needs.relay.result == 'success'",
	} {
		block := regexp.MustCompile(`(?ms)^  ` + job + `:\n    needs: [^\n]+\n(    if: [^\n]+)`).FindStringSubmatch(workflow)
		if block == nil || block[1] != want {
			t.Errorf("job %s must sit under `test` with the condition\n%s\nand has\n%v", job, want, block)
		}
	}
	if strings.Contains(workflow, `--is-ancestor "$GITHUB_SHA"`) || strings.Count(workflow, `--is-ancestor "$sha"`) != 2 {
		t.Fatal("the release order guards must check the resolved source commit")
	}
	if !strings.Contains(workflow, `go run ./cmd/codeaf-release kind "$tag"`) {
		t.Fatal("an existing release tag is not classified before its channel marks are applied")
	}
	if !strings.Contains(workflow, `gh release view "$TAG"`) {
		t.Fatal("ordinary publication does not look for an existing release before creation")
	}
	for _, source := range []string{
		`git rev-parse --verify "refs/tags/${TAG}^{commit}"`,
		`[ "$existing_sha" != "$RELEASE_SHA" ]`,
		"use the existing_version dispatch to repair it deliberately",
	} {
		if !strings.Contains(workflow, source) {
			t.Errorf("ordinary publication does not guard an existing release with %q", source)
		}
	}
	for _, source := range []string{
		`if [ -s .github/known-red.txt ]; then`,
		`skip="$(awk '!/^#/ && NF {print}' .github/known-red.txt | paste -sd'|' -)"`,
		`test_args=()`,
		`go test ${test_args[@]+"${test_args[@]}"}`,
	} {
		if !strings.Contains(workflow, source) {
			t.Errorf("the release-surface test is not safe for an empty or absent known-red ledger: missing %q", source)
		}
	}
	if strings.Contains(workflow, "known-red.txt 2>/dev/null") {
		t.Fatal("the release-surface test reads the ledger unguarded: under pipefail an absent file ends the step before go test runs")
	}
}

// V10: Dev release notes offer the proxy that installs the dev channel as
// devaf beside codeaf, and no other channel enters that conditional block.
func TestV10DevReleaseNotesNameTheDevafInstaller(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)
	const line = "curl -fsSL https://agentfield.ai/get/devaf | bash"
	if strings.Count(workflow, line) != 1 {
		t.Fatalf("devaf install line count = %d, want 1", strings.Count(workflow, line))
	}
	block := regexp.MustCompile(`(?ms)if \[ "\$CHANNEL" = "dev" \]; then\n(.*?)\n\s+fi`).FindStringSubmatch(workflow)
	if block == nil || !strings.Contains(block[1], "installs this dev channel") || !strings.Contains(block[1], line) {
		t.Fatalf("dev notes block does not describe and spell the devaf road:\n%v", block)
	}
}

// C23: Only staging notes name the installer that keeps the staging build beside codeaf.
func TestC23StagingReleaseNotesNameTheStageafInstaller(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)
	const line = "curl -fsSL https://agentfield.ai/get/stageaf | bash"
	if strings.Count(workflow, line) != 1 {
		t.Fatalf("stageaf install line count = %d, want 1", strings.Count(workflow, line))
	}
	block := regexp.MustCompile(`(?ms)if \[ "\$CHANNEL" = "staging" \]; then\n(.*?)\n\s+fi`).FindStringSubmatch(workflow)
	if block == nil || !strings.Contains(block[1], "installs this staging channel as `stageaf` beside codeaf") || !strings.Contains(block[1], line) {
		t.Fatalf("staging notes block does not name the stageaf road:\n%v", block)
	}
}

// releaseSurfacePrologue is the shell of the "Test release surface" step up to
// its go test line, with the go test replaced by a line that prints the
// arguments it would have been given.
func releaseSurfacePrologue(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "- name: Test release surface" {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatal("release.yml has no step named Test release surface")
	}
	var script []string
	inRun := false
	for _, line := range lines[start:] {
		trimmed := strings.TrimSpace(line)
		if !inRun {
			if trimmed == "run: |" {
				inRun = true
			}
			continue
		}
		if strings.HasPrefix(trimmed, "go test ") {
			script = append(script, `printf 'ARG:%s\n' ${test_args[@]+"${test_args[@]}"}`)
			return strings.Join(script, "\n") + "\n"
		}
		script = append(script, trimmed)
	}
	t.Fatal("the Test release surface step has no go test line")
	return ""
}

// THE STEP MUST SURVIVE EVERY STATE THE LEDGER CAN BE IN. The ledger only
// shrinks, so absent is its final state; empty and comment-only are the states
// on the way there; and a ledger with a name must still turn into a -skip. The
// first staging build after the ledger was deleted died in this prologue with
// nothing printed, because under `set -euo pipefail` an awk over a missing file
// is an exit inside an assignment — so this runs the real prologue under the
// real shell options in each state, rather than asserting a string.
func TestTheReleaseSurfaceTestSurvivesEveryLedgerState(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not on PATH")
	}
	prologue := releaseSurfacePrologue(t)
	cases := []struct {
		name   string
		ledger *string
		skips  string
	}{
		{name: "absent", ledger: nil},
		{name: "empty", ledger: ptr("")},
		{name: "comments only", ledger: ptr("# owed\n\n# and paid\n")},
		{name: "one name", ledger: ptr("# owed\nTestStillRed\n"), skips: "^(TestStillRed)$"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.ledger != nil {
				if err := os.MkdirAll(filepath.Join(dir, ".github"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, ".github", "known-red.txt"), []byte(*tc.ledger), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("bash", "-c", prologue)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("the prologue died with the ledger %s: %v\n%s", tc.name, err, out)
			}
			got := string(out)
			if tc.skips == "" {
				if strings.Contains(got, "-skip") {
					t.Fatalf("a ledger with nothing owed produced a -skip:\n%s", got)
				}
				return
			}
			if !strings.Contains(got, "ARG:-skip\nARG:"+tc.skips+"\n") {
				t.Fatalf("a ledger with a name did not become -skip %s:\n%s", tc.skips, got)
			}
		})
	}
}

func ptr(s string) *string { return &s }

func TestPublishedPinExamplesGiveVersionToBash(t *testing.T) {
	root := repositoryRoot(t)
	paths := []string{
		filepath.Join(root, ".github", "workflows", "release.yml"),
		// The install reference, pin examples included, moved out of README.md
		// into docs/GUIDE.md when the README became the launch page; the README
		// now points at the releases page for pinning and carries no example.
		filepath.Join(root, "docs", "GUIDE.md"),
		filepath.Join(root, "internal", "manual", "chat", "running-from-the-terminal.md"),
	}
	broken := regexp.MustCompile(`(?m)VERSION=[^ \n]+[ \t]+curl`)
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if broken.Match(raw) || !strings.Contains(string(raw), "| VERSION=") {
			t.Errorf("%s does not give VERSION to bash", path)
		}
	}
}

func TestRelayJobRunsAfterPrepareAndBlocksPublish(t *testing.T) {
	root := repositoryRoot(t)
	rawWorkflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflowText := string(rawWorkflow)

	doc := workflowDocument(t, "release.yml")
	jobs := obj(t, doc["jobs"])
	relay := obj(t, jobs["relay"])
	if relay["needs"] != "prepare" {
		t.Fatalf("relay needs = %v, want prepare", relay["needs"])
	}
	perms := obj(t, relay["permissions"])
	if len(perms) != 1 || perms["contents"] != "read" {
		t.Fatalf("relay permissions = %v, want only contents: read", perms)
	}
	concurrency := obj(t, relay["concurrency"])
	wantGroup := "relay-${{ (needs.prepare.outputs.channel == 'stable' || needs.prepare.outputs.channel == 'rc') && 'production' || needs.prepare.outputs.channel }}"
	if concurrency["group"] != wantGroup || concurrency["cancel-in-progress"] != false {
		t.Fatalf("relay concurrency = %v, want group %q and cancel-in-progress false", concurrency, wantGroup)
	}

	publish := obj(t, jobs["publish"])
	publishNeeds := list(t, publish["needs"])
	for _, want := range []string{"prepare", "build", "relay"} {
		if !containsNeed(publishNeeds, want) {
			t.Errorf("publish needs %v lacks %s", publishNeeds, want)
		}
	}
	if !strings.Contains(stringField(t, publish["if"]), "needs.relay.result == 'success'") {
		t.Fatalf("publish condition does not block on relay success: %v", publish["if"])
	}

	steps := list(t, relay["steps"])
	checkedOut := false
	pinnedAction := regexp.MustCompile(`^actions/[a-z0-9-]+@v[0-9]+$`)
	for i, rawStep := range steps {
		step := obj(t, rawStep)
		if i > 0 && step["if"] != "steps.gate.outputs.deploy == 'true'" {
			t.Errorf("relay step %d lacks gate condition: %v", i, step["if"])
		}
		if uses, ok := step["uses"].(string); ok {
			if !pinnedAction.MatchString(uses) {
				t.Errorf("relay step %d uses unpinned action %q", i, uses)
			}
			if strings.HasPrefix(uses, "actions/checkout@") {
				checkedOut = true
				if obj(t, step["with"])["ref"] != "${{ needs.prepare.outputs.sha }}" {
					t.Errorf("relay checkout does not check out needs.prepare.outputs.sha: %v", step["with"])
				}
			}
		}
		if run, ok := step["run"].(string); ok && strings.Contains(run, "scripts/") && !checkedOut {
			t.Errorf("relay step %d runs a repository script before checkout", i)
		}
	}

	deployStep := obj(t, steps[len(steps)-1])
	deployEnv := obj(t, deployStep["env"])
	if deployEnv["CLOUDFLARE_ACCOUNT_ID"] != "${{ secrets.CLOUDFLARE_ACCOUNT_ID }}" ||
		deployEnv["CLOUDFLARE_API_TOKEN"] != "${{ secrets.CLOUDFLARE_API_TOKEN }}" {
		t.Fatalf("relay deploy step does not read Cloudflare secrets through env: %v", deployEnv)
	}
	if deployEnv["CODEAF_TELEMETRY"] != "off" || deployEnv["DO_NOT_TRACK"] != "1" {
		t.Fatalf("relay deploy step must disable telemetry: %v", deployEnv)
	}

	// Neither the workflow nor the relay scripts may trace commands or echo secrets.
	secretLeak := regexp.MustCompile(`(?m)(^\s*set\s+-[A-Za-z]*x|echo\s+.*\$\{?CLOUDFLARE_(?:API_TOKEN|ACCOUNT_ID))`)
	for _, rel := range []string{
		".github/workflows/release.yml",
		"scripts/relay-deploy.sh",
		"scripts/relay-wrangler-config.sh",
		"scripts/relay-fingerprint.sh",
	} {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		if secretLeak.Match(raw) {
			t.Errorf("%s traces commands or echoes Cloudflare secrets", rel)
		}
	}

	// Both the workflow comment and the deploy script warn that a rollback
	// cannot undo a one-way storage upgrade.
	rawDeploy, err := os.ReadFile(filepath.Join(root, "scripts", "relay-deploy.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"release.yml":     workflowText,
		"relay-deploy.sh": string(rawDeploy),
	} {
		flat := strings.Join(strings.Fields(strings.ReplaceAll(content, "#", " ")), " ")
		if !strings.Contains(flat, "rollback cannot undo a one-way storage upgrade") {
			t.Errorf("%s lacks the one-way storage upgrade rollback explanation", name)
		}
	}
}

func TestRelayProductionSwitchDefaultsOffAndDevStagingHaveNoSwitch(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not on PATH")
	}
	doc := workflowDocument(t, "release.yml")
	relay := obj(t, obj(t, doc["jobs"])["relay"])
	gateStep := obj(t, list(t, relay["steps"])[0])
	gateEnv := obj(t, gateStep["env"])
	if gateEnv["CHANNEL"] != "${{ needs.prepare.outputs.channel }}" ||
		gateEnv["RELAY_DEPLOY_PRODUCTION"] != "${{ vars.RELAY_DEPLOY_PRODUCTION }}" {
		t.Fatalf("gate env = %v", gateEnv)
	}
	gateScript := stringField(t, gateStep["run"])

	for _, channel := range []string{"stable", "rc", "staging", "dev"} {
		for _, variable := range []string{"", "false", "TRUE", "0", "1", "true"} {
			t.Run(channel+"/"+variable, func(t *testing.T) {
				outFile := filepath.Join(t.TempDir(), "github_output")
				cmd := exec.Command("bash", "-c", gateScript)
				cmd.Env = append(os.Environ(),
					"CHANNEL="+channel,
					"RELAY_DEPLOY_PRODUCTION="+variable,
					"GITHUB_OUTPUT="+outFile,
				)
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("gate failed for %s/%q: %v\n%s", channel, variable, err, out)
				}
				rawOut, err := os.ReadFile(outFile)
				if err != nil {
					t.Fatal(err)
				}
				productionChannel := channel == "stable" || channel == "rc"
				wantDeploy := !productionChannel || variable == "true"
				if wantDeploy {
					if strings.TrimSpace(string(rawOut)) != "deploy=true" {
						t.Fatalf("output = %q, want deploy=true", rawOut)
					}
					if strings.TrimSpace(string(out)) != "" {
						t.Fatalf("deploy-enabled gate printed unexpected output: %q", out)
					}
					return
				}
				if strings.TrimSpace(string(rawOut)) != "deploy=false" {
					t.Fatalf("output = %q, want deploy=false", rawOut)
				}
				lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
				if len(lines) != 1 || !strings.Contains(lines[0], "RELAY_DEPLOY_PRODUCTION=true") {
					t.Fatalf("production-off gate must print one line explaining how to enable it, got %q", out)
				}
			})
		}
	}
}

func TestRelayPipelineTouchesOnlyChannelFabricWorkers(t *testing.T) {
	root := repositoryRoot(t)
	forbidden := []string{"caf-relay-staging", "caf-relay-a", "caf-relay-c", "codeaf-pool"}
	checked := []string{
		".github/workflows/release.yml",
		"scripts/relay-wrangler-config.sh",
		"scripts/relay-deploy.sh",
		"scripts/relay-fingerprint.sh",
	}
	for _, rel := range checked {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for _, bad := range forbidden {
			if strings.Contains(text, bad) {
				t.Errorf("%s names forbidden Worker %q", rel, bad)
			}
		}
	}

	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not on PATH")
	}
	gen := filepath.Join(root, "scripts", "relay-wrangler-config.sh")
	wantWorker := map[string]string{
		"stable":  "caf-fabric",
		"rc":      "caf-fabric",
		"staging": "caf-fabric-staging",
		"dev":     "caf-fabric-dev",
	}
	nameLine := regexp.MustCompile(`(?m)^name = "([^"]+)"$`)
	for channel, want := range wantWorker {
		cmd := exec.Command("bash", gen, channel)
		cmd.Env = append(os.Environ(), "CLOUDFLARE_ACCOUNT_ID=acct_test_0123456789")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("relay-wrangler-config.sh %s: %v\n%s", channel, err, out)
		}
		match := nameLine.FindSubmatch(out)
		if match == nil || string(match[1]) != want {
			t.Fatalf("channel %s generated name %q, want %q", channel, match, want)
		}
		for _, bad := range forbidden {
			if strings.Contains(string(out), bad) {
				t.Errorf("generated config for %s names forbidden Worker %q", channel, bad)
			}
		}
	}
}
