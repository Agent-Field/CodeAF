package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE JOBS SECTION, AS A PERSON MEETS IT (jobsview.go).
//
// Every case here is a reading: live jobs, settled jobs, open or shut, an
// instant, a width and a budget. There is no window, no model and no
// terminal. The four collapsed shapes, the clock that appears only at exactly
// one running job, the live-before-settled order, the remainder counted
// rather than dropped, and the emptiness law are the whole of what the
// section promises, and they are pinned here so a layout change cannot
// quietly unsay them.

func jobViewPal() palette { return newPalette(tokens.NoColor, false) }

func jobViewNow() time.Time {
	return time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
}

func liveJob(id int, name string, age time.Duration) session.JobNotice {
	now := jobViewNow()
	return session.JobNotice{
		ID:      id,
		Name:    name,
		State:   session.JobRunning,
		Started: now.Add(-age),
		Elapsed: age,
	}
}

func doneJob(id int, name string, age time.Duration) session.JobNotice {
	return session.JobNotice{
		ID:      id,
		Name:    name,
		State:   session.JobDone,
		Elapsed: age,
	}
}

func failedJob(id int, name string, code int) session.JobNotice {
	return session.JobNotice{
		ID:       id,
		Name:     name,
		State:    session.JobFailed,
		ExitCode: code,
		Elapsed:  12 * time.Second,
	}
}

func stoppedJob(id int, name string) session.JobNotice {
	return session.JobNotice{
		ID:    id,
		Name:  name,
		State: session.JobStopped,
	}
}

func jobPlain(rows []railLine) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, strings.TrimRight(plain(row.text), " "))
	}
	return out
}

func jobJoined(rows []railLine) string {
	return strings.Join(jobPlain(rows), "\n")
}

func jobLabelOf(rows []railLine) string {
	for _, row := range rows {
		if row.jobs {
			return plain(row.text)
		}
	}
	return ""
}

// A COLLAPSED SECTION IS ONE LINE OF NEWS, in each of the four shapes the
// column actually draws.
func TestACollapsedJobsSectionIsOneLineOfNews(t *testing.T) {
	now := jobViewNow()
	pal := jobViewPal()
	const width, room = 40, 20
	clock := tokens.Duration(4*time.Minute + 12*time.Second)

	cases := []struct {
		name     string
		live     []session.JobNotice
		settled  []session.JobNotice
		contains []string
		forbids  []string
	}{
		{
			name:     "two running and no history",
			live:     []session.JobNotice{liveJob(1, "run the test suite", time.Minute), liveJob(2, "watch the docs site", 2*time.Minute)},
			contains: []string{glyphShut + " " + marginJobsWord, "2 " + jobsRunningWord},
			forbids:  []string{jobsRanWord, clock, "watch the docs site"},
		},
		{
			name:     "one running carries its clock",
			live:     []session.JobNotice{liveJob(1, "run the test suite", 4*time.Minute+12*time.Second)},
			contains: []string{glyphShut + " " + marginJobsWord, "1 " + jobsRunningWord, clock},
			forbids:  []string{jobsRanWord, "run the test suite"},
		},
		{
			name: "two running and history as a count",
			live: []session.JobNotice{liveJob(1, "run the test suite", time.Minute), liveJob(2, "watch the docs site", 2*time.Minute)},
			settled: []session.JobNotice{
				doneJob(3, "a", time.Second), doneJob(4, "b", time.Second), doneJob(5, "c", time.Second),
				doneJob(6, "d", time.Second), doneJob(7, "e", time.Second), doneJob(8, "f", time.Second),
			},
			contains: []string{glyphShut + " " + marginJobsWord, "2 " + jobsRunningWord, "6 " + jobsRanWord},
			forbids:  []string{clock, "run the test suite"},
		},
		{
			name: "history alone",
			settled: []session.JobNotice{
				doneJob(3, "a", time.Second), doneJob(4, "b", time.Second), doneJob(5, "c", time.Second),
				doneJob(6, "d", time.Second), doneJob(7, "e", time.Second), doneJob(8, "f", time.Second),
			},
			contains: []string{glyphShut + " " + marginJobsWord, "6 " + jobsRanWord},
			forbids:  []string{jobsRunningWord, clock},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := jobSectionRows(tc.live, tc.settled, false, now, width, room, pal)
			if len(rows) != marginJobsCost {
				t.Fatalf("collapsed rows = %d, want %d (blank + label):\n%s", len(rows), marginJobsCost, jobJoined(rows))
			}
			if rows[0].text != "" {
				t.Fatalf("the separator spent words: %q", rows[0].text)
			}
			got := jobLabelOf(rows)
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Fatalf("collapsed label %q does not contain %q", got, want)
				}
			}
			for _, forbid := range tc.forbids {
				if strings.Contains(got, forbid) {
					t.Fatalf("collapsed label %q carried %q", got, forbid)
				}
			}
			if !rows[1].jobs {
				t.Fatal("the label line is not the jobs door")
			}
		})
	}
}

// THE CLOCK APPEARS ONLY WHEN EXACTLY ONE JOB IS RUNNING. Two live jobs have
// two durations; naming one of them on the label would belong to no
// particular row.
func TestAJobsLabelClockAppearsOnlyWhenExactlyOneIsRunning(t *testing.T) {
	now := jobViewNow()
	pal := jobViewPal()
	age := 4*time.Minute + 12*time.Second
	clock := tokens.Duration(age)

	one := jobLabelOf(jobSectionRows(
		[]session.JobNotice{liveJob(1, "run the test suite", age)},
		nil, false, now, 40, 20, pal))
	if !strings.Contains(one, clock) {
		t.Fatalf("one running job did not name its clock %q: %q", clock, one)
	}

	two := jobLabelOf(jobSectionRows(
		[]session.JobNotice{liveJob(1, "a", age), liveJob(2, "b", age)},
		nil, false, now, 40, 20, pal))
	if strings.Contains(two, clock) {
		t.Fatalf("two running jobs still named a clock: %q", two)
	}

	none := jobLabelOf(jobSectionRows(
		nil, []session.JobNotice{doneJob(1, "a", age)},
		false, now, 40, 20, pal))
	if strings.Contains(none, clock) {
		t.Fatalf("history named a live clock: %q", none)
	}
}

// ZERO JOBS IS ZERO ROWS. A label over nothing is an announcement of absence.
func TestZeroJobsDrawNothing(t *testing.T) {
	rows := jobSectionRows(nil, nil, false, jobViewNow(), 40, 20, jobViewPal())
	if len(rows) != 0 {
		t.Fatalf("zero jobs drew %d rows:\n%s", len(rows), jobJoined(rows))
	}
	open := jobSectionRows(nil, nil, true, jobViewNow(), 40, 20, jobViewPal())
	if len(open) != 0 {
		t.Fatalf("zero jobs still drew when open:\n%s", jobJoined(open))
	}
}

// EVERY RUNNING JOB DRAWS, AND IT DRAWS BEFORE ANY SETTLED ONE. The live
// work is the news; history fills whatever is left.
func TestEveryRunningJobDrawsBeforeAnySettledOne(t *testing.T) {
	now := jobViewNow()
	live := []session.JobNotice{
		liveJob(1, "run the test suite", 4*time.Minute+12*time.Second),
		liveJob(2, "watch the docs site", time.Hour+2*time.Minute),
	}
	settled := []session.JobNotice{
		failedJob(3, "build the release", 1),
		doneJob(4, "seed the fixtures", 12*time.Second),
	}
	rows := jobSectionRows(live, settled, true, now, 40, 20, jobViewPal())
	text := jobJoined(rows)
	if !strings.Contains(text, glyphOpen+" "+marginJobsWord) {
		t.Fatalf("the open label is not on the section:\n%s", text)
	}
	runAt := strings.Index(text, "run the test suite")
	watchAt := strings.Index(text, "watch the docs site")
	buildAt := strings.Index(text, "build the release")
	seedAt := strings.Index(text, "seed the fixtures")
	if runAt < 0 || watchAt < 0 || buildAt < 0 || seedAt < 0 {
		t.Fatalf("a job row is missing:\n%s", text)
	}
	if !(runAt < watchAt && watchAt < buildAt && buildAt < seedAt) {
		t.Fatalf("running work did not lead history:\n%s", text)
	}
	if !strings.Contains(text, tokens.Duration(4*time.Minute+12*time.Second)) {
		t.Fatalf("a running row did not carry its clock:\n%s", text)
	}
	if !strings.Contains(text, tokens.Duration(time.Hour+2*time.Minute)) {
		t.Fatalf("a long-running row did not carry %q:\n%s", tokens.Duration(time.Hour+2*time.Minute), text)
	}
	var jobs []int
	for _, row := range rows {
		if row.job != 0 {
			jobs = append(jobs, row.job)
		}
	}
	if got := []int{1, 2, 3, 4}; !intSeq(jobs, got) {
		t.Fatalf("job ids = %v, want live then settled %v", jobs, got)
	}
}

func intSeq(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// WHAT DOES NOT FIT IS COUNTED, NOT DROPPED. Running work still draws in
// full; the remainder of history is a `▸ N earlier` line.
func TestAJobsSectionCountsTheRemainderOnAnEarlierLine(t *testing.T) {
	now := jobViewNow()
	live := []session.JobNotice{
		liveJob(1, "run the test suite", time.Minute),
		liveJob(2, "watch the docs site", 2*time.Minute),
	}
	settled := make([]session.JobNotice, 6)
	for i := range settled {
		settled[i] = doneJob(10+i, "finished-"+itoa(i), time.Second)
	}
	// Chrome 2 + 2 live = 4, leaving 3 rows of history-budget in a room of 7:
	// two settled rows and the earlier count. Four of the six finished jobs
	// fall off.
	const room = 7
	rows := jobSectionRows(live, settled, true, now, 40, room, jobViewPal())
	text := jobJoined(rows)
	if !strings.Contains(text, "run the test suite") || !strings.Contains(text, "watch the docs site") {
		t.Fatalf("a running job was dropped:\n%s", text)
	}
	shown := 0
	for i := range settled {
		if strings.Contains(text, "finished-"+itoa(i)) {
			shown++
		}
	}
	hidden := len(settled) - shown
	if hidden != 4 {
		t.Fatalf("hid %d of 6, want 4:\n%s", hidden, text)
	}
	want := glyphShut + " " + itoa(hidden) + " " + jobsEarlierWord
	if !strings.Contains(text, want) {
		t.Fatalf("the remainder is not %q:\n%s", want, text)
	}
}

// A FAILED JOB SAYS `N · exited 1`. A STOPPED ONE SAYS `N · stopped`. A CLEAN
// FINISH SAYS `N · done`. A RUNNING ONE SAYS `N · 4m12s`. The number is the
// handle; the word (or clock) is the state. Duration on a clean finish lives
// on the page, not restated here — a frozen clock and a ticking one were the
// same shape at a glance.
func TestAJobsRowSaysHowItEnded(t *testing.T) {
	now := jobViewNow()
	pal := jobViewPal()
	rows := jobSectionRows(nil, []session.JobNotice{
		failedJob(1, "build the release", 1),
		stoppedJob(2, "watch the docs site"),
		doneJob(3, "seed the fixtures", 12*time.Second),
	}, true, now, 40, 20, pal)
	text := jobJoined(rows)
	if !strings.Contains(text, "1"+railSep+jobsExitedWord+" 1") {
		t.Fatalf("a failed job did not say %q:\n%s", "1"+railSep+jobsExitedWord+" 1", text)
	}
	if !strings.Contains(text, "2"+railSep+jobsStoppedWord) {
		t.Fatalf("a stopped job did not say %q:\n%s", "2"+railSep+jobsStoppedWord, text)
	}
	if !strings.Contains(text, "3"+railSep+jobsDoneWord) {
		t.Fatalf("a clean finish did not say %q:\n%s", "3"+railSep+jobsDoneWord, text)
	}
	clock := tokens.Duration(12 * time.Second)
	if strings.Contains(text, clock) {
		t.Fatalf("a clean finish restated its duration on the row:\n%s", text)
	}
}

// A RUNNING ROW CARRIES ITS NUMBER BESIDE ITS CLOCK, so eleven similar
// commands stay distinguishable without opening each page.
func TestARunningJobsRowCarriesItsNumber(t *testing.T) {
	now := jobViewNow()
	age := 4*time.Minute + 12*time.Second
	rows := jobSectionRows(
		[]session.JobNotice{liveJob(8, "sleep job eight done", age)},
		nil, true, now, 40, 20, jobViewPal())
	text := jobJoined(rows)
	want := "8" + railSep + tokens.Duration(age)
	if !strings.Contains(text, want) {
		t.Fatalf("a running row did not say %q:\n%s", want, text)
	}
}

// THE CLOCK COUNTS UP FROM Started. Two frames, one notice, two readings —
// the column does not re-ask the engine for Elapsed.
func TestAJobsClockTicksFromStartedWithoutAskingTheEngine(t *testing.T) {
	now := jobViewNow()
	job := liveJob(1, "run the test suite", 4*time.Minute+12*time.Second)
	job.Elapsed = 0 // what a surface that trusted Elapsed would freeze on
	pal := jobViewPal()
	first := jobJoined(jobSectionRows([]session.JobNotice{job}, nil, true, now, 40, 20, pal))
	later := jobJoined(jobSectionRows([]session.JobNotice{job}, nil, true, now.Add(5*time.Second), 40, 20, pal))
	if first == later {
		t.Fatalf("the clock did not move with the frame:\n%s", first)
	}
	if !strings.Contains(first, tokens.Duration(4*time.Minute+12*time.Second)) {
		t.Fatalf("the first frame is not the age from Started:\n%s", first)
	}
	if !strings.Contains(later, tokens.Duration(4*time.Minute+17*time.Second)) {
		t.Fatalf("the later frame is not five seconds on:\n%s", later)
	}
}

// COLLAPSED, HISTORY IS A COUNT AND NEVER ROWS.
func TestACollapsedJobsSectionDrawsNoJobRows(t *testing.T) {
	rows := jobSectionRows(
		[]session.JobNotice{liveJob(1, "run the test suite", time.Minute)},
		[]session.JobNotice{doneJob(2, "seed the fixtures", 12*time.Second)},
		false, jobViewNow(), 40, 20, jobViewPal())
	for _, row := range rows {
		if row.job != 0 {
			t.Fatalf("a collapsed section drew job %d", row.job)
		}
	}
	text := jobJoined(rows)
	if strings.Contains(text, "run the test suite") || strings.Contains(text, "seed the fixtures") {
		t.Fatalf("a collapsed section drew a job by name:\n%s", text)
	}
}
