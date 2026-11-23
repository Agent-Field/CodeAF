package connect

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Plugin struct {
	ConnectedAt  string         `json:"connected_at,omitempty"`
	KeySavedAt   string         `json:"key_saved_at,omitempty"`
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Kind         string         `json:"kind" enum:"browser,key,server"`
	Category     string         `json:"category"`
	Blurb        string         `json:"blurb"`
	Connected    bool           `json:"connected"`
	Account      string         `json:"account,omitempty"`
	KeyEnv       string         `json:"key_env,omitempty"`
	KeyCheckable bool           `json:"key_checkable"`
	Expired      bool           `json:"expired,omitempty"`
	Ask          *PluginAsk     `json:"ask,omitempty"`
	KeyHint      *PluginKeyHint `json:"key_hint,omitempty"`
}
type PluginAsk struct {
	Label   string   `json:"label"`
	Answers []string `json:"answers,omitempty"`
}
type PluginKeyHint struct {
	Words string `json:"words"`
	URL   string `json:"url,omitempty"`
}
type PluginCapability struct {
	ID     string          `json:"id"`
	Phrase string          `json:"phrase"`
	Covers string          `json:"covers"`
	Acts   bool            `json:"acts"`
	State  CapabilityState `json:"state" enum:"yes,ask,off"`
	Tools  []string        `json:"tools"`
}
type PluginTool struct {
	Name     string `json:"name"`
	Title    string `json:"title"`
	ReadOnly bool   `json:"read_only"`
}
type PluginDescription struct {
	Capabilities []PluginCapability `json:"capabilities"`
	ScopesWords  []string           `json:"scopes_words"`
	Tools        []PluginTool       `json:"tools,omitempty"`
	Notes        string             `json:"notes,omitempty"`
	Accounts     []string           `json:"accounts,omitempty"`
	KeyNote      string             `json:"key_note,omitempty"`
}

// PluginBlurb shortens the catalog's own words without a second row catalog.
func PluginBlurb(s Service) string {
	if _, tail, ok := strings.Cut(s.Blurb, "'s own tools — "); ok {
		words, _, _ := strings.Cut(tail, " — ")
		r, n := utf8.DecodeRuneInString(words)
		return string(unicode.ToUpper(r)) + words[n:]
	}
	if s.Auth == AuthKey {
		line := "Your account"
		if _, tail, ok := strings.Cut(s.Blurb, " account at "); ok {
			host, _, _ := strings.Cut(tail, ", with a key")
			line += " at " + host
		}
		return line + " · with your key"
	}
	return s.Blurb
}

func (m *Manager) Plugins() []Plugin {
	out := []Plugin{}
	for _, s := range m.Services() {
		kind := s.Auth
		if m.MCPService(s.ID) {
			kind = "server"
		}
		p := Plugin{ID: s.ID, Name: s.Name, Kind: kind, Category: s.Category, Blurb: PluginBlurb(s.Service), Connected: s.Connected, Account: s.Account, KeyEnv: s.KeyEnv}
		if plug, err := m.plug(s.ID); err == nil {
			if keyed, ok := plug.(*keyPlug); ok {
				p.KeyCheckable = keyed.probe.address != ""
			}
		}
		if s.Blank != "" {
			p.Ask = &PluginAsk{Label: s.Blank, Answers: s.Answers}
		} else if s.KeyAsk != "" {
			if plug, err := m.plug(s.ID); err == nil {
				if keyed, ok := plug.(*keyPlug); ok && keyed.blank.label != "" {
					p.Ask = &PluginAsk{Label: keyed.blank.label}
				}
			}
		}
		if s.KeyHint != "" {
			p.KeyHint = &PluginKeyHint{Words: "Where's my key?", URL: s.KeyHint}
		}
		if record, ok, _ := m.store.get(s.ID); ok {
			p.ConnectedAt, p.KeySavedAt = record.ConnectedAt, record.KeySavedAt
			if record.Keys != nil && !record.Keys.Valid() && record.Keys.RefreshToken == "" {
				p.Expired = true
				p.Connected = false
			}
		}
		out = append(out, p)
	}
	return out
}

func (m *Manager) DescribePlugin(ctx context.Context, id string) (PluginDescription, error) {
	plug, err := m.plug(id)
	if err != nil {
		return PluginDescription{}, err
	}
	s := plug.Service()
	d := PluginDescription{Capabilities: []PluginCapability{}, ScopesWords: []string{}}
	for _, scope := range s.Scopes {
		switch scope {
		case "https://www.googleapis.com/auth/gmail.modify":
			d.ScopesWords = append(d.ScopesWords, "Read, change and send your Gmail")
		case "https://www.googleapis.com/auth/calendar.events":
			d.ScopesWords = append(d.ScopesWords, "See and edit your calendar events")
		default:
			d.ScopesWords = append(d.ScopesWords, scope)
		}
	}
	if _, tail, ok := strings.Cut(s.Blurb, "signed in in your browser."); ok {
		d.Notes = strings.TrimSpace(tail)
	}
	toolCaps := map[string][]string{}
	capabilityMu.RLock()
	for tool, capability := range capabilityRegistry[normalize(id)].tools {
		toolCaps[capability] = append(toolCaps[capability], tool)
	}
	capabilityMu.RUnlock()
	if m.MCPService(id) && m.Connected(id) {
		tools, err := m.MCPTools(ctx, id)
		if err != nil {
			return d, err
		}
		for _, tool := range tools {
			d.Tools = append(d.Tools, PluginTool{Name: tool.Name, Title: tool.Title, ReadOnly: tool.ReadOnly})
			cap := MCPToolCapability(tool)
			toolCaps[cap] = append(toolCaps[cap], tool.Name)
		}
	}
	for _, cap := range m.Capabilities(id) {
		names := toolCaps[cap.ID]
		if names == nil {
			names = []string{}
		}
		sort.Strings(names)
		d.Capabilities = append(d.Capabilities, PluginCapability{ID: cap.ID, Phrase: cap.Phrase, Covers: strings.Join(names, " · "), Acts: cap.Acts, State: m.CapabilityState(id, cap.ID), Tools: names})
	}
	if record, ok, _ := m.store.get(s.ID); ok {
		if record.Account != "" {
			d.Accounts = []string{record.Account}
		}
		if record.KeyEnv != "" {
			d.KeyNote = fmt.Sprintf("Read from $%s on this computer · codeaf never shows the key itself", record.KeyEnv)
		}
	}
	return d, nil
}
