package session

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec"
)

// pageNow is message[0] as the next request will carry it.
func pageNow(agent *Agent) string {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	return messageContentText(agent.messages[0])
}

// THE `Assisted-by` LINE NAMES THE MODEL THE CONVERSATION IS TALKING TO NOW.
//
// The line was filled once, from the model the conversation was launched on,
// and `/model` never rendered the page again — so every commit after a switch
// credited a model that had not written it. The switch now re-renders the page,
// and so does the clock's own refresh later, from the live model.
//
// AND THE SWITCH COSTS THE CACHE NOTHING IT WAS GOING TO KEEP. A prompt cache
// belongs to one model, so the new model's first request is cold whatever the
// page says; what must hold is that the model's name is the only thing that
// moved, so switching back hands the old model the page it already has cached.
func TestAssistedByFollowsTheModelAfterASwitch(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.System = ""
		config.Attribution = true
	})
	launched := fmt.Sprintf(exec.AttributionAssistedBy, "test/model")
	switched := fmt.Sprintf(exec.AttributionAssistedBy, "other/model")

	first := pageNow(agent)
	if !strings.Contains(first, launched) {
		t.Fatalf("the launch page does not name the launch model in %q", launched)
	}

	agent.SetModel("other/model")
	page := pageNow(agent)
	if !strings.Contains(page, switched) || strings.Contains(page, launched) {
		t.Fatalf("after /model the page still credits the launch model; want %q", switched)
	}
	if want := strings.ReplaceAll(first, "test/model", "other/model"); page != want {
		t.Fatal("the switch moved more of the page than the model's name, so the old model's cached prefix cannot come back")
	}

	// AND THE CLOCK'S OWN RE-RENDER, LATER, KEEPS THE LIVE MODEL.
	agent.mu.Lock()
	agent.rerenderSystemLocked(time.Now().Add(time.Hour))
	agent.mu.Unlock()
	if page := pageNow(agent); !strings.Contains(page, switched) {
		t.Fatalf("a clock refresh after /model went back to the launch model; want %q", switched)
	}
}

// SWITCHING BACK IS THE PAGE THE OLD MODEL ALREADY HAS, and a page that does not
// name the model is not touched by a switch at all.
func TestASwitchBackRestoresThePageByteForByte(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.System = ""
		config.Attribution = true
	})
	first := pageNow(agent)
	agent.SetModel("other/model")
	agent.SetModel("test/model")
	if pageNow(agent) != first {
		t.Fatal("switching back did not restore the page the launch model already had")
	}

	unsigned, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.System = ""
	})
	before := pageNow(unsigned)
	unsigned.SetModel("other/model")
	if pageNow(unsigned) != before {
		t.Fatal("a page that names no model was re-rendered by a switch")
	}
}
