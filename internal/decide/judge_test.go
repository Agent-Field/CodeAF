package decide

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/roles"
)

var opts = []Option{{"yes", "Allow it"}, {"no", "Refuse it"}}

type fakeAsk struct {
	text  string
	cost  float64
	err   error
	calls int
	last  JudgeRequest
}

func (f *fakeAsk) ask(_ context.Context, r JudgeRequest) (JudgeAnswer, error) {
	f.calls++
	f.last = r
	return JudgeAnswer{Text: f.text, CostUSD: f.cost}, f.err
}

func newJudge(t *testing.T, f *fakeAsk) *Judge {
	t.Helper()
	at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	j, err := NewJudge(f.ask, func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func q() JudgeQuestion {
	return JudgeQuestion{PlaceID: "p1", Mode: ModeDeciding, Prompt: "Run the linter?", Stakes: StakesReversible, Options: opts}
}

func TestJudgeReturnsAPickPercentAndReason(t *testing.T) {
	f := &fakeAsk{text: "```json\n{\"key\":\"yes\",\"percent\":81.4,\"reason\":\"  Linting\\nis harmless. \"}\n```", cost: 0.001}
	v, err := newJudge(t, f).Judge(context.Background(), q())
	if err != nil {
		t.Fatal(err)
	}
	if v.Key != "yes" || v.Percent != 81 || v.Reason != "Linting is harmless." {
		t.Fatalf("pick %+v", v)
	}
	if f.last.Role != roles.RoleDeciding {
		t.Fatalf("role %q", f.last.Role)
	}
}

func TestJudgeIsLazy(t *testing.T) {
	f := &fakeAsk{text: `{"key":"yes","percent":90,"reason":"x"}`}
	j := newJudge(t, f)
	enough := q()
	enough.Measured = Result{Percent: 95}
	ask := q()
	ask.Mode = ModeAsk
	one := q()
	one.Options = opts[:1]
	for name, in := range map[string]JudgeQuestion{"confident": enough, "ask mode": ask, "one option": one} {
		if _, err := j.Judge(context.Background(), in); !errors.Is(err, ErrNotNeeded) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if f.calls != 0 {
		t.Fatalf("model called %d times", f.calls)
	}
}

func TestJudgeRefusesKeysOutsideTheOptions(t *testing.T) {
	for _, text := range []string{`{"key":"rm -rf /","percent":99,"reason":"x"}`, `no idea`, ``} {
		_, err := newJudge(t, &fakeAsk{text: text}).Judge(context.Background(), q())
		if !errors.Is(err, ErrBadPick) {
			t.Fatalf("%q: %v", text, err)
		}
	}
}

func TestJudgeCapsPercentAndIrreversible(t *testing.T) {
	f := &fakeAsk{text: `{"key":"yes","percent":250,"reason":"x"}`}
	v, _ := newJudge(t, f).Judge(context.Background(), q())
	if v.Percent != 100 {
		t.Fatalf("percent %d", v.Percent)
	}
	in := q()
	in.Stakes = StakesIrreversible
	v, _ = newJudge(t, f).Judge(context.Background(), in)
	if (Result{Percent: v.Percent}).Decides(ThresholdMin) {
		t.Fatalf("irreversible pick %d could decide", v.Percent)
	}
}

func TestJudgeBudgetIsPerPlacePerDay(t *testing.T) {
	f := &fakeAsk{text: `{"key":"no","percent":60,"reason":"x"}`, cost: JudgeBudgetUSD}
	day := time.Date(2026, 10, 10, 23, 0, 0, 0, time.UTC)
	j, _ := NewJudge(f.ask, func() time.Time { return day })
	if _, err := j.Judge(context.Background(), q()); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Judge(context.Background(), q()); !errors.Is(err, ErrBudget) {
		t.Fatalf("same place same day: %v", err)
	}
	other := q()
	other.PlaceID = "p2"
	if _, err := j.Judge(context.Background(), other); err != nil {
		t.Fatalf("other place: %v", err)
	}
	day = day.Add(2 * time.Hour)
	if _, err := j.Judge(context.Background(), q()); err != nil {
		t.Fatalf("next day: %v", err)
	}
	if f.calls != 3 {
		t.Fatalf("calls %d", f.calls)
	}
}

func TestJudgeCountsSpendOfAFailedCall(t *testing.T) {
	f := &fakeAsk{err: errors.New("boom"), cost: 0.02}
	j := newJudge(t, f)
	if _, err := j.Judge(context.Background(), q()); err == nil {
		t.Fatal("want error")
	}
	if got := j.Spent("p1"); got != 0.02 {
		t.Fatalf("spent %v", got)
	}
}
