package appapi

var keyContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/keys", Response: "KeysResponse"}},
	{RouteContract: RouteContract{Method: "PUT", Path: "/app/v1/keys/{provider}", Request: "PutKeyRequest", Response: "KeyCheckResult"}},
	{RouteContract: RouteContract{Method: "DELETE", Path: "/app/v1/keys/{provider}", Request: "ExpectRequest", Response: "ActionResponse"}},
}
