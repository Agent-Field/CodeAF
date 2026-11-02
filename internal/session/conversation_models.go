package session

import (
	"context"
	"errors"
	"strings"
	"time"
)

// ConversationModels belongs to one conversation. Defaults are copied when it
// opens; changing either slot never changes another conversation or a job.
type ConversationModels struct {
	Main     string `json:"main"`
	Fallback string `json:"fallback"`
}

// ModelSwitch is a durable human-facing fact, never an instruction to a model.
type ModelSwitch struct {
	ID   string    `json:"id"`
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

type conversationModelsKey struct{}

func requestConversationModels(ctx context.Context) (ConversationModels, bool) {
	pair, ok := ctx.Value(conversationModelsKey{}).(ConversationModels)
	return pair, ok
}

func (a *Agent) ConversationModelPair() (ConversationModels, bool) {
	if a == nil {
		return ConversationModels{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conversationModels == nil {
		return ConversationModels{}, false
	}
	return *a.conversationModels, true
}

// SetConversationModels changes future requests only. In particular it never
// calls hearModelLocked, whose terminal behavior may cancel an unproductive stream.
func (a *Agent) SetConversationModels(pair ConversationModels) ([]ModelSwitch, error) {
	return a.setConversationModels(pair, "")
}

// ContinueWithModel applies a receiver's usable model through the conversation's
// existing main/fallback rules and records its specific explanation once.
func (a *Agent) ContinueWithModel(model, notice string) error {
	if pair, ok := a.ConversationModelPair(); ok {
		pair.Main = model
		if pair.Fallback == model {
			pair.Fallback = ""
		}
		_, err := a.setConversationModels(pair, notice)
		return err
	}
	a.SetModel(model)
	if notice != "" {
		record := func() ModelSwitch {
			a.mu.Lock()
			defer a.mu.Unlock()
			a.continuedModelNoticed = model
			return a.recordModelSwitchLocked(notice)
		}()
		a.publishModelSwitch(record)
	}
	return nil
}

// A continued chat can supply the reason for its main-model change. Keeping
// that reason on the same write path avoids recording a second generic notice.
func (a *Agent) setConversationModels(pair ConversationModels, mainNotice string) ([]ModelSwitch, error) {
	if a == nil {
		return nil, errors.New("conversation is unavailable")
	}
	pair.Main, pair.Fallback = strings.TrimSpace(pair.Main), strings.TrimSpace(pair.Fallback)
	if pair.Main == "" {
		return nil, errors.New("choose a main model")
	}
	if pair.Main == pair.Fallback {
		return nil, errors.New("main and fallback models must be different")
	}
	a.modelChanges.Lock()
	defer a.modelChanges.Unlock()
	a.mu.Lock()
	if a.config.OneModel && pair.Fallback != "" {
		// Preserve the hard launch restriction, rather than accepting a fallback
		// that can never be used. The picker can explain this existing constraint.
		a.mu.Unlock()
		return nil, errors.New("this conversation was launched with one-model; a fallback is unavailable")
	}
	hadPair := a.conversationModels != nil
	previous := ConversationModels{Main: a.model}
	if a.conversationModels != nil {
		previous = *a.conversationModels
	}
	active := a.modelIdentity.Active != ""
	a.conversationModels = &pair
	a.model = pair.Main
	if a.clientPool != nil {
		a.clientPool.setSeat(pair.Main)
	}
	if !a.running {
		a.rebindClientLocked(pair.Main)
	}
	if a.config.ContextWindowFor != nil {
		if window := a.config.ContextWindowFor(pair.Main); window > 0 {
			a.contextWindow.Store(int64(window))
		}
	}
	a.noteModelWindow(pair.Main)
	a.scrubBlindImagePartsLocked(pair.Main)
	a.followModelOnThePageLocked()
	suffix := ""
	if active {
		suffix = " Applies to the next request."
	}
	notices := []ModelSwitch{}
	if previous.Main != pair.Main {
		if mainNotice == "" {
			mainNotice = "Main model changed from " + previous.Main + " to " + pair.Main + "." + suffix
		} else {
			a.continuedModelNoticed = pair.Main
		}
		notices = append(notices, a.recordModelSwitchLocked(mainNotice))
	}
	if previous.Fallback != pair.Fallback && (hadPair || previous.Main != pair.Main) {
		from, to := previous.Fallback, pair.Fallback
		if from == "" {
			from = "None"
		}
		if to == "" {
			to = "None"
		}
		notices = append(notices, a.recordModelSwitchLocked("Fallback model changed from "+from+" to "+to+"."+suffix))
	}
	a.mu.Unlock()
	a.noteLaneModel(pair.Main)
	a.publishModelIdentity()
	for _, n := range notices {
		a.publishModelSwitch(n)
	}
	return notices, nil
}

func (a *Agent) recordModelSwitchLocked(text string) ModelSwitch {
	n := ModelSwitch{ID: NewSessionID(), At: time.Now().UTC(), Text: text}
	if a.file != nil {
		a.file.writeLine(sessionEntry{Type: "model_switch", ModelSwitch: &n, Timestamp: n.At.Format(time.RFC3339Nano)})
	}
	return n
}
func (a *Agent) publishModelSwitch(n ModelSwitch) {
	a.mu.Lock()
	hub := a.hub
	a.mu.Unlock()
	if hub != nil {
		hub.send(Event{Kind: EventNotice, Text: n.Text, NoticeKind: "model_switch", ModelSwitch: &n})
	}
}
