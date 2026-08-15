package settings

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// tokensSeparator is the mark the surface joins action words with.
const tokensSeparator = tokens.GlyphSeparator

// The derivation law at the surface: every row this sheet draws is a row some
// live registry still contains.
//
// The sheet has no list of its own and this is the test that keeps it that way.
// It is the counterpart of internal/config's derivation_test.go — that one asks
// whether a registry row has a reader, this one asks whether a drawn row has a
// registry — and between them a row cannot exist on screen without something
// behind it at both ends.
func TestEveryDrawnRowIsBackedByItsRegistry(t *testing.T) {
	s := newSheet(t)
	if len(s.rows) == 0 {
		t.Fatal("the sheet drew nothing at all")
	}
	groups := map[string]bool{}
	for _, title := range config.SettingCategories {
		groups[title] = true
	}

	for _, r := range s.rows {
		key := r.setting.Key
		if _, ok := s.registry.Row(key); !ok {
			t.Fatalf("row %q is on the sheet and not in the registry", key)
		}
		if !groups[r.group] {
			t.Fatalf("row %q is grouped under %q, which is not a rendered group", key, r.group)
		}
		if r.setting.Kind != config.SettingModel {
			continue
		}
		slot, ok := config.ModelSlotFor(r.setting.Slot)
		if !ok {
			t.Fatalf("model row %q fronts slot %q, which the models table does not have", key, r.setting.Slot)
		}
		if slot.Role != "" && !slot.Role.Valid() {
			t.Fatalf("model row %q binds %q, which is not a role the router has", key, slot.Role)
		}
	}
}

// Every group word the sheet shows has rows under it, and every group with rows
// shows its word. An empty group announcing itself is a heading with nothing to
// head; a group of rows with no word is 15's ambiguity coming back.
func TestGroupWordsAndRowsAgree(t *testing.T) {
	s := newSheet(t)
	counted := map[string]int{}
	for _, r := range s.rows {
		counted[r.group]++
	}
	for _, word := range s.groups {
		if counted[word] == 0 {
			t.Fatalf("group %q announces itself over no rows", word)
		}
		delete(counted, word)
	}
	if len(counted) != 0 {
		t.Fatalf("rows in groups the sheet never announces: %v", counted)
	}
}

// The router routes five roles and the sheet says what every one of them runs
// on. This is the user's own report — "i don't see models for everything" — as
// an assertion: verification and naming had no row at all, so two whole classes
// of call ran on a model the person could neither see nor change.
func TestTheSheetShowsOneRowPerRouterRole(t *testing.T) {
	s := newSheet(t)
	seen := map[store.ModelRole]string{}
	for _, r := range s.rows {
		if r.setting.Kind != config.SettingModel {
			continue
		}
		slot, _ := config.ModelSlotFor(r.setting.Slot)
		if slot.Role == "" {
			continue
		}
		seen[slot.Role] = r.setting.Label
	}
	for _, role := range store.ModelRoles() {
		label, ok := seen[role]
		if !ok {
			t.Fatalf("no row for role %q", role)
		}
		if label == string(role) {
			t.Fatalf("role %q reaches the reader as machinery (14)", role)
		}
	}
	// The two the engine holds no client for read as what they follow rather
	// than as a blank or, worse, as the conversation model.
	for _, role := range []store.ModelRole{store.RoleVerify, store.RoleScribe} {
		slot, ok := config.ModelSlotFor(string(role))
		if !ok || slot.Held {
			t.Fatalf("role %q: slot held=%v ok=%v — the engine gained a client, update the reading",
				role, slot.Held, ok)
		}
		r := s.gotoRow(t, config.ModelSettingKey(string(role)))
		if got := r.setting.Value(); !strings.HasPrefix(got, "follows ") {
			t.Fatalf("role %q reads %q, want what it actually falls back to", role, got)
		}
	}
}

// A receipt is a dim fact beside a value, derived from a live read, and it is
// never drawn while the value beside it is being typed.
func TestReceiptsComeFromTheRegistryAndYieldWhileEditing(t *testing.T) {
	dir := t.TempDir()
	rows := config.NewSettings(config.SettingsOptions{
		ProfileDir:    dir,
		SpentTodayUSD: func() (float64, bool) { return 3.5, true },
		SplitPct:      func() int { return 0 },
	})
	m := New(Options{Registry: rows, Debounce: time.Hour})
	s := &sheet{Model: m, dir: dir}

	r := s.gotoRow(t, config.KeyDailyBudget)
	if got := s.receipt(r); got != "$3.5 today" {
		t.Fatalf("daily budget receipt = %q, want the day's spend", got)
	}
	if !strings.Contains(s.Render(80, 24), "$3.5 today") {
		t.Fatalf("the receipt never reached the frame:\n%s", s.Render(80, 24))
	}

	s.activate(false) // open the editor
	s.stage(r.setting, "40")
	if got := s.receipt(r); got != "" {
		t.Fatalf("receipt %q survived an edit of the value it describes", got)
	}
}

// An uncounted day is not a day of zero spend (10.2.8). With no seam the row
// carries no receipt at all rather than a $0.00 nobody earned.
func TestNoSpendSeamDrawsNoReceipt(t *testing.T) {
	s := newSheet(t)
	r := s.gotoRow(t, config.KeyDailyBudget)
	if got := s.receipt(r); got != "" {
		t.Fatalf("receipt = %q with nothing counting the day", got)
	}
}

// The verb·key grammar, held. Every action word this surface draws puts the
// verb first and the key after it — `close esc`, never `esc close`. The bug
// that named the rule was this sheet's own hint line, so the assertion lives
// here rather than in a doc comment nobody runs.
func TestActionWordsPutTheVerbBeforeTheKey(t *testing.T) {
	keys := []string{"esc", "enter", "space", "ctrl+u", "backspace", "↑↓", "←→"}
	leadsWithAKey := func(text string) bool {
		for _, key := range keys {
			if strings.HasPrefix(text, key+registry.ChipGap) {
				return true
			}
		}
		return false
	}

	s := newSheet(t)
	for _, r := range s.rows {
		for _, chip := range kindChips(r.setting) {
			if chip.Verb == "" {
				t.Fatalf("row %q draws a bare key %q with no verb on it", r.setting.Key, chip.Key)
			}
			if leadsWithAKey(chip.String()) {
				t.Fatalf("row %q draws %q key-first", r.setting.Key, chip.String())
			}
		}
	}

	// Every mode of the hint line, and the action strip under the band.
	modes := []func(){
		func() { s.cancel(); s.setQuery("") },
		func() { s.setQuery("bud") },
		func() { s.setQuery(""); s.gotoRow(t, config.KeyDocumentEngine); s.activate(false) },
		func() { s.cancel(); s.gotoRow(t, config.KeyDailyBudget); s.activate(false) },
	}
	for _, mode := range modes {
		mode()
		for _, line := range strings.Split(s.Render(96, 30), "\n") {
			for _, word := range strings.Split(line, " "+tokensSeparator+" ") {
				if leadsWithAKey(strings.TrimSpace(word)) {
					t.Fatalf("a key leads its own word: %q\nin: %q", strings.TrimSpace(word), line)
				}
			}
		}
	}
}
