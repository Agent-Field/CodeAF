package fullverification

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func writeDiscoveryFile(t *testing.T, root, relative, body string) {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverPrefersCIEntrypoints(t *testing.T) {
	// Validation contract 3: command discovery follows the TS auditor's
	// prefer-CI convention instead of blindly selecting an ecosystem default.
	workspace := t.TempDir()
	writeDiscoveryFile(t, workspace, "go.mod", "module example.test/ci\n")
	writeDiscoveryFile(t, workspace, ".github/workflows/verify.yml", `
jobs:
  verify:
    steps:
      - run: go build ./cmd/...
      - run: |
          go test -count=1 ./...
`)
	plan := Discover(workspace)
	want := []Entrypoint{
		{Kind: KindBuild, Command: "go build ./cmd/...", Source: ".github/workflows/verify.yml"},
		{Kind: KindTest, Command: "go test -count=1 ./...", Source: ".github/workflows/verify.yml"},
	}
	if !reflect.DeepEqual(plan.Entrypoints, want) {
		t.Fatalf("plan = %#v, want %#v", plan.Entrypoints, want)
	}
}

func TestDiscoverHonorsAgentInstructionsBeforePackageScripts(t *testing.T) {
	// Validation contract 3: repository agent guidance can name the canonical
	// commands and wins over generic package-script detection.
	workspace := t.TempDir()
	writeDiscoveryFile(t, workspace, "AGENTS.md", "Run `npm run compile` and then `npm test -- --runInBand`.\n")
	writeDiscoveryFile(t, workspace, "package.json", `{"scripts":{"build":"vite build","test":"vitest"}}`)
	plan := Discover(workspace)
	want := []Entrypoint{
		{Kind: KindBuild, Command: "npm run compile", Source: "AGENTS.md"},
		{Kind: KindTest, Command: "npm test -- --runInBand", Source: "AGENTS.md"},
	}
	if !reflect.DeepEqual(plan.Entrypoints, want) {
		t.Fatalf("plan = %#v, want %#v", plan.Entrypoints, want)
	}
}

func TestDiscoverPackageManagerScripts(t *testing.T) {
	// Validation contract 3: manifest scripts retain the repository's package
	// manager rather than assuming npm or a language-specific command.
	workspace := t.TempDir()
	writeDiscoveryFile(t, workspace, "package.json", `{"scripts":{"build":"tsc","test":"vitest run"}}`)
	writeDiscoveryFile(t, workspace, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	plan := Discover(workspace)
	want := []Entrypoint{
		{Kind: KindBuild, Command: "pnpm build", Source: "package.json#scripts.build"},
		{Kind: KindTest, Command: "pnpm test", Source: "package.json#scripts.test"},
	}
	if !reflect.DeepEqual(plan.Entrypoints, want) {
		t.Fatalf("plan = %#v, want %#v", plan.Entrypoints, want)
	}
}

func TestDiscoverREADMEEntrypoints(t *testing.T) {
	// Validation contract 3: documented commands remain discoverable when the
	// repository has no CI or declared task-runner scripts.
	workspace := t.TempDir()
	writeDiscoveryFile(t, workspace, "README.md", "# Development\n\n"+
		"Build with `cargo build --all-targets`.\n\n"+
		"Run the suite:\n\n```sh\ncargo test --all-targets\n```\n")
	plan := Discover(workspace)
	want := []Entrypoint{
		{Kind: KindBuild, Command: "cargo build --all-targets", Source: "README.md"},
		{Kind: KindTest, Command: "cargo test --all-targets", Source: "README.md"},
	}
	if !reflect.DeepEqual(plan.Entrypoints, want) {
		t.Fatalf("plan = %#v, want %#v", plan.Entrypoints, want)
	}
}

func TestDiscoverGitLabScriptListAndUnitScript(t *testing.T) {
	// Validation contract 3: list-form CI scripts and the TS fact-sheet's
	// standard `unit` script fallback are both recognized.
	ciWorkspace := t.TempDir()
	writeDiscoveryFile(t, ciWorkspace, ".gitlab-ci.yml", `verify:
  script:
    - cargo build --workspace
    - cargo test --workspace
`)
	wantCI := []Entrypoint{
		{Kind: KindBuild, Command: "cargo build --workspace", Source: ".gitlab-ci.yml"},
		{Kind: KindTest, Command: "cargo test --workspace", Source: ".gitlab-ci.yml"},
	}
	if plan := Discover(ciWorkspace); !reflect.DeepEqual(plan.Entrypoints, wantCI) {
		t.Fatalf("CI plan = %#v, want %#v", plan.Entrypoints, wantCI)
	}

	packageWorkspace := t.TempDir()
	writeDiscoveryFile(t, packageWorkspace, "package.json", `{"scripts":{"build":"tsc","unit":"vitest run"}}`)
	wantPackage := []Entrypoint{
		{Kind: KindBuild, Command: "npm run build", Source: "package.json#scripts.build"},
		{Kind: KindTest, Command: "npm run unit", Source: "package.json#scripts.unit"},
	}
	if plan := Discover(packageWorkspace); !reflect.DeepEqual(plan.Entrypoints, wantPackage) {
		t.Fatalf("package plan = %#v, want %#v", plan.Entrypoints, wantPackage)
	}
}

func TestDiscoverGoFullEntrypointsAndEmptyFallback(t *testing.T) {
	// Validation contract 3: Go is one ecosystem fallback among several, and a
	// repository with no discoverable convention does not invent go test.
	workspace := t.TempDir()
	writeDiscoveryFile(t, workspace, "go.mod", "module example.test/default\n")
	plan := Discover(workspace)
	want := []Entrypoint{
		{Kind: KindBuild, Command: "go build ./...", Source: "go.mod"},
		{Kind: KindTest, Command: "go test ./...", Source: "go.mod"},
	}
	if !reflect.DeepEqual(plan.Entrypoints, want) {
		t.Fatalf("go plan = %#v, want %#v", plan.Entrypoints, want)
	}
	if empty := Discover(t.TempDir()); len(empty.Entrypoints) != 0 {
		t.Fatalf("empty repository plan = %#v", empty.Entrypoints)
	}
}

func TestDiscoverRejectsWatcherScriptsAndFallsThrough(t *testing.T) {
	// Validation contracts C5.1 and C5.4: a manifest key is not usable evidence
	// when its body starts an interactive runner. A later non-watcher candidate
	// wins, while a watcher-only manifest yields no test entrypoint.
	workspace := t.TempDir()
	writeDiscoveryFile(t, workspace, "package.json", `{
  "scripts": {
    "build": "tsc",
    "test": "vitest --watch",
    "unit": "vitest run"
  }
}`)
	want := []Entrypoint{
		{Kind: KindBuild, Command: "npm run build", Source: "package.json#scripts.build"},
		{Kind: KindTest, Command: "npm run unit", Source: "package.json#scripts.unit"},
	}
	if plan := Discover(workspace); !reflect.DeepEqual(plan.Entrypoints, want) {
		t.Fatalf("watcher fallback plan = %#v, want %#v", plan.Entrypoints, want)
	}

	watcherOnly := t.TempDir()
	writeDiscoveryFile(t, watcherOnly, "package.json", `{"scripts":{"build":"tsc","test":"vitest --ui"}}`)
	want = []Entrypoint{
		{Kind: KindBuild, Command: "npm run build", Source: "package.json#scripts.build"},
	}
	if plan := Discover(watcherOnly); !reflect.DeepEqual(plan.Entrypoints, want) {
		t.Fatalf("watcher-only plan = %#v, want %#v", plan.Entrypoints, want)
	}
}

func TestDiscoverPreservesCIWorkingDirectoryAndCommandChain(t *testing.T) {
	// Validation contracts C5.2 and C5.3: extracting a recognized command from
	// a CI chain must retain its leading cd and every remaining shell step.
	workspace := t.TempDir()
	writeDiscoveryFile(t, workspace, ".github/workflows/verify.yml", `
jobs:
  verify:
    steps:
      - run: cd frontend && npm ci && npm run build && npm test
`)
	want := []Entrypoint{
		{
			Kind: KindBuild, Command: "npm ci && npm run build && npm test",
			Workdir: "frontend", Source: ".github/workflows/verify.yml",
		},
		{
			Kind: KindTest, Command: "npm ci && npm run build && npm test",
			Workdir: "frontend", Source: ".github/workflows/verify.yml",
		},
	}
	if plan := Discover(workspace); !reflect.DeepEqual(plan.Entrypoints, want) {
		t.Fatalf("CI chain plan = %#v, want %#v", plan.Entrypoints, want)
	}
}

func TestCommandClassificationUsesExecutableShellText(t *testing.T) {
	// C2.1-C2.3: words in comments and echo arguments are not process
	// evidence, while genuine build and test invocations remain discoverable.
	tests := []struct {
		name    string
		command string
		build   bool
		test    bool
	}{
		{name: "true with test comment", command: "true # pytest", test: false},
		{name: "colon with test comment", command: ": # npm test", test: false},
		{name: "echo quoted build", command: `echo "go build ./..."`, build: false},
		{name: "echo quoted shell clause", command: `echo "ignored; go build ./..."`, build: false},
		{name: "echo unquoted test", command: "echo go test ./...", test: false},
		{name: "comment after real build", command: "go build ./... # pytest", build: true, test: false},
		{name: "quoted hash is argument", command: `pytest -k '# smoke'`, test: true},
		{name: "real test", command: "go test ./...", test: true},
		{name: "masked failing test", command: "go test ./... || true", test: false},
		{name: "skipped test after true", command: "true || go test ./...", test: false},
		{name: "skipped test after exit", command: "exit 0; go test ./...", test: false},
		{name: "preceding true clause", command: "true; go test ./...", test: false},
		{name: "unguarded pipe", command: "go test ./... | cat", test: false},
		{name: "backgrounded test", command: "go test ./... & true", test: false},
		{name: "tee pipeline", command: "go test ./... 2>&1 | tee test.log", test: true},
		{name: "leading cd chain", command: "cd frontend && go test ./...", test: true},
		{name: "build and test chain", command: "go build ./... && go test ./...", build: true, test: true},
		{name: "environment assignment", command: "CGO_ENABLED=0 go test ./...", test: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isBuildCommand(test.command); got != test.build {
				t.Fatalf("isBuildCommand(%q) = %t, want %t", test.command, got, test.build)
			}
			if got := isTestCommand(test.command); got != test.test {
				t.Fatalf("isTestCommand(%q) = %t, want %t", test.command, got, test.test)
			}
		})
	}
}

func TestDiscoverPythonTypechecksAsBuildEntrypoints(t *testing.T) {
	// C3.1: each explicit compile/typecheck command satisfies the mandatory
	// build role from src/baked/agents/auditor.md:53-55.
	commands := []string{
		"mypy src",
		"pyright",
		"ruff check .",
		"tsc --noEmit",
		"python3 -m compileall src",
	}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			workspace := t.TempDir()
			writeDiscoveryFile(t, workspace, "pyproject.toml", "[project]\nname = \"demo\"\n")
			writeDiscoveryFile(t, workspace, "AGENTS.md", "Run `"+command+"`.\n")
			plan := Discover(workspace)
			want := Entrypoint{Kind: KindBuild, Command: command, Source: "AGENTS.md"}
			if len(plan.Entrypoints) != 1 || plan.Entrypoints[0] != want {
				t.Fatalf("plan = %#v, want build %#v", plan, want)
			}
			if !plan.BuildExpected {
				t.Fatal("explicit Python typecheck did not make build evidence mandatory")
			}
		})
	}
}

func TestPlainPythonBuildExemptionDependsOnDiscoveredTypecheck(t *testing.T) {
	// C3.2-C3.3: the Python carve-out applies only when discovery found no
	// explicit compile/typecheck step anywhere.
	plain := t.TempDir()
	writeDiscoveryFile(t, plain, "pyproject.toml", "[project]\nname = \"plain\"\n")
	writeDiscoveryFile(t, plain, "test_demo.py", "def test_green():\n    assert True\n")
	if plan := Discover(plain); plan.BuildExpected || planHasEntrypointKind(plan, KindBuild) {
		t.Fatalf("plain Python plan = %#v, want test-only exemption", plan)
	}

	typed := t.TempDir()
	writeDiscoveryFile(t, typed, "pyproject.toml", "[project]\nname = \"typed\"\n")
	writeDiscoveryFile(t, typed, "AGENTS.md", "Run `mypy src` and `python3 -m unittest`.\n")
	if plan := Discover(typed); !plan.BuildExpected || !planHasEntrypointKind(plan, KindBuild) {
		t.Fatalf("typed Python plan = %#v, want required build", plan)
	}
}

func TestDiscoverPlainPythonWithoutPackagingMetadata(t *testing.T) {
	// F4.1-F4.2: test/config markers identify plain Python repositories even
	// without packaging metadata, while the established metadata path remains.
	for _, test := range []struct {
		name   string
		marker string
	}{
		{name: "requirements and pytest config", marker: "requirements.txt"},
		{name: "pytest config", marker: "pytest.ini"},
		{name: "tox config", marker: "tox.ini"},
		{name: "test layout only", marker: "tests/test_example.py"},
		{name: "packaging metadata", marker: "pyproject.toml"},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			writeDiscoveryFile(t, workspace, test.marker, "# marker\n")
			if test.marker != "tests/test_example.py" {
				writeDiscoveryFile(t, workspace, "tests/test_example.py", "def test_green():\n    assert True\n")
			}
			plan := Discover(workspace)
			want := []Entrypoint{{Kind: KindTest, Command: "python3 -m pytest", Source: "Python test files"}}
			if plan.BuildExpected || !reflect.DeepEqual(plan.Entrypoints, want) {
				t.Fatalf("plain Python plan = %#v, want %#v with no build", plan, want)
			}
		})
	}
}

func TestInteractiveTestFlagsRespectTheirMeaningAndValue(t *testing.T) {
	// C8.1-C8.3: Jest's -w means workers, and an explicit false watch value is
	// non-interactive; modes that actually wait for a user remain unusable.
	tests := []struct {
		command     string
		interactive bool
	}{
		{command: "jest -w 1"},
		{command: "jest -w=2"},
		{command: "jest --maxWorkers=2"},
		{command: "vitest -w", interactive: true},
		{command: "jest -w", interactive: true},
		{command: "vitest --watch=false"},
		{command: "vitest --watch false"},
		{command: "vitest --watch", interactive: true},
		{command: "jest --watch", interactive: true},
		{command: "vitest --ui", interactive: true},
		{command: "cypress open", interactive: true},
	}
	for _, test := range tests {
		t.Run(test.command, func(t *testing.T) {
			if got := isInteractiveTestCommand(test.command); got != test.interactive {
				t.Fatalf("isInteractiveTestCommand(%q) = %t, want %t", test.command, got, test.interactive)
			}
		})
	}
}

func TestDiscoverAcceptsNonInteractiveWatchLikeScripts(t *testing.T) {
	// C8.1 and C8.3 exercise the package-script path, where watcher filtering
	// happens before the generated npm command is classified.
	for _, body := range []string{"jest -w 1", "jest -w=2", "jest --maxWorkers=2", "vitest --watch=false", "vitest --watch false"} {
		t.Run(body, func(t *testing.T) {
			workspace := t.TempDir()
			writeDiscoveryFile(t, workspace, "package.json", `{"scripts":{"build":"tsc","test":`+strconv.Quote(body)+`}}`)
			if plan := Discover(workspace); !planHasEntrypointKind(plan, KindTest) {
				t.Fatalf("plan = %#v, want usable test script", plan)
			}
		})
	}
}

func TestDiscoverRejectsBareShortWatchFlagAndFallsThrough(t *testing.T) {
	workspace := t.TempDir()
	writeDiscoveryFile(t, workspace, "package.json", `{"scripts":{"build":"tsc","test":"vitest -w","unit":"vitest run"}}`)
	plan := Discover(workspace)
	want := Entrypoint{Kind: KindTest, Command: "npm run unit", Source: "package.json#scripts.unit"}
	if len(plan.Entrypoints) != 2 || plan.Entrypoints[1] != want {
		t.Fatalf("short-watch plan = %#v, want fallback %#v", plan.Entrypoints, want)
	}
}

func TestDiscoverCIWorkingDirectoryForms(t *testing.T) {
	tests := []struct {
		name     string
		workflow string
		workdir  string
	}{
		{
			name: "step field",
			workflow: `jobs:
  verify:
    steps:
      - run: go test ./...
        working-directory: frontend
`,
			workdir: "frontend",
		},
		{
			name: "multiline leading cd",
			workflow: `jobs:
  verify:
    steps:
      - run: |
          cd frontend
          go test ./...
`,
			workdir: "frontend",
		},
		{
			name: "step field then inline cd",
			workflow: `jobs:
  verify:
    steps:
      - working-directory: packages
        run: cd frontend && go test ./...
`,
			workdir: filepath.Join("packages", "frontend"),
		},
		{
			name: "workflow defaults",
			workflow: `defaults:
  run:
    working-directory: frontend
jobs:
  verify:
    steps:
      - run: go test ./...
`,
			workdir: "frontend",
		},
		{
			name: "job defaults override workflow",
			workflow: `defaults:
  run:
    working-directory: ignored
jobs:
  verify:
    defaults:
      run:
        working-directory: frontend
    steps:
      - run: go test ./...
`,
			workdir: "frontend",
		},
		{
			name: "multiline cd after benign setup",
			workflow: `jobs:
  verify:
    steps:
      - run: |
          set -e
          export MODE=ci
          # setup complete
          cd frontend
          go test ./...
`,
			workdir: "frontend",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			writeDiscoveryFile(t, workspace, ".github/workflows/verify.yml", test.workflow)
			plan := Discover(workspace)
			want := []Entrypoint{{
				Kind: KindTest, Command: "go test ./...", Workdir: test.workdir,
				Source: ".github/workflows/verify.yml",
			}}
			if !reflect.DeepEqual(plan.Entrypoints, want) {
				t.Fatalf("plan = %#v, want %#v", plan.Entrypoints, want)
			}
		})
	}
}

func TestDiscoverJoinsCIContinuationsAndSkipsHeredocBodies(t *testing.T) {
	workspace := t.TempDir()
	writeDiscoveryFile(t, workspace, ".github/workflows/verify.yml", `jobs:
  verify:
    steps:
      - run: |
          cmake \
            --build build
          python3 <<'PY'
          go test ./...
          PY
`)
	plan := Discover(workspace)
	want := []Entrypoint{{
		Kind: KindBuild, Command: "cmake --build build", Source: ".github/workflows/verify.yml",
	}}
	if !reflect.DeepEqual(plan.Entrypoints, want) {
		t.Fatalf("multiline CI plan = %#v, want %#v", plan.Entrypoints, want)
	}
}

// TestUnaccountableWorkspaceExpectsNothing: a workspace with no project in it
// demands neither role. Both demands are unsatisfiable there — nothing to
// compile, nothing to test — and reporting them as failures sends the
// audit-fix loop after a target that does not exist.
func TestUnaccountableWorkspaceExpectsNothing(t *testing.T) {
	for _, test := range []struct {
		name  string
		files map[string]string
	}{
		{name: "empty", files: map[string]string{}},
		{name: "readme only", files: map[string]string{"README.md": "# demo\n"}},
		{name: "loose data files", files: map[string]string{
			"notes.txt": "hello\n", "data.csv": "a,b\n1,2\n",
		}},
		// The state the engine leaves behind mid-run: a shell script whose
		// name starts with "test" is not a Python test file and not a suite.
		{name: "loose shell script", files: map[string]string{
			"README.md": "# demo\n", "test_hello.sh": "#!/bin/sh\nexit 0\n",
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			for name, body := range test.files {
				writeDiscoveryFile(t, workspace, name, body)
			}
			plan := Discover(workspace)
			if plan.BuildExpected || plan.TestExpected || len(plan.Entrypoints) != 0 {
				t.Fatalf("unaccountable plan = %#v, want no demands and no entrypoints", plan)
			}
		})
	}
}

// TestAccountableWorkspaceExpectsBothRoles: the moment a workspace looks like a
// project, the fail-closed floor applies again. A real repository whose test
// command is merely undiscoverable must still fail — that strictness is the
// point of the gate, and relaxing it is not what the bare-workspace fix does.
func TestAccountableWorkspaceExpectsBothRoles(t *testing.T) {
	for _, test := range []struct {
		name              string
		files             map[string]string
		wantBuildExpected bool
	}{
		{name: "typescript config", files: map[string]string{
			"tsconfig.json": `{"compilerOptions":{"strict":true}}`,
		}, wantBuildExpected: true},
		{name: "makefile without either target", files: map[string]string{
			"Makefile": "lint:\n\techo lint\n",
		}, wantBuildExpected: true},
		{name: "test directory alone", files: map[string]string{
			"spec/example_spec.rb": "# spec\n",
		}, wantBuildExpected: true},
		// A plain-Python project keeps its build carve-out but is still held to
		// a test entrypoint, which pytest markers here do not supply.
		{name: "python packaging without tests", files: map[string]string{
			"pyproject.toml": "[project]\nname = \"demo\"\n",
		}, wantBuildExpected: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			for name, body := range test.files {
				writeDiscoveryFile(t, workspace, name, body)
			}
			plan := Discover(workspace)
			if !plan.TestExpected {
				t.Errorf("plan = %#v, want TestExpected", plan)
			}
			if plan.BuildExpected != test.wantBuildExpected {
				t.Errorf("plan = %#v, want BuildExpected=%v", plan, test.wantBuildExpected)
			}
			if planHasEntrypointKind(plan, KindTest) {
				t.Errorf("plan = %#v, want no discoverable test entrypoint in this fixture", plan)
			}
		})
	}
}

// TestDiscoveredEntrypointsMakeAWorkspaceAccountable: a documented command is
// itself proof that a project is here, whatever its shape. This is the path
// that keeps an unrecognized-ecosystem repository fail-closed
// (TestGreenTestWithoutBuildEntrypointCannotPass depends on it).
func TestDiscoveredEntrypointsMakeAWorkspaceAccountable(t *testing.T) {
	workspace := t.TempDir()
	writeDiscoveryFile(t, workspace, "checks/check_green.py", "# not a pytest layout\n")
	writeDiscoveryFile(t, workspace, "AGENTS.md",
		"Run `python3 -m unittest discover -s checks -p 'check_*.py'`.\n")
	plan := Discover(workspace)
	if !planHasEntrypointKind(plan, KindTest) {
		t.Fatalf("plan = %#v, want the documented test command discovered", plan)
	}
	if !plan.BuildExpected || !plan.TestExpected {
		t.Fatalf("plan = %#v, want both demands once a command was discovered", plan)
	}
}

// TestGoAndJSProjectsKeepTheirDiscoveredEntrypoints pins that the ecosystems
// most likely to reach this gate are unaffected by the accountability change.
func TestGoAndJSProjectsKeepTheirDiscoveredEntrypoints(t *testing.T) {
	goWorkspace := t.TempDir()
	writeDiscoveryFile(t, goWorkspace, "go.mod", "module example.test/demo\n")
	writeDiscoveryFile(t, goWorkspace, "demo_test.go", "package demo\n")
	plan := Discover(goWorkspace)
	if !plan.BuildExpected || !plan.TestExpected ||
		!planHasEntrypointKind(plan, KindTest) || !planHasEntrypointKind(plan, KindBuild) {
		t.Fatalf("go plan = %#v, want both demanded and both discovered", plan)
	}

	jsWorkspace := t.TempDir()
	writeDiscoveryFile(t, jsWorkspace, "package.json", `{"scripts":{"test":"vitest run"}}`)
	plan = Discover(jsWorkspace)
	if !plan.TestExpected || !planHasEntrypointKind(plan, KindTest) {
		t.Fatalf("js plan = %#v, want a discovered test entrypoint", plan)
	}
}

func planHasEntrypointKind(plan Plan, kind EntrypointKind) bool {
	for _, entrypoint := range plan.Entrypoints {
		if entrypoint.Kind == kind {
			return true
		}
	}
	return false
}
