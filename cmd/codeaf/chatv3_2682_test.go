package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// TestInProcessDoorAnchorsWorkingFolderLikeHostedDoor2682 verifies that
// codeaf chat --no-host and CODEAF_DEBUG=1 codeaf pass the workspace resolved
// by v3HostRoad into openV3Launch just as the hosted door does, yielding the
// same project identity, workspace, and MemoryProjectKey while keeping an
// unanchored conversation owned with no project of its own.
func TestInProcessDoorAnchorsWorkingFolderLikeHostedDoor2682(t *testing.T) {
	t.Run("openChatV3_wires_hostWorkspace_into_in_process_seed", func(t *testing.T) {
		file, err := parser.ParseFile(token.NewFileSet(), "chatv3.go", nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		wired := false
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "openChatV3" || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				lit, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}
				id, ok := lit.Type.(*ast.Ident)
				if !ok || id.Name != "v3Options" {
					return true
				}
				for _, elt := range lit.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					val, valOK := kv.Value.(*ast.Ident)
					if ok && valOK && key.Name == "Workspace" && val.Name == "hostWorkspace" {
						wired = true
					}
				}
				return true
			})
		}
		if !wired {
			t.Fatal("openChatV3 does not wire hostWorkspace into the in-process v3Options seed")
		}
	})

	t.Run("hosted_and_in_process_launches_share_project_identity", func(t *testing.T) {
		deadCatalogEndpoint(t)
		proc := v3TestProcess(t)

		ws := filepath.Join(t.TempDir(), "ws")
		if err := os.MkdirAll(ws, 0o755); err != nil {
			t.Fatal(err)
		}
		proc.LaunchDir = ws
		hostWorkspace, _ := v3Workspace(ws, "")
		if hostWorkspace != ws {
			t.Fatalf("v3Workspace(%q, \"\") = %q, want %q", ws, hostWorkspace, ws)
		}

		hostedOpts := engineLaunchOptions(remote.Hello{Version: remote.Version, Workspace: hostWorkspace}, hostWorkspace, "")
		hostedOpts.Model = "test/model"
		hostedLaunch, err := openV3Launch(proc, hostedOpts)
		if err != nil {
			t.Fatalf("hosted openV3Launch: %v", err)
		}

		inProcessOpts := v3Options{
			Model:       "test/model",
			Workspace:   hostWorkspace,
			Interactive: true,
		}
		inProcessLaunch, err := openV3Launch(proc, inProcessOpts)
		if err != nil {
			t.Fatalf("in-process openV3Launch: %v", err)
		}

		if inProcessLaunch.Place.Owned || hostedLaunch.Place.Owned {
			t.Fatalf("working folder marked owned: inProcess=%v hosted=%v", inProcessLaunch.Place.Owned, hostedLaunch.Place.Owned)
		}
		if inProcessLaunch.Project != ws || hostedLaunch.Project != ws {
			t.Fatalf("project mismatch: inProcess=%q hosted=%q want=%q", inProcessLaunch.Project, hostedLaunch.Project, ws)
		}
		if inProcessLaunch.Workspace != ws || hostedLaunch.Workspace != ws {
			t.Fatalf("workspace mismatch: inProcess=%q hosted=%q want=%q", inProcessLaunch.Workspace, hostedLaunch.Workspace, ws)
		}
		if inProcessLaunch.Config.MemoryProjectKey == "" || inProcessLaunch.Config.MemoryProjectKey != hostedLaunch.Config.MemoryProjectKey {
			t.Fatalf("MemoryProjectKey mismatch: inProcess=%q hosted=%q", inProcessLaunch.Config.MemoryProjectKey, hostedLaunch.Config.MemoryProjectKey)
		}
		if inProcessLaunch.Config.MemoryProjectName != hostedLaunch.Config.MemoryProjectName {
			t.Fatalf("MemoryProjectName mismatch: inProcess=%q hosted=%q", inProcessLaunch.Config.MemoryProjectName, hostedLaunch.Config.MemoryProjectName)
		}

		inProcessAgent, _, _, err := openV3Agent(inProcessLaunch.Config, inProcessLaunch.Project, session.New)
		if err != nil {
			t.Fatalf("openV3Agent in-process: %v", err)
		}
		const ruleAlpha = "proj rule alpha"
		receipt, err := inProcessAgent.Remember(ruleAlpha)
		if err != nil || receipt != "remembered · "+ruleAlpha+" · for this project" {
			closeV3Agent(inProcessAgent)
			t.Fatalf("in-process /remember receipt = %q, err = %v, want %q", receipt, err, "remembered · "+ruleAlpha+" · for this project")
		}
		closeV3Agent(inProcessAgent)

		hostedAgent, _, _, err := openV3Agent(hostedLaunch.Config, hostedLaunch.Project, session.New)
		if err != nil {
			t.Fatalf("openV3Agent hosted: %v", err)
		}
		defer closeV3Agent(hostedAgent)
		const ruleBeta = "proj rule beta"
		hostedReceipt, err := hostedAgent.Remember(ruleBeta)
		if err != nil || hostedReceipt != "remembered · "+ruleBeta+" · for this project" {
			t.Fatalf("hosted /remember receipt = %q, err = %v", hostedReceipt, err)
		}
		lines, err := hostedAgent.AlwaysMemories(false)
		if err != nil {
			t.Fatalf("AlwaysMemories: %v", err)
		}
		foundAlpha, foundBeta := false, false
		for _, line := range lines {
			if line.Text == ruleAlpha && line.Where == "for this project" {
				foundAlpha = true
			}
			if line.Text == ruleBeta && line.Where == "for this project" {
				foundBeta = true
			}
		}
		if !foundAlpha || !foundBeta {
			t.Fatalf("AlwaysMemories = %+v, want both %q and %q for this project", lines, ruleAlpha, ruleBeta)
		}
	})

	t.Run("genuinely_unanchored_conversation_keeps_no_project_of_its_own", func(t *testing.T) {
		deadCatalogEndpoint(t)
		proc := v3TestProcess(t)
		proc.LaunchDir = t.TempDir()

		if key := v3ProjectKey(""); key != "" {
			t.Fatalf("v3ProjectKey(\"\") = %q, want empty", key)
		}

		// An unanchored launch (opts.Workspace == "" in a placeless directory)
		// opens an owned Place whose work/ directory gets a path hash from
		// v3ProjectKey, while session.Agent.ownerFor gates project scope on
		// !Place.Owned so MemoryProject() is "" and /remember widens to
		// "for you everywhere, since this conversation has no project of its own".
		unanchoredLaunch, err := openV3Launch(proc, v3Options{
			Model:       "test/model",
			Interactive: true,
		})
		if err != nil {
			t.Fatalf("openV3Launch unanchored: %v", err)
		}
		if !unanchoredLaunch.Place.Owned {
			t.Fatal("unanchored launch did not keep Place.Owned=true")
		}
		unanchoredAgent, _, _, err := openV3Agent(unanchoredLaunch.Config, unanchoredLaunch.Project, session.New)
		if err != nil {
			t.Fatalf("openV3Agent unanchored: %v", err)
		}
		defer closeV3Agent(unanchoredAgent)
		if proj, err := unanchoredAgent.MemoryProject(); err != nil || proj != "" {
			t.Fatalf("unanchored MemoryProject() = %q, %v, want empty", proj, err)
		}
		const widened = "for you everywhere, since this conversation has no project of its own"
		if receipt, err := unanchoredAgent.Remember("prefers tabs"); err != nil || receipt != "remembered · prefers tabs · "+widened {
			t.Fatalf("unanchored Remember = %q, %v, want widened receipt", receipt, err)
		}
	})
}
