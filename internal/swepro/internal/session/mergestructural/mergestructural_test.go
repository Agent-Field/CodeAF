package mergestructural

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type structuralFunc func(Merge3Options) (Merge3Result, error)

func (f structuralFunc) Merge3(opts Merge3Options) (Merge3Result, error) {
	return f(opts)
}

func stringPtr(s string) *string { return &s }

func outPathFromArgs(t *testing.T, args []string) string {
	t.Helper()
	for i, arg := range args {
		if arg == "-o" && i+1 < len(args) {
			return args[i+1]
		}
	}
	t.Fatal("no -o in args")
	return ""
}

func TestCreateStructuralMergeAdapter(t *testing.T) {
	t.Run("clean result and driver convention arguments", func(t *testing.T) {
		var gotArgs []string
		var gotCWD string
		var tempRoot string
		adapter := CreateStructuralMerge(CreateStructuralMergeOptions{
			Binary: " weave-driver ",
			Exec: func(args []string, cwd string) (MergeExecResult, error) {
				gotArgs = append([]string{}, args...)
				gotCWD = cwd
				out := outPathFromArgs(t, args)
				tempRoot = filepath.Dir(out)
				if err := os.WriteFile(out, []byte("merged clean content\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				return MergeExecResult{
					Stdout: "weave: 2 entities auto-resolved", ExitCode: 0,
				}, nil
			},
		})
		if adapter == nil {
			t.Fatal("expected adapter")
		}
		result, err := adapter.Merge3(Merge3Options{
			BasePath: "/tmp/base.ts", LeftPath: "/work/left.ts",
			RightPath: "/tmp/right.ts", CWD: "/work",
		})
		if tempRoot != "" {
			t.Cleanup(func() { _ = os.RemoveAll(tempRoot) })
		}
		if err != nil {
			t.Fatal(err)
		}
		if result != (Merge3Result{Status: StatusClean, Merged: "merged clean content\n"}) {
			t.Fatalf("unexpected result: %#v", result)
		}
		if _, err := os.Stat(tempRoot); err != nil {
			t.Fatalf("TS leaves its implicit temp directory behind: %v", err)
		}
		if gotCWD != "/work" {
			t.Fatalf("cwd = %q", gotCWD)
		}
		if !reflect.DeepEqual(gotArgs[:3], []string{"/tmp/base.ts", "/work/left.ts", "/tmp/right.ts"}) {
			t.Fatalf("leading args = %#v", gotArgs[:3])
		}
		if !reflect.DeepEqual(gotArgs[len(gotArgs)-2:], []string{"-p", "left.ts"}) {
			t.Fatalf("logical args = %#v", gotArgs[len(gotArgs)-2:])
		}
	})

	t.Run("explicit empty logical path is not defaulted", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "out")
		var gotArgs []string
		adapter := CreateStructuralMerge(CreateStructuralMergeOptions{
			Binary: "weave-driver",
			Exec: func(args []string, _ string) (MergeExecResult, error) {
				gotArgs = append([]string{}, args...)
				if err := os.WriteFile(out, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				return MergeExecResult{}, nil
			},
		})
		empty := ""
		result, err := adapter.Merge3(Merge3Options{
			BasePath: "b", LeftPath: "left.ts", RightPath: "r",
			OutPath: &out, LogicalPath: &empty, CWD: "/work",
		})
		if err != nil || result.Status != StatusClean {
			t.Fatalf("result=%#v err=%v", result, err)
		}
		if got := gotArgs[len(gotArgs)-1]; got != "" {
			t.Fatalf("logical path defaulted: %q", got)
		}
	})

	t.Run("empty left path defaults to node empty basename", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "out")
		var gotArgs []string
		adapter := CreateStructuralMerge(CreateStructuralMergeOptions{
			Binary: "weave-driver",
			Exec: func(args []string, _ string) (MergeExecResult, error) {
				gotArgs = append([]string{}, args...)
				if err := os.WriteFile(out, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				return MergeExecResult{}, nil
			},
		})
		if _, err := adapter.Merge3(Merge3Options{
			BasePath: "b", LeftPath: "", RightPath: "r", OutPath: &out, CWD: "/work",
		}); err != nil {
			t.Fatal(err)
		}
		if got := gotArgs[len(gotArgs)-1]; got != "" {
			t.Fatalf("logical basename = %q, want empty", got)
		}
	})

	t.Run("exit one counts markers and floors at one", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			merged string
			want   int
		}{
			{"two", "<<<<<<< ours\nx\n<<<<<<< second\n", 2},
			{"no markers", "weave reported a conflict without markers", 1},
			{"indented marker does not count", " <<<<<<< ours\n", 1},
		} {
			t.Run(tc.name, func(t *testing.T) {
				out := filepath.Join(t.TempDir(), "out")
				adapter := CreateStructuralMerge(CreateStructuralMergeOptions{
					Binary: "weave-driver",
					Exec: func(_ []string, _ string) (MergeExecResult, error) {
						if err := os.WriteFile(out, []byte(tc.merged), 0o600); err != nil {
							t.Fatal(err)
						}
						return MergeExecResult{ExitCode: 1}, nil
					},
				})
				got, err := adapter.Merge3(Merge3Options{
					BasePath: "b", LeftPath: "l.ts", RightPath: "r",
					OutPath: &out, CWD: "/work",
				})
				if err != nil {
					t.Fatal(err)
				}
				if got.Status != StatusConflicts || got.Merged != tc.merged || got.ConflictCount != tc.want {
					t.Fatalf("result = %#v", got)
				}
			})
		}
	})

	t.Run("tool failures degrade to fallback", func(t *testing.T) {
		cases := []struct {
			name string
			exec MergeExec
			want string
		}{
			{
				name: "stderr wins",
				exec: func(_ []string, _ string) (MergeExecResult, error) {
					return MergeExecResult{
						Stdout: "ignored", Stderr: "  unsupported language \n", ExitCode: 2,
					}, nil
				},
				want: "weave-driver failed (exit 2): unsupported language",
			},
			{
				name: "stdout fallback",
				exec: func(_ []string, _ string) (MergeExecResult, error) {
					return MergeExecResult{Stdout: " detail ", ExitCode: 9}, nil
				},
				want: "weave-driver failed (exit 9): detail",
			},
			{
				name: "no output fallback",
				exec: func(_ []string, _ string) (MergeExecResult, error) {
					return MergeExecResult{ExitCode: 127}, nil
				},
				want: "weave-driver failed (exit 127): no output",
			},
			{
				name: "rejected custom exec",
				exec: func(_ []string, _ string) (MergeExecResult, error) {
					return MergeExecResult{}, errors.New("spawn failure")
				},
				want: "spawn failure",
			},
			{
				name: "throwing custom exec",
				exec: func(_ []string, _ string) (MergeExecResult, error) {
					panic("synchronous throw")
				},
				want: "synchronous throw",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				out := filepath.Join(t.TempDir(), "unused")
				adapter := CreateStructuralMerge(CreateStructuralMergeOptions{
					Binary: "weave-driver", Exec: tc.exec,
				})
				got, err := adapter.Merge3(Merge3Options{
					BasePath: "b", LeftPath: "l", RightPath: "r",
					OutPath: &out, CWD: "/work",
				})
				if err != nil {
					t.Fatal(err)
				}
				if got != (Merge3Result{Status: StatusFallback, Detail: tc.want}) {
					t.Fatalf("result = %#v", got)
				}
			})
		}
	})

	t.Run("accepted exit with unreadable output degrades", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "never-written")
		adapter := CreateStructuralMerge(CreateStructuralMergeOptions{
			Binary: "weave-driver",
			Exec: func(_ []string, _ string) (MergeExecResult, error) {
				return MergeExecResult{ExitCode: 0}, nil
			},
		})
		got, err := adapter.Merge3(Merge3Options{
			BasePath: "b", LeftPath: "l", RightPath: "r",
			OutPath: &out, CWD: "/work",
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != StatusFallback ||
			!strings.Contains(got.Detail, "weave-driver exit 0 but output unreadable: ENOENT") {
			t.Fatalf("result = %#v", got)
		}
	})
}

func TestMerge3ResultJSONShapesDoNotHTMLEscape(t *testing.T) {
	for _, tc := range []struct {
		result Merge3Result
		want   string
	}{
		{
			Merge3Result{Status: StatusClean, Merged: "<merged>&"},
			`{"status":"clean","merged":"<merged>&"}`,
		},
		{
			Merge3Result{Status: StatusConflicts, Merged: "<<<<<<<", ConflictCount: 2},
			`{"status":"conflicts","merged":"<<<<<<<","conflictCount":2}`,
		},
		{
			Merge3Result{Status: StatusFallback, Detail: "bad <driver>"},
			`{"status":"fallback","detail":"bad <driver>"}`,
		},
	} {
		got, err := tc.result.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != tc.want {
			t.Fatalf("got %s, want %s", got, tc.want)
		}
	}
}

func TestResolveConflictedFilesStructurallyFailSafe(t *testing.T) {
	clean := structuralFunc(func(Merge3Options) (Merge3Result, error) {
		return Merge3Result{Status: StatusClean, Merged: "x"}, nil
	})

	t.Run("empty input is a nonnil empty result", func(t *testing.T) {
		got, err := ResolveConflictedFilesStructurally(ResolveOptions{
			Dir: "/nowhere", ConflictedFiles: []string{"", ""},
		})
		if err != nil {
			t.Fatal(err)
		}
		want := StructuralConflictOutcome{
			Resolved: []string{}, Remaining: []string{}, Details: []StructuralConflictDetail{},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})

	t.Run("kill switch escalates without invoking adapter", func(t *testing.T) {
		t.Setenv("CODEAF_STRUCTURAL_MERGE", "0")
		invoked := false
		got, err := ResolveConflictedFilesStructurally(ResolveOptions{
			Dir: "/nowhere", ConflictedFiles: []string{"a.ts", "", "b.py"},
			StructuralSet: true,
			Structural: structuralFunc(func(Merge3Options) (Merge3Result, error) {
				invoked = true
				return Merge3Result{Status: StatusClean, Merged: "x"}, nil
			}),
		})
		if err != nil {
			t.Fatal(err)
		}
		if invoked || !reflect.DeepEqual(got.Remaining, []string{"a.ts", "b.py"}) {
			t.Fatalf("invoked=%v outcome=%#v", invoked, got)
		}
		for _, detail := range got.Details {
			if detail.Reason != "disabled via CODEAF_STRUCTURAL_MERGE=0" {
				t.Fatalf("detail = %#v", detail)
			}
		}
	})

	t.Run("forced missing adapter escalates", func(t *testing.T) {
		t.Setenv("CODEAF_STRUCTURAL_MERGE", "1")
		got, err := ResolveConflictedFilesStructurally(ResolveOptions{
			Dir: "/nowhere", ConflictedFiles: []string{"a.ts"}, StructuralSet: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.Details[0].Reason != "weave-driver binary unavailable" {
			t.Fatalf("outcome = %#v", got)
		}
	})

	t.Run("unsupported paths do not call git or weave", func(t *testing.T) {
		t.Setenv("CODEAF_STRUCTURAL_MERGE", "1")
		called := false
		got, err := ResolveConflictedFilesStructurally(ResolveOptions{
			Dir: "/nowhere", ConflictedFiles: []string{"README.txt", "data.json"},
			StructuralSet: true, Structural: clean,
			Git: func([]string, string) (GitResult, error) {
				called = true
				return GitResult{}, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if called || !reflect.DeepEqual(got.Remaining, []string{"README.txt", "data.json"}) {
			t.Fatalf("called=%v outcome=%#v", called, got)
		}
		for _, detail := range got.Details {
			if detail.Reason != "unsupported language" {
				t.Fatalf("detail = %#v", detail)
			}
		}
	})

	t.Run("all stages are requested before missing-stage escalation", func(t *testing.T) {
		t.Setenv("CODEAF_STRUCTURAL_MERGE", "1")
		var calls [][]string
		got, err := ResolveConflictedFilesStructurally(ResolveOptions{
			Dir: "/repo", ConflictedFiles: []string{"a.ts"},
			StructuralSet: true, Structural: clean,
			Git: func(argv []string, _ string) (GitResult, error) {
				calls = append(calls, append([]string{}, argv...))
				if strings.HasPrefix(argv[2], ":2:") {
					return GitResult{Code: 1}, nil
				}
				return GitResult{Code: 0, Stdout: []byte("stage")}, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(calls) != 3 {
			t.Fatalf("git calls = %#v", calls)
		}
		if got.Details[0].Reason != "missing merge stage (add/add or delete/modify)" {
			t.Fatalf("outcome = %#v", got)
		}
	})
}

func TestResolveConflictedFilesStructurallyOutcomes(t *testing.T) {
	t.Setenv("CODEAF_STRUCTURAL_MERGE", "1")

	type scenario struct {
		name       string
		result     Merge3Result
		mergeErr   error
		panicValue any
		wantReason string
	}
	for _, tc := range []scenario{
		{
			name: "structural conflict",
			result: Merge3Result{
				Status: StatusConflicts, Merged: "<<<<<<<", ConflictCount: 1,
			},
			wantReason: "structural conflict",
		},
		{
			name: "fallback",
			result: Merge3Result{
				Status: StatusFallback, Detail: "driver unavailable",
			},
			wantReason: "fallback: driver unavailable",
		},
		{
			name: "defensive marker gate",
			result: Merge3Result{
				Status: StatusClean, Merged: "clean?\n>>>>>>> theirs",
			},
			wantReason: "clean status but markers present",
		},
		{
			name:       "rejected merge",
			mergeErr:   errors.New("adapter exploded"),
			wantReason: "error: adapter exploded",
		},
		{
			name:       "throwing merge",
			panicValue: "adapter threw",
			wantReason: "error: adapter threw",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "a.ts"), []byte("conflicted"), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := ResolveConflictedFilesStructurally(ResolveOptions{
				Dir: dir, ConflictedFiles: []string{"a.ts"}, StructuralSet: true,
				Structural: structuralFunc(func(Merge3Options) (Merge3Result, error) {
					if tc.panicValue != nil {
						panic(tc.panicValue)
					}
					return tc.result, tc.mergeErr
				}),
				Git: func(argv []string, _ string) (GitResult, error) {
					if argv[1] != "show" {
						t.Fatalf("unexpected git call %#v", argv)
					}
					return GitResult{Code: 0, Stdout: []byte("stage")}, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Resolved) != 0 || got.Details[0].Reason != tc.wantReason {
				t.Fatalf("outcome = %#v", got)
			}
			if raw, err := os.ReadFile(filepath.Join(dir, "a.ts")); err != nil || string(raw) != "conflicted" {
				t.Fatalf("working file changed: %q, %v", raw, err)
			}
		})
	}
}

func TestResolveConflictedFilesStructurallyStagesResolution(t *testing.T) {
	t.Setenv("CODEAF_STRUCTURAL_MERGE", "1")
	dir := t.TempDir()
	file := filepath.Join(dir, "src", "a.ts")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("conflicted"), 0o600); err != nil {
		t.Fatal(err)
	}

	var calls [][]string
	var mergeOpts Merge3Options
	var logEvent string
	var logMeta StructuralLogMeta
	got, err := ResolveConflictedFilesStructurally(ResolveOptions{
		Dir: dir, ConflictedFiles: []string{"src/a.ts"}, StructuralSet: true,
		Structural: structuralFunc(func(opts Merge3Options) (Merge3Result, error) {
			mergeOpts = opts
			for stage, path := range []string{opts.BasePath, opts.LeftPath, opts.RightPath} {
				raw, err := os.ReadFile(path)
				if err != nil || string(raw) != "stage-"+string(rune('1'+stage)) {
					t.Fatalf("stage %d content=%q err=%v", stage+1, raw, err)
				}
			}
			return Merge3Result{Status: StatusClean, Merged: "merged\n"}, nil
		}),
		Git: func(argv []string, _ string) (GitResult, error) {
			calls = append(calls, append([]string{}, argv...))
			if argv[1] == "show" {
				stage := argv[2][1]
				return GitResult{Code: 0, Stdout: []byte("stage-" + string(stage))}, nil
			}
			return GitResult{Code: 0}, nil
		},
		Log: func(event string, meta StructuralLogMeta) {
			logEvent, logMeta = event, meta
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Resolved, []string{"src/a.ts"}) || len(got.Remaining) != 0 {
		t.Fatalf("outcome = %#v", got)
	}
	if mergeOpts.LogicalPath == nil || *mergeOpts.LogicalPath != "src/a.ts" || mergeOpts.CWD != dir {
		t.Fatalf("merge opts = %#v", mergeOpts)
	}
	if raw, err := os.ReadFile(file); err != nil || string(raw) != "merged\n" {
		t.Fatalf("working file=%q err=%v", raw, err)
	}
	if gotCall := calls[len(calls)-1]; !reflect.DeepEqual(gotCall, []string{"git", "add", "--", "src/a.ts"}) {
		t.Fatalf("last git call = %#v", gotCall)
	}
	if logEvent != "structural merge pass" ||
		logMeta != (StructuralLogMeta{
			Dir: dir, Considered: 1, ResolvedStructurally: 1, Escalated: 0,
		}) {
		t.Fatalf("log event=%q meta=%#v", logEvent, logMeta)
	}
}

func TestResolveConflictedFilesStructurallyRestoresAfterAddFailure(t *testing.T) {
	t.Setenv("CODEAF_STRUCTURAL_MERGE", "1")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.ts"), []byte("conflicted"), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	got, err := ResolveConflictedFilesStructurally(ResolveOptions{
		Dir: dir, ConflictedFiles: []string{"a.ts"}, StructuralSet: true,
		Structural: structuralFunc(func(Merge3Options) (Merge3Result, error) {
			return Merge3Result{Status: StatusClean, Merged: "merged"}, nil
		}),
		Git: func(argv []string, _ string) (GitResult, error) {
			calls = append(calls, append([]string{}, argv...))
			switch argv[1] {
			case "show":
				return GitResult{Code: 0, Stdout: []byte("stage")}, nil
			case "add":
				return GitResult{Code: 1, Stderr: []byte("cannot stage")}, nil
			default:
				return GitResult{Code: 0}, nil
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Details[0].Reason != "git add failed: cannot stage" {
		t.Fatalf("outcome = %#v", got)
	}
	wantLast := []string{"git", "checkout", "--merge", "--", "a.ts"}
	if !reflect.DeepEqual(calls[len(calls)-1], wantLast) {
		t.Fatalf("last call = %#v", calls[len(calls)-1])
	}
}

func TestResolveConflictedFilesStructurallyRestoreRejectionIsCaught(t *testing.T) {
	t.Setenv("CODEAF_STRUCTURAL_MERGE", "1")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.ts"), []byte("conflicted"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveConflictedFilesStructurally(ResolveOptions{
		Dir: dir, ConflictedFiles: []string{"a.ts"}, StructuralSet: true,
		Structural: structuralFunc(func(Merge3Options) (Merge3Result, error) {
			return Merge3Result{Status: StatusClean, Merged: "merged"}, nil
		}),
		Git: func(argv []string, _ string) (GitResult, error) {
			switch argv[1] {
			case "show":
				return GitResult{Code: 0, Stdout: []byte("stage")}, nil
			case "add":
				return GitResult{Code: 1, Stderr: []byte("cannot stage")}, nil
			default:
				return GitResult{}, errors.New("restore rejected")
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Details[0].Reason != "error: restore rejected" {
		t.Fatalf("outcome = %#v", got)
	}
}

func TestDefaultGitRunnerEndToEndStagesCleanResult(t *testing.T) {
	t.Setenv("CODEAF_STRUCTURAL_MERGE", "1")
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	git("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.ts"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "a.ts")
	git("commit", "-q", "-m", "base")
	git("checkout", "-q", "-b", "ours")
	if err := os.WriteFile(filepath.Join(dir, "a.ts"), []byte("ours\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("commit", "-qam", "ours")
	git("checkout", "-q", "main")
	git("checkout", "-q", "-b", "theirs")
	if err := os.WriteFile(filepath.Join(dir, "a.ts"), []byte("theirs\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("commit", "-qam", "theirs")
	git("checkout", "-q", "ours")
	merge := exec.Command("git", "merge", "--no-commit", "theirs")
	merge.Dir = dir
	merge.Env = append(os.Environ(), "GIT_EDITOR=true")
	if err := merge.Run(); err == nil {
		t.Fatal("expected textual conflict")
	}

	got, err := ResolveConflictedFilesStructurally(ResolveOptions{
		Dir: dir, ConflictedFiles: []string{"a.ts"}, StructuralSet: true,
		Structural: structuralFunc(func(opts Merge3Options) (Merge3Result, error) {
			base, err := os.ReadFile(opts.BasePath)
			if err != nil {
				t.Fatal(err)
			}
			ours, err := os.ReadFile(opts.LeftPath)
			if err != nil {
				t.Fatal(err)
			}
			theirs, err := os.ReadFile(opts.RightPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(base) != "base\n" || string(ours) != "ours\n" || string(theirs) != "theirs\n" {
				t.Fatalf("stages base=%q ours=%q theirs=%q", base, ours, theirs)
			}
			return Merge3Result{Status: StatusClean, Merged: "combined\n"}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Resolved, []string{"a.ts"}) {
		t.Fatalf("outcome = %#v", got)
	}
	cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=U")
	cmd.Dir = dir
	unmerged, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if len(unmerged) != 0 {
		t.Fatalf("still unmerged: %q", unmerged)
	}
}

func TestResolveWeaveBinaryOrder(t *testing.T) {
	if vendoredBinary() != "" && executableFile(vendoredBinary()) {
		t.Skip("repository has a vendored weave binary; it intentionally wins")
	}
	previousCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousCWD) })

	cwd := t.TempDir()
	cwdBinary := filepath.Join(cwd, "vendor", "bin", "weave-driver")
	if err := os.MkdirAll(filepath.Dir(cwdBinary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cwdBinary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pathDir := t.TempDir()
	pathBinary := filepath.Join(pathDir, "weave-driver")
	if err := os.WriteFile(pathBinary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pathDir)
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	if got := ResolveWeaveBinary(); got != cwdBinary {
		t.Fatalf("cwd candidate lost precedence: got %q, want %q", got, cwdBinary)
	}

	if err := os.Chmod(cwdBinary, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ResolveWeaveBinary(); got != pathBinary {
		t.Fatalf("PATH candidate = %q, want %q", got, pathBinary)
	}
	if err := os.Chmod(pathBinary, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ResolveWeaveBinary(); got != "" {
		t.Fatalf("nonexecutable candidate resolved: %q", got)
	}
}
