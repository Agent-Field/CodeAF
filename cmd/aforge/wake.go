package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/lease"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

const (
	defaultWakeMaxSeconds = 120
	maxWakeTicks          = 32
)

type wakeBuilder func(graph *store.Store, path string) (*resident.Reconciler, error)

// runWake opens the resident store, serves the full resident role for one
// bounded pass, and exits. It starts no permanent process or worker runner.
func runWake(args []string) error {
	return runWakeWith(args, os.Stdout, func(graph *store.Store, path string) (*resident.Reconciler, error) {
		settings, err := config.Load()
		if err != nil {
			return nil, err
		}
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
		return newResidentReconciler(settings, graph, chatClient, taskClient, plans), nil
	})
}

func runWakeWith(args []string, output io.Writer, build wakeBuilder) error {
	flags := flag.NewFlagSet("wake", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	database := flags.String("db", defaultChatDB(), "path to the durable graph database")
	maxSeconds := flags.Int("max-seconds", defaultWakeMaxSeconds, "maximum resident pass duration")
	if err := flags.Parse(reorder(args, map[string]bool{"db": true, "max-seconds": true})); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: aforge wake [--db path] [--max-seconds N]")
	}
	if *maxSeconds <= 0 {
		return fmt.Errorf("wake max-seconds must be positive")
	}
	path, err := expandHome(strings.TrimSpace(*database))
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	holder, err := lease.ProbeResident(dir)
	if err != nil {
		return err
	}
	if holder != nil {
		_, err = fmt.Fprintf(output, "resident alive (pid %d) — skipping wake\n", holder.PID)
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("open wake store: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("open wake store: %s is not a regular database file", path)
	}
	releaseResident, heldBy, err := lease.AcquireResident(dir, "wake")
	if err != nil {
		return err
	}
	if releaseResident == nil {
		_, err = fmt.Fprintf(output, "resident alive (pid %d) — skipping wake\n", heldBy.PID)
		return err
	}
	defer releaseResident()

	graph, err := store.Open(path)
	if err != nil {
		return err
	}
	defer graph.Close()
	reconciler, err := build(graph, path)
	if err != nil {
		return err
	}
	startSeq, err := graph.LatestEventSeq()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*maxSeconds)*time.Second)
	defer cancel()
	var pass resident.WatchPass
	for tick := 0; tick < maxWakeTicks; tick++ {
		before, watermarkErr := graph.LatestEventSeq()
		if watermarkErr != nil {
			return watermarkErr
		}
		tickErr := reconciler.Tick(ctx)
		addWatchPass(&pass, reconciler.LastWatchPass())
		if tickErr != nil {
			if errors.Is(tickErr, context.DeadlineExceeded) || errors.Is(tickErr, context.Canceled) {
				break
			}
			return tickErr
		}
		after, watermarkErr := graph.LatestEventSeq()
		if watermarkErr != nil {
			return watermarkErr
		}
		if after == before || ctx.Err() != nil {
			break
		}
	}
	events, err := graph.Events(startSeq, 0)
	if err != nil {
		return err
	}
	practice, learning := 0, 0
	for _, event := range events {
		switch event.Kind {
		case store.EventQuestionPracticeStarted:
			practice++
		case store.EventFactLearned, store.EventFactActivated:
			learning++
		}
	}
	_, err = fmt.Fprintf(output, "examined %d, checked %d, fired %d, no %d, errors %d, rail waits %d, practice %d, learning %d\n",
		pass.Examined, pass.Checked, pass.Fired, pass.No, pass.Errors, pass.RailWaits, practice, learning)
	return err
}

func addWatchPass(total *resident.WatchPass, pass resident.WatchPass) {
	total.Examined += pass.Examined
	total.Woken += pass.Woken
	total.Checked += pass.Checked
	total.Fired += pass.Fired
	total.Proposed += pass.Proposed
	total.No += pass.No
	total.Errors += pass.Errors
	total.Quota += pass.Quota
	total.Expired += pass.Expired
	total.RailWaits += pass.RailWaits
}
