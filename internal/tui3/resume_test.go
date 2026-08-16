package tui3

import (
	"strings"
	"testing"
	"time"
)

// rosterApp is a surface with a directory of past conversations behind it and a
// door back into one of them — the two seams cmd/aforge wires (chatv3.go).
func rosterApp(t *testing.T, agent *fakeAgent, list []Session) (*app, *[]string) {
	t.Helper()
	t.Setenv("AFORGE_HOME", t.TempDir())
	a := newTestApp(agent)
	// Wide enough for a name AND the sentence beside it: the row gives the
	// description up before it gives up the age (see [sessionNote]), and a
	// 60-column frame would be asserting that rule rather than this one.
	a.width = 120
	opened := &[]string{}
	a.recentSessions = func() []Session { return list }
	a.resume = func(file string) (Agent, error) {
		*opened = append(*opened, file)
		return &fakeAgent{model: agent.model, past: agent.past}, nil
	}
	return a, opened
}

func rosterSessions() []Session {
	now := time.Now()
	return []Session{
		{
			Title: "port the resume picker to tui3",
			Last:  "run the migration against the staging database",
			File:  "/s/20260816-150405_a3f2.jsonl",
			At:    now.Add(-2 * time.Hour),
		},
		{
			// Never named: the row has to call it something anyway.
			Opening: "why does the welcome box flicker on a narrow terminal?",
			Last:    "it was the sweep, not the box",
			File:    "/s/20260815-090102_b7c1.jsonl",
			At:      now.Add(-3 * 24 * time.Hour),
		},
		{
			Title: "fix_session_lock",
			Last:  "the second window took the file",
			File:  "/s/20260814-113000_c9d4.jsonl",
			At:    now.Add(-4 * 24 * time.Hour),
		},
	}
}

func rosterFiles(a *app) []string {
	out := make([]string, 0, len(a.roster.hits))
	for _, at := range a.roster.hits {
		out = append(out, a.roster.all[at].File)
	}
	return out
}

// The whole gesture: /resume lists what this directory has been used for, in
// words, and enter opens the one under the cursor.
func TestResumeListsSessionsByNameAndOpensTheOneChosen(t *testing.T) {
	list := rosterSessions()
	a, opened := rosterApp(t, &fakeAgent{model: "openai/gpt-4.1-mini"}, list)

	typeLine(t, a, "/resume")
	if !a.roster.open {
		t.Fatal("/resume has to open the session list")
	}
	got := plain(frame(a))
	// A NAME, A DESCRIPTION AND AN AGE — and no file name anywhere.
	for _, want := range []string{
		"Port the Resume Picker to Tui3",
		"run the migration against the staging database",
		"2h ago",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the row is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, ".jsonl") {
		t.Fatalf("a row named a transcript file:\n%s", got)
	}

	drive(t, a, key("down"))
	drive(t, a, key("enter"))
	if a.roster.open {
		t.Fatal("enter has to close the list")
	}
	if len(*opened) != 1 || (*opened)[0] != list[1].File {
		t.Fatalf("opened %v, want %s", *opened, list[1].File)
	}
	if a.file != list[1].File {
		t.Fatalf("the surface is on %s, want %s", a.file, list[1].File)
	}
	if got := plain(frame(a)); !strings.Contains(got, "resumed "+list[1].File) {
		t.Fatalf("the resume was not said out loud:\n%s", got)
	}
}

// The list opens on the conversation this window is already in, and enter on
// that row does NOT close and reopen the file it is holding.
func TestThePickerOpensOnTheSessionThisWindowIsInAndDoesNotReopenIt(t *testing.T) {
	list := rosterSessions()
	agent := &fakeAgent{model: "openai/gpt-4.1-mini"}
	a, opened := rosterApp(t, agent, list)
	a.file = list[2].File

	typeLine(t, a, "/resume")
	chosen, ok := a.roster.choice()
	if !ok || chosen.File != list[2].File {
		t.Fatalf("the list opened on %+v, want the session in use", chosen)
	}
	drive(t, a, key("enter"))
	if len(*opened) != 0 {
		t.Fatalf("the session already open was reopened: %v", *opened)
	}
	if agent.closes != 0 {
		t.Fatal("the agent in use was closed to arrive where it already was")
	}
	if got := plain(frame(a)); !strings.Contains(got, "already here · Fix Session Lock") {
		t.Fatalf("the no-op was not explained:\n%s", got)
	}
}

// The filter runs over the description as well as the name, which is what
// makes an unnamed session findable by what was said in it.
func TestTheFilterReachesTheDescriptionAndTheDerivedName(t *testing.T) {
	list := rosterSessions()
	a, _ := rosterApp(t, &fakeAgent{model: "m"}, list)

	typeLine(t, a, "/resume")
	typeInto(t, a, "migration")
	if got := rosterFiles(a); len(got) != 1 || got[0] != list[0].File {
		t.Fatalf("filtering on a description word gave %v", got)
	}

	drive(t, a, key("ctrl+u"))
	typeInto(t, a, "flicker")
	if got := rosterFiles(a); len(got) != 1 || got[0] != list[1].File {
		t.Fatalf("filtering on a derived name gave %v", got)
	}

	drive(t, a, key("ctrl+u"))
	typeInto(t, a, "zzz")
	if len(a.roster.hits) != 0 {
		t.Fatalf("filtered to %v, want nothing", rosterFiles(a))
	}
	if got := plain(frame(a)); !strings.Contains(got, "no session matches") {
		t.Fatalf("an empty filter result has to say so:\n%s", got)
	}
	// And esc leaves everything exactly as it was.
	drive(t, a, key("esc"))
	if a.roster.open {
		t.Fatal("esc has to close the list")
	}
}

// A machine with nothing to resume is told so, in the one sentence that says
// what to do about it — and no overlay opens over an empty list.
func TestAMachineWithNoSessionsSaysSoRatherThanOpeningAnEmptyList(t *testing.T) {
	a, _ := rosterApp(t, &fakeAgent{model: "m"}, nil)
	typeLine(t, a, "/resume")
	if a.roster.open {
		t.Fatal("a list with no rows must not open")
	}
	if got := plain(frame(a)); !strings.Contains(got, noSessionsWord) {
		t.Fatalf("the empty state is missing:\n%s", got)
	}
}

// A surface the door never wired says so instead of doing nothing.
func TestResumeWithoutADoorSaysSo(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	typeLine(t, a, "/resume")
	if got := plain(frame(a)); !strings.Contains(got, resumeUnavailableWord) {
		t.Fatalf("an unwired surface has to say so:\n%s", got)
	}
}

// PickSession is `aforge resume`: the list is up on the first frame.
func TestPickSessionOpensTheListOverTheFirstFrame(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	list := rosterSessions()
	a := newApp(t.Context(), Options{
		Agent:          &fakeAgent{model: "m"},
		Workspace:      "/tmp/lab",
		RecentSessions: func() []Session { return list },
		Resume:         func(string) (Agent, error) { return &fakeAgent{model: "m"}, nil },
		PickSession:    true,
	})
	a.width, a.height = 60, 20
	if !a.roster.open {
		t.Fatal("aforge resume has to open on the list")
	}
	if a.welcome.open {
		t.Fatal("the welcome box and the list say the same thing twice")
	}
}

// The name on a row, all the way down the ladder.
func TestHumanNameReadsLikeSomethingAPersonWrote(t *testing.T) {
	cases := []struct {
		name    string
		session Session
		want    string
	}{
		{
			name:    "a model's lowercase title is title-cased, small words kept small",
			session: Session{Title: "port the resume picker to tui3"},
			want:    "Port the Resume Picker to Tui3",
		},
		{
			name:    "a machine-shaped single token is unpacked",
			session: Session{Title: "fix_session-lock.now"},
			want:    "Fix Session Lock Now",
		},
		{
			name:    "a hyphen inside a real sentence is NOT unpacked",
			session: Session{Title: "the state-of-the-art router"},
			want:    "The State-of-the-art Router",
		},
		{
			name:    "capitals the person wrote survive",
			session: Session{Title: "wire OpenAI keys into the sheet"},
			want:    "Wire OpenAI Keys into the Sheet",
		},
		{
			name:    "no title falls back to the opening line, bounded",
			session: Session{Opening: "why does the welcome box flicker on a narrow terminal?"},
			want:    "Why Does the Welcome Box Flicker on",
		},
		{
			name:    "nothing at all falls back to the file, without its extension",
			session: Session{File: "/s/20260816-150405_a3f2.jsonl"},
			want:    "20260816 150405 A3f2",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := humanName(test.session); got != test.want {
				t.Fatalf("humanName = %q, want %q", got, test.want)
			}
		})
	}
}

// The age is a span with "ago" on it and a date without, because "16 Aug ago"
// is not a thing anybody says.
func TestAgeReadsAsASpanOrADate(t *testing.T) {
	now := time.Now()
	cases := []struct {
		at   time.Time
		want string
	}{
		{at: now.Add(-30 * time.Second), want: "now"},
		{at: now.Add(-90 * time.Minute), want: "1h ago"},
		{at: now.Add(-3 * 24 * time.Hour), want: "3d ago"},
		{at: time.Time{}, want: ""},
	}
	for _, test := range cases {
		if got := ago(test.at); got != test.want {
			t.Fatalf("ago(%v) = %q, want %q", test.at, got, test.want)
		}
	}
	old := now.Add(-90 * 24 * time.Hour)
	if got := ago(old); got != old.Format("2 Jan") {
		t.Fatalf("a months-old session reads %q, want a date", got)
	}
}

// A narrow frame drops the description rather than the age: the sentence still
// says what it was about when it ends early, and a clipped "2h" is a wrong
// number.
func TestANarrowRowKeepsTheAgeAndGivesUpTheDescription(t *testing.T) {
	session := Session{
		Title: "port the resume picker",
		Last:  "run the migration against the staging database",
		At:    time.Now().Add(-2 * time.Hour),
	}
	name := humanName(session)
	if note := sessionNote(session, 100, name); !strings.HasSuffix(note, "· 2h ago") ||
		!strings.HasPrefix(note, "run the migration") {
		t.Fatalf("a wide row reads %q", note)
	}
	// The width has to be spent by the NAME for the sentence to be squeezed out,
	// and the frame has to be one where the two still share a line: under
	// [tierPhone] the tail has a line of its own and the arithmetic is a
	// different one (palette.go's [overlayLines]).
	long := Session{
		Title: "port the resume picker to the new surface",
		Last:  "run the migration against the staging database",
		At:    time.Now().Add(-2 * time.Hour),
	}
	if note := sessionNote(long, 60, humanName(long)); note != "2h ago" {
		t.Fatalf("a narrow row reads %q, want the age alone", note)
	}
}
