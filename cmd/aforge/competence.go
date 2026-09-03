package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

func runCompetence(args []string) error {
	return runCompetenceTo(args, os.Stdout, time.Now().UTC())
}

func runCompetenceTo(args []string, output io.Writer, now time.Time) error {
	flags := commandFlags("competence")
	database := flags.String("db", defaultChatDB(), "path to the durable graph database")
	modelFlag := flags.String("model", "", "working model whose profile buckets to include")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: aforge competence [--db path] [--model slug]")
	}
	path, err := expandHome(strings.TrimSpace(*database))
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("open competence map: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("open competence map: %s is not a regular database file", path)
	}
	graph, err := store.Open(path)
	if err != nil {
		return err
	}
	defer graph.Close()

	prefs := loadChatPrefs(filepath.Dir(path))
	model := firstNonEmptyString(*modelFlag, prefs.TaskModel, os.Getenv("AFORGE_MODEL"), config.DefaultModel)
	competence, err := measureCompetence(graph, os.Getenv("AFORGE_PROFILE_DIR"), model, now)
	if err != nil {
		return err
	}
	return writeCompetence(output, competence, now)
}

func measureCompetence(graph *store.Store, profileDir, model string, now time.Time) (store.CompetenceMap, error) {
	measured, err := profile.Load(profileDir, model, "linear")
	if err != nil {
		return store.CompetenceMap{}, fmt.Errorf("load competence profile: %w", err)
	}
	return graph.CompetenceMap(store.CompetenceOptions{Profile: measured, Now: now})
}

func writeCompetence(output io.Writer, competence store.CompetenceMap, now time.Time) error {
	if len(competence.Scopes) == 0 {
		_, err := fmt.Fprintln(output, "No competence evidence yet.")
		return err
	}
	groups := []store.CompetenceClass{
		store.CompetenceStrong,
		store.CompetenceFrontier,
		store.CompetenceWeak,
		store.CompetenceStale,
	}
	wroteGroup := false
	for _, class := range groups {
		var rows []store.ScopeCompetence
		for _, scope := range competence.Scopes {
			if scope.Class == class {
				rows = append(rows, scope)
			}
		}
		if len(rows) == 0 {
			continue
		}
		if wroteGroup {
			if _, err := fmt.Fprintln(output); err != nil {
				return err
			}
		}
		wroteGroup = true
		if _, err := fmt.Fprintln(output, class); err != nil {
			return err
		}
		for _, scope := range rows {
			if _, err := fmt.Fprintf(output, "  %s — %s\n", competenceLabel(scope), competenceEvidence(scope, now)); err != nil {
				return err
			}
		}
	}
	return nil
}

func competenceLabel(scope store.ScopeCompetence) string {
	if scope.Kind == store.CompetenceProfile {
		return strings.TrimPrefix(scope.Scope, "profile:") + " work"
	}
	return scope.Scope
}

func competenceEvidence(scope store.ScopeCompetence, now time.Time) string {
	parts := make([]string, 0, 4)
	if scope.Class == store.CompetenceStale {
		parts = append(parts, "last touched "+store.AgeLabel(scope.LastTouched, now))
	} else if scope.Samples == 0 {
		parts = append(parts, "installed, not yet exercised")
	} else {
		failed := int(scope.FailureRate*100 + 0.5)
		parts = append(parts, fmt.Sprintf("%d runs · %d%% failed", scope.Samples, failed))
		if scope.SurpriseTrend != store.SurpriseUnknown {
			parts = append(parts, "surprise "+string(scope.SurpriseTrend))
		}
	}
	if count := len(scope.InstalledSkills); count > 0 {
		parts = append(parts, fmt.Sprintf("%d installed %s", count, pluralWord(count, "skill")))
	}
	return strings.Join(parts, " · ")
}

func pluralWord(count int, word string) string {
	if count == 1 {
		return word
	}
	return word + "s"
}
