package directory_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// A service missing only folders must fail the release probe with that promise.
func TestMissingNamesChatFolders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(directory.ConditionalTakeHeader, "1")
		_ = json.NewEncoder(w).Encode(directory.CellView{})
	}))
	defer srv.Close()
	client := directory.NewHTTP(srv.URL, fakeSign("id_one", devA), srv.Client())
	missing, err := client.Missing(t.Context(), cellX)
	if err != nil || len(missing) != 1 || missing[0].Header != directory.ChatFoldersHeader {
		t.Fatalf("missing = %v, %v; want only chat folders", missing, err)
	}
}
