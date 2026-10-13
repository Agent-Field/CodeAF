package appapi

// The settings lane mounts these handlers only through the guarded server.
var settingContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/settings", Response: "Settings"}},
	{RouteContract: RouteContract{Method: "PATCH", Path: "/app/v1/settings", Request: "PatchSettingsRequest", Response: "Settings"}},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/setup", Response: "SetupResponse"}},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/setup", Request: "SetupRequest", Response: "SetupResponse"}},
}
