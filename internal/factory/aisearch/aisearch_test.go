package aisearch

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/route/judge"
)

// fake is a model that keeps, on the screen, every item whose line says word,
// and answers yes on the judge when the item's title says it.
type fake struct {
	word    string
	screens atomic.Int32
	judges  atomic.Int32
	badJSON bool
}

var bracketed = regexp.MustCompile(`(?m)^\[(\d+)\](.*)$`)

func (f *fake) call(_ context.Context, req judge.Request) (judge.Reply, error) {
	if req.System == ScreenSystem {
		f.screens.Add(1)
		if f.badJSON {
			return judge.Reply{Text: "sure, here they are", USD: 0.001}, nil
		}
		var ids []string
		for _, m := range bracketed.FindAllStringSubmatch(req.User, -1) {
			if strings.Contains(strings.ToLower(m[2]), f.word) {
				ids = append(ids, m[1])
			}
		}
		// An invented number is ignored.
		ids = append(ids, "99999")
		return judge.Reply{Text: `{"related": [` + strings.Join(ids, ",") + `]}`, USD: 0.001, Model: "served/model"}, nil
	}
	f.judges.Add(1)
	title := regexp.MustCompile(`(?m)^title: (.*)$`).FindStringSubmatch(req.User)
	yes := len(title) > 1 && strings.Contains(strings.ToLower(title[1]), f.word)
	return judge.Reply{Text: fmt.Sprintf(`{"yes": %t, "why": "It is about %s."}`, yes, f.word), USD: 0.0001}, nil
}

func items(n int, every int, word string) []factory.Item {
	out := make([]factory.Item, n)
	for i := range out {
		title := "Fix the thing " + strconv.Itoa(i)
		body := "nothing here"
		switch {
		case i%every == 0:
			title = "Make " + word + " work " + strconv.Itoa(i)
		case i%every == 1:
			body = "mentions " + word + " only in the body"
		}
		out[i] = factory.Item{ID: i + 1, Repo: "acme/api", Num: i + 1, Kind: factory.KindIssue, Title: title, Body: body}
	}
	return out
}

// Eighty items: two screen reads, the judge asked only about what the screen
// kept, a match only on the judge's yes, and every step told as progress.
func TestASearchScreensInBatchesThenJudgesTheShortlist(t *testing.T) {
	f := &fake{word: "ledger"}
	var told []Status
	got := Run(context.Background(), Options{Question: "? issues about the ledger", Items: items(80, 10, "ledger"), Model: "m", Call: f.call, Progress: func(s Status) { told = append(told, s) }})
	if got.Phase != PhaseDone || got.Total != 80 || got.Read != 80 {
		t.Fatalf("status %+v", got)
	}
	if f.screens.Load() != 2 {
		t.Fatalf("80 items took %d screen reads, want 2", f.screens.Load())
	}
	// Every tenth title and every tenth body says ledger: 16 kept, 8 match.
	if got.Related != 16 || f.judges.Load() != 16 || len(got.Matches) != 8 {
		t.Fatalf("related %d judged %d matched %d", got.Related, f.judges.Load(), len(got.Matches))
	}
	if got.Matches[0].Job != 1 || got.Matches[0].Why == "" || got.Model == "" {
		t.Fatalf("first match %+v model %q", got.Matches[0], got.Model)
	}
	if want := 2*0.001 + 16*0.0001; abs(got.USD-want) > 1e-9 {
		t.Fatalf("cost %v, want %v", got.USD, want)
	}
	if len(told) < 4 || told[0].Phase != PhaseScreening || told[len(told)-1].Phase != PhaseDone {
		t.Fatalf("progress %+v", told)
	}
	if w := Words(Status{Phase: PhaseScreening, Total: 80, Read: 40, Related: 12}); w != "Reading 80 issues… 12 look related" {
		t.Fatalf("words %q", w)
	}
	for _, c := range []struct {
		s    Status
		want string
	}{
		{Status{Phase: PhaseScreening, Total: 1, Read: 1, Related: 1}, "Reading 1 issue… 1 looks related"},
		{Status{Phase: PhaseJudging, Related: 4}, "Checking 4… 0 matches so far"},
		{Status{Phase: PhaseJudging, Related: 4, Matches: []Match{{Job: 1}}}, "Checking 4… 1 match so far"},
	} {
		if w := Words(c.s); w != c.want {
			t.Fatalf("words %q, want %q", w, c.want)
		}
	}
}

// Five thousand items cost a bounded number of calls: 125 reads, and the
// judge never more than JudgeMost, the rest said as Over.
func TestASearchOfFiveThousandIsBounded(t *testing.T) {
	f := &fake{word: "auth"}
	got := Run(context.Background(), Options{Question: "auth", Items: items(5000, 10, "auth"), Model: "m", Call: f.call})
	if f.screens.Load() != 125 || f.judges.Load() != JudgeMost {
		t.Fatalf("%d reads and %d questions", f.screens.Load(), f.judges.Load())
	}
	if got.Over != 1000-JudgeMost || got.Phase != PhaseDone {
		t.Fatalf("status %+v", got)
	}
}

// The same question again reads its kept answers: no call at all, the same
// matches. An edited item is asked again.
func TestASearchAgainReusesItsAnswersAndAsksOnlyWhatChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	f := &fake{word: "export"}
	list := items(80, 10, "export")
	first := Run(context.Background(), Options{Question: "Exports", Items: list, Model: "m", Call: f.call, Cache: OpenCache(path)})
	c := OpenCache(path)
	if err := (func() error {
		Run(context.Background(), Options{Question: "Exports", Items: list, Model: "m", Call: f.call, Cache: c})
		return c.Save()
	})(); err != nil {
		t.Fatal(err)
	}
	// The first run's cache was never saved; the second asked again and saved.
	f.screens.Store(0)
	f.judges.Store(0)
	again := Run(context.Background(), Options{Question: " exports ", Items: list, Model: "m", Call: f.call, Cache: OpenCache(path)})
	if f.screens.Load() != 0 || f.judges.Load() != 0 || again.USD != 0 || len(again.Matches) != len(first.Matches) || !again.Matches[0].Kept {
		t.Fatalf("reopen asked %d/%d and cost %v: %+v", f.screens.Load(), f.judges.Load(), again.USD, again)
	}
	list[5].Title = "Now about export too"
	Run(context.Background(), Options{Question: "exports", Items: list, Model: "m", Call: f.call, Cache: OpenCache(path)})
	if f.screens.Load() != 1 || f.judges.Load() != 1 {
		t.Fatalf("one edited item took %d reads and %d questions", f.screens.Load(), f.judges.Load())
	}
	// That run was not saved, so the edited item is still unknown.
	if e := EstimateFor("exports", list, OpenCache(path), &Price{In: 0.1, Out: 0.4}); e.Reused != 79 || e.Reads != 1 || *e.Most == 0 {
		t.Fatalf("estimate over a kept question %+v", e)
	}
}

// A screen that cannot be read sends its whole batch to the judge, and the
// answer says it was incomplete; it never matches on its own.
func TestAnUnreadableScreenFallsToTheJudge(t *testing.T) {
	f := &fake{word: "ledger", badJSON: true}
	got := Run(context.Background(), Options{Question: "ledger", Items: items(30, 10, "ledger"), Model: "m", Call: f.call})
	if got.Failed != 1 || got.Related != 30 || len(got.Matches) != 3 || got.Phase != PhaseDone {
		t.Fatalf("status %+v", got)
	}
}

func TestTheEstimateCountsReadsAndAPrice(t *testing.T) {
	e := EstimateFor("ledger", items(80, 10, "ledger"), nil, &Price{In: 0.1, Out: 0.4})
	if e.Items != 80 || e.Reads != 2 || e.JudgeMost != 60 || e.Typical == nil || e.Most == nil || *e.Typical >= *e.Most || *e.Most > 0.01 {
		t.Fatalf("estimate %+v", e)
	}
	if e := EstimateFor("ledger", items(3, 10, "ledger"), nil, nil); e.Typical != nil || e.Reads != 1 {
		t.Fatalf("no price gives no figure: %+v", e)
	}
	// Measured: 61 factory-demo issues on deepseek/deepseek-v4.1-flash
	// ($0.30 in, $1.20 out a million) kept 19 and cost $0.0132.
	e = EstimateFor("ledger", items(61, 10, "ledger"), nil, &Price{In: 0.3, Out: 1.2})
	if *e.Typical < 0.006 || *e.Typical > 0.0132 || *e.Most < 0.0132 {
		t.Fatalf("the estimate does not bracket the measured run: %+v typical %v most %v", e, *e.Typical, *e.Most)
	}
}

func TestParseScreenIsStrict(t *testing.T) {
	for _, bad := range []string{"", "[1,2]", `{"related": "1"}`, `{"related": [1], "x": 1}`, `{"related": [1]} more`} {
		if _, err := ParseScreen(bad); err == nil {
			t.Fatalf("%q read", bad)
		}
	}
	if ids, err := ParseScreen("```json\n{\"related\": [3, 4]}\n```"); err != nil || len(ids) != 2 {
		t.Fatalf("fenced reply: %v %v", ids, err)
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
