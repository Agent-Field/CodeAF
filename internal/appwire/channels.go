package appwire

import (
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/intake"
)

type ChannelListParams struct {
	App string `json:"app"`
}

type ChannelListResult struct {
	Channels []intake.Channel `json:"channels"`
}

type ChannelIntake interface {
	ListChannels(app string) ([]intake.Channel, error)
	PostChannel(intake.Post) (data.IntakeReceipt, error)
}

func init() {
	methods["channels.list"] = typed(func(e Engine, p ChannelListParams) (*ChannelListResult, error) {
		s, ok := e.(ChannelIntake)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve channels.list")
		}
		app, err := required("app", p.App)
		if err != nil {
			return nil, err
		}
		channels, err := s.ListChannels(app)
		return &ChannelListResult{Channels: nonNil(channels)}, err
	})
	methods["channels.post"] = typed(func(e Engine, p intake.Post) (*data.IntakeReceipt, error) {
		s, ok := e.(ChannelIntake)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve channels.post")
		}
		var err error
		if p.App, err = required("app", p.App); err != nil {
			return nil, err
		}
		if p.Channel, err = required("channel", p.Channel); err != nil {
			return nil, err
		}
		receipt, err := s.PostChannel(p)
		return &receipt, err
	})
}
