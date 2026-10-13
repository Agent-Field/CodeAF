package appapi

var transportContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/health", Response: "HealthResponse"}},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/shutdown", Request: "ShutdownRequest", Response: "ActionResponse"}},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/events", Response: "StreamHelloEvent"}},
}
