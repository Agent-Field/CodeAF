package clusterctl

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

type l4NamesBackend struct{}

func (l4NamesBackend) Status(context.Context, string) (Status, error) {
	return Status{Self: 4, Members: []cs.MemberLabel{{Node: 4, Device: "dev_laptop"}}}, nil
}
func (l4NamesBackend) Lead(context.Context, cs.Command) (cs.LeadershipReceipt, error) {
	return cs.LeadershipReceipt{}, nil
}

// L4-M14a: optional name mappings never break an older client's strict status decoder.
func TestL4ClusterNamesAreNegotiated(t *testing.T) {
	handler := Handler(func(*http.Request, []byte) (string, string, error) { return "id", "dev", nil }, func(string) (Backend, error) { return l4NamesBackend{}, nil })
	server := httptest.NewServer(handler)
	defer server.Close()
	response, err := http.Get(server.URL + Path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"members"`) {
		t.Fatalf("legacy reply contains optional fields: %s", raw)
	}
	client := Client{Base: server.URL, Sign: func(*http.Request, []byte) {}}
	status, err := client.Status(context.Background())
	if err != nil || len(status.Members) != 1 || status.Members[0].Device != "dev_laptop" {
		t.Fatalf("%+v %v", status, err)
	}
}
