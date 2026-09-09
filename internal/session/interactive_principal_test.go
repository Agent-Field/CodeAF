package session

import "testing"

func TestInteractivePrincipalDoesNotFreezeTheFirstGoal(t *testing.T) {
	for _, interactive := range []bool{false, true} {
		a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
			c.Interactive, c.Unattended, c.Budget = interactive, true, Budget{USD: 1}
		})
		a.hearAsk("write a Markdown report")
		a.hearAsk("make the report CSV instead")
		if interactive {
			if a.steward() != nil || a.who().Ask() != "make the report CSV instead" {
				t.Fatal("interactive chat retained a frozen first goal")
			}
		} else if a.steward() == nil || a.who().Ask() != "write a Markdown report" {
			t.Fatal("fixed headless goal changed")
		}
	}
}
