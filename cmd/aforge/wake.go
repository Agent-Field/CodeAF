package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/head"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

type wakeBuilder func(graph *store.Store, path string) (*resident.Reconciler, error)

// runWake opens the resident store, runs one bounded watch pass, and exits. It
// starts neither the reconciler daemon nor the worker runner.
func runWake(args []string) error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	return runWakeWith(args, os.Stdout, func(graph *store.Store, path string) (*resident.Reconciler, error) {
		prefs := loadChatPrefs(filepath.Dir(path))
		chatClient, err := newLiveClient(settings, firstNonEmptyString(prefs.ChatModel, settings.Model))
		if err != nil {
			return nil, err
		}
		taskClient, err := newLiveClient(settings, firstNonEmptyString(prefs.TaskModel, settings.Model))
		if err != nil {
			return nil, err
		}
		measured, _ := profile.Load(settings.ProfileDir, taskClient.Model(), "linear")
		plan.UseAnchors(measured.Anchors)
		plans := &jobPlans{graphs: map[string]plannedJob{}}
		compiler := head.NewCompiler(chatClient)
		reconciler := resident.New(graph,
			func(ctx context.Context, instruction, graphContext string) (resident.Compiled, error) {
				brief, err := compiler.Compile(settings.Context(ctx, instruction), instruction, graphContext)
				if err != nil {
					return resident.Compiled{}, err
				}
				return resident.Compiled{
					Goal: brief.Goal, Assumptions: brief.Assumptions, Scale: brief.Scale,
					TrialOf: brief.TrialOf, BuildsOn: brief.BuildsOn, Question: brief.Question,
				}, nil
			},
			planSubtree(settings, taskClient, plans, graph),
		).WithTitler(titleGoal(settings, chatClient)).
			WithWatchEngine(settings.DailyBudgetUSD, checkSentinel(settings, chatClient))
		return reconciler, nil
	})
}

func runWakeWith(args []string, output io.Writer, build wakeBuilder) error {
	flags := flag.NewFlagSet("wake", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	database := flags.String("db", defaultChatDB(), "path to the durable graph database")
	if err := flags.Parse(reorder(args, map[string]bool{"db": true})); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: aforge wake [--db path]")
	}
	path, err := expandHome(strings.TrimSpace(*database))
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("open wake store: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("open wake store: %s is not a regular database file", path)
	}
	graph, err := store.Open(path)
	if err != nil {
		return err
	}
	defer graph.Close()
	reconciler, err := build(graph, path)
	if err != nil {
		return err
	}
	pass, err := reconciler.WatchOnce(context.Background())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "examined %d, checked %d, fired %d, no %d, errors %d, rail waits %d\n",
		pass.Examined, pass.Checked, pass.Fired, pass.No, pass.Errors, pass.RailWaits)
	return err
}
