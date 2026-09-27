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

type dailyBudgetWait struct {
	question Question
	limit    float64
	raiseTo  float64
	user     userMessage
	ctx      context.Context
	stream   *eventStream
	release  func()
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

// holdDailyBudgetLocked raises the same question card used by other bounded
// work and keeps the original Submit stream until the person decides.
func (a *Agent) holdDailyBudgetLocked(ctx context.Context, user userMessage, spend DailySpend) <-chan Event {
	if a.dailyBudget != nil {
		a.mu.Unlock()
		return refusedStream(errors.New("today's spending limit is waiting for your answer"))
	}
	a.dailyBudgetSeq++
	q := dailyBudgetQuestion(a.dailyBudgetSeq, spend)
	q.Asked = time.Now()
	stream := newEventStream()
	a.dailyBudget = &dailyBudgetWait{question: q, limit: spend.Limit, raiseTo: teams.RaiseTo(spend.Limit), user: user, ctx: ctx, stream: stream}
	a.mu.Unlock()
	release := a.presenceAskingWhole(q, nil)
	a.mu.Lock()
	if a.dailyBudget != nil && a.dailyBudget.question.ID == q.ID {
		a.dailyBudget.release = release
	} else {
		release()
	}
	a.mu.Unlock()
	return stream.out
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
		release := wait.release
		stream := wait.stream
		a.mu.Unlock()
		if release != nil {
			release()
		}
		refuseOn(stream, errors.New(DailySpendAction(wait.limit)))
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
	a.startTurnLocked(wait.ctx, wait.user, wait.stream)
	release := wait.release
	a.mu.Unlock()
	if release != nil {
		release()
	}
	return nil
}
