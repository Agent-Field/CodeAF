package appwire

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// FeatureOrgBots is the hello word for the organization bot methods.
const FeatureOrgBots = "org_bots"

type OrgBotNewParams struct {
	OrgConn
	api.BotNew
}

type OrgBotParams struct {
	OrgConn
	Bot string `json:"bot"`
}

type OrgBotTokenNewParams struct {
	OrgBotParams
	api.BotTokenNew
}

type OrgBotTokenRevokeParams struct {
	OrgBotParams
	Token string `json:"token"`
}

const orgBotsPath = "/v1/bots"

func (p OrgBotParams) path() (string, error) {
	id, err := orgSegment("bot", p.Bot)
	if err != nil {
		return "", err
	}
	return orgBotsPath + "/" + url.PathEscape(id), nil
}

func withFactoryOrgBotsFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryOrg); ok {
		hello = withFeature(hello, FeatureOrgBots)
	}
	return hello
}

func init() {
	methods["factory.org_bots"] = orgMethod[OrgConn, json.RawMessage]("factory.org_bots", func(OrgConn) (OrgRequest, error) {
		return OrgRequest{Method: http.MethodGet, Path: orgBotsPath}, nil
	})
	methods["factory.org_bot_new"] = orgMethod[OrgBotNewParams, json.RawMessage]("factory.org_bot_new", func(p OrgBotNewParams) (OrgRequest, error) {
		return OrgRequest{Method: http.MethodPost, Path: orgBotsPath, Body: p.BotNew}, nil
	})
	methods["factory.org_bot_token_new"] = orgMethod[OrgBotTokenNewParams, json.RawMessage]("factory.org_bot_token_new", func(p OrgBotTokenNewParams) (OrgRequest, error) {
		path, err := p.path()
		return OrgRequest{Method: http.MethodPost, Path: path + "/tokens", Body: p.BotTokenNew}, err
	})
	methods["factory.org_bot_token_revoke"] = orgMethod[OrgBotTokenRevokeParams, json.RawMessage]("factory.org_bot_token_revoke", func(p OrgBotTokenRevokeParams) (OrgRequest, error) {
		path, err := p.path()
		if err != nil {
			return OrgRequest{}, err
		}
		token, err := orgSegment("token", p.Token)
		if err != nil {
			return OrgRequest{}, err
		}
		return OrgRequest{Method: http.MethodDelete, Path: path + "/tokens/" + url.PathEscape(token)}, nil
	})
	methods["factory.org_bot_remove"] = orgMethod[OrgBotParams, json.RawMessage]("factory.org_bot_remove", func(p OrgBotParams) (OrgRequest, error) {
		path, err := p.path()
		return OrgRequest{Method: http.MethodDelete, Path: path}, err
	})
}
