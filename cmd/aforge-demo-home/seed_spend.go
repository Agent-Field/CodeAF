package main

// seed_spend.go is the usage ledger: what this machine spent, by day, by model,
// on what.
//
// Every line goes through [session.RecordUsage], which is the same door the
// engine writes through, and the ids on it JOIN — a line about a conversation
// names a conversation on the disk, a line about a piece of work names a row in
// that project's index, a line about a standing order names an item in the
// standing store. That is what the spend page's `what it was for` column reads,
// and a ledger of orphan ids would draw rows a person cannot follow anywhere.
//
// A LINE THAT SPENT NOTHING IS NOT WRITTEN — the ledger's own law, kept by its
// writer — so nothing here is priced at zero.

import (
	"fmt"
	"math"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// demoModels are the three the demo machine has spent money on. They are real
// provider slugs because the surface shortens a slug to the word a person says
// (internal/tui2/modelui's ModelWord) and hands back anything it does not
// recognise whole — an invented id would draw as itself.
var demoModels = []struct {
	slug string
	// rate is dollars per thousand tokens, roughly, so the sums across the
	// fourteen days come out in proportion to what each model is actually for.
	rate float64
}{
	{"anthropic/claude-opus-4.1", 0.020},
	{"anthropic/claude-sonnet-4", 0.006},
	{"openai/gpt-5-mini", 0.001},
}

// demoDayAnchor is the hour of the day a fixture's lines cluster around. Nine
// in the morning is late enough that an offset an hour or two either side of it
// is still that morning, which is the whole point of anchoring at all.
const demoDayAnchor = 9 * time.Hour

// demoMoment is when a fixture line meant for daysAgo days ago is stamped, and
// IT NEVER LEAVES THE DAY IT NAMES.
//
// The offsets the seeders add — a firing seventeen minutes before the last one,
// a turn eleven minutes after the one before it — used to be added to `now`
// itself, so a demo home built at ten past midnight stamped TODAY's standing
// firing fifty minutes earlier, which is yesterday, and the spend page's
// cost-per-firing clause then drew nothing on a fixture whose entire purpose is
// to have something on every place. One built at half past eleven at night put
// today's turns on tomorrow's row of the day axis. The day is chosen first, the
// offset is applied inside it, and the result is clamped to the day at both
// ends — and to `now` as well, because money spent in the future reads as a
// broken fixture rather than a full one.
func demoMoment(now time.Time, daysAgo int, offset time.Duration) time.Time {
	day := now.AddDate(0, 0, -daysAgo)
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	at := start.Add(demoDayAnchor + offset)
	if end := start.AddDate(0, 0, 1).Add(-time.Second); at.After(end) {
		at = end
	}
	if at.Before(start) {
		at = start
	}
	if at.After(now) {
		at = now
	}
	return at
}

// usageDays is how far back the ledger goes. Fourteen is what the spend page's
// widest window asks for, so the demo has something under every column of it.
const usageDays = 14

// writeUsage lays down the ledger and answers how many lines it wrote.
//
// The three SUBJECTS a line can name are all here, because the page's binding
// column is only interesting when it has more than one thing to say: a
// conversation (a turn somebody took), a piece of work (a task node's own
// calls), and a standing order (a firing, which is unattended and which a person
// recognises long before they recognise the run it spawned).
func writeUsage(path string, projects map[string]*demoProject, ids map[string]string, now time.Time) (int, error) {
	written := 0
	// A stable, boring rhythm rather than a random one: the same fixture on two
	// machines is the same fixture, and a demo that looked different every time
	// it was built would be a demo nobody could describe to anybody else.
	step := 0
	next := func() int { step++; return step }

	for day := usageDays - 1; day >= 0; day-- {
		// The hour a day's lines cluster around, spread a little across the
		// fortnight so the page does not draw fourteen identical days. It is an
		// offset INSIDE the day and never a shift of the day itself.
		spread := -time.Duration(day%7) * time.Hour

		// TWO CONVERSATIONS A DAY, ROTATING, and not every conversation on every
		// day it was alive. A person's ledger is a handful of lines a day, and a
		// fixture that wrote one per conversation per day would put four hundred
		// rows behind a page whose whole argument is that it stays readable.
		alive := aliveOn(demoConversations, day)
		for pick := 0; pick < 2 && len(alive) > 0; pick++ {
			talk := alive[next()%len(alive)]
			id := ids[talk.title]
			project := projects[talk.project]
			if id == "" || project == nil {
				continue
			}
			model := demoModels[next()%len(demoModels)]
			tokens := 3_000 + 900*(next()%7)
			if err := record(path, session.UsageLine{
				At:        demoMoment(now, day, spread+time.Duration(next()%9)*11*time.Minute),
				Model:     model.slug,
				Calls:     1 + next()%3,
				Input:     tokens,
				Output:    tokens / 4,
				USD:       round(float64(tokens) * model.rate / 1000),
				Session:   id,
				Workspace: project.dir,
			}); err != nil {
				return written, err
			}
			written++
		}

		// The work, every other day. A line about a task names the NODE's own
		// journal id and the node id beside it, which is the pair that says where
		// the money went.
		if day%2 == 0 {
			task := demoTasks[next()%len(demoTasks)]
			id, project := ids[task.talk], projects[task.project]
			if id != "" && project != nil {
				model := demoModels[next()%len(demoModels)]
				tokens := 6_000 + 1_500*(next()%5)
				if err := record(path, session.UsageLine{
					At:        demoMoment(now, day, spread+time.Duration(next()%7)*13*time.Minute),
					Model:     model.slug,
					Calls:     2 + next()%4,
					Input:     tokens,
					Output:    tokens / 3,
					USD:       round(float64(tokens) * model.rate / 1000),
					Session:   id,
					Task:      task.entry.ID,
					Workspace: project.dir,
				}); err != nil {
					return written, err
				}
				written++
			}
		}

		// One standing check a day. A firing is cheap and unattended, so it runs
		// on the smallest model and names its ITEM rather than the run it
		// spawned — which is the id a person recognises.
		if firing := spendingOrders(); len(firing) > 0 {
			order := firing[next()%len(firing)]
			if project := projects[order.project]; project != nil {
				// A check names the role it was made for; internal/roles is the
				// vocabulary, and it is NOT the five router slots — nothing in the
				// program records which slot a call ran under.
				if err := record(path, session.UsageLine{
					At:        demoMoment(now, day, spread+time.Duration(next()%5)*19*time.Minute),
					Model:     demoModels[len(demoModels)-1].slug,
					Role:      string(roles.RoleIntake),
					Calls:     1,
					Input:     1_400 + 200*(next()%4),
					Output:    300,
					USD:       round(0.004 + 0.001*float64(next()%5)),
					Standing:  order.item.ID,
					Workspace: project.dir,
				}); err != nil {
					return written, err
				}
				written++
			}
		}

		// The naming calls beside the turns — the one auxiliary role a person
		// meets by name, and the reason the page's role column has a second word
		// on it.
		if day%4 == 0 {
			for _, title := range []string{"The Tab Bar's Counts", "Pricing Research"} {
				id := ids[title]
				if id == "" {
					continue
				}
				if err := record(path, session.UsageLine{
					At:      demoMoment(now, day, spread+time.Duration(next()%3)*23*time.Minute),
					Model:   demoModels[len(demoModels)-1].slug,
					Role:    string(roles.RoleTitle),
					Calls:   1,
					Input:   900,
					Output:  40,
					USD:     round(0.001 + 0.0005*float64(next()%3)),
					Session: id,
				}); err != nil {
					return written, err
				}
				written++
			}
		}
	}

	// The writer is a background goroutine per ledger — it never blocks a turn —
	// so nothing is on the disk until this returns.
	session.FlushUsage()
	return written, nil
}

// record writes one line and reports the one thing that could go wrong here:
// a line the ledger would drop because it spent nothing.
func record(path string, line session.UsageLine) error {
	if line.USD <= 0 {
		return fmt.Errorf("a spending line that spent nothing: %+v", line)
	}
	// The local calendar day is left to the writer, which stamps it from At
	// (usage_ledger.go). A second spelling here would be a second answer to
	// "which day is this" the moment one of them moved.
	session.RecordUsage(path, line)
	return nil
}

// round keeps a price to the cent-and-a-bit a provider actually bills, so the
// sums on the page add up to what the rows say.
func round(usd float64) float64 { return math.Round(usd*10_000) / 10_000 }

// aliveOn is the conversations that existed that many days ago — a ledger line
// against a conversation nobody had started yet would be a join that points
// backwards in time.
func aliveOn(talks []demoTalk, daysAgo int) []demoTalk {
	var alive []demoTalk
	for _, talk := range talks {
		if talk.ago <= time.Duration(daysAgo+1)*24*time.Hour {
			alive = append(alive, talk)
		}
	}
	return alive
}

// spendingOrders is the standing orders that can cost money. A hold never wakes,
// so it can never spend, and a ledger line against one would be a fact about
// this fixture and about nothing the product does.
func spendingOrders() []demoOrder {
	var spending []demoOrder
	for _, order := range demoOrders {
		if order.item.When.Kind != standing.WhenHold {
			spending = append(spending, order)
		}
	}
	return spending
}
