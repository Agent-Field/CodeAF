package merger

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func validDecision() MergerDecision {
	return MergerDecision{
		Result: "resolved", Reason: "combined both changes",
		FilesTouched: []FileTouched{
			{File: "src/a.ts", Summary: "kept both APIs"},
		},
		Unresolved: nil,
	}
}

func minimalInput() Input {
	return Input{
		ParentSessionID: "parent", PromptOps: struct{}{},
		Worktree: "/work", TaskID: "17", TaskTitle: "resolve",
		SourceBranch: "plandb/17", TargetBranch: "main",
		UserGoal: "keep intent", PriorFailure: "conflicted",
	}
}

func quietRunner() Runner {
	return RunnerFunc(func([]string, string) (ProcessResult, error) {
		return ProcessResult{}, nil
	})
}

func TestDispatchMergerReturnsValidDispatcherResult(t *testing.T) {
	want := DispatchResult{Data: validDecision(), FirstTry: true}
	var request DispatchRequest
	got := DispatchMerger(minimalInput(), Dependencies{
		Runner: quietRunner(),
		Dispatcher: DispatcherFunc(func(
			_ context.Context,
			input DispatchRequest,
		) (DispatchResult, error) {
			request = input
			return want, nil
		}),
	})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("result = %#v", got)
	}
	if request.ParentSessionID != "parent" ||
		request.Workspace != "/work" ||
		request.TimeoutMS != 900000 ||
		request.MaxRetries != 1 ||
		!request.Tools.Edit || !request.Tools.ApplyPatch {
		t.Fatalf("request = %#v", request)
	}
}

func TestDispatchMergerFailureTimeoutPanicAndInvalidAllDegradeIdentically(t *testing.T) {
	want := fallbackResult()
	cases := []struct {
		name       string
		dispatcher Dispatcher
		timeout    time.Duration
	}{
		{
			name: "failure",
			dispatcher: DispatcherFunc(func(
				context.Context, DispatchRequest,
			) (DispatchResult, error) {
				return DispatchResult{}, errors.New("failed")
			}),
		},
		{
			name: "panic",
			dispatcher: DispatcherFunc(func(
				context.Context, DispatchRequest,
			) (DispatchResult, error) {
				panic("defect")
			}),
		},
		{
			name:    "timeout",
			timeout: time.Millisecond,
			dispatcher: DispatcherFunc(func(
				ctx context.Context, _ DispatchRequest,
			) (DispatchResult, error) {
				<-ctx.Done()
				return DispatchResult{}, ctx.Err()
			}),
		},
		{
			name: "invalid typed result",
			dispatcher: DispatcherFunc(func(
				context.Context, DispatchRequest,
			) (DispatchResult, error) {
				return DispatchResult{
					Data: MergerDecision{Result: "resolved", Reason: ""},
				}, nil
			}),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DispatchMerger(minimalInput(), Dependencies{
				Runner: quietRunner(), Dispatcher: tc.dispatcher,
				CallTimeout: tc.timeout,
			})
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("result = %#v\nwant %#v", got, want)
			}
		})
	}
}

func TestVerifyMergeResolvedWithRealGrep(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".plandb"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "conflicted.txt"),
		[]byte("<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> branch\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, ".plandb", "report.md"),
		[]byte("=======\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	got := VerifyMergeResolved(root)
	if got.OK || !reflect.DeepEqual(got.RemainingMarkers, []string{"./conflicted.txt"}) {
		t.Fatalf("result = %#v", got)
	}

	if err := os.WriteFile(
		filepath.Join(root, "conflicted.txt"),
		[]byte("resolved\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	got = VerifyMergeResolved(root)
	if !got.OK || len(got.RemainingMarkers) != 0 {
		t.Fatalf("resolved result = %#v", got)
	}
}

func TestVerifyMergeResolvedRunnerPanicFailsOpen(t *testing.T) {
	got := VerifyMergeResolved("/work", RunnerFunc(func([]string, string) (ProcessResult, error) {
		panic("grep missing")
	}))
	if !got.OK || len(got.RemainingMarkers) != 0 {
		t.Fatalf("result = %#v", got)
	}
}

func TestParseDecisionStrictShape(t *testing.T) {
	got, err := ParseDecision([]byte(
		`{"result":"resolved","reason":"ok","files_touched":[],"unresolved":null}`,
	))
	if err != nil || got.Result != "resolved" ||
		got.FilesTouched == nil || got.Unresolved != nil {
		t.Fatalf("decision=%#v err=%v", got, err)
	}
	for _, raw := range []string{
		`{"result":"resolved","reason":"","files_touched":[],"unresolved":null}`,
		`{"result":"other","reason":"x","files_touched":[],"unresolved":null}`,
		`{"result":"resolved","reason":"x","files_touched":[],"unresolved":null,"extra":1}`,
		`{"result":"resolved","reason":"x","files_touched":[{"file":"","summary":"x"}],"unresolved":null}`,
		`{"result":"resolved","reason":"x","files_touched":[{"file":"a","summary":"x","extra":1}],"unresolved":null}`,
	} {
		if _, err := ParseDecision([]byte(raw)); err == nil {
			t.Fatalf("expected invalid: %s", raw)
		}
	}
}

func TestUTF16PromptSliceBoundary(t *testing.T) {
	input := minimalInput()
	input.PriorFailure = strings.Repeat("x", 2999) + "😀tail"
	got := BuildMergerPrompt(input, "", nil, "/out")
	if !strings.Contains(got, strings.Repeat("x", 2999)+"�\n```") {
		t.Fatal("split surrogate was not replaced at Go string boundary")
	}
}
