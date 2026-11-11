package shell

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// I0 and N3 pin argv through the executor at every shell door.
// Unix parity 2 contract items 1–3 keep Unix headless/health argv at the base
// and Windows foreground non-login plus health resolver/pipefail in thin leaves.
// Injected Unix preference tests do not require an installed /bin/bash.
func TestUnixDoorArgv(t *testing.T) {
	// Pipefail remains at command doors except the historical Unix resident
	// health leaf, whose pipeline status belongs to the last command.
	for _, tc := range []struct{ file, function, call string }{
		{"exec/tools.go", "runShell", `append([]string{bash}, commandshell.ForegroundArgs(bash, command)...)`},
		{"exec/jobs.go", "startBackground", `detachedCommand(executor.For(ctx), jobCtx, r.workspace.Root(), command, environment, logFile, 3*time.Second)`},
		{"exec/services.go", "detachedCommand", `append([]string{bash}, commandshell.PipefailArgs(bash, []string{"-lc"}, command)...)`},
		{"resident/services.go", "Healthy", `serviceHealthCommand(service.Health.Value)`},
		{"resident/services_unix.go", "serviceHealthCommand", `[]string{"bash", "-lc", command}`},
		{"resident/services_windows.go", "serviceHealthCommand", `shell.Bash()`},
		{"resident/services_windows.go", "serviceHealthCommand", `append([]string{bash}, shell.PipefailArgs(bash, []string{"-lc"}, command)...)`},
		{"shell/shell.go", "ForegroundArgs", `flag := "-lc"`},
		{"shell/shell.go", "ForegroundArgs", "if runtime.GOOS == \"windows\" {\n\tflag = \"-c\"\n}"},
		{"shell/shell.go", "ForegroundArgs", `PipefailArgs(bash, []string{flag}, command)`},
		{"exec/bare/tools.go", "getShellConfig", `shell.Preferred()`},
		{"exec/bare/tools.go", "getShellConfig", `shell.PipefailArgs(program, flags, command)`},
		{"exec/bare/tools.go", "newBashTool", `append([]string{shell}, shellArgs...)`},
		{"exec/bare/streaming.go", "StreamingShell", `getShellConfig(command)`},
		{"session/jobs.go", "jobShell", `shell.Preferred()`},
		{"session/jobs.go", "jobShell", `shell.PipefailArgs(program, flags, command)`},
		// Job spawning moved under the cancellable admission door without changing its argv.
		{"session/jobs.go", "startInUnder", `append([]string{program}, shellArgs...)`},
		{"session/tools_watch.go", "runTick", `append([]string{shell}, shellArgs...)`},
		{"session/automation_run.go", "automationLookCommand", `append([]string{shell}, args...)`},
		{"session/principal_audit.go", "runOneCheck", `[]string{bash, "-c", check}`},
		{"verify/run.go", "RunReading", `[]string{bash, "-c", strictPreamble + strategy.Command}`},
	} {
		t.Run(tc.file+"/"+tc.function, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, filepath.Join("..", tc.file), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			rawSpawn := false
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Name.Name != tc.function {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					switch n.(type) {
					case ast.Expr, ast.Stmt:
					default:
						return true
					}
					call := n
					if node, ok := call.(*ast.CallExpr); ok {
						if selector, ok := node.Fun.(*ast.SelectorExpr); ok {
							if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == "exec" && strings.HasPrefix(selector.Sel.Name, "Command") {
								rawSpawn = true
							}
						}
					}
					var out bytes.Buffer
					if err := format.Node(&out, fset, call); err != nil {
						t.Fatal(err)
					}
					if out.String() == tc.call {
						found = true
					}
					return true
				})
			}
			if rawSpawn {
				t.Errorf("I0: %s bypasses the executor", tc.function)
			}
			if !found {
				t.Fatalf("Per-OS argv changed: %s must retain %s", tc.function, tc.call)
			}
		})
	}
}
