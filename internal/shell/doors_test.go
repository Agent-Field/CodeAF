package shell

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// C5 pins the argv expressions at every Unix engine door. Together with the
// injected Unix preference tests, this does not require an installed /bin/bash.
func TestUnixDoorArgv(t *testing.T) {
	for _, tc := range []struct{ file, function, call string }{
		{"exec/tools.go", "runShell", `exec.CommandContext(runCtx, bash, "-lc", command)`},
		{"exec/jobs.go", "startBackground", `exec.CommandContext(jobCtx, bash, "-lc", command)`},
		{"exec/services.go", "StartDetachedService", `exec.Command(bash, "-lc", command)`},
		{"exec/bare/tools.go", "getShellConfig", `shell.Preferred()`},
		{"exec/bare/tools.go", "newBashTool", `exec.Command(shell, shellArgs...)`},
		{"exec/bare/streaming.go", "StreamingShell", `append(append([]string{}, shellArgs...), command)`},
		{"service/runtime_platform.go", "Launch", `exec.Command(gateShell, append([]string{"-c", gateScript, program}, args...)...)`},
		{"service/runtime_platform.go", "runCheckCommand", `exec.Command(shell, args...)`},
		{"session/jobs.go", "jobShell", `shell.Preferred()`},
		{"session/jobs.go", "startInUnder", `exec.Command(program, shellArgs...)`},
		{"session/tools_watch.go", "runTick", `exec.CommandContext(tickCtx, shell, append(shellArgs, spec.command)...)`},
		{"session/automation_run.go", "automationLookCommand", `exec.CommandContext(ctx, shell, append(args, command)...)`},
		{"session/principal_audit.go", "runOneCheck", `exec.CommandContext(ctx, bash, "-c", check)`},
		{"verify/run.go", "RunReading", `exec.CommandContext(ctx, bash, "-c", strictPreamble+strategy.Command)`},
		{"factory/run/checkstage.go", "runCheck", `exec.CommandContext(runCtx, opts.Shell, "-c", command)`},
	} {
		t.Run(tc.file+"/"+tc.function, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, filepath.Join("..", tc.file), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Name.Name != tc.function {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
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
			if !found {
				t.Fatalf("Unix argv changed: %s must retain %s", tc.function, tc.call)
			}
		})
	}
}
