package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/sample"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestShortenClause(t *testing.T) {
	for _, tt := range []struct {
		name, text, want string
		limit            int
	}{
		{"short", "Deal · name", "Deal · name", 200},
		{"semicolon before comma", "Deal; detail, more words", "Deal", 20},
		{"dash", "Request — approval details", "Request", 20},
		{"comma", "Vendor, contact and renewal details", "Vendor", 25},
		{"and", "Approvals and finance sign off", "Approvals", 20},
		{"word", "Renewal dates coming soon", "Renewal dates", 16},
		{"complete word", "Renewal dates coming soon", "Renewal dates", 13},
		{"unicode", "企業名 — 更新日と詳細", "企業名", 8},
		{"unbroken", "abcdefghijk", "abcde", 5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := shortenClause(tt.text, tt.limit); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPlanLengthSafetyNetBeforeRealCheck(t *testing.T) {
	for _, length := range []int{259, 215} {
		for _, group := range []string{"data", "automation", "screens"} {
			t.Run(fmt.Sprintf("%s/%d", group, length), func(t *testing.T) {
				r := newRig(t)
				files, _ := spec.GoldenCRM()
				files[spec.AgentFile] = []byte(strings.ReplaceAll(string(files[spec.AgentFile]), "records.update, gmail", "records.update, records.delete, gmail"))
				var p spec.Plan
				_ = json.Unmarshal(files[spec.PlanFile], &p)
				lines := map[string][]spec.PlanLine{"data": p.Data, "automation": p.Automation, "screens": p.Screens}
				short := "A useful summary of the owner's work"
				long := short + "; " + strings.Repeat("detail ", 40)
				long = long[:length]
				lines[group][0].Line = long
				files[spec.PlanFile], _ = json.Marshal(p)
				if issues := spec.Validate(files); len(issues) != 1 || !strings.Contains(issues[0].Message, fmt.Sprint(length)) {
					t.Fatalf("expected the original length failure, got %+v", issues)
				}
				r.svc.all.Check = check.New(svc.Config{}, &svc.Services{Sample: sample.New(svc.Config{}, nil)})
				b := &build{app: "crm", plan: planFromSpec(p)}
				report, err := r.svc.checkRound(context.Background(), b, files, 1)
				if err != nil || !report.Pass {
					t.Fatalf("Check after shortening: %+v %v", report, err)
				}
				_ = json.Unmarshal(files[spec.PlanFile], &p)
				got := map[string][]spec.PlanLine{"data": p.Data, "automation": p.Automation, "screens": p.Screens}[group][0].Line
				if got != short || p.Ask == "" {
					t.Fatalf("plan lost summary or ask: %+v", p)
				}
				wantLog := fmt.Sprintf("plan.json:/%s/0/line from %d to %d characters (limit %d)", group, length, utf8.RuneCountInString(short), planLineLimit())
				events := r.hub.events()
				if events[0].Step != bridge.StepPlan || !strings.Contains(events[0].Log, wantLog) {
					t.Fatalf("missing plan log before Check: %+v", events)
				}
				_, saved := r.store.current()
				if string(saved[spec.PlanFile]) != string(files[spec.PlanFile]) {
					t.Fatal("shortened plan not saved")
				}
				before := len(events)
				if err := r.svc.shortenPlanLines(context.Background(), b, files); err != nil || len(r.hub.events()) != before {
					t.Fatal("shortening must be idempotent")
				}
			})
		}
	}
}

func TestMaxLengthHintReachesFix(t *testing.T) {
	for _, tt := range []struct {
		file, path    string
		limit, length int
	}{
		{spec.PlanFile, "/data/1/line", 200, 259},
		{spec.PlanFile, "/screens/0/line", 200, 215},
		{spec.AppFile, "/name", 60, 81},
		{"screens/deals.json", "/title", 80, 95},
	} {
		t.Run(tt.file+tt.path, func(t *testing.T) {
			r := newRig(t)
			files, _ := spec.GoldenCRM()
			r.model.fixes = []string{goldenFiles([]string{spec.AppFile})}
			report := bridge.CheckReport{Issues: []spec.Issue{{File: tt.file, Path: tt.path, Rule: "schema", Message: fmt.Sprintf("length must be <= %d, but got %d", tt.limit, tt.length)}}}
			b := &build{app: "crm", plan: planFromSpec(mustPlan(t, files)), budget: 20}
			if _, err := r.svc.fixOnly(context.Background(), b, files, formatReport(report), ""); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"fix: shorten " + tt.file + ":" + tt.path, fmt.Sprintf("at most %d characters", tt.limit), fmt.Sprintf("current length %d", tt.length)} {
				if !strings.Contains(r.model.prompts[0], want) {
					t.Errorf("FIX lacks %q", want)
				}
			}
		})
	}
}

func mustPlan(t *testing.T, files spec.Files) spec.Plan {
	t.Helper()
	var p spec.Plan
	if err := json.Unmarshal(files[spec.PlanFile], &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSafetyNetPreservesOtherPlanErrors(t *testing.T) {
	r := newRig(t)
	files, _ := spec.GoldenCRM()
	var p map[string]any
	_ = json.Unmarshal(files[spec.PlanFile], &p)
	p["extra"] = "Check must still catch this"
	p["data"].([]any)[0].(map[string]any)["line"] = "Deal; " + strings.Repeat("detail ", 40)
	files[spec.PlanFile], _ = json.Marshal(p)
	if err := r.svc.shortenPlanLines(context.Background(), &build{app: "crm"}, files); err != nil {
		t.Fatal(err)
	}
	issues := spec.Validate(files)
	if len(issues) != 1 || !strings.Contains(issues[0].Message, "extra") {
		t.Fatalf("unrelated error lost: %+v", issues)
	}
}

func TestLengthHintIgnoresOtherIssues(t *testing.T) {
	for _, issue := range []spec.Issue{{Rule: "schema", Message: "missing properties"}, {Rule: "kit/label", Message: "length must be <= 20, but got 25"}} {
		if got := lengthHint(issue); got != "" {
			t.Fatal(got)
		}
	}
}
