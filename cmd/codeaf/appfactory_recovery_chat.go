package main

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

// A person can resume a dismissed decision from its manager chat. Match the
// command as a complete request so status questions and conversational advice
// cannot start another attempt.
func recoveryChatCommand(text string) (words string, ok bool) {
	command, words, _ := strings.Cut(strings.TrimSpace(text), ":")
	command = strings.ToLower(strings.TrimRight(strings.TrimSpace(command), ".!"))
	command = strings.TrimPrefix(command, "please ")
	switch command {
	case "try again", "retry", "resume":
		return strings.TrimSpace(words), true
	default:
		return "", false
	}
}

const recoveryChatExchange = "factory-retry"
const decisionResumeSuffix = ":resume"

func (b *appBackend) tryFactoryRecovery(a *appAgent, text, clientID string, shown *appSending) (uint64, bool, error) {
	words, ok := recoveryChatCommand(text)
	if !ok || !plainFactoryMessage(text, shown) {
		return 0, false, nil
	}
	var transcript, agent string
	var factoryOwned bool
	b.locked(func() { transcript, agent, factoryOwned = a.transcript, a.view.ID, a.view.FactoryOwned })
	owner, marked := readFactoryOwner(transcript)
	if (marked && owner.Step != "") || (!marked && !factoryOwned) {
		return 0, false, nil
	}
	var host *appFactoryHost
	func() {
		b.local.mu.Lock()
		defer b.local.mu.Unlock()
		host = b.local.host
	}()
	if host == nil {
		return 0, false, nil
	}
	var it factory.Item
	var err error
	if marked && owner.Root == chatKey(host.floor.Root()) {
		it, err = host.floor.Get(owner.Job)
	} else {
		// Existing managers made before the ownership sidecar are still bound
		// by the authoritative job's recorded conversation.
		it, err = (seamRunDoor{st: host.floor}).managed(transcript)
	}
	if err != nil || it.Talk == "" || sameFileKey(it.Talk) != sameFileKey(transcript) {
		return 0, false, nil
	}
	if err := b.workAllowed(a); err != nil {
		return 0, true, err
	}
	var requestID string
	resumed := factory.PausedDecision(it)
	if clientID != "" {
		key := factoryExchangeKey(agent, recoveryChatExchange, clientID)
		requestID = fmt.Sprintf("%s:%x", key, sha256.Sum256([]byte(text)))
		kept, err := b.readAppFooters(transcript)
		if err != nil {
			return 0, true, appEngineError(err)
		}
		for _, f := range kept {
			if f.Entry.ID == key+":user" && f.Entry.Text != text {
				return 0, true, appwire.Errorf(appwire.CodeConflict, "This message was already sent with different words.")
			}
			if f.Entry.ID == key+":answer" {
				return b.keepFactoryExchange(a, text, clientID, recoveryChatExchange, f.Entry.Text)
			}
		}
		if strings.HasPrefix(it.LastRecoveryResume, key+":") {
			saved, wasResume := strings.CutSuffix(it.LastRecoveryResume, decisionResumeSuffix)
			if saved != requestID {
				return 0, true, appwire.Errorf(appwire.CodeConflict, "This message was already sent with different words.")
			}
			requestID, resumed = it.LastRecoveryResume, wasResume
		} else if resumed {
			requestID += decisionResumeSuffix
		}
	}
	replay := requestID != "" && it.LastRecoveryResume == requestID
	if !replay && !factory.RecoveryPaused(it) {
		return 0, false, nil
	}
	if utf8.RuneCountInString(words) > api.MaxRejectPlanWords {
		return 0, true, appwire.Errorf(appwire.CodeBadParams, "Retry instructions must be at most %d characters.", api.MaxRejectPlanWords)
	}
	if !replay {
		if requestID == "" {
			requestID = rand.Text()
		}
		desk, err := lease.Open(host.floor, lease.Options{})
		if err != nil {
			return 0, true, appEngineError(err)
		}
		err = lease.NewDoors(desk).RetryPausedRecovery(it.ID, it.Talk, it.PausedRecoveryQuestion, words, requestID)
		if err != nil {
			cur, readErr := host.floor.Get(it.ID)
			if readErr != nil || cur.LastRecoveryResume != requestID {
				return 0, true, appEngineError(err)
			}
		}
	}
	ack := "Retry requested. Check the job for current progress."
	if resumed {
		ack = "Pause lifted. Check the job for current progress."
	}
	turn, _, err := b.keepFactoryExchange(a, text, clientID, recoveryChatExchange, ack)
	if err != nil {
		return turn, true, &appUnsureSend{err: appwire.Errorf(appwire.CodeUncertain, "%s", appUnsureWords)}
	}
	return turn, true, nil
}
