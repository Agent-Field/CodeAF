package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/rotate"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// rotation is the part of a rotation the door drives; rotate.Env is one.
type rotation interface {
	Plan(ctx context.Context) (rotate.Plan, error)
	Rotate(ctx context.Context) (rotate.Result, error)
	Abandon(ctx context.Context) error
}

// rotationFactory opens the rotation of this computer's identity, with the grace
// the person asked for and a line-by-line progress sink, and answers what to
// call when it is over to tidy what it left.
type rotationFactory func(grace time.Duration, say func(string)) (rotation, func() error, error)

// realRotation is the rotation over this computer's relay and engine.
func realRotation(grace time.Duration, say func(string)) (rotation, func() error, error) {
	s, ok, err := syncsetup.OpenForRotation(home.Dir())
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, errors.New("a rotation needs a relay: " + chatlist.SyncOff)
	}
	env := s.Rotation(cellstore.EngineFor(""), heldChat, deviceName(), say)
	env.Grace = grace
	return env, s.Tidy, nil
}

// heldChat finds the folder a chat has on this computer, if it has one.
func heldChat(id string) (cell.Cell, bool) {
	c, err := openCellByID(id)
	return c, err == nil
}

type rotateOptions struct {
	grace   time.Duration
	yes     bool
	abandon bool
}

func parseRotate(args []string) (rotateOptions, error) {
	var o rotateOptions
	flags := flag.NewFlagSet("rotate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.DurationVar(&o.grace, "grace", 0, "")
	flags.BoolVar(&o.yes, "yes", false, "")
	flags.BoolVar(&o.abandon, "abandon", false, "")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return o, errors.New(identityUsage)
	}
	return o, nil
}

// identityRotate replaces the identity of every computer you keep: a new root,
// every chat and the vault sealed again under it, and the old identity retired
// by the relay after a grace period.
func identityRotate(d identityDoor, args []string) error {
	opt, err := parseRotate(args)
	if err != nil {
		return err
	}
	say := func(line string) { fmt.Fprintln(d.out, line) }
	rot, tidy, err := d.rotation(opt.grace, say)
	if err != nil {
		return err
	}
	defer tidy()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if opt.abandon {
		return abandonRotation(d, ctx, rot)
	}
	if !rotate.Pending(d.home) {
		if err := confirmRotation(d, ctx, rot, opt); err != nil {
			return err
		}
	} else {
		say("finishing the rotation that was started here")
	}
	res, err := rot.Rotate(ctx)
	if err != nil {
		return err
	}
	return rotationCard(d, res)
}

func abandonRotation(d identityDoor, ctx context.Context, rot rotation) error {
	if err := rot.Abandon(ctx); err != nil {
		return err
	}
	fmt.Fprintln(d.out, "rotation cancelled; nothing changed")
	return nil
}

// confirmRotation says what a rotation costs and asks. Nothing has changed yet,
// so ctrl+c or n here costs nothing.
func confirmRotation(d identityDoor, ctx context.Context, rot rotation, opt rotateOptions) error {
	plan, err := rot.Plan(ctx)
	if err != nil {
		return err
	}
	fmt.Fprint(d.out, planText(plan))
	if opt.yes {
		return nil
	}
	fmt.Fprint(d.out, "continue? y / n  ")
	answer, _ := bufio.NewReader(d.in).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
		return errors.New("rotation cancelled; nothing changed")
	}
	return nil
}

// planText is the plan in words: what moves, how much, about how long, and what
// it does to the other computers.
func planText(p rotate.Plan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d chats", p.Chats)
	if p.Missing > 0 {
		fmt.Fprintf(&b, ", %d of them not on this computer (%s to fetch first)", p.Missing, megabytes(p.DownBytes))
	}
	fmt.Fprintf(&b, ".\nabout %s goes up to the relay; allow about %s.\n", megabytes(p.UpBytes), minutes(p.Wait))
	b.WriteString("your other computers stop syncing until you pair them again.\n")
	b.WriteString("the old identity stays readable on the relay for a while, then the relay deletes it.\n")
	b.WriteString("once it has switched this cannot be undone.\n")
	return b.String()
}

func megabytes(n int64) string { return fmt.Sprintf("%.1f MB", float64(n)/(1<<20)) }

func minutes(d time.Duration) string {
	if d < time.Minute {
		return "a minute"
	}
	return fmt.Sprintf("%d minutes", int(d.Round(time.Minute)/time.Minute))
}

// rotationCard is what a person reads when it is done: what changed, what to do
// next, and, plainly, what a computer they wanted out still holds.
func rotationCard(d identityDoor, res rotate.Result) error {
	names := vaultNames(d.home)
	fmt.Fprintf(d.out, "rotated. your chats now belong to %s (was %s).\n", res.NewID, res.OldID)
	fmt.Fprintln(d.out, "other computers: run /pair here, then `codeaf pair <code>` there, for each one.")
	fmt.Fprintf(d.out, "the old copy on the relay is read-only and is deleted on %s.\n", d.now().Add(res.RetireAfter).Format("2006-01-02"))
	fmt.Fprintln(d.out, "a computer you lost still holds everything it had: your chats up to now and every secret in your vault.")
	if len(names) > 0 {
		fmt.Fprintf(d.out, "change these at their providers: %s\n", strings.Join(names, ", "))
	}
	return nil
}

// vaultNames lists the names of the secrets in the vault, never their values.
func vaultNames(dir string) []string {
	v, err := keys.Open(dir)
	if err != nil {
		return nil
	}
	ids, _ := v.IDs("")
	seen := map[string]bool{}
	for _, id := range ids {
		if e, err := v.Get(id); err == nil && e.Name != "" {
			seen[e.Name] = true
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
