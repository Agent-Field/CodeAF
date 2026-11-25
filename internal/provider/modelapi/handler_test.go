package modelapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
)

func TestHandlerServesInProcess(t *testing.T) {
	funnel := &script{reply: words("hello", 0.0123)}
	server, api := open(t, modelapi.Config{CompleterFor: funnel.completerFor})
	request := httptest.NewRequest(http.MethodPost, modelapi.ChatURL(api.BaseURL),
		strings.NewReader(`{"model":"stub/scripted","messages":[{"role":"user","content":"hi"}]}`))
	request.Header.Set("Authorization", "Bearer "+api.Token)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	var reply struct {
		Usage struct {
			TotalTokens int     `json:"total_tokens"`
			Cost        float64 `json:"cost"`
		}
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &reply); err != nil ||
		recorder.Code != http.StatusOK || reply.Usage.TotalTokens != 120 || reply.Usage.Cost != 0.0123 {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body)
	}
}
