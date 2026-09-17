package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/roles"
)

// ── the vocabulary ──────────────────────────────────────────────────────────

func TestTheSeatVocabularyIsSixWordsCheapestFirstTalkLast(t *testing.T) {
	want := []Seat{SeatReflex, SeatLow, SeatWorker, SeatHigh, SeatMastermind, SeatTalk}
	if len(Seats) != len(want) {
		t.Fatalf("the vocabulary holds %d words, want the six: %v", len(Seats), Seats)
	}
	for i, seat := range want {
		if Seats[i] != seat {
			t.Fatalf("Seats[%d] is %q, want %q — cheapest first, talk last", i, Seats[i], seat)
		}
	}
}

func TestSeatValidComparesExactly(t *testing.T) {
	for _, seat := range Seats {
		if !SeatValid(string(seat)) {
			t.Fatalf("SeatValid(%q) is false, want true", seat)
		}
	}
	for _, word := range []string{"", "Talk", "TALK", " talk", "talk ", "worker\n", "work", "leaf", "mastermind "} {
		if SeatValid(word) {
			t.Fatalf("SeatValid(%q) is true, want false — no trimming, no folding", word)
		}
	}
}

func TestSeatOfTierMapsEveryTierAndNothingElse(t *testing.T) {
	for _, tier := range roles.Tiers {
		seat, ok := SeatOfTier(tier)
		if !ok {
			t.Fatalf("SeatOfTier(%q) is false — a tier of the registry has no seat word", tier)
		}
		if !SeatValid(string(seat)) {
			t.Fatalf("SeatOfTier(%q) wrote %q, which is not one of the six", tier, seat)
		}
	}
	// Talk is the word for a conversation's own turns, and no tier governs
	// those, so no tier answers talk.
	for _, tier := range []roles.Tier{"", "talk", "speech", "execution"} {
		if _, ok := SeatOfTier(tier); ok {
			t.Fatalf("SeatOfTier(%q) answered, want false", tier)
		}
	}
}

func TestSeatOfRoleIsOneHopThroughTheRegistry(t *testing.T) {
	want := map[roles.Role]Seat{
		roles.RoleWorker:   SeatWorker,
		roles.RoleAuditor:  SeatHigh,
		roles.RoleRepair:   SeatHigh,
		roles.RolePlanner:  SeatMastermind,
		roles.RoleTitle:    SeatLow,
		roles.RoleReflex:   SeatReflex,
		roles.RoleHandoff:  SeatMastermind,
		roles.RoleGuardian: SeatLow,
		roles.RoleCaption:  SeatLow,
	}
	for role, seat := range want {
		got, ok := SeatOfRole(role)
		if !ok {
			t.Fatalf("SeatOfRole(%q) is false — a registered role has no seat", role)
		}
		if got != seat {
			t.Fatalf("SeatOfRole(%q) is %q, want %q", role, got, seat)
		}
	}
	// The media roles are declared and deliberately never registered, so they
	// seat nobody: false, and the row carries no word rather than a guess.
	for _, role := range []roles.Role{roles.RoleSpeech, roles.RoleVideo, "hand", "nowhere"} {
		if seat, ok := SeatOfRole(role); ok {
			t.Fatalf("SeatOfRole(%q) answered %q, want false — a role the registry never had", role, seat)
		}
	}
}

func TestSeatOfAgentIsTheTurnsOwnSeat(t *testing.T) {
	want := map[AgentKind]Seat{
		AgentChat:   SeatTalk,
		AgentTask:   SeatWorker,
		AgentAudit:  SeatHigh,
		AgentRepair: SeatHigh,
	}
	for kind, seat := range want {
		got, ok := SeatOfAgent(kind)
		if !ok {
			t.Fatalf("SeatOfAgent(%q) is false, want %q", kind, seat)
		}
		if got != seat {
			t.Fatalf("SeatOfAgent(%q) is %q, want %q", kind, got, seat)
		}
	}
	for _, kind := range []AgentKind{"", "crew", "Talk", "subharness"} {
		if seat, ok := SeatOfAgent(kind); ok {
			t.Fatalf("SeatOfAgent(%q) answered %q, want false — an unknown kind seats nobody", kind, seat)
		}
	}
}

func TestAgentKindReadsTheThreeConfigFacts(t *testing.T) {
	want := []struct {
		name string
		more []func(*Config)
		kind AgentKind
	}{
		{"a conversation", nil, AgentChat},
		{"an errand beside one", []func(*Config){func(config *Config) { config.Errand = true }}, AgentChat},
		{"a task node", []func(*Config){func(config *Config) { config.InTask = true }}, AgentTask},
		{"the checker", []func(*Config){func(config *Config) { config.InTask = true; config.crewRole = roles.RoleAuditor }}, AgentAudit},
		{"a repair round", []func(*Config){func(config *Config) { config.InTask = true; config.repairRound = true }}, AgentRepair},
	}
	for _, want := range want {
		agent, _ := ledgerAgent(t, filepath.Join(t.TempDir(), UsageLedgerName), want.more...)
		if got := agent.agentKind(); got != want.kind {
			t.Fatalf("%s: agentKind() is %q, want %q", want.name, got, want.kind)
		}
	}
}

// ── TagUsage ────────────────────────────────────────────────────────────────

func TestTagUsageIsTheOneDoorForARowsNames(t *testing.T) {
	lines := []struct {
		name string
		line UsageLine
		role roles.Role
		seat Seat
		want func(t *testing.T, got UsageLine)
	}{
		{
			name: "a clean role and the seat that goes with it",
			line: UsageLine{Model: "opus"},
			role: roles.RoleTitle, seat: SeatLow,
			want: func(t *testing.T, got UsageLine) {
				if got.Role != "title" || got.Seat != SeatLow {
					t.Fatalf("the row names %q seated %q, want title/low", got.Role, got.Seat)
				}
			},
		},
		{
			name: "the seat argument wins even against the role's own tier",
			line: UsageLine{Model: "opus"},
			role: roles.RolePlanner, seat: SeatWorker,
			want: func(t *testing.T, got UsageLine) {
				if got.Seat != SeatWorker {
					t.Fatalf("the seat is %q, want worker — the argument wins", got.Seat)
				}
			},
		},
		{
			name: "a word outside the set falls back to the role's own tier",
			line: UsageLine{Model: "opus"},
			role: roles.RolePlanner, seat: Seat("leaf"),
			want: func(t *testing.T, got UsageLine) {
				if got.Seat != SeatMastermind {
					t.Fatalf("the seat is %q, want mastermind — the role's own tier", got.Seat)
				}
			},
		},
		{
			name: "an empty role leaves whatever was there",
			line: UsageLine{Role: "caption", Seat: SeatLow},
			role: "", seat: Seat(""),
			want: func(t *testing.T, got UsageLine) {
				if got.Role != "caption" || got.Seat != SeatLow {
					t.Fatalf("the row names %q seated %q, want caption/low untouched", got.Role, got.Seat)
				}
			},
		},
		{
			name: "an unseatable call keeps the word it already carried",
			line: UsageLine{Role: "hand", Seat: SeatHigh},
			role: "hand", seat: Seat("broad"),
			want: func(t *testing.T, got UsageLine) {
				if got.Role != "hand" || got.Seat != SeatHigh {
					t.Fatalf("the row names %q seated %q, want hand/high kept", got.Role, got.Seat)
				}
			},
		},
		{
			name: "nothing else on the line is touched",
			line: UsageLine{Model: "opus", USD: 0.02, Input: 900, Output: 40, Lane: "Fast", TTFTms: 640},
			role: roles.RoleGuardian, seat: SeatLow,
			want: func(t *testing.T, got UsageLine) {
				if got.Model != "opus" || got.USD != 0.02 || got.Input != 900 || got.Output != 40 || got.Lane != "Fast" || got.TTFTms != 640 {
					t.Fatalf("the row moved: %+v", got)
				}
			},
		},
	}
	for _, want := range lines {
		got := TagUsage(want.line, want.role, want.seat)
		want.want(t, got)
	}
}

// ── the rows through the bank door ──────────────────────────────────────────

// A conversation's own turns answer under talk, the one seat no tier governs.
func TestAConversationTurnsRowNamesItsSeatTalk(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := ledgerAgent(t, ledger)

	var turn Usage
	bankCall(agent, &turn, "deepseek/deepseek-v4-flash", 900, 232, 0.31, laneFacts{})

	row := sealRow(t, ledger)
	wantRowFields(t, row, map[string]any{"seat": "talk"})
}

// A node's turns bill to the seat that does the work, and an errand the node
// runs beside that work bills to the errand's OWN tier — the seat names who
// answered, not who asked.
func TestATaskNodeSeatsItsTurnsWorkerAndItsErrandsByTheirRole(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := ledgerAgent(t, ledger, func(config *Config) { config.InTask = true })

	var turn Usage
	bankCall(agent, &turn, "deepseek/deepseek-v4-flash", 900, 232, 0.31, laneFacts{})
	agent.addAuxiliaryUsageAs(billedCall("haiku-4.5", 300, 12, 0.002), "haiku-4.5", 1, auxRoleHandoff)

	rows := rawUsageRows(t, ledger)
	if len(rows) != 2 {
		t.Fatalf("wrote %d rows, want the node's turn and the handoff errand: %v", len(rows), rows)
	}
	for _, row := range rows {
		if row["role"] == auxRoleHandoff {
			wantRowFields(t, row, map[string]any{"role": "handoff", "seat": "mastermind"})
		} else {
			wantRowFields(t, row, map[string]any{"seat": "worker"})
		}
	}
}

// The checker is the node as far as everybody watching is concerned, and its
// bill is the gate's: high.
func TestTheAuditorsTurnsRowSeatsHigh(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := ledgerAgent(t, ledger, func(config *Config) {
		config.InTask = true
		config.crewRole = roles.RoleAuditor
	})

	var turn Usage
	bankCall(agent, &turn, "deepseek/deepseek-v4-flash", 900, 232, 0.31, laneFacts{})

	wantRowFields(t, sealRow(t, ledger), map[string]any{"seat": "high"})
}

// A repair round's fresh worker escalates onto the careful tier, and its rows
// say so rather than hiding the cascade's bill inside the ordinary work's.
func TestARepairRoundsRowSeatsHigh(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := ledgerAgent(t, ledger, func(config *Config) {
		config.InTask = true
		config.repairRound = true
	})

	var turn Usage
	bankCall(agent, &turn, "deepseek/deepseek-v4-flash", 900, 232, 0.31, laneFacts{})

	wantRowFields(t, sealRow(t, ledger), map[string]any{"seat": "high"})
}

// A named errand carries its registry name AND the tier's word that name sits
// on, whatever agent ran it.
func TestANamedErrandRowCarriesItsRoleAndItsTierSeat(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := ledgerAgent(t, ledger)

	agent.addAuxiliaryUsageAs(billedCall("haiku-4.5", 300, 12, 0.002), "haiku-4.5", 1, auxRoleTitle)

	rows := rawUsageRows(t, ledger)
	if len(rows) != 1 {
		t.Fatalf("wrote %d rows, want the one named errand: %v", len(rows), rows)
	}
	wantRowFields(t, rows[0], map[string]any{"role": "title", "seat": "low"})
}

// An errand whose word the registry never had carries the word and no seat:
// the word is still true, the tier is not.
func TestAnUnregisteredErrandWordCarriesItsRoleButNoSeat(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := ledgerAgent(t, ledger)

	agent.addAuxiliaryUsageAs(billedCall("haiku-4.5", 300, 12, 0.002), "haiku-4.5", 1, auxRoleHand)

	rows := rawUsageRows(t, ledger)
	if len(rows) != 1 {
		t.Fatalf("wrote %d rows, want the one worded errand: %v", len(rows), rows)
	}
	row := rows[0]
	wantRowFields(t, row, map[string]any{"role": "hand"})
	wantRowSilentAbout(t, row, "seat")
}

// An errand that resolved its model outside the registry — a media pin, a
// document reader, a tool ask — names nothing and seats nobody: the row is
// exactly as wide as it was.
func TestAnUnnamedErrandRowStaysSeatless(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := ledgerAgent(t, ledger)

	agent.addAuxiliaryUsage(billedCall("vendor/pin", 10, 900, 0.04), "vendor/pin", 1)

	row := sealRow(t, ledger)
	wantRowSilentAbout(t, row, "role", "seat")
}

// A receipt that arrived after the call names no role of its own, so it seats
// nobody either: the figures are true, the word was never in them.
func TestAReconciledReceiptStaysSeatless(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := ledgerAgent(t, ledger)

	agent.bank(bankedCall{
		used:  Usage{Calls: 1, Input: 100, Output: 10, CostUSD: 0.02},
		model: "deepseek/deepseek-v4-flash", lane: laneFacts{},
		ledger: true, late: true, reconciled: true,
	})

	row := sealRow(t, ledger)
	wantRowFields(t, row, map[string]any{"reconciled": true})
	wantRowSilentAbout(t, row, "role", "seat")
}

// EVERY ROW ALREADY IN THE FILE STILL DECODES: an old row reads back with an
// empty seat and an empty role, which is the truth about it.
func TestAnOldRowStillDecodesWithoutASeat(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	old := `{"at":"2026-01-05T09:00:00Z","day":"2026-01-05","model":"opus","calls":1,"in":10,"out":2,"usd":0.01}`
	if err := os.WriteFile(ledger, []byte(old+"\n"), 0o600); err != nil {
		t.Fatalf("write the old ledger: %v", err)
	}
	lines, err := ReadUsage(ledger, time.Time{})
	if err != nil || len(lines) != 1 {
		t.Fatalf("read %d lines, %v", len(lines), err)
	}
	if lines[0].Seat != "" || lines[0].Role != "" || lines[0].Model != "opus" {
		t.Fatalf("the old row read back as %+v, want seatless and roleless", lines[0])
	}
}

// The field is spelled `seat`, and a row nobody could seat is exactly as wide
// as it was.
func TestTheSeatIsSpelledSeatAndOmittedWhenUnset(t *testing.T) {
	seated, err := json.Marshal(UsageLine{At: time.Now(), Day: "2026-09-17", Model: "m", Calls: 1, Input: 5, USD: 0.01, Session: "abc", Seat: SeatWorker})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(seated), `"seat":"worker"`) {
		t.Fatalf("a seated row carries no seat word: %s", seated)
	}
	wide, err := json.Marshal(UsageLine{At: time.Now(), Day: "2026-09-17", Model: "m", Calls: 1, Input: 5, USD: 0.01, Session: "abc"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(wide), `"seat"`) {
		t.Fatalf("an unseated row grew a seat key: %s", wide)
	}
}
