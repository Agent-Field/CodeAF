package appwire

import (
	"reflect"
	"strings"

	"github.com/Agent-Field/codeaf/internal/connect"
)

const (
	FeaturePlugins         = "plugins"
	EventPluginsChanged    = "plugins.changed"
	EventPluginsNeeded     = "plugins.needed"
	EventPluginsNeededDone = "plugins.needed_done"
)

type PluginsResult struct {
	Plugins       []connect.Plugin                 `json:"plugins"`
	Providers     []Provider                       `json:"providers"`
	Featured      []string                         `json:"featured"`
	FeaturedWords map[string]connect.FeaturedWords `json:"featured_words"`
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
}
type PluginKeyParams struct {
	ID     string `json:"id"`
	Key    string `json:"key,omitempty"`
	KeyEnv string `json:"key_env,omitempty"`
	Answer string `json:"answer,omitempty"`
}
type PluginKeyResult struct {
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
		return withFeature(h, FeaturePlugins)
	}
	return h
}
