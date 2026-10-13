package appapi

type EmptyRequest struct{}

// serviceAccessValue is shared by account actions and tool servers.
type serviceAccessValue string

const (
	ServiceYes serviceAccessValue = "yes"
	ServiceAsk serviceAccessValue = "ask_first"
	ServiceOff serviceAccessValue = "off"
)

type ServiceAction struct {
	ID          string             `json:"id"`
	Label       string             `json:"label"`
	Scope       string             `json:"scope"`
	Access      serviceAccessValue `json:"access" jsonschema:"enum=yes,enum=ask_first,enum=off"`
	ActingAsYou bool               `json:"acting_as_you"`
}
type ConnectedService struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	State      string             `json:"state" jsonschema:"enum=connected,enum=disconnected,enum=revoked"`
	Detail     string             `json:"detail"`
	Access     serviceAccessValue `json:"access" jsonschema:"enum=yes,enum=ask_first,enum=off"`
	Actions    []ServiceAction    `json:"actions"`
	ConnectURL string             `json:"connect_url"`
}
type AccountsResponse []ConnectedService
type ServiceAccessRequest struct {
	Access serviceAccessValue `json:"access" jsonschema:"enum=yes,enum=ask_first,enum=off"`
}
type ServiceConnectResponse struct {
	ConnectURL string `json:"connect_url"`
}
type McpServer struct {
	ID      string             `json:"id"`
	Name    string             `json:"name"`
	Address string             `json:"address"`
	Tools   int                `json:"tools"`
	State   string             `json:"state" jsonschema:"enum=connected,enum=not_answering"`
	Since   string             `json:"since,omitempty"`
	Access  serviceAccessValue `json:"access" jsonschema:"enum=yes,enum=ask_first,enum=off"`
}
type McpServersResponse []McpServer
type AddMcpServerRequest struct {
	Address string `json:"address"`
}

// Credential is the opaque launch-key payload, returned once after browser completion for the app's Keychain writer.
type AccountConnectResponse struct {
	ConnectedService
	Credential string `json:"credential,omitempty"`
}
