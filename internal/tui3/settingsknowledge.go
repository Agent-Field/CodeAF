package tui3

import (
	"net/url"
	"strings"
)

// Knowledge compilation uses OpenRouter directly. A local or direct provider's
// catalog cannot describe models that this compiler can actually call.
func (a *app) knowledgeModelChoices() []Model {
	if !a.sources.Empty() {
		service := a.sources.Default()
		address := service.Address
		if address == "" {
			address = service.Source.Address
		}
		endpoint, err := url.Parse(address)
		if !service.HasCredentials() || err != nil || !strings.EqualFold(endpoint.Hostname(), "openrouter.ai") {
			return nil
		}
	}
	return a.modelsForDefault(chatModel)
}
