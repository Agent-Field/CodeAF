package appwire

import (
	"reflect"
	"strings"

	"github.com/Agent-Field/codeaf/internal/connect"
)

const (
	FeaturePlugins = "plugins"
	// FeaturePluginsGitHub says this engine lists GitHub as a connector and
	// serves plugins.connect_local; an older engine simply does not list it.
	FeaturePluginsGitHub   = "plugins_github"
	EventPluginsChanged    = "plugins.changed"
	EventPluginsNeeded     = "plugins.needed"
	EventPluginsNeededDone = "plugins.needed_done"
)

type PluginsResult struct {
	Plugins       []connect.Plugin                 `json:"plugins"`
	Providers     []PluginProvider                 `json:"providers"`
	Featured      []string                         `json:"featured"`
	FeaturedWords map[string]connect.FeaturedWords `json:"featured_words"`
}

// PluginProvider extends the provider view only on the plugins page.
type PluginProvider struct {
	Provider
	Account     string            `json:"account,omitempty"`
	KeyEnv      string            `json:"key_env,omitempty"`
	ConnectedAt string            `json:"connected_at,omitempty"`
	Running     *bool             `json:"running,omitempty"`
	PlanPaused  *PluginPlanPaused `json:"plan_paused,omitempty"`
}
type PluginPlanPaused struct {
	ResetsAt string `json:"resets_at,omitempty"`
}

type PluginID struct {
	ID string `json:"id"`
}
type PluginBrowserParams struct {
	ID     string `json:"id"`
	Answer string `json:"answer,omitempty"`
}
type PluginBrowserResult struct {
	State string `json:"state" enum:"waiting"`
	Link  string `json:"link"`
	// Code is the one-time code the person types on Link, for a device
	// sign-in (GitHub's). The sheet shows it before anything opens Link.
	Code string `json:"code,omitempty"`
}
type PluginKeyParams struct {
	ID     string `json:"id"`
	Key    string `json:"key,omitempty"`
	KeyEnv string `json:"key_env,omitempty"`
	Answer string `json:"answer,omitempty"`
}
type PluginKeyResult struct {
	Checked bool   `json:"checked"`
	Account string `json:"account,omitempty"`
	Error   string `json:"error,omitempty" enum:"shape,refused,no_answer"`
	Words   string `json:"words,omitempty"`
}
type PluginCapabilityParams struct {
	ID         string                  `json:"id"`
	Capability string                  `json:"capability"`
	State      connect.CapabilityState `json:"state" enum:"yes,ask,off"`
}
type PluginDisconnectParams struct {
	ID      string `json:"id"`
	Account string `json:"account,omitempty"`
}
type Plugins interface {
	ListPlugins() (PluginsResult, error)
	DescribePlugin(string) (connect.PluginDescription, error)
	ConnectPluginBrowser(PluginBrowserParams) (PluginBrowserResult, error)
	CancelPlugin(string) error
	ConnectPluginKey(PluginKeyParams) (PluginKeyResult, error)
	SetPluginCapability(PluginCapabilityParams) error
	DisconnectPlugin(PluginDisconnectParams) error
}

// PluginsLocal is the engine's half of plugins.connect_local: adopt a login
// already on this computer (GitHub: gh's). Its answer is connect_key's shape.
type PluginsLocal interface {
	ConnectPluginLocal(PluginID) (PluginKeyResult, error)
}

type PluginsChanged struct {
	ID    string `json:"id"`
	Error string `json:"error,omitempty"`
}
type PluginsNeeded struct {
	Agent  string `json:"agent"`
	Plugin string `json:"plugin"`
	Why    string `json:"why"`
	AskID  string `json:"ask_id"`
}
type PluginsNeededDone struct {
	AskID string `json:"ask_id"`
}

func pluginMethod[P any, R any](id func(P) string, call func(Plugins, P) (R, error)) method {
	return typed(func(e Engine, p P) (*R, error) {
		plugins, ok := e.(Plugins)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "plugins are unavailable")
		}
		if id != nil {
			if _, err := required("id", id(p)); err != nil {
				return nil, err
			}
		}
		out, err := call(plugins, p)
		return &out, err
	})
}
func init() {
	methods["plugins.list"] = pluginMethod(nil, func(e Plugins, _ struct{}) (PluginsResult, error) { return e.ListPlugins() })
	methods["plugins.describe"] = pluginMethod(func(p PluginID) string { return p.ID }, func(e Plugins, p PluginID) (connect.PluginDescription, error) { return e.DescribePlugin(p.ID) })
	methods["plugins.connect_browser"] = pluginMethod(func(p PluginBrowserParams) string { return p.ID }, func(e Plugins, p PluginBrowserParams) (PluginBrowserResult, error) { return e.ConnectPluginBrowser(p) })
	methods["plugins.cancel"] = pluginMethod(func(p PluginID) string { return p.ID }, func(e Plugins, p PluginID) (Empty, error) { return Empty{}, e.CancelPlugin(p.ID) })
	methods["plugins.connect_key"] = pluginMethod(func(p PluginKeyParams) string { return p.ID }, func(e Plugins, p PluginKeyParams) (PluginKeyResult, error) {
		if strings.TrimSpace(p.Key) != "" && strings.TrimSpace(p.KeyEnv) != "" {
			return PluginKeyResult{}, Errorf(CodeBadParams, "supply key or key_env, not both")
		}
		return e.ConnectPluginKey(p)
	})
	methods["plugins.connect_local"] = typed(func(e Engine, p PluginID) (*PluginKeyResult, error) {
		local, ok := e.(PluginsLocal)
		if _, plugins := e.(Plugins); !ok || !plugins {
			return nil, Errorf(CodeUnknownMethod, "plugins are unavailable")
		}
		if _, err := required("id", p.ID); err != nil {
			return nil, err
		}
		out, err := local.ConnectPluginLocal(p)
		return &out, err
	})
	methods["plugins.set_capability"] = pluginMethod(func(p PluginCapabilityParams) string { return p.ID }, func(e Plugins, p PluginCapabilityParams) (Empty, error) {
		if _, err := required("capability", p.Capability); err != nil {
			return Empty{}, err
		}
		switch p.State {
		case connect.StateYes, connect.StateAsk, connect.StateOff:
		default:
			return Empty{}, Errorf(CodeBadParams, "state is yes, ask or off")
		}
		return Empty{}, e.SetPluginCapability(p)
	})
	methods["plugins.disconnect"] = pluginMethod(func(p PluginDisconnectParams) string { return p.ID }, func(e Plugins, p PluginDisconnectParams) (Empty, error) { return Empty{}, e.DisconnectPlugin(p) })
	eventShapes[EventPluginsChanged] = reflect.TypeFor[PluginsChanged]()
	eventShapes[EventPluginsNeeded] = reflect.TypeFor[PluginsNeeded]()
	eventShapes[EventPluginsNeededDone] = reflect.TypeFor[PluginsNeededDone]()
}
func withPluginsFeature(h Hello, e Engine) Hello {
	if _, ok := e.(Plugins); ok {
		h = withFeature(h, FeaturePlugins)
		if _, local := e.(PluginsLocal); local {
			h = withFeature(h, FeaturePluginsGitHub)
		}
	}
	return h
}
