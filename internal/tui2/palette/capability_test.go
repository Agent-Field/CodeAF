package palette

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/registry"
)

func capBody(c *Capability, width, height int) []string {
	frame := c.Render(width, height)
	if frame == "" {
		return nil
	}
	lines := strings.Split(frame, "\n")
	if len(lines) <= c.bodyTop {
		return nil
	}
	return lines[c.bodyTop:]
}

// TestCapabilityShowsTheBeltForThisScopeAndNothingElse: `?` answers "what can
// THIS room do", so rooms and settings — which belong to the palette's
// catalog-of-everything — must not appear.
func TestCapabilityShowsTheBeltForThisScopeAndNothingElse(t *testing.T) {
	for _, scope := range []registry.Scope{registry.ScopeThread, registry.ScopeNode, registry.ScopeTalk} {
		cat := demoCatalog()
		cat.Scope = scope
		c := NewCapability(Options{})
		c.SetCatalog(cat)

		if want := len(registry.ForScope(scope)); c.Total() != want {
			t.Errorf("scope %d listed %d verbs, registry has %d", scope, c.Total(), want)
		}
		for i := range c.list.rows {
			r := &c.list.rows[i]
			if r.sec != sectionActions {
				t.Fatalf("scope %d listed a %s row", scope, r.sec.title())
			}
			id := r.result.(RunEntry).ID
			e, ok := registry.ByID(id)
			if !ok || !e.Scope.Has(scope) {
				t.Errorf("scope %d listed out-of-scope entry %q", scope, id)
			}
		}

		// The section headers are the visible half of the same claim: only
		// `actions` may appear. (A verb's own description is prose and may
		// legitimately contain a room's name — "ask aforge" is a real
		// sentence — so the assertion is made on structure, not on substrings.)
		for _, line := range capBody(c, 100, 60) {
			for _, sec := range []section{sectionRooms, sectionHistory, sectionSettings} {
				if strings.HasPrefix(line, sec.title()) {
					t.Errorf("the capability overlay drew a %q section", sec.title())
				}
			}
		}
	}
}

// TestCapabilityNamesTheRoom: the header answers "what can WHICH room do".
func TestCapabilityNamesTheRoom(t *testing.T) {
	c := NewCapability(Options{})
	c.SetCatalog(demoCatalog())
	header := strings.Split(c.Render(90, 20), "\n")[0]
	if !strings.Contains(header, "wisp-parity") {
		t.Errorf("the header does not name the room: %q", header)
	}
	if !strings.Contains(header, escHint) {
		t.Errorf("the header does not advertise the exit: %q", header)
	}

	unnamed := NewCapability(Options{})
	unnamed.SetCatalog(Catalog{Scope: registry.ScopeThread})
	header = strings.Split(unnamed.Render(90, 20), "\n")[0]
	if !strings.Contains(header, titleFallback) {
		t.Errorf("an unnamed room's header says %q", header)
	}
}

// TestCapabilityListsDisabledVerbsWithTheirReason is 5.20 rule 3 verbatim:
// nothing is hidden, and a disabled affordance says why.
func TestCapabilityListsDisabledVerbsWithTheirReason(t *testing.T) {
	const reason = "settled — ask aforge"
	cat := demoCatalog()
	cat.Scope = registry.ScopeNode
	cat.Reason = func(string) string { return reason }

	c := NewCapability(Options{})
	c.SetCatalog(cat)
	if c.Total() != len(registry.ForScope(registry.ScopeNode)) {
		t.Fatalf("a fully disabled room hid verbs: %d of %d", c.Total(), len(registry.ForScope(registry.ScopeNode)))
	}
	body := capBody(c, 100, 80)
	named := 0
	for _, line := range body {
		if strings.Contains(line, reason) {
			named++
		}
	}
	if named != c.Total() {
		t.Errorf("%d of %d disabled verbs carry their reason", named, c.Total())
	}
	if _, ok := c.Selected(); ok {
		t.Error("a disabled verb reported itself as choosable")
	}
}

func TestCapabilityKeys(t *testing.T) {
	rec := &recorder{}
	c := NewCapability(rec.options())
	c.SetCatalog(demoCatalog())
	c.Render(90, 30)

	c.Key(typeRune('j'))
	c.Key(typeRune('j'))
	if c.list.cursor != 2 {
		t.Errorf("j moved the cursor to %d, want 2", c.list.cursor)
	}
	c.Key(typeRune('k'))
	if c.list.cursor != 1 {
		t.Errorf("k moved the cursor to %d, want 1", c.list.cursor)
	}
	c.Key(namedKey(tea.KeyDown))
	if c.list.cursor != 2 {
		t.Errorf("down moved the cursor to %d, want 2", c.list.cursor)
	}
	c.Key(typeRune('g'))
	if c.list.cursor != 0 {
		t.Errorf("g left the cursor at %d", c.list.cursor)
	}
	c.Key(typeRune('G'))
	if c.list.cursor != c.Count()-1 {
		t.Errorf("G left the cursor at %d, want %d", c.list.cursor, c.Count()-1)
	}

	want, ok := c.Selected()
	if !ok {
		t.Fatal("nothing selected")
	}
	c.Key(namedKey(tea.KeyEnter))
	if len(rec.chosen) != 1 || rec.chosen[0] != want {
		t.Errorf("enter yielded %v, want %v", rec.chosen, want)
	}
	if rec.closes != 1 {
		t.Errorf("enter closed %d times, want 1", rec.closes)
	}

	rec.closes = 0
	c.Key(namedKey(tea.KeyEscape))
	if rec.closes != 1 {
		t.Errorf("esc closed %d times, want 1", rec.closes)
	}
	// `?` is the door in; it is also the door out, so the key that opened the
	// surface never becomes a key that does nothing while it is up.
	rec.closes = 0
	c.Key(typeRune('?'))
	if rec.closes != 1 {
		t.Errorf("? closed %d times, want 1", rec.closes)
	}
}

func TestCapabilityEmptyBeltSaysSo(t *testing.T) {
	c := NewCapability(Options{})
	c.SetCatalog(Catalog{Scope: registry.ScopeThread, Actions: []Action{}})
	body := strings.Join(capBody(c, 60, 10), "\n")
	if !strings.Contains(body, emptyBeltText) {
		t.Errorf("an empty belt renders %q", body)
	}
}

func TestCapabilityClickRuns(t *testing.T) {
	rec := &recorder{}
	cat := demoCatalog()
	cat.Actions = []Action{{Entry: mustEntry(t, "slash.help")}}
	c := NewCapability(rec.options())
	c.SetCatalog(cat)
	c.Render(90, 30)

	// bodyTop + 0 is the `actions` header, + 1 is the single verb.
	msg, pt := clickAt(c.bodyTop + 1)
	c.Mouse(msg, pt)
	if len(rec.chosen) != 1 || rec.chosen[0] != Result(RunEntry{ID: "slash.help"}) {
		t.Errorf("clicking the verb yielded %v", rec.chosen)
	}
}
