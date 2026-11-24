package appwire

import (
	"encoding/json"
	"net/http"
)

// Work page reads are carried unchanged; the server owns membership and the page's task facts.
func init() {
	methods["factory.org_work_home_get"] = orgMethod[OrgConn, json.RawMessage]("factory.org_work_home_get", func(OrgConn) (OrgRequest, error) {
		return OrgRequest{Method: http.MethodGet, Path: "/v1/work"}, nil
	})
	methods["factory.org_workstream_overview_get"] = orgMethod[OrgTasksParams, json.RawMessage]("factory.org_workstream_overview_get", func(p OrgTasksParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.WS)
		return OrgRequest{Method: http.MethodGet, Path: path + "/overview"}, err
	})
}
