package appapi

// Handlers are mounted by registerModels and registerSignIn through the same guard.
var modelContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/models", Response: "ModelsResponse"}},
	{RouteContract: RouteContract{Method: "PUT", Path: "/app/v1/models/pinned", Request: "PinnedModelsRequest", Response: "PinnedModelsResponse"}},
	{RouteContract: RouteContract{Method: "PUT", Path: "/app/v1/models/default", Request: "DefaultModelRequest", Response: "DefaultModelResponse"}},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/models/roles", Response: "ModelRolesResponse"}},
	{RouteContract: RouteContract{Method: "PUT", Path: "/app/v1/models/roles/{role}", Request: "ModelRoleRequest", Response: "ModelRole"}},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/providers/{provider}/sign-in", Request: "ProviderSignInRequest", Response: "ProviderSignInResponse"}},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/providers/{provider}/sign-in", Response: "ProviderSignInStatusResponse"}},
	{RouteContract: RouteContract{Method: "DELETE", Path: "/app/v1/providers/{provider}/sign-in", Response: "ActionResponse"}},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/providers/{provider}/sign-in/claim", Request: "ProviderSignInRequest", Response: "ProviderSignInClaimResponse"}},
}
