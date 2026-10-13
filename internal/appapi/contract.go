package appapi

import (
	"net/http"
	"sort"
	"strings"
)

//go:generate go run ./schema -out ../../docs/design/app-v2/schema -typescript ../../docs/design/app-v2/wire.ts

// RouteContract is the method, path and named JSON shape reserved for a resource.
// Empty Request means there is no request body; follow responses are documented separately.
type RouteContract struct {
	Resource string `json:"resource"`
	Method   string `json:"method"`
	Path     string `json:"path"`
	Request  string `json:"request,omitempty"`
	Response string `json:"response"`
}

type contractRoute struct {
	RouteContract
	handle func(*Server, http.ResponseWriter, *http.Request)
	// The owning handler file clears reserved when its implementation lands.
	reserved bool
}
type eventContract struct {
	Name    EventType
	Payload any
}
type resourceContract struct {
	Name   string
	Types  []any
	Routes []contractRoute
	Events []eventContract
}

// Resource tables are assembled here once. Each lane replaces only its own handler file.
func resourceContracts() []resourceContract {
	return []resourceContract{commonContract(), transportContract(), agentContract(), keyContract(), turnContract(), fileContract(), taskContract(), quickTaskContract(), processContract(), orderContract(), askContract(), permissionContract(), inboxContract(), crewContract(), memberContract(), roleContract(), crewWorkContract(), memoryContract(), spendingContract(), modelContract(), settingContract(), searchContract(), aiContract(), factoryContract()}
}

// ContractRoutes includes both existing handlers and the remaining reserved routes.
func ContractRoutes() []RouteContract {
	var result []RouteContract
	for _, resource := range resourceContracts() {
		for _, route := range resource.Routes {
			value := route.RouteContract
			value.Resource = resource.Name
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path+result[i].Method < result[j].Path+result[j].Method })
	return result
}

// ContractTypes groups every public JSON shape by its owning resource.
// The schema generator consumes values rather than maintaining a second type list.
func ContractTypes() map[string][]any {
	result := make(map[string][]any)
	for _, resource := range resourceContracts() {
		result[resource.Name] = append([]any(nil), resource.Types...)
	}
	return result
}

// ContractEvents names payload types for both the main stream and separate follow streams.
func ContractEvents() map[EventType]any {
	result := make(map[EventType]any)
	for _, resource := range resourceContracts() {
		for _, event := range resource.Events {
			result[event.Name] = event.Payload
		}
	}
	return result
}

func (s *Server) registerContractRoutes() {
	grouped := make(map[string][]contractRoute)
	for _, resource := range resourceContracts() {
		for _, route := range resource.Routes {
			if route.handle != nil {
				grouped[route.Path] = append(grouped[route.Path], route)
			}
		}
	}
	for path, routes := range grouped {
		s.mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			var allow []string
			for _, route := range routes {
				if route.Method == r.Method {
					route.handle(s, w, r)
					return
				}
				allow = append(allow, route.Method)
			}
			sort.Strings(allow)
			w.Header().Set("Allow", strings.Join(allow, ", "))
			fail(w, http.StatusMethodNotAllowed, "not_allowed", "this route does not accept that method")
		})
	}
}

func notBuilt(w http.ResponseWriter, r *http.Request) {
	write(w, http.StatusNotImplemented, NotBuiltResponse{Error: "not built yet", Route: r.Method + " " + r.Pattern})
}
