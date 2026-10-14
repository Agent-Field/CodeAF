package builder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// pointerText is a JSON pointer or a file named with a position inside it.
var pointerText = regexp.MustCompile(`\.(json|yaml|md):|:?/(data|automation|screens|steps|records)/|/\d+/`)

func assertOwnerWords(t *testing.T, events []bridge.BuildEvent) {
	t.Helper()
	for _, ev := range events {
		if pointerText.MatchString(ev.Log) {
			t.Errorf("the person would read a pointer: %q", ev.Log)
		}
	}
}

// captureDebug routes the debug log into a buffer for one test.
func captureDebug(t *testing.T) *bytes.Buffer {
	t.Helper()
	var out bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &out
}

func TestOwnerWords(t *testing.T) {
	budget := bridge.Errorf(bridge.CodeBudget, "the Builder has spent $20.00 of $20.00")
	for _, tt := range []struct {
		name string
		err  error
		want string
	}{
		{"budget", budget, "the Builder has spent $20.00 of $20.00"},
		{"wrapped budget", fmt.Errorf("write: %w", budget), "the Builder has spent $20.00 of $20.00"},
		{"model", spoken{errors.New("write model m: no key")}, "write model m: no key"},
		{"empty reply", spoken{errEmptyReply}, errEmptyReply.Error()},
		{"missing file", errors.New("the reply has no screens/deals.json"), stoppedWords},
		{"parse", errors.New("prompt: screens/deals.json: content is not valid JSON"), stoppedWords},
		{"decode", fmt.Errorf("app.json: %w", &json.SyntaxError{Offset: 14}), stoppedWords},
		{"invalid", &spec.InvalidError{Issues: []spec.Issue{{File: spec.PlanFile, Path: "/data/0/line", Rule: "schema", Message: "too long"}}}, stoppedWords},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := ownerWords(tt.err); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestShortenedWords(t *testing.T) {
	for lines, want := range map[int]string{1: "Shortened one plan line to fit", 3: "Shortened some plan lines to fit"} {
		if got := shortenedWords(lines); got != want {
			t.Errorf("%d lines: got %q, want %q", lines, got, want)
		}
	}
}

func TestSeveralLongLinesAreOneOwnerLine(t *testing.T) {
	r := newRig(t)
	debug := captureDebug(t)
	files, _ := spec.GoldenCRM()
	var p map[string]any
	_ = json.Unmarshal(files[spec.PlanFile], &p)
	long := "Deal; " + strings.Repeat("detail ", 40)
	p["data"].([]any)[0].(map[string]any)["line"] = long
	p["screens"].([]any)[0].(map[string]any)["line"] = long
	files[spec.PlanFile], _ = json.Marshal(p)
	if err := r.svc.shortenPlanLines(context.Background(), &build{app: "crm"}, files); err != nil {
		t.Fatal(err)
	}
	events := r.hub.events()
	if len(events) != 1 || events[0].Log != "Shortened some plan lines to fit" {
		t.Fatalf("events %+v", events)
	}
	for _, path := range []string{"path=/data/0/line", "path=/screens/0/line"} {
		if !strings.Contains(debug.String(), path) {
			t.Errorf("debug log lacks %s", path)
		}
	}
}

func TestFailedStepKeepsTheCauseInTheDebugLog(t *testing.T) {
	r := newRig(t)
	debug := captureDebug(t)
	cause := errors.New("prompt: screens/deals.json: content is not valid JSON")
	r.svc.failed(&build{app: "crm"}, bridge.StepWrite, cause)
	events := r.hub.events()
	if len(events) != 1 || events[0].Status != bridge.BuildFailed || events[0].Log != stoppedWords {
		t.Fatalf("events %+v", events)
	}
	assertOwnerWords(t, events)
	if !strings.Contains(debug.String(), cause.Error()) {
		t.Fatalf("debug log lost the cause: %s", debug.String())
	}
}
