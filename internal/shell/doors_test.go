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

// I0 and N3 pin unchanged argv through the executor at every shell door. Together with the
// injected Unix preference tests, this does not require an installed /bin/bash.
func TestUnixDoorArgv(t *testing.T) {
	// Bash doors keep pipefail so a failed pipeline stage cannot look successful.
	for _, tc := range []struct{ file, function, call string }{
		{"exec/tools.go", "runShell", `[]string{bash, "-o", "pipefail", "-lc", command}`},
		{"exec/jobs.go", "startBackground", `detachedCommand(executor.For(ctx), jobCtx, r.workspace.Root(), command, environment, logFile, 3*time.Second)`},
		{"exec/services.go", "detachedCommand", `[]string{bash, "-o", "pipefail", "-lc", command}`},
		{"exec/bare/tools.go", "getShellConfig", `shell.Preferred()`},
		{"exec/bare/tools.go", "newBashTool", `append([]string{shell}, shellArgs...)`},
		{"exec/bare/streaming.go", "StreamingShell", `append(append([]string{}, shellArgs...), command)`},
		{"session/jobs.go", "jobShell", `shell.Preferred()`},
		// Job spawning moved under the cancellable admission door without changing its argv.
		{"session/jobs.go", "startInUnder", `append([]string{program}, shellArgs...)`},
		{"session/tools_watch.go", "runTick", `append([]string{shell}, append(shellArgs, spec.command)...)`},
		{"session/automation_run.go", "automationLookCommand", `append([]string{shell}, append(args, command)...)`},
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
					call, ok := n.(ast.Expr)
					if !ok {
						return true
					}
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
				t.Fatalf("Unix argv changed: %s must retain %s", tc.function, tc.call)
			}
		})
	}
}
