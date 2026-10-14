package config

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/credits"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// The persisted row, the settings label and the request ladder must agree.
// In particular, a model switch must move an unpinned helper without a reload.
func TestReflexFollowsTheLiveConversationUnlessWritten(t *testing.T) {
	const tier = ModelTierReflex
	for _, state := range []string{"unset", "written", "cleared", "auto"} {
		t.Run(tier+"/"+state, func(t *testing.T) {
			dir := crewProfile(t)
			if state != "unset" {
				value := map[string]string{"written": "vendor/pinned:high", "cleared": "", "auto": "auto"}[state]
				if err := writeProfileValue(dir, tierKeyFor(tier), value); err != nil {
					t.Fatal(err)
				}
			}
			seat := TierSeatAt(dir, tier)
			wantSource := SeatConversation
			wantValue := "follows the conversation"
			if state == "written" {
				wantSource, wantValue = SeatPinned, "vendor/pinned:high"
			}
			if seat.Source != wantSource {
				t.Fatalf("seat = %+v, want source %s", seat, wantSource)
			}
			row := mustRow(t, registry(t, dir), tierKeyFor(tier))
			if row.Value() != wantValue {
				t.Fatalf("settings = %q, want %q", row.Value(), wantValue)
			}
			source := roles.Source(func(key string) (string, bool) {
				value := TierModelAt(dir, tier)
				return value, key == roles.TierKey(roles.Tier(tier)) && value != ""
			})
			role := roles.RoleReflex
			for _, chat := range []string{"vendor/chat", "vendor/changed-chat", FreeChatModel} {
				call, err := roles.ResolveCall(source, role, chat)
				if err != nil {
					t.Fatal(err)
				}
				want := chat
				if state == "written" {
					want = "vendor/pinned"
				}
				if call.Model != want {
					t.Fatalf("call = %+v, want model %s", call, want)
				}
			}
		})
	}
}

// Low credit changes an unwritten reflex only beside a paid chat. Clearing
// remains an explicit answer, and a written model stays chosen in both cases.
func TestLowCreditReflexDefaultRespectsTheLiveChatAndWrittenRows(t *testing.T) {
	original := CrewCatalog
	CrewCatalog = func() []catalog.Model {
		return []catalog.Model{{ID: "vendor/zero-price"}, {ID: "vendor/unknown-price", PriceUnknown: true}}
	}
	t.Cleanup(func() { CrewCatalog = original })
	dir := creditsProfile(t)
	if err := WriteAPIKey(dir, "key"); err != nil {
		t.Fatal(err)
	}
	if err := WriteCreditsReading(dir, "key", credits.Reading{Known: true, Low: true}); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"unset", "written", "cleared"} {
		if state != "unset" {
			value := ""
			if state == "written" {
				value = "vendor/pinned"
			}
			if err := writeProfileValue(dir, KeyTierReflexModel, value); err != nil {
				t.Fatal(err)
			}
		}
		for _, chat := range []string{"vendor/paid", FreeChatModel, FreeChatModel + ":high", "vendor/zero-price", "vendor/unknown-price"} {
			want := ""
			if state == "written" {
				want = "vendor/pinned"
			}
			if state == "unset" && (chat == "vendor/paid" || chat == "vendor/unknown-price") {
				want = freeHelperModels[ModelTierReflex]
			}
			if got := TierModelAt(dir, ModelTierReflex, chat); got != want {
				t.Fatalf("%s beside %s = %q, want %q", state, chat, got, want)
			}
		}
	}
}
