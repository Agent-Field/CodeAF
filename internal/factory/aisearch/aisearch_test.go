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
	"time"

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

// Eighty items: four screen reads, the judge asked only about what the screen
// kept, a match only on the judge's yes, and every step told as progress.
func TestASearchScreensInBatchesThenJudgesTheShortlist(t *testing.T) {
	f := &fake{word: "ledger"}
	var told []Status
	got := Run(context.Background(), Options{Question: "? issues about the ledger", Items: items(80, 10, "ledger"), Model: "m", Call: f.call, Progress: func(s Status) { told = append(told, s) }})
	if got.Phase != PhaseDone || got.Total != 80 || got.Read != 80 {
		t.Fatalf("status %+v", got)
	}
	if f.screens.Load() != 4 {
		t.Fatalf("80 items took %d screen reads, want 4", f.screens.Load())
	}
	// Every tenth title and every tenth body says ledger: 16 kept, 8 match.
	if got.Related != 16 || f.judges.Load() != 16 || len(got.Matches) != 8 {
		t.Fatalf("related %d judged %d matched %d", got.Related, f.judges.Load(), len(got.Matches))
	}
	if got.Matches[0].Job != 1 || got.Matches[0].Why == "" || got.Model == "" {
		t.Fatalf("first match %+v model %q", got.Matches[0], got.Model)
	}
	if want := 4*0.001 + 16*0.0001; abs(got.USD-want) > 1e-9 {
		t.Fatalf("cost %v, want %v", got.USD, want)
	}
	if len(told) < 4 || told[0].Phase != PhaseScreening || told[len(told)-1].Phase != PhaseDone {
		t.Fatalf("progress %+v", told)
	}
	for i := 1; i < len(told); i++ {
		if told[i].Share < told[i-1].Share {
			t.Fatalf("progress went back: %v then %v", told[i-1].Share, told[i].Share)
		}
	}
	if got.Share != 1 {
		t.Fatalf("a finished search is at %v, want 1", got.Share)
	}
	for _, c := range []struct {
		s    Status
		want string
	}{
		{Status{Phase: PhaseScreening, Total: 61}, "Reading 61 issues…"},
		{Status{Phase: PhaseScreening, Total: 61, Read: 40}, "Read 40 of 61"},
		{Status{Phase: PhaseScreening, Total: 61, Read: 40, Matches: []Match{{Job: 1}, {Job: 2}}}, "Read 40 of 61 · 2 matches so far"},
		{Status{Phase: PhaseJudging, Total: 61, Read: 61, Judging: 9, Checked: 4, Matches: []Match{{Job: 1}}}, "Read 61 of 61 · checking 5 more · 1 match so far"},
		{Status{Phase: PhaseDone, Total: 61, Read: 61, Matches: []Match{{Job: 1}}}, "1 match"},
	} {
		if w := Words(c.s); w != c.want {
			t.Fatalf("words %q, want %q", w, c.want)
		}
	}
}

// A match is told while the rest is still being read: the second screen read
// is held until progress has carried a match, so a search that judged only
// after reading everything would never get there and the read would give up.
func TestMatchesArriveWhileTheRestIsStillRead(t *testing.T) {
	f := &fake{word: "ledger"}
	seen := make(chan struct{})
	var once atomic.Bool
	var screens atomic.Int32
	call := func(ctx context.Context, req judge.Request) (judge.Reply, error) {
		if req.System == ScreenSystem && screens.Add(1) == 2 {
			select {
			case <-seen:
			case <-time.After(5 * time.Second):
				return judge.Reply{}, fmt.Errorf("no match was told before the last read")
			}
		}
		return f.call(ctx, req)
	}
	got := Run(context.Background(), Options{Question: "ledger", Items: items(80, 10, "ledger"), Model: "m", Call: call, Parallel: 2, Progress: func(s Status) {
		if len(s.Matches) > 0 && s.Read < s.Total && once.CompareAndSwap(false, true) {
			close(seen)
		}
	}})
	if got.Failed != 0 || len(got.Matches) != 8 {
		t.Fatalf("failed %d matched %d: the matches waited for every read", got.Failed, len(got.Matches))
	}
}

// Five thousand items cost a bounded number of calls: 250 reads, and the
// judge never more than JudgeMost, the rest said as Over.
func TestASearchOfFiveThousandIsBounded(t *testing.T) {
	f := &fake{word: "auth"}
	got := Run(context.Background(), Options{Question: "auth", Items: items(5000, 10, "auth"), Model: "m", Call: f.call})
	if f.screens.Load() != 250 || f.judges.Load() != JudgeMost {
		t.Fatalf("%d reads and %d questions", f.screens.Load(), f.judges.Load())
	}
	if got.Over != 1000-JudgeMost || got.Phase != PhaseDone {
		t.Fatalf("status %+v", got)
	}
}

// A search that reaches its limit stops asking at once: it never spends more
// than the limit and the one call that crossed it, keeps what it found, and
// says how many items it never finished.
func TestASearchStopsAtItsCostLimit(t *testing.T) {
	f := &fake{word: "auth"}
	const limit = 0.01
	got := Run(context.Background(), Options{Question: "auth", Items: items(5000, 10, "auth"), Model: "m", Call: f.call, Parallel: 1, Limit: limit})
	if !got.Limited || got.Phase != PhaseDone {
		t.Fatalf("not stopped at the limit: %+v", got)
	}
	if got.USD > limit+0.001+1e-9 || f.screens.Load() > 11 {
		t.Fatalf("spent %v over %d reads past a limit of %v", got.USD, f.screens.Load(), limit)
	}
	if want := got.Total - got.Read + got.Judging - got.Checked; got.Unread != want || got.Unread < 4000 {
		t.Fatalf("unread %d, want %d", got.Unread, want)
	}
	if w := Words(got); !strings.Contains(w, " · stopped at the cost limit · ") || !strings.HasSuffix(w, fmt.Sprintf("%d not read", got.Unread)) {
		t.Fatalf("words %q", w)
	}
	// Stopped with questions under way: the matches already found are kept,
	// and the reads and questions not made are what is unread.
	fj := &fake{word: "auth"}
	seen := 0
	mid := Run(context.Background(), Options{Question: "auth", Items: items(200, 10, "auth"), Model: "m", Call: fj.call, Parallel: 1, Limit: 0.0105,
		Progress: func(s Status) { seen = max(seen, len(s.Matches)) }})
	if !mid.Limited || len(mid.Matches) != seen || mid.Unread != mid.Total-mid.Read+mid.Judging-mid.Checked || mid.Unread == 0 || mid.USD > 0.0105+0.001 {
		t.Fatalf("stopped while judging: %+v", mid)
	}
	// No limit: the same search reads everything.
	f2 := &fake{word: "auth"}
	if all := Run(context.Background(), Options{Question: "auth", Items: items(200, 10, "auth"), Model: "m", Call: f2.call}); all.Limited || all.Read != 200 {
		t.Fatalf("an unlimited search stopped: %+v", all)
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
	if got.Failed != 2 || got.Related != 30 || len(got.Matches) != 3 || got.Phase != PhaseDone {
		t.Fatalf("status %+v", got)
	}
}

func TestTheEstimateCountsReadsAndAPrice(t *testing.T) {
	e := EstimateFor("ledger", items(80, 10, "ledger"), nil, &Price{In: 0.1, Out: 0.4})
	if e.Items != 80 || e.Reads != 4 || e.JudgeMost != 60 || e.Typical == nil || e.Most == nil || *e.Typical >= *e.Most || *e.Most > 0.02 {
		t.Fatalf("estimate %+v", e)
	}
	if e := EstimateFor("ledger", items(3, 10, "ledger"), nil, nil); e.Typical != nil || e.Reads != 1 {
		t.Fatalf("no price gives no figure: %+v", e)
	}
	// Measured 2026-11-07: 65 factory-demo issues on deepseek/deepseek-v4.1-flash
	// ($0.30 in, $1.20 out a million) cost $0.0017, $0.0079 and $0.0263 for
	// three questions. The typical figure sits among them and the most covers
	// the dearest.
	e = EstimateFor("ledger", items(65, 10, "ledger"), nil, &Price{In: 0.3, Out: 1.2})
	if *e.Typical < 0.0017 || *e.Typical > 0.0263 || *e.Most < 0.0263 {
		t.Fatalf("the estimate does not bracket the measured runs: %+v typical %v most %v", e, *e.Typical, *e.Most)
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

// The search's question fits the judge's bound whole, however long the
// person's words are: a cut would drop the kinds of work that count.
func TestTheAskFitsTheJudgeWhole(t *testing.T) {
	if n := len([]rune(Ask(strings.Repeat("a", QuestionMost+50)))); n > judge.AskMost {
		t.Fatalf("the ask is %d runes, the judge keeps %d", n, judge.AskMost)
	}
}
