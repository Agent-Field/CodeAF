package factory

import (
	"strings"
	"testing"
)

func TestPublicExportPaths(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`cd /Users/private-person/.codeaf/v3/factory/work/repo-59/agents/go && go test ./... -count=1`, `cd ./agents/go && go test ./... -count=1`},
		{`cd '/home/private-person/.codeaf/v3/factory/work/repo-59/with space' && go test ./...`, `cd './with space' && go test ./...`},
		{`log at /Users/private-person/Desktop/run.log`, `log at [local home]/Desktop/run.log`},
		{`log C:\Users\private-person\logs\run`, `log [local home]\logs\run`},
		{`https://example.com/home/someone/factory/work/job/file`, `https://example.com/home/someone/factory/work/job/file`},
		{`log "/Users/private person/Desktop/file"`, `log "[local home]/Desktop/file"`},
		{`go test ./... -run '^TestPrivate$'`, `go test ./... -run '^TestPrivate$'`},
		{`go test ./internal/users/... ./src/home/... ./pkg/root/...`, `go test ./internal/users/... ./src/home/... ./pkg/root/...`},
		{`npm test -- app/Users/list.test.ts web/home/page.test.tsx`, `npm test -- app/Users/list.test.ts web/home/page.test.tsx`},
		{`go test ./tmp/factory/work/fixture/...`, `go test ./tmp/factory/work/fixture/...`},
		{`--dir=/home/private-person/src cd /root/x`, `--dir=[local home]/src cd [local home]/x`},
		{`/Users/private-person/notes`, `[local home]/notes`},
		{`Fix the /users/ endpoint and GET /users/:id or /home/{tab}`, `Fix the /users/ endpoint and GET /users/:id or /home/{tab}`},
		{`ran in /home/private-person and passed`, `ran in [local home] and passed`},
		{`cc -I/Users/private-person/include -L/home/private-person/lib`, `cc -I[local home]/include -L[local home]/lib`},
		{`open file:///Users/private-person/report.html`, `open file://[local home]/report.html`},
		{`go test -run=TestX ./internal/users/...`, `go test -run=TestX ./internal/users/...`},
	} {
		if got := PublicText(tc.in); got != tc.want {
			t.Errorf("got %q want %q", got, tc.want)
		}
	}
	note := `Command receipt: {"command":"cd /home/private-person/.codeaf/v3/factory/work/repo/module && go test ./...","outcome":"passed"}`
	it := Item{Title: "path /home/private-person/project", Kind: KindIssue, Num: 59, Stream: &Stream{Phases: []Phase{{Name: "proof", Result: &StageResult{Notes: []string{note}, PublishSummary: []string{"Fixed /home/private-person/project."}}}}}, Proof: []Claim{{OK: true, Text: "Check /home/private-person/log"}}}
	body := PublishBody(it, "See /home/private-person/trailer")
	if strings.Contains(body, "private-person") || !strings.Contains(body, "cd ./module && go test ./...") {
		t.Fatal(body)
	}
	if it.Stream.Phases[0].Result.Notes[0] != note {
		t.Fatal("private receipt mutated")
	}
}
