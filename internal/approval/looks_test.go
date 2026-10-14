package approval

import (
	"encoding/json"
	"testing"
)

// Every look the table names, in the spellings it accepts.
func TestReadOnlyLookAcceptsOnlyTheTable(t *testing.T) {
	looks := []string{
		"gh issue view 12",
		"gh issue view 12 -R o/r --comments",
		"gh issue list --repo o/r --state open -L 5",
		"gh issue list --repo=o/r --json number,title",
		"gh issue status",
		"gh pr view 7 --json title",
		"gh pr view",
		"gh pr list -R o/r --limit 3 --draft",
		"gh pr status",
		"gh pr diff 7 --name-only",
		"gh pr checks 7 --required",
		"gh repo view o/r",
		"gh repo list myorg -L 10",
		"gh release view v1.2.3 -R o/r",
		"gh release list -R o/r",
		"gh run view 123 --log-failed",
		"gh run list -R o/r -L 5 --workflow ci.yml",
		"gh workflow view ci.yml",
		"gh workflow list",
		"gh label list -R o/r",
		"gh api repos/o/r/issues",
		"gh api /repos/o/r/releases --paginate",
		"gh api -X GET repos/o/r",
		"gh api --method GET repos/o/r",
		"gh api --method=GET repos/o/r --jq .name",
		"  gh   issue   list  ",
	}
	for _, line := range looks {
		if family, ok := ReadOnlyLook(line); !ok || family != "gh" {
			t.Errorf("%q is a look and was refused", line)
		}
	}
}

// The adversarial table: writes, credential printers, and every way a write
// can be dressed as a look. NONE may be read-only; each one asks again.
func TestReadOnlyLookRefusesEveryWriteLookalike(t *testing.T) {
	refused := []string{
		// quoting, escaping and expansion: the shell would rewrite the argv
		`gh api repos/o/r "-X" DELETE`,
		`gh api x '-f' a=b`,
		`gh api x -\X POST`,
		`gh api x "--method=DELETE"`,
		"gh api x `id`",
		"gh issue view $(whoami)",
		"gh issue view $ISSUE",
		"gh issue view ${N}",
		"gh issue view ~/x",
		"gh issue view *",
		"gh issue view ?",
		"gh issue view [1]",
		"gh issue view {1,2}",
		"gh issue view 1 # comment",
		"gh issue view !!",
		"gh issue view 1\tx",
		"gh issue view 1\ngh pr merge 1",
		// separators, pipes and redirects
		"gh issue view 12 | sh",
		"gh issue view 12 > out.txt",
		"gh issue view 12 >> out.txt",
		"gh issue view 12 < in.txt",
		"gh issue list && gh pr merge 1",
		"gh issue list ; gh pr merge 1",
		"gh issue list & gh pr merge 1",
		"gh issue list || gh pr merge 1",
		"(gh issue list)",
		// environment prefixes and other programs
		"GH_HOST=evil.example gh issue list",
		"GH_TOKEN=x gh issue list",
		"env gh issue list",
		"sudo gh issue list",
		"command gh issue list",
		"/tmp/gh issue list",
		"./gh issue list",
		"gh",
		"hub issue list",
		// gh api: methods, bodies, headers, clusters, graphql, other hosts
		"gh api -X POST repos/o/r/issues",
		"gh api -XPOST repos/o/r/issues",
		"gh api -XGET repos/o/r",
		"gh api -X get repos/o/r",
		"gh api -X",
		"gh api --method PATCH repos/o/r",
		"gh api --method=DELETE repos/o/r",
		"gh api --method",
		"gh api repos/o/r -X DELETE",
		"gh api repos/o/r -iX POST",
		"gh api repos/o/r -if a=b",
		"gh api repos/o/r/issues -f title=x",
		"gh api repos/o/r/issues -F title=x",
		"gh api repos/o/r/issues -Ftitle=x",
		"gh api repos/o/r/issues --field title=x",
		"gh api repos/o/r/issues --raw-field title=x",
		"gh api repos/o/r --input body.json",
		"gh api repos/o/r --input=-",
		"gh api repos/o/r -H X-HTTP-Method-Override:DELETE",
		"gh api repos/o/r --header X-HTTP-Method-Override:DELETE",
		"gh api repos/o/r --hostname evil.example",
		"gh api graphql -f query=q",
		"gh api graphql",
		"gh api /graphql",
		"gh api GraphQL",
		"gh api https://evil.example/x",
		"gh api repos/o/r repos/o/s",
		"gh api",
		"gh api -- repos/o/r",
		"gh api --paginate=true repos/o/r",
		// gh subcommands that write, run, download or print credentials
		"gh alias set co pr-checkout",
		"gh alias list",
		"gh extension install o/r",
		"gh extension exec x",
		"gh auth token",
		"gh auth status --show-token",
		"gh auth status",
		"gh auth login",
		"gh secret set X",
		"gh secret list",
		"gh variable set X",
		"gh repo delete o/r",
		"gh repo clone o/r",
		"gh repo fork o/r",
		"gh repo sync",
		"gh repo create x",
		"gh repo edit o/r --visibility public",
		"gh pr merge 7",
		"gh pr checkout 7",
		"gh pr close 7",
		"gh pr create --fill",
		"gh pr review 7 --approve",
		"gh pr comment 7 -b hi",
		"gh pr edit 7 --title x",
		"gh pr ready 7",
		"gh issue create -t x",
		"gh issue close 12",
		"gh issue comment 12 -b hi",
		"gh issue edit 12 --title x",
		"gh issue delete 12",
		"gh release download v1",
		"gh release create v1",
		"gh release delete v1",
		"gh run rerun 1",
		"gh run cancel 1",
		"gh run download 1",
		"gh run watch 1",
		"gh workflow run ci.yml",
		"gh workflow disable ci.yml",
		"gh gist create f.txt",
		"gh gist view abc",
		"gh codespace ssh",
		"gh browse",
		"gh config set editor vim",
		"gh label create bug",
		"gh search issues x",
		"gh status",
		// look verbs with a flag or shape the table does not name
		"gh issue view 12 --web",
		"gh issue view 12 -w",
		"gh pr view 7 -cw",
		"gh pr checks 7 --watch",
		"gh pr diff 7 --web",
		"gh issue view",
		"gh issue view 1 2",
		"gh issue list extra",
		"gh issue list --state",
		"gh issue list --state --web",
		"gh issue list --draft=true",
		"gh issue list -Ro/r",
		"gh issue list -R=o/r",
		"gh issue list --unknown x",
		"gh issue list -- --web",
		"gh issue",
		"gh pr",
		// not gh at all: other account CLIs are never a look
		"aws ec2 describe-instances",
		"aws s3 ls s3://b",
		"aws iam list-users",
		"aws secretsmanager get-secret-value --secret-id x",
		"aws s3api get-object --bucket b --key k out",
		"gcloud compute instances list",
		"gcloud run services describe api",
		"gcloud secrets versions access latest --secret x",
		"glab issue list",
		"git status",
		"curl https://x",
		"",
	}
	for _, line := range refused {
		if family, ok := ReadOnlyLook(line); ok || family != "" {
			t.Errorf("%q was classified read-only (%q)", line, family)
		}
	}
}

// Every flag in the table is spelled as a flag, so a typo cannot open a hole
// by naming a positional.
func TestTheLookTableNamesOnlyFlags(t *testing.T) {
	for name, spec := range ghLooks {
		for flag := range spec.flags {
			short := len(flag) == 2 && flag[0] == '-' && flag[1] != '-'
			long := len(flag) > 3 && flag[:2] == "--"
			if !short && !long {
				t.Errorf("%s: %q is not a flag", name, flag)
			}
		}
		if spec.minArgs > spec.maxArgs {
			t.Errorf("%s: args %d..%d", name, spec.minArgs, spec.maxArgs)
		}
	}
}

// A look is off the always-ask floor. If [AlwaysAsks] were true for a plain
// `gh pr view`, a "for this task" yes would be inert: the gate would ask
// again on every call.
func TestALookIsNotOnTheAlwaysAskFloor(t *testing.T) {
	for _, line := range []string{"gh pr view", "gh pr view 7", "gh issue list -R o/r", "gh api repos/o/r/issues"} {
		if _, ok := ReadOnlyLook(line); !ok {
			t.Fatalf("%q is not a look", line)
		}
		raw, _ := json.Marshal(map[string]string{"command": line})
		if AlwaysAsks(ToolBash, raw) {
			t.Errorf("%q is on the always-ask floor", line)
		}
	}
}
