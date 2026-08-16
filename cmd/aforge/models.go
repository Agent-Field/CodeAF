package main

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/router"
)

// runModels prints what the harness has learned about its panel.
//
// It is the only window onto the ledger, and it is worth having for one reason
// above the others: a rating that is wrong is invisible everywhere else. The
// counts are printed next to the ratings because a rating with three
// observations behind it and one with three hundred are different claims, and
// the table is the only place that difference can be seen.
func runModels(args []string) error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	ledger, err := router.LoadLedger(settings.ProfileDir)
	if err != nil {
		return err
	}
	entries := ledger.Entries()
	// The same daily-cached listing every other surface reads. This one is a
	// report and may wait for it: a panel line without the model's own
	// capabilities is the line this command exists to improve on.
	models := catalog.Load(context.Background(), catalog.Options{
		BaseURL: settings.BaseURL, APIKey: settings.APIKey, Dir: settings.ProfileDir,
	})

	if len(settings.Panel.Models) == 0 {
		line := "panel:  none — AFORGE_MODELS is unset, so every call goes to " + settings.Model
		if word := reasoningWord(models, settings.Model); word != "" {
			line += "  " + word
		}
		fmt.Println(line)
	} else {
		fmt.Println("panel:")
		for _, spec := range settings.Panel.Models {
			line := "  " + spec.Slug
			if spec.Role != "" {
				line += "  (" + spec.Role + ")"
			}
			if spec.Price > 0 {
				line += fmt.Sprintf("  $%.3f/M out", spec.Price)
			}
			if word := reasoningWord(models, spec.Slug); word != "" {
				line += "  " + word
			}
			fmt.Println(line)
		}
	}
	if aliases := ledger.Aliases(); len(aliases) > 0 {
		// A floating alias is a moving target and the ledger keys on what it
		// actually served, so which snapshot that was is not a detail.
		fmt.Println("\nresolved:")
		slugs := make([]string, 0, len(aliases))
		for slug := range aliases {
			slugs = append(slugs, slug)
		}
		sort.Strings(slugs)
		for _, slug := range slugs {
			fmt.Printf("  %-44s → %s\n", slug, aliases[slug])
		}
	}

	if len(entries) == 0 {
		fmt.Println("\nnothing measured yet. Ratings appear once calls have been graded — " +
			"run a plan or a graph with AFORGE_MODELS set.")
		return nil
	}
	fmt.Printf("\n  %-22s %-38s %7s %7s %6s\n", "class", "model", "rating", "p(pass)", "n")
	for _, entry := range entries {
		// Whether a rating is being *used* is a different question from what it
		// says, and it is the one worth seeing: under the gate the ordering reads
		// the cold-start prior instead, so a striking number with n=5 beside it is
		// not driving anything. Arm B's collapse is what happens when that is
		// invisible.
		gate := ""
		if entry.Count < router.MinGraded {
			gate = fmt.Sprintf("  under the gate — ordering uses the prior until n=%d", router.MinGraded)
		}
		fmt.Printf("  %-22s %-38s %+7.2f %7.2f %6d%s\n",
			entry.Class, clip(entry.Model, 38), entry.Rating, router.Ability(entry.Rating), entry.Count, gate)
	}
	// Said once, at the bottom, because it is the thing most likely to be
	// misread: these are relative abilities within one class, on a logit scale,
	// and they are not comparable across classes.
	fmt.Println("\n  rating is a Rasch ability in logits, comparable only within a class.")
	fmt.Println("  p(pass) is that rating against an average call of the class.")
	fmt.Println("  a class written class/shape is one sub-population of it, rated separately.")
	return nil
}

// reasoningWord is the catalog's phrase for what a model does with reasoning,
// for a slug this command holds rather than a row. A model the catalog has
// never heard of says nothing, which is the same silence as a model that does
// not reason — neither is a claim.
func reasoningWord(models *catalog.Catalog, slug string) string {
	model, known := models.Model(slug)
	if !known {
		return ""
	}
	return catalog.ReasoningWord(model, provider.ReasoningMandatory(slug))
}

func panelSlugs(panel router.Panel) []string {
	names := make([]string, 0, len(panel.Models))
	for _, spec := range panel.Models {
		names = append(names, spec.Slug)
	}
	return names
}

// closeRouter flushes what a run learned, when there was a router to learn it.
// Ratings are written as they are earned, so this is only ever picking up a
// flush that a busy file lock deferred.
func closeRouter(client any) {
	panel, routed := client.(*router.Router)
	if !routed {
		return
	}
	if err := panel.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "note: could not save the router ledger: %v\n", err)
	}
}
