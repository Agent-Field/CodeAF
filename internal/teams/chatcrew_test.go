package teams

import "testing"

func TestChatCrewPrefersTheCrewItManagesThenItsHomeAndNeverTheRoot(t *testing.T) {
	is := func(m Member) bool { return m.Key == "chat" }
	root := Team{ID: "root", Root: true, Members: []Member{{Key: "chat"}}, Manager: "chat"}
	parent := Team{ID: "parent", Members: []Member{{Key: "other"}, {Key: "chat", Home: true}}}
	led := Team{ID: "led", Members: []Member{{Key: "chat"}, {Key: "m"}}, Manager: "chat"}
	closed := Team{ID: "closed", State: TeamClosed, Members: []Member{{Key: "chat"}}, Manager: "chat"}
	plain := Team{ID: "plain", Members: []Member{{Key: "chat"}}}

	cases := []struct {
		name    string
		open    []Team
		want    string
		manager bool
	}{
		{"manages one crew and reports into another", []Team{root, parent, led}, "led", true},
		{"home over a plain membership", []Team{plain, parent}, "parent", false},
		{"first plain membership", []Team{plain, {ID: "later", Members: []Member{{Key: "chat"}}}}, "plain", false},
		{"only the root and a closed crew", []Team{root, closed}, "", false},
	}
	for _, c := range cases {
		got, manager, ok := ChatCrew(c.open, is)
		if got.ID != c.want || manager != c.manager || ok != (c.want != "") {
			t.Errorf("%s: ChatCrew = %q manager=%v ok=%v, want %q manager=%v", c.name, got.ID, manager, ok, c.want, c.manager)
		}
	}
}
