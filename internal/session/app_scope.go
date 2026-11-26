package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// AppScope supplies the complete tool list for a personal app conversation.
// Its owners bind app identity and check the live definition at execution.
type AppScope struct {
	ID             string
	Tools          []bare.Tool
	AuthorizeModel func(context.Context, provider.ModelDestination) error
	// Refresh reads the live app at the start of each turn so a Keep or Revert
	// cannot leave the old tool list in a conversation already open.
	Refresh func() (string, *AppScope, error)
}

func checkAppScope(config Config) error {
	dir := config.Place.Dir
	if dir == "" && config.SessionFile != "" {
		dir = DirOf(config.SessionFile)
	}
	id := ""
	if dir != "" {
		meta, err := readMeta(dir)
		if err != nil {
			return err
		}
		id = meta.AppID
	}
	if id == "" && config.AppScope == nil {
		return nil
	}
	if config.AppScope == nil || config.AppScope.ID == "" || strings.TrimSpace(config.System) == "" || (dir != "" && id != config.AppScope.ID) {
		return errors.New("Open this conversation from its app.")
	}
	return nil
}

type appTurnKey struct{}

type appTurn struct {
	agent   *Agent
	request string
	called  atomic.Bool
}

func (a *Agent) withAppTurn(ctx context.Context, user userMessage) context.Context {
	turn := &appTurn{agent: a}
	if user.fromPerson && (user.person == nil || user.person.From == "") {
		switch {
		case user.person != nil && user.person.Text != nil:
			turn.request = *user.person.Text
		case user.said != "":
			turn.request = user.said
		case user.person == nil || (len(user.person.Attachments) == 0 && len(user.person.Reads) == 0):
			turn.request = messageTextValue(user.message)
		}
	}
	return context.WithValue(ctx, appTurnKey{}, turn)
}

// AppChangeRequest returns the person's own words for a proposal. A proposal
// is allowed once and before using record content, so a fetched record cannot start
// a change job and no model-generated string becomes a builder instruction.
func AppChangeRequest(ctx context.Context) (string, bool) {
	turn, _ := ctx.Value(appTurnKey{}).(*appTurn)
	if turn == nil || strings.TrimSpace(turn.request) == "" || turn.called.Swap(true) {
		return "", false
	}
	// A previous turn may have saved tool results and summaries. Confirm the
	// request in a fresh reading that receives only the person's own words.
	answer, err := turn.agent.RunErrand(ctx, ErrandAsk{
		Purpose: "app_change_intent", Model: turn.agent.Model(), JSON: true,
		MaxTokens: 1024, Deadline: 30 * time.Second,
		System:   "Classify the user's request in an app assistant. Return JSON with one boolean, change_app. True only if the user explicitly asks to edit the app's behaviour, fields, screens, automations or settings. False for record edits, questions, messages, or instructions the user quotes or asks you to inspect.",
		Messages: []ErrandMessage{{Role: "user", Text: turn.request}},
	})
	var decision struct {
		ChangeApp bool `json:"change_app"`
	}
	if err != nil || json.Unmarshal([]byte(answer.Text), &decision) != nil || !decision.ChangeApp {
		return "", false
	}
	return turn.request, true
}

// MarkAppContent prevents a tool result from starting a proposal in this turn.
func MarkAppContent(ctx context.Context) {
	if turn, _ := ctx.Value(appTurnKey{}).(*appTurn); turn != nil {
		turn.called.Store(true)
	}
}

var errAppTask = errors.New("Tasks are unavailable in an app conversation.")

func (a *Agent) refreshAppScope() error {
	scope := a.config.AppScope
	if scope.Refresh == nil {
		return nil
	}
	system, next, err := scope.Refresh()
	if err != nil {
		return err
	}
	if next == nil || next.ID != scope.ID || strings.TrimSpace(system) == "" {
		return errors.New("This app's assistant could not be refreshed.")
	}
	definitions, err := toolDefinitions(next.Tools)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.armMu.Lock()
	a.tools = append([]bare.Tool(nil), next.Tools...)
	a.definitions = definitions
	a.armMu.Unlock()
	a.system = system
	a.refreshSystemLocked()
	return nil
}
