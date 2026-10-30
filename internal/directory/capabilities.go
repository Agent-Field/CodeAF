package directory

import (
	"context"
	"net/http"
)

// Capability is a promise a sync service makes in a response header, and the
// one feature that waits while a service does not make it. Every other feature
// works on a service that lacks it.
type Capability struct {
	Header  string
	Feature string
}

// Capabilities is what this build of codeaf needs a sync service to promise.
// The release gate (codeaf-release service) refuses to ship a build whose
// service does not answer every one, and the client says which feature waits.
var Capabilities = []Capability{
	{Header: ConditionalTakeHeader, Feature: conditionalTakeFeature},
}

// conditionalTakeFeature is what waits on a service without conditional takes:
// displacing a live holder. A lapsed chat is taken on every service.
const conditionalTakeFeature = "continuing a chat that another computer is using right now"

// missingFrom answers the capabilities a response did not promise.
func missingFrom(h http.Header) []Capability {
	var missing []Capability
	for _, c := range Capabilities {
		if h.Get(c.Header) != "1" {
			missing = append(missing, c)
		}
	}
	return missing
}

// Missing reads chat id and answers the capabilities the service did not
// promise on that read. A chat's own read carries every promise, because the
// promises concern what may be done to a chat.
func (h *HTTP) Missing(ctx context.Context, id string) ([]Capability, error) {
	var v CellView
	hdr, err := h.doHeader(ctx, http.MethodGet, cellPath(id, ""), nil, &v)
	if err != nil {
		return nil, err
	}
	return missingFrom(hdr), nil
}
