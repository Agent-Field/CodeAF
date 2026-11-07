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

func TestFactoryWebhookAddsListsAndRemoves(t *testing.T) {
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

	if err := runFactoryWebhook(append([]string{"add", "inbox:ops", "https://ops.example.com/hook"}, at...), &out); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.HasPrefix(got, "secret: whsec_") || strings.Count(got, "\n") != 1 {
		t.Fatalf("add printed %q", got)
	}
	if !strings.Contains(said.String(), "inbox:ops's jobs are now told to https://ops.example.com/hook") {
		t.Fatalf("add said %q", said.String())
	}
	out.Reset()
	if err := runFactoryWebhook(append([]string{"list"}, at...), &out); err != nil || out.String() != "inbox:ops · https://ops.example.com/hook\n" {
		t.Fatalf("list = %q, %v", out.String(), err)
	}
	out.Reset()
	if err := runFactoryWebhook(append([]string{"remove", "inbox:ops"}, at...), &out); err != nil || out.String() != "removed the webhook of inbox:ops\n" {
		t.Fatalf("remove = %q, %v", out.String(), err)
	}
	if err := runFactoryWebhook(append([]string{"remove", "inbox:ops"}, at...), &out); err == nil || !strings.Contains(err.Error(), "no webhook is set for that source") {
		t.Fatalf("second remove = %v", err)
	}
	for _, bad := range [][]string{{"add", "ops"}, {"add", "Ops", "https://x"}, {"add", "github", "https://x"}, {"add", "ops", "ftp://x"}, {"rotate", "ops"}} {
		if err := runFactoryWebhook(append(bad, at...), &out); err == nil {
			t.Errorf("%v was taken", bad)
		}
	}
}
