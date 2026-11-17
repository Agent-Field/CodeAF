package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/redact"
)

func TestAppFactoryRefusalDoesNotEchoRegisteredCredentials(t *testing.T) {
	secret := "factory-bearer-fixture-keep-out"
	redact.Register(secret)
	body, err := json.Marshal(map[string]string{"error": "request refused for " + secret})
	if err != nil {
		t.Fatal(err)
	}
	err = appFactoryFailure(appFactoryReply{status: http.StatusBadRequest, body: body})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("factory refusal must withhold credentials: %v", err)
	}
}
