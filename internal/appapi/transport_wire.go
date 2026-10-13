package appapi

// These wire types reserve the app vocabulary before this resource has behaviour.
type HealthResponse struct {
	Status        string `json:"status" jsonschema:"enum=ready"`
	CodeafVersion string `json:"codeaf_version"`
	Contract      int    `json:"contract"`
	Epoch         string `json:"epoch"`
}
type ShutdownRequest struct {
	Reason string `json:"reason,omitempty"`
}
type StreamHelloEvent struct {
	Epoch         string `json:"epoch"`
	CodeafVersion string `json:"codeaf_version"`
	Contract      int    `json:"contract"`
}
type StreamResetEvent struct {
	Epoch  string `json:"epoch"`
	Reason string `json:"reason" jsonschema:"enum=unknown,enum=expired,enum=epoch_changed"`
}

func transportContract() resourceContract {
	return resourceContract{Name: "transport", Types: []any{HealthResponse{}, ShutdownRequest{}, StreamHelloEvent{}, StreamResetEvent{}, Hello{}}, Routes: transportContractRoutes, Events: []eventContract{{"hello", StreamHelloEvent{}}, {"reset", StreamResetEvent{}}}}
}
