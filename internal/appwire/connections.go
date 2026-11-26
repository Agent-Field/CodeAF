package appwire

import "github.com/Agent-Field/codeaf/internal/apps/connections"

type ConnectionScope struct {
	App string `json:"app"`
}

type ConnectionTest struct {
	App        string `json:"app"`
	Connection string `json:"connection"`
}

type ConnectionList struct {
	Connections []connections.Summary `json:"connections"`
}

type ConnectionTestResult struct {
	OK bool `json:"ok"`
}

type ConnectionReader interface {
	ListAppConnections(ConnectionScope) (ConnectionList, error)
	TestAppConnection(ConnectionTest) (ConnectionTestResult, error)
	ReadAppConnection(connections.Query) (connections.Page, error)
}

func connectionMethod[P, R any](fn func(ConnectionReader, P) (R, error)) method {
	return typed(func(e Engine, p P) (*R, error) {
		reader, ok := e.(ConnectionReader)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve app connections")
		}
		result, err := fn(reader, p)
		return &result, err
	})
}

func init() {
	methods["connections.list"] = connectionMethod(ConnectionReader.ListAppConnections)
	methods["connections.test"] = connectionMethod(ConnectionReader.TestAppConnection)
	methods["connections.read"] = connectionMethod(ConnectionReader.ReadAppConnection)
}
