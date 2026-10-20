package appwire

import "time"

// ConversationPoint is one authored turn, newest first. Point is a message boundary.
type ConversationPoint struct {
	Point      int       `json:"point"`
	At         time.Time `json:"at"`
	Words      string    `json:"words"`
	TurnsAfter int       `json:"turns_after"`
}

type ConversationPoints struct {
	Points []ConversationPoint `json:"points"`
}

type RewindToken struct {
	Token string `json:"token"`
}
type ConversationDebug struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// ConversationActions is the shared conversation command door, including host forwarding.
type ConversationActions interface {
	ConversationPoints(string) ([]ConversationPoint, error)
	RewindConversation(string, int) (string, error)
	UndoConversationRewind(string, string) error
	DeleteAgent(string) error
	DebugAgent(string) (ConversationDebug, error)
	ManualAgent(string, string, string) (uint64, error)
}

func conversationActions(e Engine) (ConversationActions, error) {
	c, ok := e.(ConversationActions)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve conversation actions")
	}
	return c, nil
}

func withConversationFeatures(h Hello, e Engine) Hello {
	if _, ok := e.(ConversationActions); ok {
		for _, name := range []string{"agents.rewind.points", "agents.rewind", "agents.rewind.undo", "agents.delete", "agents.debug", "agents.manual"} {
			h = withFeature(h, name)
		}
	}
	return h
}

func init() {
	methods["agents.rewind.points"] = typed(func(e Engine, p IDParams) (*ConversationPoints, error) {
		if err := needID("agents.rewind.points", p.ID); err != nil {
			return nil, err
		}
		c, err := conversationActions(e)
		if err != nil {
			return nil, err
		}
		rows, err := c.ConversationPoints(p.ID)
		return &ConversationPoints{Points: rows}, err
	})
	methods["agents.rewind"] = typed(func(e Engine, p struct {
		ID    string `json:"id"`
		Point *int   `json:"point"`
	}) (*RewindToken, error) {
		if err := needID("agents.rewind", p.ID); err != nil {
			return nil, err
		}
		if p.Point == nil || *p.Point < 1 {
			return nil, Errorf(CodeBadParams, "agents.rewind needs a positive point")
		}
		c, err := conversationActions(e)
		if err != nil {
			return nil, err
		}
		token, err := c.RewindConversation(p.ID, *p.Point)
		if err != nil {
			return nil, err
		}
		return &RewindToken{token}, nil
	})
	methods["agents.rewind.undo"] = typed(func(e Engine, p struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}) (*Empty, error) {
		if err := needID("agents.rewind.undo", p.ID); err != nil {
			return nil, err
		}
		if len(p.Token) == 0 || len(p.Token) > 128 {
			return nil, Errorf(CodeBadParams, "agents.rewind.undo needs a token of at most 128 bytes")
		}
		c, err := conversationActions(e)
		if err != nil {
			return nil, err
		}
		return done(c.UndoConversationRewind(p.ID, p.Token))
	})
	methods["agents.delete"] = typed(func(e Engine, p IDParams) (*Empty, error) {
		if err := needID("agents.delete", p.ID); err != nil {
			return nil, err
		}
		c, err := conversationActions(e)
		if err != nil {
			return nil, err
		}
		return done(c.DeleteAgent(p.ID))
	})
	methods["agents.debug"] = typed(func(e Engine, p IDParams) (*ConversationDebug, error) {
		if err := needID("agents.debug", p.ID); err != nil {
			return nil, err
		}
		c, err := conversationActions(e)
		if err != nil {
			return nil, err
		}
		out, err := c.DebugAgent(p.ID)
		return &out, err
	})
	methods["agents.manual"] = typed(func(e Engine, p struct {
		ID       string `json:"id"`
		Question string `json:"question"`
		ClientID string `json:"client_id"`
	}) (*TurnResult, error) {
		if err := needID("agents.manual", p.ID); err != nil {
			return nil, err
		}
		if len(p.Question) > 32<<10 || len(p.ClientID) > 128 {
			return nil, Errorf(CodeBadParams, "question exceeds 32768 bytes or client_id exceeds 128 bytes")
		}
		c, err := conversationActions(e)
		if err != nil {
			return nil, err
		}
		turn, err := c.ManualAgent(p.ID, p.Question, p.ClientID)
		if err != nil {
			return nil, err
		}
		return &TurnResult{Turn: turn}, nil
	})
}
