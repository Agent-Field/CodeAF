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

// THE FIRST OFFER'S DOORS: a known checkout with no recipe file is owed the
// offer; WriteRecipe writes the default recipe there and answers the branch the
// checkout stands on; a file already there is never written over; `not now` is
// kept by the store, so a second seam over the same store owes nothing; and a
// repository with no known checkout is owed nothing.
func TestLocalRecipeOfferWriteAndNotNow(t *testing.T) {
	_, st := openLocal(t)
	work := t.TempDir()
	codeaf, notes := filepath.Join(work, "codeaf"), filepath.Join(work, "notes")
	for _, d := range []string{codeaf, notes} {
		if err := os.MkdirAll(filepath.Join(d, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(codeaf, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirs := func(repo string) string {
		switch repo {
		case "agentfield/codeaf":
			return codeaf
		case "santoshkumarradha/notes":
			return notes
		}
		return ""
	}
	seam := factory.LocalSeam(st, time.Now(), factory.WithRepoDirs(dirs))
	for _, door := range []string{"recipeoffer", "writerecipe", "recipenotnow"} {
		if !seam.Has(door) {
			t.Fatalf("a seam told where repositories are has no %s door", door)
		}
	}
	if bare := factory.LocalSeam(st, time.Now()); bare.Has("writerecipe") || bare.Has("recipeoffer") {
		t.Fatal("a seam with nowhere to write offers a recipe file")
	}
	ctx := context.Background()
	if due, err := seam.RecipeOffer(ctx, "agentfield/codeaf"); err != nil || !due {
		t.Fatalf("a checkout with no recipe file is owed nothing: %v, %v", due, err)
	}
	if due, _ := seam.RecipeOffer(ctx, "agentfield/elsewhere"); due {
		t.Fatal("a repository with no known checkout is owed the offer")
	}
	branch, err := seam.WriteRecipe(ctx, "agentfield/codeaf")
	if err != nil || branch != "main" {
		t.Fatalf("WriteRecipe = %q, %v", branch, err)
	}
	r, probs, err := factory.Load(codeaf)
	if err != nil || len(probs) > 0 || len(r.For(factory.KindIssue)) != len(factory.DefaultRecipe().For(factory.KindIssue)) {
		t.Fatalf("the written recipe reads %+v, %v, %v", r, probs, err)
	}
	if due, _ := seam.RecipeOffer(ctx, "agentfield/codeaf"); due {
		t.Fatal("a checkout with a recipe file is still owed the offer")
	}
	if _, err := seam.WriteRecipe(ctx, "agentfield/codeaf"); err == nil || !strings.Contains(err.Error(), "has a recipe already") {
		t.Fatalf("a second write = %v, want a refusal", err)
	}

	if due, _ := seam.RecipeOffer(ctx, "santoshkumarradha/notes"); !due {
		t.Fatal("notes is not owed the offer")
	}
	if err := seam.RecipeNotNow("santoshkumarradha/notes"); err != nil {
		t.Fatal(err)
	}
	again := factory.LocalSeam(st, time.Now(), factory.WithRepoDirs(dirs))
	if due, err := again.RecipeOffer(ctx, "santoshkumarradha/notes"); err != nil || due {
		t.Fatalf("not now was not kept: %v, %v", due, err)
	}
	if b, err := again.WriteRecipe(ctx, "santoshkumarradha/notes"); err != nil || b != "" {
		t.Fatalf("a checkout whose HEAD is unreadable answered %q, %v", b, err)
	}
}
