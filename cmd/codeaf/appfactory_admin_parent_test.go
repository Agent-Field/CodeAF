package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestAppFactoryAdminDoesNotEchoPrivateSessionFromRefusal(t *testing.T) {
	const privateSession = "session_parentPrivacyRegression_a84f1d0c"
	for _, status := range []int{400, 404, 422, 429, 418} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			b, _, conn := adminStub(t, meBody(privateSession, false), func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "refused " + r.URL.Path})
			})
			_, err := admin(b, "sign_out", conn.ID)
			if err == nil {
				t.Fatal("refusal claimed success")
			}
			if strings.Contains(err.Error(), privateSession) || strings.Contains(err.Error(), "/v1/me/sessions/") {
				t.Fatal("private session path escaped in the public error")
			}
		})
	}
}

func TestAppFactoryAdminIdentityRefusalDoesNotForwardPrivateProse(t *testing.T) {
	b, _, conn := adminStub(t, `!404 {"error":"private session_sessionPrivacyFixture_c2a0b7ef"}`, func(w http.ResponseWriter, r *http.Request) {
		t.Error("identity refusal must not send a write")
		w.WriteHeader(500)
	})
	_, err := admin(b, "sign_out", conn.ID)
	if err == nil || strings.Contains(err.Error(), "sessionPrivacyFixture") {
		t.Fatal("identity refusal forwarded private error prose")
	}
}
