package teams

import (
	"strings"
	"testing"
)

func TestDeriveHandle(t *testing.T) {
	for title, want := range map[string]string{
		"":                                "",
		"   ":                             "",
		"Refactor the parser":             "parser",
		"Fix the login bug":               "login",
		"The API docs":                    "api-docs",
		"Add OAuth2 support":              "oauth2",
		"Internationalization pipeline":   "pipeline",
		"Internationalization":            "internationa",
		"a the of":                        "chat",
		"日本語":                             "chat",
		"x":                               "chat",
		"Port: codeaf -> linux/arm64":     "arm64",
		"Can you help me with benchmarks": "benchmarks",
		"release 2026":                    "release",
		"lexer rewrite":                   "lexer",
		"benchmark sweep":                 "benchmark",
	} {
		got := DeriveHandle(title)
		if got != want {
			t.Errorf("DeriveHandle(%q) = %q, want %q", title, got, want)
		}
		if got != "" && ValidHandle(got) != nil {
			t.Errorf("DeriveHandle(%q) = %q is not valid: %v", title, got, ValidHandle(got))
		}
	}
}

// THE TITLES THAT GAVE @checking, @review, @agent AND @te, as they were on the
// machine where a team of them was first made. Each handle names what the
// conversation is about, none is a filler word or a two-letter fragment, and
// the team's handles are all different.
func TestHandlesFromRealTitlesNameTheWork(t *testing.T) {
	titles := map[string]string{
		"checking codeaf branches for qa binary":              "qa-binary",
		"review santosh dev2 branch code complexity security": "security",
		"reviewing codeaf repo issue tags and milestones":     "milestones",
		"agent native user journey automated testing":         "testing",
		"can you te": "chat",
	}
	tm := Team{ID: "t"}
	for title, want := range titles {
		got := DeriveHandle(title)
		if got != want {
			t.Errorf("DeriveHandle(%q) = %q, want %q", title, got, want)
		}
		if want != "chat" && (fillerWords[got] || len(got) < handleWordMin) {
			t.Errorf("DeriveHandle(%q) = %q, a filler word or a fragment", title, got)
		}
		tm.Members = append(tm.Members, Member{Key: title, Word: title})
	}
	assignHandles(&tm)
	seen := map[string]bool{}
	for _, m := range tm.Members {
		if seen[m.Handle] {
			t.Fatalf("two members of one team were given @%s", m.Handle)
		}
		seen[m.Handle] = true
	}
}

func TestHandleCollisionsAreNumberedWithinTheLimit(t *testing.T) {
	tm := Team{ID: "t"}
	for i := 0; i < 12; i++ {
		tm.Members = append(tm.Members, Member{Key: string(rune('a' + i)), Word: "Internationalization"})
	}
	assignHandles(&tm)
	seen := map[string]bool{}
	for i, m := range tm.Members {
		if ValidHandle(m.Handle) != nil || seen[m.Handle] {
			t.Fatalf("member %d has handle %q", i, m.Handle)
		}
		seen[m.Handle] = true
	}
	if tm.Members[0].Handle != "internationa" || tm.Members[1].Handle != "internation2" || tm.Members[11].Handle != "internatio12" {
		t.Fatalf("handles %+v", tm.Members)
	}
	// A reserved word is never a handle, even when a title suggests one.
	tm = Team{ID: "t", Members: []Member{{Key: "m", Word: "Manager"}}}
	assignHandles(&tm)
	if tm.Members[0].Handle != "manager2" {
		t.Fatalf("a title of Manager gave %q", tm.Members[0].Handle)
	}
}

func TestSetHandleValidates(t *testing.T) {
	f := &File{Teams: []Team{{ID: "t1", Members: []Member{{Key: "a", Word: "alpha"}, {Key: "b", Word: "beta"}}}}}
	tidy(f.Teams)
	for h, why := range map[string]string{
		"beta":          "another member's handle",
		"b":             "too short",
		"thirteenchars": "too long",
		"Alpha":         "upper case",
		"al pha":        "a space",
		"-alpha":        "a leading hyphen",
		"everyone":      "a reserved address",
	} {
		if err := f.SetHandle("t1", "a", h); err == nil {
			t.Errorf("%q (%s) was accepted", h, why)
		}
	}
	if err := f.SetHandle("t1", "a", "alpha"); err != nil {
		t.Fatalf("a member's own handle again: %v", err)
	}
	if err := f.SetHandle("t1", "a", "lead-1"); err != nil {
		t.Fatal(err)
	}
	if m, ok := f.Teams[0].ByHandle("lead-1"); !ok || m.Key != "a" {
		t.Fatalf("by handle: %+v %v", m, ok)
	}
	if err := f.SetHandle("t1", "zz", "free"); err == nil || !strings.Contains(err.Error(), "not in team") {
		t.Fatalf("a stranger's handle: %v", err)
	}
}

// A FILE WITH A REPEATED OR BROKEN HANDLE IS PUT RIGHT: the first keeps it,
// and a later one is given a fresh one.
func TestRepairClearsRepeatedAndInvalidHandles(t *testing.T) {
	tm := Team{ID: "t", Members: []Member{
		{Key: "a", Word: "alpha", Handle: "lead"},
		{Key: "b", Word: "beta", Handle: "lead"},
		{Key: "c", Word: "gamma", Handle: "NOT VALID"},
		{Key: "d", Word: "delta", Handle: "fine"},
	}}
	if !assignHandles(&tm) {
		t.Fatal("the repair reported no change")
	}
	var got []string
	for _, m := range tm.Members {
		got = append(got, m.Handle)
	}
	if strings.Join(got, ",") != "lead,beta,gamma,fine" {
		t.Fatalf("handles %v", got)
	}
	if assignHandles(&tm) {
		t.Fatal("a second repair changed something")
	}
}

// THE TITLE MODEL CHOOSES THE WORD, AND THE STORE KEEPS THE RULES. The three
// titles the manager had made @review, @reviewing and @session of, each given
// the word a model answers for it: each member takes its word, a derived
// handle is replaced once and marked as the model's, and a second choice is
// never asked for again.
func TestChooseHandleTakesTheModelsWord(t *testing.T) {
	f := &File{Teams: []Team{{ID: "t1", Name: "test"}}}
	titles := map[string]string{
		"a": "santosh dev2 branch code complexity & security review",
		"b": "CodeAF repo issue tags & milestones",
		"c": "quantum gravity research updates / session monitor",
	}
	legacy := map[string]string{"a": "review", "b": "reviewing", "c": "session"}
	for _, key := range []string{"a", "b", "c"} {
		// Written by a build that did not keep who chose: read as derived.
		f.Teams[0].Members = append(f.Teams[0].Members, Member{Key: key, Word: titles[key], Handle: legacy[key]})
	}
	model := map[string][]string{"a": {"security"}, "b": {"milestones"}, "c": {"gravity"}}
	for _, key := range []string{"a", "b", "c"} {
		old, now, err := f.ChooseHandle("t1", key, model[key], titles[key])
		if err != nil {
			t.Fatal(err)
		}
		if old != legacy[key] || now != model[key][0] {
			t.Errorf("%s: %q -> %q, want %q -> %q", key, old, now, legacy[key], model[key][0])
		}
		m, _ := f.Teams[0].Member(key)
		if m.Handle != now || m.HandleBy != HandleByModel || m.HandleDerived() {
			t.Errorf("%s: stored %+v", key, m)
		}
	}
	// Chosen once: a later answer changes nothing.
	if old, now, _ := f.ChooseHandle("t1", "a", []string{"complexity"}, titles["a"]); old != "security" || now != "security" {
		t.Errorf("a chosen handle was chosen again: %q -> %q", old, now)
	}
}

// A CLASH TAKES THE SECOND CHOICE, THEN A QUALIFIER, NEVER A DIGIT SOUP; and a
// handle given by a person or the manager is never replaced.
func TestChooseHandleClashesAndTypedHandles(t *testing.T) {
	f := &File{Teams: []Team{{ID: "t1", Name: "test"}}}
	if err := f.AddMember("t1", Member{Key: "sec", Word: "security audit", Handle: "security"}); err != nil {
		t.Fatal(err)
	}
	if m, _ := f.Teams[0].Member("sec"); m.HandleBy != HandleByTyped {
		t.Fatalf("a handle that arrived with its member is not typed: %+v", m)
	}
	if err := f.AddMember("t1", Member{Key: "b", Word: "api token security"}); err != nil {
		t.Fatal(err)
	}
	if m, _ := f.Teams[0].Member("b"); m.HandleBy != HandleByWords || !m.HandleDerived() {
		t.Fatalf("a guessed handle is not marked the word list's: %+v", m)
	}
	if _, now, _ := f.ChooseHandle("t1", "b", []string{"security", "tokens"}, "api token security"); now != "tokens" {
		t.Errorf("second choice: got %q", now)
	}
	if err := f.AddMember("t1", Member{Key: "c", Word: "oauth token security"}); err != nil {
		t.Fatal(err)
	}
	_, now, _ := f.ChooseHandle("t1", "c", []string{"security", "tokens"}, "oauth token security")
	// "oauth-security" is past HandleMax, so the second choice is qualified.
	if now != "oauth-tokens" {
		t.Errorf("qualified: got %q", now)
	}
	if strings.IndexAny(now, "0123456789") >= 0 {
		t.Errorf("a digit in %q", now)
	}
	// The typed one is kept whatever the model says.
	if old, now, _ := f.ChooseHandle("t1", "sec", []string{"audit"}, "security audit"); old != "security" || now != "security" {
		t.Errorf("a typed handle was replaced: %q -> %q", old, now)
	}
	if err := f.SetHandle("t1", "b", "tok"); err != nil {
		t.Fatal(err)
	}
	if old, now, _ := f.ChooseHandle("t1", "b", []string{"jwt"}, "api token security"); now != "tok" || old != "tok" {
		t.Errorf("SetHandle's handle was replaced: %q -> %q", old, now)
	}
	// Nothing usable changes nothing.
	if err := f.AddMember("t1", Member{Key: "d", Word: "release notes draft"}); err != nil {
		t.Fatal(err)
	}
	before, _ := f.Teams[0].Member("d")
	if _, _, err := f.ChooseHandle("t1", "d", []string{"Two Words", "x", "manager"}, ""); err == nil {
		t.Error("an unusable answer was taken")
	}
	if after, _ := f.Teams[0].Member("d"); after != before {
		t.Errorf("an unusable answer changed the member: %+v", after)
	}
}
