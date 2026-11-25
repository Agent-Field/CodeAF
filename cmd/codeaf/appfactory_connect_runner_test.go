package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// whoGitHubSays is a floor link that names the person only while the app holds
// a GitHub key, as the real one does, and counts how often it was asked.
func whoGitHubSays(asks *atomic.Int32) func(factory.Seam) factory.Seam {
	return func(seam factory.Seam) factory.Seam {
		seam.GitHub = func(context.Context) (factory.GitHubLink, error) {
			defer asks.Add(1)
			if key, _ := config.AppCredentialSource().Key("github"); key == "" {
				return factory.GitHubLink{}, nil
			}
			return factory.GitHubLink{Login: "owner"}, nil
		}
		return seam
	}
}

// waitForAsk reads identity until GitHub has been asked at least once, so the
// factory has heard "nobody" and is waiting before it asks again.
func waitForAsk(t *testing.T, s *localSetup, id string, asks *atomic.Int32) {
	t.Helper()
	eventually(t, "the factory asked GitHub who the person is", func() bool {
		var me appwire.FactoryMe
		s.must("factory.me", map[string]any{"factory": id}, &me)
		return asks.Load() > 0 && me.Handle == ""
	})
	settle()
}

// A PERSON WHO HAS JUST CONNECTED GITHUB IS KNOWN AT ONCE. A Personal factory
// already runs folder jobs without GitHub; connecting ends the cached anonymous
// identity's wait, so factory.me names the person within a read or two.
func TestLocalFactoryKnowsThePersonAsSoonAsSetupConnectsGitHub(t *testing.T) {
	s := newLocalSetup(t)
	var asks atomic.Int32
	s.backend.local.seamHook = whoGitHubSays(&asks)
	st := s.start()
	if !st.Runner {
		t.Fatalf("the Personal runner did not start without GitHub: %+v", st)
	}
	waitForAsk(t, s, st.Factory, &asks)

	if v := s.setup("factory.setup_connect", map[string]any{"source": "token", "token": setupSecret}); !v.GitHub.Connected {
		t.Fatalf("connect: %+v", v)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var me appwire.FactoryMe
		s.must("factory.me", map[string]any{"factory": st.Factory}, &me)
		if me.Handle == "owner" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("5s after connecting, the factory still does not know the person: %+v", me)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A KEY THE APP HANDS OVER LATER ENDS THE WAIT THE SAME WAY: keys.set at launch
// can arrive after the first ask. Another service's key does not disturb it.
func TestLocalFactoryAsksAgainOnlyWhenTheGitHubKeyChanges(t *testing.T) {
	s := newLocalSetup(t)
	var asks atomic.Int32
	s.backend.local.seamHook = whoGitHubSays(&asks)
	st := s.start()
	waitForAsk(t, s, st.Factory, &asks)

	config.AppCredentialSource().Set("openrouter", "not-github")
	before := asks.Load()
	var me appwire.FactoryMe
	s.must("factory.me", map[string]any{"factory": st.Factory}, &me)
	settle()
	if got := asks.Load(); got != before || me.Handle != "" {
		t.Fatalf("another service's key asked GitHub again: asks %d then %d, %+v", before, got, me)
	}

	config.AppCredentialSource().Set("github", setupSecret)
	eventually(t, "the person was known after the GitHub key arrived", func() bool {
		var now appwire.FactoryMe
		s.must("factory.me", map[string]any{"factory": st.Factory}, &now)
		return now.Handle == "owner" && asks.Load() > before
	})

	before = asks.Load()
	s.must("factory.me", map[string]any{"factory": st.Factory}, &me)
	settle()
	if got := asks.Load(); got != before || me.Handle != "owner" {
		t.Fatalf("an unchanged GitHub key disturbed the person: asks %d then %d, %+v", before, got, me)
	}
}
