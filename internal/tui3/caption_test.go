package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

func captionFixture() []entry {
	base := time.Unix(100, 0)
	return []entry{
		{kind: entryUser, text: "why?", turn: 1},
		{kind: entryAssistant, text: "checking where the fold is minted.\nThe details follow.", turn: 1, settled: true},
		{kind: entryTool, tool: "read", turn: 1, status: toolOK, detail: toolDetail{Args: `{"path":"internal/tui3/render.go"}`}, began: base, ended: base.Add(time.Second)},
		{kind: entryTool, tool: "read", turn: 1, status: toolOK, detail: toolDetail{Args: `{"path":"internal/tui3/workfold.go"}`}, began: base, ended: base.Add(2 * time.Second)},
		{kind: entryAssistant, text: "The fold is minted in deckRows.", turn: 1, settled: true},
	}
}

func captionsOf(es []entry, running int) []caption {
	stampHierarchy(es, deriveWorkfolds(es, running))
	return deriveCaptions(es, running)
}

func TestACaptionIsTheFirstLineOfTheProseThatWorkFollows(t *testing.T) {
	es := captionFixture()
	got := captionsOf(es, 0)
	if len(got) != 1 || got[0].text != "checking where the fold is minted" || got[0].source != captionSaid {
		t.Fatalf("caption = %#v", got)
	}
	stampCaptions(es, got)
	if !es[1].capHead || es[1].capCut != strings.IndexByte(es[1].text, '\n')+1 {
		t.Fatalf("head was not lifted: %#v", es[1])
	}
}

func TestTheAnswersFirstLineIsNeverACaption(t *testing.T) {
	es := captionFixture()
	for _, c := range captionsOf(es, 0) {
		if strings.Contains(c.text, "fold is minted in deckRows") {
			t.Fatalf("answer became a caption: %#v", c)
		}
	}
}

func TestASingleCallStillGetsACaption(t *testing.T) {
	es := captionFixture()
	es = append(es[:3], es[4:]...)
	got := captionsOf(es, 0)
	if len(got) != 1 || got[0].text == "" {
		t.Fatalf("single call lost its caption: %#v", got)
	}
}

func TestTheCompositeStandsWhenTheModelSaidNothing(t *testing.T) {
	es := captionFixture()
	es = append(es[:1], es[2:]...)
	got := captionsOf(es, 0)
	if len(got) != 1 || got[0].text != "reading 2 files in internal/tui3" || got[0].source != captionMade {
		t.Fatalf("composite = %#v", got)
	}
}

func TestABashFloorNamesTheWorkNotTheVerb(t *testing.T) {
	es := []entry{{
		kind: entryTool, tool: "bash", status: toolOK,
		detail: toolDetail{Args: `{"command":"gh issue list --repo Agent-Field/aforge-v2 --state open --limit 60"}`},
	}}
	if got := composeCaption(es, 0, 1); got != "listing github issues" {
		t.Fatalf("bash floor = %q", got)
	}
}

func TestThinkingNeverBecomesACaption(t *testing.T) {
	es := []entry{
		{kind: entryUser, text: "why?", turn: 1},
		{kind: entryThinking, text: "I should look at the open issues first.\nThen rank them.", turn: 1, settled: true},
		{kind: entryTool, tool: "bash", turn: 1, status: toolOK,
			detail: toolDetail{Args: `{"command":"gh issue list"}`}},
	}
	got := captionsOf(es, 0)
	if len(got) != 1 {
		t.Fatalf("caption = %#v", got)
	}
	if got[0].text == "I should look at the open issues first" ||
		strings.Contains(got[0].text, "I should") {
		t.Fatalf("thinking leaked into the step title: %#v", got[0])
	}
	if got[0].source != captionMade || got[0].text != "listing github issues" {
		t.Fatalf("want a tool floor step, got %#v", got[0])
	}
}

func TestFourReadsUnderOneDirectoryNameThatDirectory(t *testing.T) {
	var es []entry
	for _, name := range []string{"a.go", "b.go", "c.go", "d.go"} {
		es = append(es, entry{kind: entryTool, tool: "read", turn: 1, status: toolOK,
			detail: toolDetail{Args: `{"path":"internal/tui3/` + name + `"}`}})
	}
	if got := composeCaption(es, 0, len(es)); got != "reading 4 files in internal/tui3" {
		t.Fatalf("composite = %q", got)
	}
}

func TestConsecutiveHeadsWithNoWorkBetweenThemMerge(t *testing.T) {
	es := captionFixture()
	es = append(es[:2], append([]entry{
		{kind: entryAssistant, text: "another paragraph.", turn: 1, settled: true},
	}, es[2:]...)...)
	got := captionsOf(es, 0)
	if len(got) != 1 || got[0].head != 1 {
		t.Fatalf("heads did not merge: %#v", got)
	}
}

func TestACaptionIsOneShortSentence(t *testing.T) {
	// Two sentences: skip the thin opener, keep the fuller short sentence.
	got := shortCaption("Good leads. Fetching the key pages to confirm which are open.")
	if got != "Fetching the key pages to confirm which are open" {
		t.Fatalf("shortCaption = %q", got)
	}
	// A long single sentence shrinks without ending mid-clause.
	long := "fetching the key pages to confirm which are actually still open tonight in toronto"
	got = shortCaption(long)
	if got != "fetching the key pages to confirm" {
		t.Fatalf("long shortCaption = %q", got)
	}
	if strings.Contains(got, "…") || strings.HasSuffix(got, "are") || strings.HasSuffix(got, "which") {
		t.Fatalf("caption ended mid-clause: %q", got)
	}
}

func TestCaptionWordsKeepsTokenPunctuationAndSentenceBoundaries(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "file extension",
			line: "Reading livesteps.go now.",
			want: "Reading livesteps.go now",
		},
		{
			name: "dotted token in a sentence",
			line: "I will update config.json and then run the suite.",
			want: "I will update config.json and then run the suite",
		},
		{
			name: "version number",
			line: "Bumping the pin to v1.2.3 across the three services.",
			want: "Bumping the pin to v1.2.3 across the three services",
		},
		{
			name: "dot inside a path without a sentence end",
			line: "searching ~/.aforge/v3/projects for the transcript",
			want: "searching ~/.aforge/v3/projects for the transcript",
		},
		{
			name: "real sentence boundary",
			line: "Good leads. Fetching the key pages to confirm which are open.",
			want: "Fetching the key pages to confirm which are open",
		},
		{
			name: "newline boundary",
			line: "Reading livesteps.go now\nThen checking the renderer.",
			want: "Reading livesteps.go now",
		},
		{
			name: "word limit and dangling words",
			line: "fetching the key pages to confirm which are actually still open tonight in toronto",
			want: "fetching the key pages to confirm",
		},
		{
			name: "question mark inside a token",
			line: "fetching https://api.example.com/v1?limit=10 for the list",
			want: "fetching https://api.example.com/v1?limit=10 for the list",
		},
		{
			name: "exclamation mark inside a token",
			line: "checking cache!primary before reading the fallback",
			want: "checking cache!primary before reading the fallback",
		},
		{
			name: "question mark at a sentence end",
			line: "Ready? Fetching the key pages to confirm which are open.",
			want: "Fetching the key pages to confirm which are open",
		},
		{
			name: "terminator run at a sentence end",
			line: "Done!! Fetching the key pages to confirm which are open.",
			want: "Fetching the key pages to confirm which are open",
		},
		{
			name: "closing punctuation before a sentence end",
			line: "Good (“confirmed!”). Fetching the key pages to confirm which are open.",
			want: "Fetching the key pages to confirm which are open",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := captionWords(tt.line)
			if got != tt.want {
				t.Fatalf("captionWords(%q) = %q, want %q", tt.line, got, tt.want)
			}
			if strings.Contains(got, "…") {
				t.Fatalf("captionWords(%q) appended an ellipsis: %q", tt.line, got)
			}
		})
	}
}

func TestTheDrawnCaptionKeepsAFileExtension(t *testing.T) {
	es := captionFixture()
	es[1].text = "Reading livesteps.go now."
	got := captionsOf(es, 0)
	if len(got) != 1 {
		t.Fatalf("captions = %#v, want one", got)
	}

	a := newTestApp(&fakeAgent{model: "m"})
	rows := a.captionRows(got[0], false, false, 80, a.conversation())
	var drawn strings.Builder
	for _, row := range rows {
		drawn.WriteString(plain(row.text))
		drawn.WriteByte('\n')
	}
	if page := drawn.String(); !strings.Contains(page, "livesteps.go") {
		t.Fatalf("drawn caption lost the file extension:\n%s", page)
	}
}

func TestANarrowCaptionWrapsWithoutEllipsis(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	c := caption{text: "listing open github issues for quality", start: 1, calls: 2, began: time.Unix(100, 0), ended: time.Unix(102, 0)}
	rows := a.captionRows(c, false, false, 28, a.conversation())
	if len(rows) < 2 {
		t.Fatalf("expected a wrap on a narrow frame, got %d rows: %#v", len(rows), rows)
	}
	var body strings.Builder
	for _, r := range rows {
		body.WriteString(plain(r.text))
		body.WriteByte('\n')
	}
	page := body.String()
	if strings.Contains(page, "…") || strings.Contains(page, "...") {
		t.Fatalf("wrapped caption used an ellipsis:\n%s", page)
	}
	if !strings.Contains(page, "listing") || !strings.Contains(page, "quality") {
		t.Fatalf("wrapped caption lost words:\n%s", page)
	}
}

func TestExpandingACaptionStopsItsShimmerAndStartsTheRowSpinners(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.pal = newPalette(tokens.TrueColor, false)
	c := caption{text: "checking the fold", start: 1, calls: 2, began: time.Unix(100, 0)}
	a.clock = func() time.Time { return time.Unix(104, 0) }
	captionTimeAt(a, 0)
	closed := a.captionRow(c, true, false, 80, a.conversation()).text
	captionTimeAt(a, shimmerPeriod/4)
	if next := a.captionRow(c, true, false, 80, a.conversation()).text; next == closed {
		t.Fatal("collapsed live caption did not shimmer")
	}
	captionTimeAt(a, 0)
	open := a.captionRow(c, true, true, 80, a.conversation()).text
	captionTimeAt(a, shimmerPeriod/4)
	if next := a.captionRow(c, true, true, 80, a.conversation()).text; next != open {
		t.Fatal("expanded caption kept shimmering")
	}
}

func TestTheLinearTierDrawsNoShimmer(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.linear = true
	a.paints = 0
	first := a.shimmer("checking")
	captionTimeAt(a, shimmerPeriod/4)
	if second := a.shimmer("checking"); second != first {
		t.Fatalf("linear shimmer moved: %q then %q", first, second)
	}
}

func TestTheChipOpensToTheOutlineAndNotTheMachinery(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries, a.workMode = foldFixture(), config.WorkFold
	a.toggleLatestWorkfold()
	page := strings.Join(plainRows(a), "\n")
	if !strings.Contains(page, "I will inspect it") {
		t.Fatalf("outline lost its caption:\n%s", page)
	}
	if strings.Contains(page, "read") || strings.Contains(page, "bash") {
		t.Fatalf("outline exposed machinery:\n%s", page)
	}
}

func TestALiveTurnKeepsPastCaptionsShutAndTheFrontierOpen(t *testing.T) {
	base := time.Unix(100, 0)
	es := []entry{
		{kind: entryUser, text: "go", turn: 1},
		{kind: entryTool, tool: "read", turn: 1, status: toolOK,
			detail: toolDetail{Args: `{"path":"a.go"}`}, began: base, ended: base.Add(time.Second)},
		{kind: entryTool, tool: "read", turn: 1, status: toolOK,
			detail: toolDetail{Args: `{"path":"b.go"}`}, began: base, ended: base.Add(2 * time.Second)},
		// Second step begins after the first batch finished — sequential, not parallel.
		{kind: entryTool, tool: "edit", turn: 1, status: toolRunning,
			detail: toolDetail{Args: `{"path":"c.go"}`}, began: base.Add(3 * time.Second)},
	}
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries = es
	a.turn = 1
	a.state = stateWorking
	// INSIDE THE OPENED WORK, which is where a running turn's outline lives now:
	// the conversation draws three compact step lines until somebody asks for the
	// machinery (livesteps.go), and this law is about what they are shown once
	// they have — the past steps shut, the step still running open.
	showLiveWork(t, a)
	page := strings.Join(plainRows(a), "\n")
	if !strings.Contains(page, "reading 2 files") {
		t.Fatalf("past caption missing:\n%s", page)
	}
	if !strings.Contains(page, "editing") {
		t.Fatalf("frontier caption missing:\n%s", page)
	}
	if !strings.Contains(page, "c.go") {
		t.Fatalf("frontier tools not open:\n%s", page)
	}
	for _, line := range strings.Split(page, "\n") {
		if (strings.Contains(line, "a.go") || strings.Contains(line, "b.go")) &&
			(strings.Contains(line, "read") || strings.Contains(line, "▶")) {
			t.Fatalf("past tools still open:\n%s", page)
		}
	}
}

func TestOpeningACaptionUnderTheChipShowsItsCalls(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries, a.workMode = foldFixture(), config.WorkFold
	a.toggleLatestWorkfold()
	stampHierarchy(a.entries, deriveWorkfolds(a.entries, 0))
	caps := deriveCaptions(a.entries, 0)
	if len(caps) == 0 {
		t.Fatal("no captions to open")
	}
	a.toggleCap(caps[0].start)
	page := strings.Join(plainRows(a), "\n")
	if !strings.Contains(page, "read") && !strings.Contains(page, "bash") {
		t.Fatalf("opened caption hid its calls:\n%s", page)
	}
}

func TestACaptionKeepsItsWordsAndItsTenseAfterSettle(t *testing.T) {
	es := captionFixture()
	before := captionsOf(es, 1)[0].text
	after := captionsOf(es, 0)[0].text
	if before != after || after != "checking where the fold is minted" {
		t.Fatalf("caption changed from %q to %q", before, after)
	}
}

func TestAnInterruptedTurnLeavesItsLastCaptionStill(t *testing.T) {
	es := captionFixture()
	for i := range es {
		if es[i].turn == 1 && es[i].kind != entryUser {
			es[i].cut = true
		}
	}
	if got := captionsOf(es, 0); len(got) != 1 {
		t.Fatalf("interrupted caption disappeared: %#v", got)
	}
}

func TestATurnWithNoWorkHasNoCaptions(t *testing.T) {
	es := []entry{{kind: entryUser, text: "hello", turn: 1},
		{kind: entryAssistant, text: "hello", turn: 1, settled: true}}
	if got := captionsOf(es, 0); len(got) != 0 {
		t.Fatalf("text-only turn got captions: %#v", got)
	}
}
