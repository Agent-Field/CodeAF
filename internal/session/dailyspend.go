package session

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// DailySpend is the one reading of the machine-wide daily spending limit. The
// limit includes a day-scoped raise, and Spent is the same usage ledger every
// road writes; callers must not re-do either half of this arithmetic.
type DailySpend struct {
	Limit   float64
	Spent   float64
	Reached bool
}

type dailyBudgetRaise struct {
	Day    string  `json:"day"`
	Limit  float64 `json:"limit"`
	Origin string  `json:"origin,omitempty"`
}

type dailySpendAuthorizationKey struct{}

// WithDailySpendPreauthorized carries explicit permission past today's limit
// through a run and its child/checker contexts. It changes no stored setting.
func WithDailySpendPreauthorized(ctx context.Context) context.Context {
	return context.WithValue(ctx, dailySpendAuthorizationKey{}, true)
}

// DailySpendPreauthorized reads only the authorization carried by this run.
func DailySpendPreauthorized(ctx context.Context) bool {
	allowed, _ := ctx.Value(dailySpendAuthorizationKey{}).(bool)
	return allowed
}

type dailyBudgetWait struct {
	question Question
	limit    float64
	raiseTo  float64
	user     userMessage
	ctx      context.Context
	stream   *eventStream
	release  func()
	// Task admission waits on its existing caller rather than opening a turn.
	taskResult chan error
	done       chan struct{}
}

type dailyBudgetReached struct{ spend DailySpend }

func (e dailyBudgetReached) Error() string { return DailySpendAction(e.spend.Limit) }

const dailyBudgetRaisesName = "daily_budget_raises.jsonl"

// DailySpendAt reads today's effective limit and spend. A missing or unreadable
// usage ledger means no measured spend, matching the headless door's existing
// conservative ledger fallback; a bad budget setting remains an error. The
// optional ledger path is for private session ledgers; production callers use
// the machine-wide path.
func DailySpendAt(profileDir string, now time.Time, ledgerPath ...string) (DailySpend, error) {
	limit, err := dailyBudgetLimitAt(profileDir, now)
	if err != nil {
		return DailySpend{}, err
	}
	path := UsageLedgerPath()
	if len(ledgerPath) > 0 && strings.TrimSpace(ledgerPath[0]) != "" {
		path = ledgerPath[0]
	}
	lines, err := ReadUsage(path, now.Add(-48*time.Hour))
	if err != nil {
		return DailySpend{Limit: limit}, nil
	}
	spent := SpendToday(lines, now)
	return DailySpend{Limit: limit, Spent: spent, Reached: limit > 0 && spent >= limit}, nil
}

// RaiseDailySpend persists a larger ceiling for today's local day. The base
// setting remains unchanged, so tomorrow starts at the configured amount.
func RaiseDailySpend(profileDir string, now time.Time, limit float64, origin string) error {
	base, err := config.DailyBudgetUSDAt(profileDir)
	if err != nil {
		return err
	}
	if limit <= base || math.IsNaN(limit) || math.IsInf(limit, 0) || strings.TrimSpace(origin) == "" {
		return fmt.Errorf("raise daily spending limit: positive larger limit and origin are required")
	}
	path := config.ProfilePath(profileDir, dailyBudgetRaisesName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create daily spending limit journal: %w", err)
	}
	line, err := json.Marshal(dailyBudgetRaise{Day: localDay(now), Limit: limit, Origin: origin})
	if err != nil {
		return fmt.Errorf("encode daily spending limit raise: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open daily spending limit journal: %w", err)
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		_ = file.Close()
		return fmt.Errorf("write daily spending limit raise: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close daily spending limit journal: %w", err)
	}
	return nil
}

func dailyBudgetLimitAt(profileDir string, now time.Time) (float64, error) {
	base, err := config.DailyBudgetUSDAt(profileDir)
	if err != nil {
		return 0, err
	}
	if base <= 0 {
		return 0, nil
	}
	file, err := os.Open(config.ProfilePath(profileDir, dailyBudgetRaisesName))
	if err != nil {
		if os.IsNotExist(err) {
			return base, nil
		}
		return base, nil
	}
	defer file.Close()
	limit := base
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var raise dailyBudgetRaise
		if json.Unmarshal(scanner.Bytes(), &raise) == nil && raise.Day == localDay(now) && raise.Limit > limit {
			limit = raise.Limit
		}
	}
	return limit, nil
}

func localDay(now time.Time) string { return now.Local().Format("2006-01-02") }

// DailySpendAction is the refusal sentence shared by interactive chat and
// headless runs when today's limit leaves no work to start.
func DailySpendAction(limit float64) string {
	return fmt.Sprintf("today's spending limit of $%.2f is spent, so nothing was started", limit)
}

func dailyBudgetQuestion(id uint64, spend DailySpend) Question {
	raiseTo := teams.RaiseTo(spend.Limit)
	return Question{
		ID:     id,
		Kind:   QuestionDailyBudget,
		Ask:    AskChoice,
		Form:   FormCard,
		Asker:  Asker{Kind: AskerEngine},
		Head:   fmt.Sprintf("today's spending limit of %s is spent", teams.Money(spend.Limit)),
		Reason: "new work cannot start until you choose whether to raise it or stop for today",
		Options: []AnswerOption{
			{Key: "1", Label: "Raise to " + teams.Money(raiseTo), Consequence: "new work may spend up to " + teams.Money(raiseTo) + " today"},
			{Key: "2", Label: "Stop for today", Consequence: "nothing new starts until tomorrow", Safe: true},
		},
		Stakes:   StakesCostly,
		Blocking: Blocking{Turn: true},
	}
}

// holdDailyBudgetLocked keeps the original Submit stream until the person
// decides, so raising the limit resumes the same turn.
func (a *Agent) holdDailyBudgetLocked(ctx context.Context, user userMessage, spend DailySpend) <-chan Event {
	stream := newEventStream()
	wait := &dailyBudgetWait{user: user, ctx: ctx, stream: stream, done: make(chan struct{})}
	if err := a.openDailyBudgetLocked(spend, wait); err != nil {
		refuseOn(stream, err)
		return stream.out
	}
	guard.Go("daily-budget-wait", func() {
		select {
		case <-wait.done:
		case <-ctx.Done():
			a.mu.Lock()
			owned := a.dailyBudget == wait
			if owned {
				a.dailyBudget = nil
			}
			a.mu.Unlock()
			if owned {
				wait.finish(ctx.Err())
			}
		}
	})
	return stream.out
}

// openDailyBudgetLocked publishes one budget question for either a turn or a
// task admission. It always releases mu; publication itself needs that lock.
func (a *Agent) openDailyBudgetLocked(spend DailySpend, wait *dailyBudgetWait) error {
	if a.closed {
		a.mu.Unlock()
		return errAgentClosed
	}
	if a.dailyBudget != nil {
		a.mu.Unlock()
		return errors.New("today's spending limit is waiting for your answer")
	}
	a.dailyBudgetSeq++
	q := dailyBudgetQuestion(a.dailyBudgetSeq, spend)
	q.Asked = time.Now()
	wait.question, wait.limit, wait.raiseTo = q, spend.Limit, teams.RaiseTo(spend.Limit)
	a.dailyBudget = wait
	a.mu.Unlock()
	release := a.presenceAskingWhole(q, nil)
	a.mu.Lock()
	if a.dailyBudget == wait {
		wait.release = release
		a.mu.Unlock()
	} else {
		a.mu.Unlock()
		release()
	}
	return nil
}

// awaitTaskDailyBudget gates both typed tasks and approved proposals before
// either engine admits work. A raise is checked again because another window
// may have spent beyond even that limit while the question was standing.
func (a *Agent) awaitTaskDailyBudget(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		a.mu.Lock()
		closed := a.closed
		a.mu.Unlock()
		if closed {
			return errAgentClosed
		}
		daily, err := DailySpendAt(a.config.ProfileDir, time.Now(), a.config.usageLedger)
		if err != nil {
			return err
		}
		if !daily.Reached {
			return nil
		}
		wait := &dailyBudgetWait{taskResult: make(chan error, 1)}
		a.mu.Lock()
		if err := a.openDailyBudgetLocked(daily, wait); err != nil {
			return err
		}
		select {
		case err := <-wait.taskResult:
			if err != nil {
				return err
			}
		case <-ctx.Done():
			a.mu.Lock()
			if a.dailyBudget == wait {
				a.dailyBudget = nil
				a.mu.Unlock()
				wait.finish(ctx.Err())
			} else {
				a.mu.Unlock()
			}
			return ctx.Err()
		}
	}
}

// finish releases the card before its caller can report a refusal or a start.
// The owner detaches the wait under mu first, so exactly one ending reaches it.
func (w *dailyBudgetWait) finish(err error) {
	if w.done != nil {
		close(w.done)
	}
	if w.release != nil {
		w.release()
	}
	if w.taskResult != nil {
		w.taskResult <- err
	} else if err != nil {
		refuseOn(w.stream, err)
	}
}

func (a *Agent) resolveDailyBudget(answer Answer) error {
	key := answer.FirstKey()
	a.mu.Lock()
	wait := a.dailyBudget
	if wait == nil || wait.question.ID != answer.ID {
		a.mu.Unlock()
		return errAnswerGone
	}
	if key == "2" {
		a.dailyBudget = nil
		a.mu.Unlock()
		wait.finish(errors.New(DailySpendAction(wait.limit)))
		return nil
	}
	if key != "1" {
		a.mu.Unlock()
		return errAnswerEmpty
	}
	profile, origin := a.config.ProfileDir, "chat:daily-budget"
	a.mu.Unlock()
	if err := RaiseDailySpend(profile, time.Now(), wait.raiseTo, origin); err != nil {
		return err
	}
	a.mu.Lock()
	if a.dailyBudget != wait {
		a.mu.Unlock()
		return errAnswerGone
	}
	a.dailyBudget = nil
	if wait.taskResult == nil {
		a.startTurnLocked(wait.ctx, wait.user, wait.stream)
	}
	a.mu.Unlock()
	wait.finish(nil)
	return nil
}
