package main

// The door's half of the ambient side: where the standing store lives, who
// ticks it, and what a firing is allowed to do.
//
// internal/standing owns the files and the pass; internal/session owns what a
// firing DOES (standing_run.go). This file is the seam between them and the
// person's own machine — the one place that says which directory, which daily
// rail, and which posture a firing runs under.
//
// ── WHO TICKS ──
//
// Any window that is open takes the store's lock and runs a pass every
// [standing.Interval]; the OS timer running `aforge tick` is the backup for
// "no terminal open". Both build the ticker through [v3StandingTicker], so
// there is exactly one answer to "what does a pass do" and it cannot drift
// between the two callers.
//
// ── WHAT A FIRING MAY DO ──
//
// A firing runs under the person's PROFILE rules and not under a repository's.
// The gate a conversation runs behind is resolved per project (chatv3.go), and
// a checked-in file that could widen what runs while nobody is watching would
// be a repository granting itself permissions its author never met. So the
// posture below is resolved against the person's home directory: their own
// banked rules, and nothing a clone can add to them. Anything those rules would
// have asked about refuses, the run stops, and the item says it needs them.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/tui3"
)

// standingLogName is where a failed pass says so.
//
// IT IS A FILE AND NEVER THE SCREEN, for [sweepLogName]'s reason: the pass runs
// while a surface owns the terminal, and a line on stderr is either scrolled
// past or painted through a frame somebody is typing into. A pass that could not
// read one item is not a person's problem — the next pass reads it — so the
// honest destination is a file an operator can read afterwards.
const standingLogName = "standing.log"

// v3StandingRoot is where everything standing lives: ~/.aforge/v3/standing,
// resolved through internal/home so AFORGE_HOME moves it with the rest
// (Decision 26 — one home, one seam).
func v3StandingRoot() string { return home.Join("v3", "standing") }

// v3Standing builds the seam a conversation proposes through, or nil.
//
// NIL IS THE AMBIENT SIDE OFF, and every caller reads it that way: no belt
// tool, no card, nothing armed. A store that cannot be opened is exactly that
// case — a capability that cannot work is absent, not broken — so the error is
// swallowed here rather than failing somebody's launch over a folder.
func v3Standing(profileDir string) *session.Standing {
	store, err := standing.Open(v3StandingRoot())
	if err != nil {
		return nil
	}
	return &session.Standing{
		Store: store,
		Watch: standingWatch(store),
		// The person's own daily budget is what the card quotes beside the
		// per-run cap. A profile that cannot be read quotes nothing rather than
		// a figure nobody set, which is the emptiness law applied to money.
		DailyRailUSD: v3StandingDailyRail(profileDir),
	}
}

// v3StandingDailyRail is the daily ceiling on everything standing spends. It is
// the person's existing daily budget row and NOT a new setting: a second number
// beside it would be two answers to one question, and the first day they
// disagreed the honest one would be whichever the card did not show.
func v3StandingDailyRail(profileDir string) float64 {
	rail, err := config.DailyBudgetUSDAt(profileDir)
	if err != nil || rail < 0 {
		return 0
	}
	return rail
}

// standingWatch is the OS timer that keeps checking with no window open: a
// launchd agent or a systemd user timer running `aforge tick` every five
// minutes (internal/standing's watch.go). Nil is the honest answer on a host
// the package cannot arrange one for, and every caller reads nil as "the offer
// is never made" — a question nobody can keep is a question nobody is asked.
func standingWatch(store *standing.Store) standing.Watch {
	watch, err := standing.NewWatch(standing.WatchOptions{WakeLog: store.WakeLogPath()})
	if err != nil {
		return nil
	}
	return watch
}

// v3StandingTicker builds one pass. IT IS THE ONE CONSTRUCTOR: a window's
// goroutine below and `aforge tick` both call exactly this, so the two can
// never disagree about what a pass is allowed to do.
func v3StandingTicker(store *standing.Store) (*standing.Ticker, error) {
	if store == nil {
		return nil, fmt.Errorf("standing: no store")
	}
	settings, err := config.Load()
	if err != nil {
		return nil, err
	}
	posture, err := v3StandingPosture(settings)
	if err != nil {
		return nil, err
	}
	return &standing.Ticker{
		Store:        store,
		Sentinel:     session.NewStandingSentinel(posture),
		Runner:       session.NewStandingRunner(posture, store.Root()),
		Idle:         session.StandingIdle(),
		DailyRailUSD: v3StandingDailyRail(settings.ProfileDir),
	}, nil
}

// v3StandingPosture is the config a firing inherits: the person's models, keys,
// accounts and approval rules, resolved against their HOME rather than against
// any project (see this file's header).
//
// The workspace here is only where the rows are read from. Every firing runs in
// its own item's workspace, which the runner sets before it opens anything.
func v3StandingPosture(settings config.Config) (session.Config, error) {
	root, err := os.UserHomeDir()
	if err != nil || root == "" {
		root = os.TempDir()
	}
	cfg := session.Config{
		Workspace:      root,
		Model:          v3TalkModel("", settings),
		APIKey:         settings.APIKey,
		BaseURL:        settings.BaseURL,
		SiteURL:        settings.SiteURL,
		SiteName:       settings.SiteName,
		SiteCategories: settings.SiteCategories,
		CompactEnabled: true,
		ProfileDir:     settings.ProfileDir,
		ArtifactsIndex: artifactsIndexPath(),
	}
	// NOT yolo, ever, whatever a window was started with: --yolo is a posture
	// somebody took for a session they were sitting in front of, and carrying it
	// into work that runs while they sleep would be reading a flag as a standing
	// promise about calls nobody has written yet.
	cfg, err = applyV3Governance(cfg, settings.ProfileDir, false)
	if err != nil {
		return session.Config{}, err
	}
	// The media pair, resolved the way a conversation resolves it (chatv3.go):
	// a firing briefed to draw a diagram needs the hand that draws it, and the
	// resolver is what says which model does. The catalog is LAZY and is never
	// waited for — a pass whose catalog has not resolved simply has no media
	// verbs on its belt, which is the same absence a cold conversation has.
	models := catalog.LoadLazy(context.Background(), catalog.Options{
		BaseURL: settings.BaseURL, APIKey: settings.APIKey, Dir: settings.ProfileDir,
	})
	cfg.Media = v3MediaClient(settings)
	cfg.MediaModel = v3MediaModel(models, settings.ProfileDir, cfg.RolesSource)
	cfg.SupportsParameter = models.SupportsParameter
	cfg.NearestModels = v3NearestModels(models)
	// AskConsent stays false and Standing stays nil: nobody is watching a
	// firing, and nothing that fires may arm anything else.
	return cfg, nil
}

// startStandingTicks runs a pass every [standing.Interval] for as long as this
// process lives, once per process.
//
// IT NEVER BLOCKS A LAUNCH and it never says anything on screen. The first pass
// is one interval away, so a process that opens and exits — a --once run, a
// smoke test — has ticked nothing at all, which is the correct behaviour for a
// door nobody is sitting at.
//
// [standing.ErrHeld] is SILENT: another window is ticking, which is the design
// working rather than a fault, and a log line per five minutes per window would
// be a file nobody could read.
func startStandingTicks(store *standing.Store) {
	if store == nil {
		return
	}
	standingOnce.Do(func() {
		guard.Go("chatv3/standing", func() {
			standingTicks.Store(true)
			ticker := time.NewTicker(standing.Interval)
			defer ticker.Stop()
			for range ticker.C {
				runStandingTick(store)
			}
		})
	})
}

var standingOnce sync.Once

// standingTicks is whether the loop above is actually running in this process,
// and [standingTicking] is how the surface asks ([tui3.StandingSeam.Ticking]).
//
// IT IS SET INSIDE THE GOROUTINE AND NOT BESIDE THE Do, so it is true exactly
// when there is something keeping time. /status says `while a window is open`
// on the strength of this flag, and a flag set by the intention to start a
// goroutine would be the screen vouching for a pass that never began.
var standingTicks atomic.Bool

func standingTicking() bool { return standingTicks.Load() }

// runStandingTick is one pass, bounded, with everything it can say written to a
// file.
func runStandingTick(store *standing.Store) {
	// A PANIC HERE MUST NOT END THE TICKING. The loop above is this process's
	// whole contribution to the ambient side, and a goroutine that unwound out
	// of it would leave a window that looks like it is keeping watch and is not.
	defer guard.Recover("standing tick")
	pass, err := v3StandingTicker(store)
	if err != nil {
		noteStanding("could not start a pass: " + err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), standing.TickWindow)
	defer cancel()
	if _, err := pass.Tick(ctx); err != nil {
		if err == standing.ErrHeld {
			return
		}
		noteStanding(err.Error())
	}
}

// noteStanding writes one line, and opens the file only when there is a line to
// write: a pass that behaved leaves nothing behind at all ([noteSweep]).
func noteStanding(line string) {
	path := home.Join("v3", standingLogName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	fmt.Fprintf(file, "%s %s\n", time.Now().Format(time.RFC3339), line)
}

// v3StandingSeam is the surface's reading of the same seam the session
// proposes through. A nil seam is a zero StandingSeam, which internal/tui3 reads
// as the ambient side absent: no band on home, no segment, no /status line.
//
// Running IS supplied, and what makes that honest is that something on disk now
// says it. A firing runs inside whichever process holds the tick lock — a live
// window, or the operating system's timer running `aforge tick` with nobody
// sitting anywhere — and internal/standing's running.go is that process leaving
// a marker in the item's folder for the length of the pass it is doing. Every
// other window reads it, doubts it (a dead pid, an age past one pass) and draws
// `●` only on what survives, so the glyph is derived rather than asserted.
func v3StandingSeam(seam *session.Standing) tui3.StandingSeam {
	if seam == nil || seam.Store == nil {
		return tui3.StandingSeam{}
	}
	store, watch := seam.Store, seam.Watch
	out := tui3.StandingSeam{
		Items: func(workspace string) []standing.Item {
			items, err := store.ForWorkspace(workspace)
			if err != nil {
				return nil
			}
			return items
		},
		Save: store.Save,
		// WHETHER THIS PROCESS IS KEEPING TIME, asked at the moment the line is
		// drawn rather than latched when the seam was built: the ticking starts
		// during the launch (startStandingTicks) and a boolean captured here
		// would be a claim about the order of two lines in this file.
		Ticking: standingTicking,
		// And why nobody is, when nobody is. The marker lives under the store
		// root and internal/session owns its shape (tools_standing.go).
		WatchAsked: func() (bool, bool) { return session.WatchAsked(store.Root()) },
		// The ledger's last days, per item, for the `this week` line on a card.
		// A read that fails answers nothing rather than a wrong figure — the
		// card simply has one less true thing to say.
		Runs: func(since time.Time) map[string]standing.Spend {
			runs, err := store.RunsSince(since)
			if err != nil {
				return nil
			}
			return runs
		},
		// The marker in the item's own folder, doubted by the store before it
		// answers. This is the whole of "some other process is on this item
		// right now" — the surface's `●`, the card's `checking now`, and the
		// one segment on the status line that moves.
		Running: store.Running,
	}
	if watch != nil {
		out.Watch = func() (standing.WatchStatus, bool) {
			status, err := watch.Status()
			return status, err == nil
		}
	}
	return out
}
