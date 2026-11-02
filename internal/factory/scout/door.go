package scout

import (
	"context"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// Limits are what scouts may spend: dollars this month across the factory,
// and scouts a day per repository. Cached looks are free and not counted.
type Limits struct {
	Month      float64
	PerRepoDay int
}

// DefaultLimits are the page AI's month ($10, as Repo notes) and a per
// repository day that a busy morning of Look closer stays well inside.
var DefaultLimits = Limits{Month: 10, PerRepoDay: 20}

var (
	ErrMonthLimit = errors.New("monthly limit for AI reached")
	ErrDayLimit   = errors.New("daily limit for this repo reached")
)

// Runner scouts one job; [Run] bound to the job's checkout and a model.
type Runner func(ctx context.Context, it factory.Item) (factory.Scout, error)

// Head is the commit the repository's default branch is at now.
type Head func(ctx context.Context, repo string) (string, error)

// Floor is the slice of the factory store a look reads and writes.
type Floor interface {
	Get(id int) (factory.Item, error)
	Annotate(id int, change func(*factory.Item) error) error
	NoteScout(repo string, usd float64, at time.Time) error
	Scouts(now time.Time) (store.ScoutLedger, error)
}

// Door is Look closer on one floor.
type Door struct {
	Floor  Floor
	Run    Runner
	Head   Head
	Limits Limits
	Now    func() time.Time
}

// Look answers a job's scout. A look of the same issue text at the same head
// is the kept one, free (cached true), unless refresh asks for a new one. A
// new one is refused at the month's or the repository's day's limit, runs,
// is noted on the floor's scout ledger, and is written on the job ([Apply]).
func (d Door) Look(ctx context.Context, id int, refresh bool) (factory.Scout, bool, error) {
	it, err := d.Floor.Get(id)
	if err != nil {
		return factory.Scout{}, false, err
	}
	head, err := d.Head(ctx, it.Repo)
	if err != nil {
		return factory.Scout{}, false, err
	}
	if kept := it.Scout; kept != nil && !refresh && !kept.Stale(it) && kept.Head == head {
		return *kept, true, nil
	}
	now := d.Now()
	if err := d.allowed(it.Repo, now); err != nil {
		return factory.Scout{}, false, err
	}
	s, err := d.Run(ctx, it)
	_ = d.Floor.NoteScout(it.Repo, s.USD, now)
	if err != nil {
		return s, false, err
	}
	s.At = now
	err = d.Floor.Annotate(id, func(cur *factory.Item) error { Apply(cur, s); return nil })
	return s, false, err
}

func (d Door) allowed(repo string, now time.Time) error {
	l, err := d.Floor.Scouts(now)
	switch {
	case err != nil:
		return err
	case l.Month >= d.Limits.Month:
		return ErrMonthLimit
	case l.Today[repo] >= d.Limits.PerRepoDay:
		return ErrDayLimit
	}
	return nil
}

// Apply writes a scout on its job. Its rubric replaces the read's (it saw the
// repository), and its band becomes the size where the size was the read's
// or nobody's; a measured diff stays. Its unknowns become the job's questions
// when the job has none.
func Apply(it *factory.Item, s factory.Scout) {
	it.Scout = &s
	if s.Rubric != nil {
		it.Triage.Rubric = s.Rubric
		if it.Triage.SizeFrom != factory.SizeFromDiff {
			it.Triage.Size, it.Triage.SizeFrom = factory.BandSize(s.Rubric.Band()), factory.SizeFromRead
		}
	}
	if len(it.Triage.Questions) == 0 {
		it.Triage.Questions = append([]string(nil), s.Unknowns...)
	}
}
