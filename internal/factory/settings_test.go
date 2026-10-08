package factory_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE LOCAL SEAM'S SETTINGS DOORS: the watched repositories and the rail are
// the store's, the list is the lister's, and the floor reads the rail back.
func TestLocalSettingsDoors(t *testing.T) {
	_, st := openLocal(t)
	listed := []factory.RepoInfo{{Full: "agentfield/codeaf", Owner: "agentfield", Name: "codeaf", Private: true}}
	seam := factory.LocalSeam(st, time.Now(), factory.WithRepoLister(func(context.Context) ([]factory.RepoInfo, error) { return listed, nil }))
	for _, door := range []string{"repos", "setrepos", "setrail"} {
		if !seam.Has(door) {
			t.Fatalf("the local seam has no %s door", door)
		}
	}
	for _, door := range []string{"github", "ghlogin", "connectgithub", "recipeat", "saverecipe"} {
		if seam.Has(door) {
			t.Fatalf("the local seam answered %s with nothing to stand behind it", door)
		}
	}
	if err := seam.SetRepos([]string{"agentfield/codeaf", "santoshkumarradha/notes"}); err != nil {
		t.Fatal(err)
	}
	watched, available, err := seam.Repos(context.Background())
	if err != nil || strings.Join(watched, " ") != "agentfield/codeaf santoshkumarradha/notes" || len(available) != 1 {
		t.Fatalf("Repos = %v, %v, %v", watched, available, err)
	}
	if err := seam.SetRail(60); err != nil {
		t.Fatal(err)
	}
	if err := seam.SetRail(-1); err == nil {
		t.Fatal("a rail below nothing was kept")
	}
	snap, err := seam.Load()
	if err != nil || snap.Rail != 60 {
		t.Fatalf("the floor reads rail %v, %v", snap.Rail, err)
	}
	if err := seam.SetRail(0); err != nil {
		t.Fatal(err)
	}
	if snap, _ := seam.Load(); snap.Rail != 0 {
		t.Fatalf("a rail taken off reads %v", snap.Rail)
	}

	// WITH CHECKOUTS KNOWN each listed repository carries its own, and one
	// this machine has not cloned carries none.
	listed = append(listed, factory.RepoInfo{Full: "santoshkumarradha/notes", Owner: "santoshkumarradha", Name: "notes", Open: -1})
	dirs := map[string]string{"agentfield/codeaf": "/work/codeaf"}
	here := factory.LocalSeam(st, time.Now(),
		factory.WithRepoLister(func(context.Context) ([]factory.RepoInfo, error) {
			return append([]factory.RepoInfo(nil), listed...), nil
		}),
		factory.WithRepoDirs(func(repo string) string { return dirs[repo] }))
	if _, available, err := here.Repos(context.Background()); err != nil || len(available) != 2 || available[0].Dir != "/work/codeaf" || available[1].Dir != "" || available[1].Open != -1 {
		t.Fatalf("Repos with checkouts = %+v, %v", available, err)
	}

	// With no lister the picker lists only what is watched.
	bare := factory.LocalSeam(st, time.Now())
	if _, available, err := bare.Repos(context.Background()); err != nil || available != nil {
		t.Fatalf("no lister listed %v, %v", available, err)
	}
}

// WITH A CHECKOUT KNOWN, RecipeAt reads the file with its problems and
// SaveRecipe writes it whole; an unknown checkout reads the default recipe
// and no folder, and saving there is refused in words.
func TestLocalRecipeDoors(t *testing.T) {
	_, st := openLocal(t)
	repoDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoDir, ".codeaf"), 0o755); err != nil {
		t.Fatal(err)
	}
	text := "# factory recipe · codeaf\n\n## issue\n1. plan · chat · read it · until clen\n"
	if err := os.WriteFile(filepath.Join(repoDir, factory.RecipeFile), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	seam := factory.LocalSeam(st, time.Now(), factory.WithRepoDirs(func(repo string) string {
		if repo == "agentfield/codeaf" {
			return repoDir
		}
		return ""
	}))
	r, probs, dir, err := seam.RecipeAt("agentfield/codeaf")
	if err != nil || dir != repoDir || len(probs) != 1 || probs[0].Line != 4 {
		t.Fatalf("RecipeAt = %v, %+v, %q, %v", r, probs, dir, err)
	}
	r = factory.DefaultRecipe()
	r.ByKind = map[factory.Kind][]factory.Stage{factory.KindPR: {{Name: "read", Ask: "the diff", On: true}}}
	if err := seam.SaveRecipe("agentfield/codeaf", r); err != nil {
		t.Fatal(err)
	}
	got, probs, _, err := seam.RecipeAt("agentfield/codeaf")
	if err != nil || len(probs) != 0 || got.For(factory.KindPR)[0].Name != "read" {
		t.Fatalf("after SaveRecipe: %+v, %+v, %v", got.For(factory.KindPR), probs, err)
	}
	if _, _, dir, _ := seam.RecipeAt("agentfield/agentfield"); dir != "" {
		t.Fatalf("an unknown checkout answered %q", dir)
	}
	if err := seam.SaveRecipe("agentfield/agentfield", r); err == nil || !strings.Contains(err.Error(), "does not know where") {
		t.Fatalf("saving with no checkout said %v", err)
	}
}
