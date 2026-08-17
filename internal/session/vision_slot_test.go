package session

// The looking slot: one model answers every picture question, wherever it is
// asked.
//
// docs/MULTIMODAL.md Decision 8 is what these tests pin: the slot resolver is
// the front door, and the roles ladder is what a surface built before it still
// falls to. blindswap_test.go is the other half of the same decision.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/roles"
)

// withSlot is the knob lane's resolver, scripted: one model per modality.
func withSlot(answers map[string]string) func(*Config) {
	return func(config *Config) {
		config.MediaModel = func(modality string) string { return answers[modality] }
	}
}

// ── (1) who looks ───────────────────────────────────────────────────────────

// The slot wins outright. A pin naming a different model is a rung of the
// resolver's OWN ladder, so consulting it again here would resurrect a model
// the resolver may have passed over for being unable to see.
func TestTheLookingSlotOutranksTheRolePin(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		withSlot(map[string]string{"vision": "vendor/slot-eyes"})(config)
		config.RolesSource = func(key string) (string, bool) {
			if key == roles.PinKey(roles.RoleVision) {
				return "vendor/pinned-eyes", true
			}
			return "", false
		}
	})
	if seer := agent.visionSeer(); seer != "vendor/slot-eyes" {
		t.Fatalf("the seer is %q, want the slot's model", seer)
	}
}

// A resolver that answers "" has said there is no capable model, and that is
// the end of it: the roles ladder is not asked afterwards.
func TestASilentLookingSlotIsNotOverruledByThePin(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		withSlot(map[string]string{})(config)
		config.RolesSource = func(key string) (string, bool) {
			if key == roles.PinKey(roles.RoleVision) {
				return "vendor/pinned-eyes", true
			}
			return "", false
		}
	})
	if seer := agent.visionSeer(); seer != "" {
		t.Fatalf("the seer is %q, want nothing", seer)
	}
}

// And a surface with no resolver at all — a test, a door built before the slot
// existed — still resolves exactly as it did before.
func TestWithNoResolverTheRolesLadderStillAnswers(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.RolesSource = func(key string) (string, bool) {
			if key == roles.PinKey(roles.RoleVision) {
				return "vendor/pinned-eyes", true
			}
			return "", false
		}
	})
	if agent.config.MediaModel != nil {
		t.Fatal("this case is about a nil resolver")
	}
	if seer := agent.visionSeer(); seer != "vendor/pinned-eyes" {
		t.Fatalf("the seer is %q, want the pinned model", seer)
	}
}

// The whole point of one slot: the picture a person attaches to a blind model
// goes to the model the slot names, and the reply says who spoke.
func TestTheVisionFallbackRidesTheSlot(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.SupportsImages = func(model string) bool { return model == "vendor/slot-eyes" }
		withSlot(map[string]string{"vision": "vendor/slot-eyes"})(config)
	})

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	events, err := agent.SubmitImage(ctx, "what is this?", []Image{
		{Path: writeImage(t, workspace, "shot.png", "PHOTOBYTES")},
	})
	if err != nil {
		t.Fatalf("SubmitImage: %v", err)
	}
	collect(t, events)

	if got := completer.model(0); got != "vendor/slot-eyes" {
		t.Fatalf("the look rode %q, want the slot's model", got)
	}
	sent := completer.request(0)
	if len(sent) != 1 {
		t.Fatalf("the seer was sent %d messages, want the one it was asked about", len(sent))
	}
	if urls := imagePartURLs(sent[0]); len(urls) != 1 {
		t.Fatalf("the seer was sent %d pictures, want 1", len(urls))
	}
}

// A slot naming the model that cannot see is a configuration, not a
// capability, and the refusal is the one SubmitImage has always returned.
func TestASlotNamingTheBlindModelIsRefused(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.SupportsImages = func(string) bool { return false }
		withSlot(map[string]string{"vision": "test/model"})(config)
	})

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	_, err := agent.SubmitImage(ctx, "look", []Image{
		{Path: writeImage(t, workspace, "shot.png", "BYTES")},
	})
	if err == nil {
		t.Fatal("a slot naming the blind model was accepted")
	}
	if !strings.Contains(err.Error(), "test/model cannot read images") {
		t.Fatalf("refusal = %q, want the blind refusal", err)
	}
	if completer.requests() != 0 {
		t.Fatalf("the model was called %d times for a refused message", completer.requests())
	}
}
