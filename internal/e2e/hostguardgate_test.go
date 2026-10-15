package e2e

// THE HOST GUARD IS A LAW FOR EVERY LAUNCHER. A codeaf process started by this
// suite takes the old feature's timers off whatever login it runs as, and a
// window it opens starts the automations clock (hostguard_test.go says both
// in full). An unguarded launch would do the first to the developer's own
// launchd or systemd and the second against their home. It began as #1631, when
// a launch could still INSTALL a timer and replaced the developer's with a unit
// pointing at a deleted checkout. This untagged gate reads tagged sources as
// well.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// These launches run tools other than this machine's codeaf. The docker harness
// starts the product inside a container with its own scheduler.
var hostGuardAllowlist = map[string]string{
	"remote_test.go/runOut": "docker harness",
	// The screenshot renderer is freeze, which never starts the product.
	"questions_e2e_test.go/shot": "freeze renderer",
}

// These are the only doors allowed to build a product launch. Both must keep
// calling guardHost so a future edit cannot silently remove the protection.
var hostGuardDoors = map[string]string{
	"hostguard_test.go/guardedCommand": "exec",
	"tmux_test.go/startWithEnv":        "tmux",
}

func TestEveryLaunchOfCodeafStandsBehindTheHostGuard(t *testing.T) {
	dir := filepath.Join(moduleRoot(t), "internal", "e2e")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	files, commands := 0, 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		files++
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			key := name + "/" + fn.Name.Name
			if _, ok := hostGuardAllowlist[key]; ok {
				found[key] = true
			}
			if _, ok := hostGuardDoors[key]; ok {
				found[key] = true
				guarded := false
				ast.Inspect(fn.Body, func(node ast.Node) bool {
					if call, ok := node.(*ast.CallExpr); ok && hostCallName(call) == "guardHost" {
						guarded = true
					}
					return true
				})
				if !guarded {
					t.Errorf("%s:%d door no longer calls guardHost; use the temporary HOME and the machine's stand-ins before launching codeaf (#1631)", name, fset.Position(fn.Pos()).Line)
					if key == "tmux_test.go/startWithEnv" {
						ast.Inspect(fn.Body, func(node ast.Node) bool {
							call, ok := node.(*ast.CallExpr)
							if ok && hostCallName(call) == "exec.Command" && len(call.Args) > 1 {
								program, literal := hostString(call.Args[0])
								if literal && program == "tmux" && hostTmuxLaunch(call.Args[1:]) {
									last, literal := hostString(call.Args[len(call.Args)-1])
									if !literal || strings.Contains(last, "codeaf") {
										t.Errorf("%s:%d tmux respawn-window can start codeaf without the host guard; it would take the developer's old timers off their login and start a clock against their home (#1631)", name, fset.Position(call.Pos()).Line)
									}
								}
							}
							return true
						})
					}
				}
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok && (hostCallName(call) == "exec.Command" || hostCallName(call) == "exec.CommandContext") {
				commands++
			}
			return true
		})
		for _, violation := range hostGuardViolations(fset, file, name) {
			t.Error(violation)
		}
	}
	if files < 2 || commands == 0 {
		t.Fatalf("host guard gate is vacuous: parsed %d files and found %d exec.Command calls", files, commands)
	}
	for key := range hostGuardAllowlist {
		if !found[key] {
			t.Errorf("%s allowlist entry has no function; review the launch before renaming it (#1631)", key)
		}
	}
	for key := range hostGuardDoors {
		if !found[key] {
			t.Errorf("%s host guard door is missing; every codeaf launch needs a temporary HOME and the machine's stand-ins (#1631)", key)
		}
	}
}

// hostGuardViolations attributes calls in nested closures to their containing
// function, since moving an exec into a callback does not protect the machine.
func hostGuardViolations(fset *token.FileSet, file *ast.File, name string) []string {
	var problems []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		key := name + "/" + fn.Name.Name
		_, door := hostGuardDoors[key]
		_, allowed := hostGuardAllowlist[key]
		guardedVars := map[string]bool{}
		report := func(node ast.Node, reason string) {
			problems = append(problems, name+":"+strconv.Itoa(fset.Position(node.Pos()).Line)+" "+reason+"; an unguarded codeaf takes the developer's old timers off their login and starts a clock against their home (#1631). Use guardedCommand or startWithEnv")
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.AssignStmt:
				for _, lhs := range n.Lhs {
					if sel, ok := lhs.(*ast.SelectorExpr); ok {
						if id, ok := sel.X.(*ast.Ident); ok && guardedVars[id.Name] && (sel.Sel.Name == "Env" || sel.Sel.Name == "Path") {
							report(n, "assignment to guarded command ."+sel.Sel.Name+" removes the host guard")
						}
					}
				}
				for i, rhs := range n.Rhs {
					call, ok := rhs.(*ast.CallExpr)
					if !ok || hostCallName(call) != "guardedCommand" || i >= len(n.Lhs) {
						continue
					}
					if id, ok := n.Lhs[i].(*ast.Ident); ok {
						guardedVars[id.Name] = true
					}
				}
			case *ast.CallExpr:
				call := hostCallName(n)
				switch call {
				case "exec.Command", "exec.CommandContext":
					index := 0
					if call == "exec.CommandContext" {
						index = 1
					}
					if len(n.Args) <= index {
						return true
					}
					program, literal := hostString(n.Args[index])
					if literal && filepath.Base(program) == "codeaf" {
						report(n, "literal codeaf program depends on the developer's PATH")
					} else if !literal && !door && !allowed {
						report(n, "dynamic exec program outside a host guard door")
					}
					if call == "exec.Command" && program == "tmux" && hostTmuxLaunch(n.Args[index+1:]) {
						last, literal := hostString(n.Args[len(n.Args)-1])
						if literal && strings.Contains(last, "codeaf") {
							report(n, "tmux command literal starts codeaf without a host guard")
						} else if !literal && key != "tmux_test.go/startWithEnv" {
							report(n, "built tmux launch outside startWithEnv")
						}
					}
				case "os.StartProcess", "syscall.Exec", "syscall.ForkExec":
					if !door {
						report(n, call+" bypasses the host guard")
					}
				}
			case *ast.CompositeLit:
				if hostExprName(n.Type) == "exec.Cmd" && !door {
					report(n, "exec.Cmd literal bypasses the host guard")
				}
			}
			return true
		})
	}
	return problems
}

func hostCallName(call *ast.CallExpr) string { return hostExprName(call.Fun) }

func hostExprName(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		if id, ok := value.X.(*ast.Ident); ok {
			return id.Name + "." + value.Sel.Name
		}
	}
	return ""
}

func hostString(expr ast.Expr) (string, bool) {
	literal, ok := expr.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	return value, err == nil
}

func hostTmuxLaunch(args []ast.Expr) bool {
	for _, arg := range args {
		value, ok := hostString(arg)
		if ok && (value == "respawn-window" || value == "respawn-pane" || value == "new-window" || value == "split-window" || value == "new-session") {
			return true
		}
	}
	return false
}

func TestTheHostGuardGateCatchesAnUnguardedLauncher(t *testing.T) {
	source := `package e2e
import "os/exec"
func fake(t any) {
exec.Command(binary(t), "clock")
exec.Command("codeaf", "chat")
exec.Command("tmux", "respawn-window", buildCommand())
cmd := guardedCommand(t, nil, "", nil, "", "")
cmd.Env = nil
exec.Command("git", "status")
}`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fake_e2e_test.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	problems := hostGuardViolations(fset, file, "fake_e2e_test.go")
	if len(problems) != 4 {
		t.Fatalf("wanted four launcher failures with lines, got %d: %v", len(problems), problems)
	}
	for _, want := range []string{"fake_e2e_test.go:4", "fake_e2e_test.go:5", "fake_e2e_test.go:6", "fake_e2e_test.go:8"} {
		if !strings.Contains(strings.Join(problems, "\n"), want) {
			t.Errorf("missing %s in %v", want, problems)
		}
	}
}
