package main

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/tui3"
	"github.com/Agent-Field/codeaf/internal/workspace"
	"github.com/Agent-Field/codeaf/internal/wsapi"
	"github.com/Agent-Field/codeaf/internal/wsexec"
)

func TestWorkingStoreWiresExecAndCorruptStoreLeavesItAbsent(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	folders := openV3Folders()
	if folders == nil {
		t.Fatal("openV3Folders returned nil")
	}
	var options tui3.Options
	attachSurfaceFolders(&options, folders)
	if options.Exec == nil {
		t.Fatal("a working collections.db must assign Options.Exec so launch state is present")
	}
	if sessionExecOf(folders, "aaaaaaaaaaaaaaaa") == nil {
		t.Fatal("session Exec must be present so launch-or-join is on the belt")
	}
}

func TestCorruptStoreLeavesExecNil(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEAF_HOME", home)
	path := filepath.Join(home, "v3", "collections.db")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not a database"), 0600); err != nil {
		t.Fatal(err)
	}
	var options tui3.Options
	attachSurfaceFolders(&options, openV3Folders())
	if options.Exec != nil {
		t.Fatal("Options.Exec must stay nil when the store cannot open")
	}
	if sessionExecOf(openV3Folders(), "aaaaaaaaaaaaaaaa") != nil {
		t.Fatal("session Exec must stay nil when the store cannot open")
	}
}

func TestRealStoreGrantAndLaunchPersist(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	svc, _ := openV3FolderServiceWith(nil)
	if svc == nil || svc.Workspace() == nil {
		t.Fatal("production wsapi.Open must bind collections.db")
	}
	ctx := context.Background()
	grant, err := svc.IssueGrant(ctx, wsapi.GrantRequest{
		CoordinatorID: "aaaaaaaaaaaaaaaa", Goal: "add a readme comment",
		ActionClasses: []string{workspace.ClassExecute, workspace.ClassSteer, workspace.ClassStop},
	})
	if err != nil || grant.ID == "" {
		t.Fatalf("IssueGrant on real store: %+v, %v", grant, err)
	}
	held, err := svc.Workspace().GetGrant(ctx, grant.ID)
	if err != nil || held.Origin != workspace.OriginPerson {
		t.Fatalf("origin %q, want person so a model cannot stamp the grant", held.Origin)
	}
	view, err := svc.LaunchOrJoin(ctx, wsapi.LaunchWorkRequest{
		GrantID: grant.ID, CoordinatorID: "aaaaaaaaaaaaaaaa", OwnerChatID: "aaaaaaaaaaaaaaaa",
		Brief: "add a readme comment", EquivalenceKey: "issue-42", IdempotencyKey: "rk-real-1",
	})
	if err == nil && (view.State == workspace.BindCompleted || view.State == "100%") {
		t.Fatalf("launch must not fabricate completed: %+v", view)
	}
	if err != nil && !errors.Is(err, wsexec.ErrAbsent) {
		t.Fatalf("launch without a live host must be labelled absence: %v", err)
	}
	row, err := svc.Workspace().BindingByRequestKey(ctx, "rk-real-1")
	if err != nil || row.RequestKey != "rk-real-1" || row.RunInstanceID != "" {
		t.Fatalf("reserved row: %+v, %v", row, err)
	}
	if row.State != workspace.BindReserved {
		t.Fatalf("state %q, want reserved before runtime admission", row.State)
	}
}

func TestRecoverFindsReservedRowAndDoesNotAdmitTwice(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	svc, _ := openV3FolderServiceWith(nil)
	if svc == nil || svc.Workspace() == nil {
		t.Fatal("production wsapi.Open must bind collections.db")
	}
	ctx := context.Background()
	grant, err := svc.IssueGrant(ctx, wsapi.GrantRequest{
		CoordinatorID: "aaaaaaaaaaaaaaaa", Goal: "add a readme comment",
		ActionClasses: []string{workspace.ClassExecute},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = svc.LaunchOrJoin(ctx, wsapi.LaunchWorkRequest{
		GrantID: grant.ID, CoordinatorID: "aaaaaaaaaaaaaaaa", OwnerChatID: "aaaaaaaaaaaaaaaa",
		Brief: "add a readme comment", EquivalenceKey: "issue-42", IdempotencyKey: "rk-recover-1",
	})
	recoverUnboundWork(ctx, svc.Workspace())
	row, err := svc.Workspace().BindingByRequestKey(ctx, "rk-recover-1")
	if err != nil {
		t.Fatal(err)
	}
	if row.RunInstanceID != "" {
		t.Fatalf("recover without a live runtime must not invent a run-instance: %+v", row)
	}
	again, err := svc.LaunchOrJoin(ctx, wsapi.LaunchWorkRequest{
		GrantID: grant.ID, CoordinatorID: "bbbbbbbbbbbbbbbb", OwnerChatID: "bbbbbbbbbbbbbbbb",
		Brief: "add a readme comment", EquivalenceKey: "issue-42", IdempotencyKey: "rk-recover-2",
	})
	if err != nil && !errors.Is(err, wsexec.ErrAbsent) {
		t.Fatalf("second launch: %v", err)
	}
	if again.Joined && again.RequestKey != "rk-recover-1" {
		t.Fatalf("join should follow the reserved row: %+v", again)
	}
	if again.State == workspace.BindCompleted {
		t.Fatalf("join must not fabricate completed: %+v", again)
	}
}

func TestLaunchStateReadsBindingsFromRealStore(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	handles := openV3FolderHandles()
	if handles.folders == nil {
		t.Fatal("folders missing")
	}
	var options tui3.Options
	attachSurfaceFolders(&options, handles.folders)
	if options.Exec == nil {
		t.Fatal("Options.Exec missing")
	}
	wrapped, ok := handles.folders.(*sessionFolders)
	if !ok || wrapped.svc == nil {
		t.Fatal("production folders must wrap wsapi")
	}
	svc := wrapped.svc
	ctx := context.Background()
	grant, err := svc.IssueGrant(ctx, wsapi.GrantRequest{
		CoordinatorID: "aaaaaaaaaaaaaaaa", Goal: "add a readme comment",
		ActionClasses: []string{workspace.ClassExecute},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = svc.LaunchOrJoin(ctx, wsapi.LaunchWorkRequest{
		GrantID: grant.ID, CoordinatorID: "aaaaaaaaaaaaaaaa", OwnerChatID: "aaaaaaaaaaaaaaaa",
		Brief: "add a readme comment", IdempotencyKey: "rk-tui-1",
	})
	works, err := options.Exec.LaunchState(ctx, "aaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	if len(works) == 0 {
		t.Fatal("launch state must show the reserved binding")
	}
	low := strings.ToLower(works[0].State + works[0].Title + works[0].WorkID)
	if strings.Contains(low, "100%") || strings.Contains(low, "0 runs") {
		t.Fatalf("emptiness/dummy words painted: %+v", works[0])
	}
}

func TestPauseCoordinationDoesNotStopWorkOnRealStore(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	svc, _ := openV3FolderServiceWith(nil)
	ctx := context.Background()
	from, to := "aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb"
	collab := &sessionCollab{svc: svc, chatID: from}
	if err := collab.CoordinateSelected(ctx, []string{to}); err != nil {
		t.Fatal(err)
	}
	grant, err := svc.IssueGrant(ctx, wsapi.GrantRequest{
		CoordinatorID: from, Goal: "add a readme comment",
		ActionClasses: []string{workspace.ClassExecute, workspace.ClassStop},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = svc.LaunchOrJoin(ctx, wsapi.LaunchWorkRequest{
		GrantID: grant.ID, CoordinatorID: from, OwnerChatID: from,
		Brief: "add a readme comment", IdempotencyKey: "rk-pause-1",
	})
	if err := svc.PauseCoordination(ctx, from); err != nil {
		t.Fatal(err)
	}
	row, err := svc.Workspace().BindingByRequestKey(ctx, "rk-pause-1")
	if err != nil || row.State == workspace.BindStopped {
		t.Fatalf("pause coordination stopped work: %+v, %v", row, err)
	}
	_, err = svc.LaunchOrJoin(ctx, wsapi.LaunchWorkRequest{
		GrantID: grant.ID, CoordinatorID: from, OwnerChatID: from,
		Brief: "another change", IdempotencyKey: "rk-pause-2",
	})
	if err == nil {
		t.Fatal("paused coordinator must refuse a new launch")
	}
	if !strings.Contains(err.Error(), "paused") && !errors.Is(err, workspace.ErrInvalid) {
		t.Fatalf("new launch while paused: %v", err)
	}
}

func TestExecWiringFunctionsStayUnderTheCeiling(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	set := token.NewFileSet()
	for _, file := range files {
		name := file.Name()
		if file.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if !strings.HasPrefix(name, "exec_") {
			continue
		}
		source, err := parser.ParseFile(set, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range source.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			got := execCyclomatic(function.Body)
			if got > 15 {
				t.Errorf("%s %s is %d, ceiling is 15", name, function.Name.Name, got)
			}
		}
	}
}

func execCyclomatic(body *ast.BlockStmt) int {
	decisions := 1
	ast.Inspect(body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			decisions++
		case *ast.CaseClause:
			if typed.List != nil {
				decisions++
			}
		case *ast.CommClause:
			if typed.Comm != nil {
				decisions++
			}
		case *ast.BinaryExpr:
			if typed.Op == token.LAND || typed.Op == token.LOR {
				decisions++
			}
		}
		return true
	})
	return decisions
}
