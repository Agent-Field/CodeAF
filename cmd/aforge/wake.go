package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/craft"
	"github.com/Agent-Field/aforge-v2/internal/lease"
	"github.com/Agent-Field/aforge-v2/internal/provider/pool"
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
		// Same resolution as the chat surface: an explicit plan choice splits
		// structuring from execution, empty follows the work model.
		planClient, err := newLiveClient(settings, firstNonEmptyString(prefs.PlanModel, settings.PlanModel, taskClient.Model()))
		if err != nil {
			return nil, err
		}
		// A wake pass is unattended by definition, which makes an unbounded
		// structuring call worse here than in chat: there is nobody to notice.
		chatClient.WithCallWall(pool.DefaultCallWall)
		planClient.WithCallWall(pool.DefaultCallWall)
		installMeasuredRulers(settings.ProfileDir, taskClient.Model())
		plans := &jobPlans{graphs: map[string]plannedJob{}}
		// A wake pass has no live boost slot to resolve model words against;
		// jobs born here run on the configured work model.
		// A wake pass is the standing half of the product doing its job, not a
		// one-shot errand: charters are exactly what it exists to serve.
		// A wake pass has no shared workspace to show the planner: the jobs it
		// admits work in per-job directories that do not exist yet, so terrain
		// renders nothing and every prompt it sends is the prompt it always sent.
		reconciler := newResidentReconciler(settings, graph, chatClient, taskClient, planClient, plans, "", nil, false)
		// Craft is not a chat ornament. Without it an overnight charter plans
		// from scratch a job that has a proven learned workflow, and any craft
		// that overnight job would have taught is discarded before it can even
		// be filed as a lesson — the forge returns early for want of a mind.
		// The repository lives beside the graph it serves; a missing or broken
		// one leaves craft dormant, exactly as it does in chat.
		if craftRepo, craftErr := craft.Open(filepath.Join(filepath.Dir(path), "craft")); craftErr == nil {
			reconciler = reconciler.
				WithCraftRunner(resident.NewCraftRunner(graph, craftRepo, craftRepo.Dir())).
				WithCraftMind(resident.NewCraftMind(craftRepo, craftRepo.Dir(),
					fillCraftParams(settings, chatClient), repairCraft(settings, chatClient)))
		} else {
			log.Printf("note: craft repository unavailable: %v", craftErr)
		}
		return reconciler, nil
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
	holder, err := lease.ProbeResident(path)
	if err != nil {
		return err
	}
	// A holder that is alive but has not completed a pass in several intervals
	// is not serving the role, only occupying it. Deferring to it forever is how
	// a resident whose loop died silently stopped every standing watch on the
	// machine while still printing "alive" at anyone who asked.
	if holder != nil && !holder.Stuck {
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
	releaseResident, heldBy, err := lease.AcquireResident(path, "wake")
	if err != nil {
		return err
	}
	if releaseResident == nil {
		if !heldBy.Stuck {
			_, err = fmt.Fprintf(output, "resident alive (pid %d) — skipping wake\n", heldBy.PID)
			return err
		}
		// The flock cannot be taken from a live process, so the pass runs
		// without it. Two residents against one store is the risk this lease
		// exists to prevent; a holder that has stopped ticking is not the
		// second one, and saying so out loud is how an operator finds out.
		if _, err := fmt.Fprintf(output, "resident (pid %d) has not ticked since %s — waking anyway\n",
			heldBy.PID, heldBy.LastTick.Format(time.RFC3339)); err != nil {
			return err
		}
	} else {
		defer releaseResident()
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
		if tickErr == nil {
			// Stamping liveness is what makes the role reclaimable: a pass that
			// completes says so on the lease, and a holder that stops saying it
			// stops being deferred to.
			_ = lease.NoteResidentTick(path, time.Now())
		}
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
	if err := graph.RecordStandingWatchPass(store.StandingWatchPass{
		Examined: pass.Examined, Woken: pass.Woken, Checked: pass.Checked,
		Fired: pass.Fired, Proposed: pass.Proposed, No: pass.No, Errors: pass.Errors,
		Quota: pass.Quota, Expired: pass.Expired, RailWaits: pass.RailWaits,
	}); err != nil {
		return err
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
