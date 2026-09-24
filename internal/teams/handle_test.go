package teams

import (
	"strings"
	"testing"
)

func TestDeriveHandle(t *testing.T) {
	for title, want := range map[string]string{
		"":                                "",
		"   ":                             "",
		"Refactor the parser":             "refactor",
		"Fix the login bug":               "fix-login",
		"The API docs":                    "api-docs",
		"Add OAuth2 support":              "add-oauth2",
		"Internationalization pipeline":   "internationa",
		"a the of":                        "a-the",
		"日本語":                             "chat",
		"x":                               "chat",
		"Port: codeaf -> linux/arm64":     "port",
		"Can you help me with benchmarks": "help",
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
