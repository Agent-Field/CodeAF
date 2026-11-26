package main

import (
	"container/list"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/calllog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
)

const hostedAdapterLimit = 64

type hostedAdapterAddress struct {
	base  string
	model string
}

type hostedAdapter struct {
	address   hostedAdapterAddress
	key       string
	completer modelapi.Completer
}

func hostedCompleterFor(profileDir string) func(base, key string) func(model string) modelapi.Completer {
	type profile struct {
		dir string
		err error
	}
	funnelProfile := sync.OnceValue(func() profile {
		root := filepath.Join(profileDir, "hosted-model")
		if err := os.MkdirAll(root, 0o700); err != nil {
			return profile{err: err}
		}
		dir, err := os.MkdirTemp(root, "funnel-")
		if err == nil {
			// The provider call log is process-wide. Point this factory
			// process at the isolated directory before it makes hosted calls.
			calllog.Open(dir)
		}
		return profile{dir: dir, err: err}
	})

	var mu sync.Mutex
	lru := list.New()
	adapters := make(map[hostedAdapterAddress]*list.Element)
	return func(base, key string) func(model string) modelapi.Completer {
		if base == "" {
			base = factoryWorkstreamAgentBaseURL()
		}
		return func(model string) modelapi.Completer {
			profile := funnelProfile()
			if profile.err != nil {
				return refusingCompleter{err: profile.err}
			}
			address := hostedAdapterAddress{base, model}
			mu.Lock()
			defer mu.Unlock()
			found := adapters[address]
			if found != nil {
				cached := found.Value.(hostedAdapter)
				if cached.key == key {
					lru.MoveToFront(found)
					return cached.completer
				}
			}

			settings := config.Config{APIKey: key, BaseURL: base, ProfileDir: profile.dir, DropRefusedEffort: true}
			configured := settings.ClientConfig(model)
			// The key belongs to this adapter alone. Construct it without
			// handing that key to the process-wide model catalog.
			callKey := configured.APIKey
			configured.APIKey = ""
			client, err := provider.NewClient(configured)
			if err != nil {
				return refusingCompleter{err: err}
			}
			if err := client.SetAPIKey(callKey); err != nil {
				return refusingCompleter{err: err}
			}
			adapter := hostedAdapter{address, key, wireCompleter{client: client, wire: configured.Model}}
			if found != nil {
				// Calls and receipts already using the old adapter keep
				// their own key when a different key takes this slot.
				found.Value = adapter
				lru.MoveToFront(found)
			} else {
				adapters[address] = lru.PushFront(adapter)
				if lru.Len() > hostedAdapterLimit {
					old := lru.Back()
					delete(adapters, old.Value.(hostedAdapter).address)
					lru.Remove(old)
				}
			}
			return adapter.completer
		}
	}
}

// hostedP1Settings reads the settings that make hosted chats safe to give to
// other people: the server key that keeps their own keys sealed, the spending
// cap of one chat, the git hosts a chat may reach through this server, and the
// way real model calls are made. It then checks them against what the store
// already holds, so a server started with the wrong key, or with none while
// people's keys are stored, stops with a sentence BEFORE it serves anyone
// instead of failing one person's chat at a time. Nothing here repeats a
// secret in an error.
func hostedP1Settings(hosted *serve.HostedConfig, st *store.Store, get func(string) string) error {
	hosted.ServerKey = strings.TrimSpace(get("CODEAF_SERVER_KEY"))
	if raw := strings.TrimSpace(get("CODEAF_HOSTED_SESSION_CAP_USD")); raw != "" {
		cap, err := strconv.ParseFloat(raw, 64)
		if err != nil || cap <= 0 || cap > 100000 || math.IsNaN(cap) {
			return errors.New("CODEAF_HOSTED_SESSION_CAP_USD must be a number of dollars above zero, such as 5")
		}
		hosted.SessionCapUSD = cap
	}
	for _, word := range strings.Split(get("CODEAF_HOSTED_GIT_HOSTS"), ",") {
		if word = strings.TrimSpace(word); word != "" {
			hosted.GitHosts = append(hosted.GitHosts, word)
		}
	}
	hosted.CompleterFor = hostedCompleterFor(home.Dir())
	return hosted.Check(st)
}
