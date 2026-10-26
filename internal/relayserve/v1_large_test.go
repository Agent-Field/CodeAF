package relayserve_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/relayserve"
)

// R25 preserves archived legal requests at both compatibility size boundaries.
func TestR25ProductionArchivedLargeRequests(t *testing.T) {
	raw, err := os.ReadFile("../raftdirectory/testdata/v1-large-requests.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Now   int64
		Cases []struct {
			Name, Method, URI, Body string
			Headers                 map[string]string
			ReadHeaders             map[string]string `json:"read_headers"`
			Hash                    string            `json:"body_sha256"`
		}
	}
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	store := t.TempDir()
	for pass := 0; pass < 2; pass++ {
		func() {
			svc := relayserve.New(relayserve.Config{Store: store, Now: func() time.Time { return time.UnixMilli(f.Now) }})
			srv := httptest.NewServer(svc.Handler)
			defer srv.Close()
			defer svc.Close()
			for i, one := range f.Cases {
				wantSize := 65537
				if i == 1 {
					wantSize = directory.MaxBody - 1
				}
				if len(one.Body) != wantSize {
					t.Fatalf("archived body %d", len(one.Body))
				}
				hash := sha256.Sum256([]byte(one.Body))
				if hex.EncodeToString(hash[:]) != one.Hash {
					t.Fatal("archived request changed")
				}
				method, uri, body := one.Method, one.URI, one.Body
				headers := one.Headers
				if pass == 1 {
					method, body, headers = "GET", "", one.ReadHeaders
				}

				req, err := http.NewRequest(method, srv.URL+uri, strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				for k, v := range headers {
					req.Header.Set(k, v)
				}
				resp, err := srv.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				result, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				want := 200
				if resp.StatusCode != want {
					t.Fatalf("%s pass %d: status %d: %.200s", one.Name, pass, resp.StatusCode, result)
				}
				{
					var view directory.CellView
					var init directory.CellInit
					if json.Unmarshal(result, &view) != nil || json.Unmarshal([]byte(one.Body), &init) != nil {
						t.Fatal("response decode")
					}
					if view.Cell.Title != init.Title || view.Cell.Head != init.Head || view.Cell.Lease.Fence != 1 || !reflect.DeepEqual(view.Cell.Frames, init.Frames) || !reflect.DeepEqual(view.Cell.Keys, init.Keys) || view.Cell.Size != init.Size || view.Cell.ParentCell != init.ParentCell || view.Cell.OrphanTurns != init.OrphanTurns {
						t.Fatal("large request lost content or changed fence")
					}
				}
			}
		}()
	}
}
