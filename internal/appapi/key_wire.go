package appapi

// These wire types reserve the app vocabulary before this resource has behaviour.
type PutKeyRequest struct {
	Key string `json:"key"`
}
type KeysResponse []KeyStatus

func keyContract() resourceContract {
	return resourceContract{Name: "key", Types: []any{PutKeyRequest{}, KeysResponse{}, KeyStatus{}, KeyCheckResult{}}, Routes: keyContractRoutes, Events: []eventContract{{"keys.changed", KeysChangedEvent{}}}}
}
