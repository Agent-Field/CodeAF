package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/Agent-Field/aforge-v2/internal/config"
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

	if len(settings.Panel.Models) == 0 {
		fmt.Println("panel:  none — AFORGE_MODELS is unset, so every call goes to " + settings.Model)
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
	fmt.Printf("\n  %-16s %-44s %7s %7s %6s\n", "class", "model", "rating", "p(pass)", "n")
	for _, entry := range entries {
		fmt.Printf("  %-16s %-44s %+7.2f %7.2f %6d\n",
			entry.Class, clip(entry.Model, 44), entry.Rating, router.Ability(entry.Rating), entry.Count)
	}
	// Said once, at the bottom, because it is the thing most likely to be
	// misread: these are relative abilities within one class, on a logit scale,
	// and they are not comparable across classes.
	fmt.Println("\n  rating is a Rasch ability in logits, comparable only within a class.")
	fmt.Println("  p(pass) is that rating against an average call of the class.")
	return nil
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
