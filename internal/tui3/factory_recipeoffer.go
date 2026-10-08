package tui3

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── THE FIRST OFFER OF A RECIPE FILE ────────────────────────────────────────
//
// A REPOSITORY WITH NO RECIPE FILE IS OFFERED ONE ONCE (owner decision,
// 2026-10-08). When an item of a repository stands landed on the floor and
// the seam says the repository is owed the offer ([factory.Seam.RecipeOffer]:
// a known checkout with no `.codeaf/factory.md`, never put away), the bottom
// of the right column offers it where the habit offer stands, in the habit
// offer's own two lines:
//
//	no recipe in codeaf yet
//	write the recipe into the repo so the team shares it? [y] write it · [n] not now
//
// `y` writes it through [factory.Seam.WriteRecipe] and the note line says
// where; `n` puts it away through [factory.Seam.RecipeNotNow], which the store
// keeps, so it is asked once. Each repository is asked of the seam at most
// once a window. THE OFFER NEVER SHOWS WHERE THE WRITE DOOR IS ABSENT, and a
// habit offer standing outranks it: one `y` means one thing.

// factoryRecipeOfferWait is how long the seam may take to say whether a
// repository is owed the offer: a stat and a small file.
const factoryRecipeOfferWait = factoryGHWait

// factoryRecipeOfferCheck asks the seam, off the loop and off the ordered
// line, whether a repository with a landed item on the floor is owed the
// offer, and stands the offer for the first that is. nil when there is nothing
// to ask.
func (a *app) factoryRecipeOfferCheck() tea.Cmd {
	seam := a.factory
	if !seam.Has("recipeoffer") || !seam.Has("writerecipe") || a.fp.act.recipeOffer != "" {
		return nil
	}
	var repos []string
	for _, it := range a.fp.snap.Items {
		repo := strings.TrimSpace(it.Repo)
		if it.State != factory.StateLanded || repo == "" || a.fp.act.recipeAsked[repo] {
			continue
		}
		if a.fp.act.recipeAsked == nil {
			a.fp.act.recipeAsked = map[string]bool{}
		}
		a.fp.act.recipeAsked[repo] = true
		repos = append(repos, repo)
	}
	if len(repos) == 0 {
		return nil
	}
	return a.besideLine(func() func(bool) tea.Cmd {
		owed := ""
		for _, repo := range repos {
			ctx, cancel := context.WithTimeout(context.Background(), factoryRecipeOfferWait)
			due, err := seam.RecipeOffer(ctx, repo)
			cancel()
			if err == nil && due {
				owed = repo
				break
			}
		}
		return func(bool) tea.Cmd {
			if owed != "" && a.fp.act.recipeOffer == "" {
				a.fp.act.recipeOffer = owed
				a.touch()
			}
			return nil
		}
	})
}

// factoryRecipeOfferShown says whether the offer stands where a person can
// answer it: offered, the write door there, and no habit offer over it.
func (a *app) factoryRecipeOfferShown() bool {
	return a.fp.act.recipeOffer != "" && a.fp.act.habit == "" && a.factory.Has("writerecipe")
}

// factoryRecipeOfferKey is `y` or `n` while the offer stands, and false for
// every other key.
func (a *app) factoryRecipeOfferKey(k string) (tea.Cmd, bool) {
	if !a.factoryRecipeOfferShown() {
		return nil, false
	}
	switch k {
	case keyYes:
		return a.factoryRecipeWrite(), true
	case keyNo:
		return a.factoryRecipeNotNow(), true
	}
	return nil, false
}

// factoryRecipeWrite is `y`: the recipe written into the repository's
// checkout, and the note line saying where.
func (a *app) factoryRecipeWrite() tea.Cmd {
	repo := a.fp.act.recipeOffer
	a.fp.act.recipeOffer = ""
	a.touch()
	branch := ""
	return a.factoryDo(func(s factory.Seam) error {
		var err error
		branch, err = s.WriteRecipe(context.Background(), repo)
		return err
	}, func(err error) {
		if err == nil {
			a.factorySay(factoryRecipeWrittenWords(repo, branch))
		}
	})
}

// factoryRecipeNotNow is `n`: the offer put away for the repository, for good
// when the seam can keep the no.
func (a *app) factoryRecipeNotNow() tea.Cmd {
	repo := a.fp.act.recipeOffer
	a.fp.act.recipeOffer = ""
	a.touch()
	if !a.factory.Has("recipenotnow") {
		return nil
	}
	return a.factoryDo(func(s factory.Seam) error { return s.RecipeNotNow(repo) }, nil)
}

// factoryRecipeWrittenWords is the note line after `y`:
// `recipe written · .codeaf/factory.md on main`, and the repository in place
// of the branch when the checkout stands on none.
func factoryRecipeWrittenWords(repo, branch string) string {
	where := strings.TrimSpace(branch)
	if where == "" {
		where = factoryRepoShort(repo)
	}
	return wordRecipeWritten + rowSep + factory.RecipeFile + " " + wordOn + " " + where
}

// factoryRecipeOfferRows is the offer's two lines at the bottom of the right
// column, each at most measure cells, and none while it does not stand.
func (a *app) factoryRecipeOfferRows(measure int) []string {
	if !a.factoryRecipeOfferShown() {
		return nil
	}
	pal := a.pal
	return []string{
		pal.ink(fit(wordNoRecipe+" "+factoryRepoShort(a.fp.act.recipeOffer)+" "+wordYet, measure)),
		fit(pal.muted(wordWriteRecipeAsk+" ")+pal.accent("["+keyYes+"] "+wordWriteIt)+pal.dim(rowSep+"["+keyNo+"] "+wordNotNow), measure),
	}
}
