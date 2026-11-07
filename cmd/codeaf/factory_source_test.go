package main

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestFactorySourceAddsListsAndRevokes(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := serve.New(factory.FixtureSeam(time.Now()), serve.Config{Store: st, AccessToken: "floor-secret"})
	defer h.Close()
	srv := httptest.NewServer(h)
	defer srv.Close()
	at := []string{"--factory", srv.URL, "--token", "floor-secret"}

	var out, said bytes.Buffer
	was := aside
	aside = &said
	defer func() { aside = was }()
	if err := runFactorySource(append([]string{"add", "inbox:ops", "--callback", "https://ops.example.com/hook", "--trust", "collaborator"}, at...), &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.HasPrefix(got, "token: cft_") || !strings.Contains(got, "\nsigning secret: whsec_") || strings.Count(got, "\n") != 2 {
		t.Fatalf("add printed %q", got)
	}
	if !strings.HasPrefix(said.String(), "registered inbox:ops · trust collaborator\nKeep both now") {
		t.Fatalf("add said %q", said.String())
	}
	out.Reset()
	if err := runFactorySource(append([]string{"list"}, at...), &out); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "inbox:ops · trust collaborator · webhooks to https://ops.example.com/hook\n" {
		t.Fatalf("list printed %q", got)
	}
	out.Reset()
	if err := runFactorySource(append([]string{"revoke", "inbox:ops"}, at...), &out); err != nil || !strings.HasPrefix(out.String(), "revoked inbox:ops") {
		t.Fatalf("revoke = %q, %v", out.String(), err)
	}
	if err := runFactorySource(append([]string{"revoke", "inbox:ops"}, at...), &out); err == nil || !strings.Contains(err.Error(), "no source with that name") {
		t.Fatalf("second revoke = %v", err)
	}
	for _, bad := range [][]string{{"add"}, {"add", "Ops"}, {"add", "ops", "--trust", "owner"}, {"rotate", "ops"}} {
		if err := runFactorySource(append(bad, at...), &out); err == nil {
			t.Errorf("%v was taken", bad)
		}
	}
}
