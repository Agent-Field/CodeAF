package appwire

import (
	"context"
	"encoding/json"
)

// FeatureFactoryJobSend makes the manager conversation only when a person sends.
const FeatureFactoryJobSend = "factory.job_send"

type FactoryJobSendParams struct {
	Factory  string `json:"factory"`
	Job      int    `json:"job"`
	Text     string `json:"text"`
	ClientID string `json:"client_id,omitempty"`
}

type FactoryJobSent struct {
	Agent string `json:"agent"`
	Turn  uint64 `json:"turn"`
}

type FactoryJobSender interface {
	FactoryJobSend(context.Context, FactoryJobSendParams) (FactoryJobSent, error)
}

func init() {
	methods[FeatureFactoryJobSend] = shaped[FactoryJobSendParams, FactoryJobSent](func(e Engine, raw json.RawMessage) (*FactoryJobSent, error) {
		sender, ok := e.(FactoryJobSender)
		if _, factory := e.(Factory); !ok || !factory {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", FeatureFactoryJobSend)
		}
		var p FactoryJobSendParams
		if err := decodeStrict(FeatureFactoryJobSend, raw, &p); err != nil {
			return nil, err
		}
		chats := FactoryJobChatsParams{Factory: p.Factory, Job: p.Job}
		if err := prepJobChats(&chats); err != nil {
			return nil, err
		}
		p.Factory = chats.Factory
		if p.Text == "" {
			return nil, Errorf(CodeBadParams, "text is required")
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := sender.FactoryJobSend(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}
