package main

import (
	"context"
	"database/sql"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/embed"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/wsapi"
	"github.com/Agent-Field/codeaf/internal/wsdiscover"
	_ "modernc.org/sqlite"
)

type scriptedEmbedWire struct {
	vector []float32
	model  string
	calls  int
}

func (s *scriptedEmbedWire) Embed(_ context.Context, request provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	s.calls++
	data := make([]provider.Embedding, len(request.Input))
	for i := range request.Input {
		data[i] = provider.Embedding{Index: i, Embedding: append([]float32(nil), s.vector...)}
	}
	model := s.model
	if model == "" {
		model = request.Model
	}
	return &provider.EmbeddingResponse{Model: model, Data: data}, nil
}

func TestSQLiteOpenBindsWorkspaceStoreAndDiscoveryDB(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEAF_HOME", home)
	svc, adapter := openV3FolderServiceWith(nil)
	if svc == nil {
		t.Fatal("Open must bind *workspace.Store, got nil service")
	}
	t.Cleanup(func() { _ = svc.Close() })
	if adapter == nil {
		t.Fatal("Open must bind a real discovery.db, got nil adapter")
	}
	if _, err := os.Stat(filepath.Join(home, "v3", "collections.db")); err != nil {
		t.Fatalf("collections.db: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "v3", "discovery.db")); err != nil {
		t.Fatalf("discovery.db: %v", err)
	}
	if adapter.currentEmbedder() != nil {
		t.Fatal("a keyless Open must not bind a dummy embedder")
	}
	folder, err := svc.CreateFolder(context.Background(), "Billing")
	if err != nil || folder.Name != "Billing" {
		t.Fatalf("real store CreateFolder: %+v %v", folder, err)
	}
	view, err := svc.IndexProgress(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !view.Delayed || view.Detail != embed.LabelDelayed {
		t.Fatalf("nil embedder must be delayed, got %+v", view)
	}
	if strings.Contains(view.Detail, embed.Checked) {
		t.Fatalf("delayed path claimed checked: %+v", view)
	}
}

func TestSQLiteHybridSearchThroughWsapi(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	wire := &scriptedEmbedWire{vector: []float32{1, 0}, model: "openai/text-embedding-3-small"}
	client := embed.New(wire, "openai/text-embedding-3-small", nil)
	svc, adapter := openV3FolderServiceWith(client)
	if svc == nil || adapter == nil {
		t.Fatal("Open must bind both stores")
	}
	t.Cleanup(func() { _ = svc.Close() })
	ctx := context.Background()
	if err := adapter.store.Ingest(ctx, []wsdiscover.Record{
		passage("chat-a", "authenticate receipt links"),
	}, client); err != nil {
		t.Fatal(err)
	}
	assertDiscoveryAppID(t, discoveryPath())
	hits, err := svc.SearchEvidence(ctx, wsapi.SearchQuery{Query: "receipt", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("hybrid search missed the ingested passage")
	}
	foundEmbed := false
	for _, hit := range hits {
		if hit.ScoreKind == wsapi.ScoreEmbed && !hit.Degraded && strings.Contains(hit.Passage, "receipt") {
			foundEmbed = true
		}
		if hit.Degraded || hit.ScoreKind == wsapi.ScoreExpansion {
			t.Fatalf("live embedder must not take the degraded path: %+v", hit)
		}
	}
	if !foundEmbed {
		t.Fatalf("expected an embed hit, got %+v", hits)
	}
	view, err := svc.IndexProgress(ctx)
	if err != nil || view.Passages < 1 || view.Vectors < 1 {
		t.Fatalf("software counters %+v %v", view, err)
	}
	if view.Delayed || view.Degraded || view.Detail != "" {
		t.Fatalf("caught-up index must not say delayed: %+v", view)
	}
}

func TestEmptyIndexWithLiveEmbedderIsDelayed(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	wire := &scriptedEmbedWire{vector: []float32{1, 0}, model: "openai/text-embedding-3-small"}
	client := embed.New(wire, "openai/text-embedding-3-small", nil)
	svc, adapter := openV3FolderServiceWith(client)
	if svc == nil || adapter == nil {
		t.Fatal("Open must bind both stores")
	}
	t.Cleanup(func() { _ = svc.Close() })
	view, err := svc.IndexProgress(context.Background())
	if err != nil || !view.Delayed || view.Passages != 0 {
		t.Fatalf("uningested chats must not look caught-up: %+v %v", view, err)
	}
	if view.Detail != embed.LabelDelayed {
		t.Fatalf("empty index detail %+v", view)
	}
}

func TestSQLiteDownEmbedderIsDegradedNotDummy(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	svc, adapter := openV3FolderServiceWith(nil)
	if svc == nil || adapter == nil {
		t.Fatal("Open must bind discovery.db even when the embedder is down")
	}
	t.Cleanup(func() { _ = svc.Close() })
	ctx := context.Background()
	if err := adapter.store.Ingest(ctx, []wsdiscover.Record{
		passage("chat-a", "authenticate receipt links"),
	}, nil); err != nil {
		t.Fatal(err)
	}
	hits, err := svc.SearchEvidence(ctx, wsapi.SearchQuery{Query: "receipts", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("degraded expansion must still search the words")
	}
	sawDegraded := false
	for _, hit := range hits {
		if hit.ScoreKind == wsapi.ScoreExpansion {
			if !hit.Degraded {
				t.Fatalf("expansion hit must be labelled degraded: %+v", hit)
			}
			sawDegraded = true
		}
		if hit.ScoreKind == wsapi.ScoreEmbed && !hit.Degraded {
			t.Fatalf("down embedder must not claim a live embed hit: %+v", hit)
		}
	}
	if !sawDegraded {
		t.Fatalf("expected expansion hits, got %+v", hits)
	}
	view, err := svc.IndexProgress(ctx)
	if err != nil || !view.Delayed || !view.Degraded || view.Detail != embed.LabelDelayed {
		t.Fatalf("down embedder %+v %v", view, err)
	}
	if strings.Contains(view.Detail, embed.Checked) {
		t.Fatalf("degraded path claimed checked: %+v", view)
	}
}

func TestSQLiteCorruptDiscoveryLeavesFoldersUp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEAF_HOME", home)
	path := filepath.Join(home, "v3", "discovery.db")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("this is not a discovery database"), 0600); err != nil {
		t.Fatal(err)
	}
	svc, adapter := openV3FolderServiceWith(nil)
	if svc == nil {
		t.Fatal("a failed discovery Open must not take folders down")
	}
	t.Cleanup(func() { _ = svc.Close() })
	if adapter != nil {
		t.Fatal("corrupt discovery.db must not bind a discoverer")
	}
	if _, err := svc.CreateFolder(context.Background(), "Billing"); err != nil {
		t.Fatalf("membership must still work: %v", err)
	}
	view, err := svc.IndexProgress(context.Background())
	if err != nil || !view.Delayed || view.Detail != "discovery delayed" {
		t.Fatalf("failed discovery Open must be delayed, got %+v %v", view, err)
	}
	root, err := svc.RootSnapshot(context.Background())
	if err != nil || len(root.Folders) != 1 {
		t.Fatalf("must not invent an empty RootView: %+v %v", root, err)
	}
}

func TestBindSessionEmbedderUpgradesThePin(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	folders := openV3FoldersWith(nil)
	if folders == nil {
		t.Fatal("openV3FoldersWith returned nil")
	}
	wrapped := folders.(*sessionFolders)
	t.Cleanup(func() { _ = wrapped.Close() })
	if wrapped.disc == nil || wrapped.disc.currentEmbedder() != nil {
		t.Fatal("keyless bind must start delayed, not with a dummy")
	}
	wire := &scriptedEmbedWire{vector: []float32{0, 1}, model: "openai/text-embedding-3-small"}
	client := embed.New(wire, "openai/text-embedding-3-small", nil)
	bindSessionEmbedder(folders, client)
	if wrapped.disc.currentEmbedder() == nil {
		t.Fatal("session pin must replace the delayed embedder")
	}
	if _, ok := wrapped.disc.currentEmbedder().(*embed.Client); !ok {
		t.Fatalf("production bind must be *embed.Client, got %T", wrapped.disc.currentEmbedder())
	}
}

func TestProductionBindDoesNotUseTheTestFake(t *testing.T) {
	for _, name := range []string{"discovery_adapter.go", "chatv3_folders.go", "chatv3.go", "chatv3_process.go", "chatv3_embed.go", "organize_bind.go", "chatv3_standing.go"} {
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "wsdiscover.Fake") {
			t.Errorf("%s binds the test fake; production must use RoleEmbed or stay delayed", name)
		}
	}
}

func TestBindFunctionsStayUnderTheCeiling(t *testing.T) {
	const ceiling = 15
	set := token.NewFileSet()
	for _, name := range []string{"discovery_adapter.go", "chatv3_folders.go", "organize_bind.go", "chatv3_embed.go", "chatv3_wave2.go"} {
		source, err := parser.ParseFile(set, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range source.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			got := bindCyclomatic(function.Body)
			if got > ceiling {
				t.Errorf("%s %s is %d, ceiling is %d", name, function.Name.Name, got, ceiling)
			}
		}
	}
}

func bindCyclomatic(body *ast.BlockStmt) int {
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

func passage(session, text string) wsdiscover.Record {
	return wsdiscover.Record{
		SessionID:  session,
		SourceRef:  "chat:" + session,
		Speaker:    "user",
		Text:       text,
		Generation: 1,
		Ordinal:    1,
	}
}

func assertDiscoveryAppID(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var app int
	if err := db.QueryRow("PRAGMA application_id").Scan(&app); err != nil {
		t.Fatal(err)
	}
	if app != 0x41464453 {
		t.Fatalf("discovery application_id = %d, want AFDS", app)
	}
}
