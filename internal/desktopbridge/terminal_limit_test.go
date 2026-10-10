package desktopbridge

import "testing"

// TestTerminalLimitRefusesTheSeventeenthLiveTerminal is DESIGN-QUESTIONS Q6:
// one conversation holds sixteen live terminals. The seventeenth start answers
// 409 and the limit sentence. Closing one ends it, so it stops counting and
// the next start is accepted.
func TestTerminalLimitRefusesTheSeventeenthLiveTerminal(t *testing.T) {
	if maxTerminals != 16 {
		t.Fatalf("Q6 is 16 live terminals per conversation; maxTerminals is %d", maxTerminals)
	}
	r := newTermRig(t)
	// sleep keeps the slot. A command that exits immediately is not live, and
	// the cap counts only a terminal whose state is still running.
	hold := `{"command":"sleep 120"}`
	ids := make([]string, 0, maxTerminals)
	for range maxTerminals {
		info := r.start(t, hold)
		if info.State != "running" {
			t.Fatalf("start %d: %+v", len(ids)+1, info)
		}
		ids = append(ids, info.ID)
	}

	var refused struct {
		Error string `json:"error"`
	}
	const sentence = "16 terminals are already running; close one first"
	if code := r.do(t, "POST", r.base, hold, &refused); code != 409 || refused.Error != sentence {
		t.Fatalf("17th start: %d %q", code, refused.Error)
	}
	var list []TerminalInfo
	if code := r.do(t, "GET", r.base, "", &list); code != 200 || len(list) != maxTerminals {
		t.Fatalf("refused start changed the list: %d %+v", code, list)
	}

	if code := r.do(t, "POST", r.base+"/"+ids[0]+"/close", `{}`, nil); code != 200 {
		t.Fatalf("close: %d", code)
	}
	freed := r.start(t, hold)
	if freed.State != "running" || freed.ID == "" || freed.ID == ids[0] {
		t.Fatalf("closing one did not free a slot: %+v", freed)
	}
	if code := r.do(t, "POST", r.base, hold, &refused); code != 409 || refused.Error != sentence {
		t.Fatalf("back at the cap: %d %q", code, refused.Error)
	}
}
