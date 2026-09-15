package release

import (
	"os"
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
		"publish": "    if: always() && needs.prepare.result == 'success' && needs.build.result == 'success'",
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
		`skip="$(awk '!/^#/ && NF {print}' .github/known-red.txt 2>/dev/null | paste -sd'|' -)"`,
		`test_args=()`,
		`go test "${test_args[@]}"`,
	} {
		if !strings.Contains(workflow, source) {
			t.Errorf("the release-surface test is not safe for an empty known-red ledger: missing %q", source)
		}
	}
}

func TestPublishedPinExamplesGiveVersionToBash(t *testing.T) {
	root := repositoryRoot(t)
	paths := []string{
		filepath.Join(root, ".github", "workflows", "release.yml"),
		filepath.Join(root, "README.md"),
		filepath.Join(root, "docs", "rules", "promotion.md"),
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
