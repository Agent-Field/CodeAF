package mailer

import (
	"testing"

	"bloop/shop/fake"
)

func TestTemplateReadsTheBody(t *testing.T) {
	server := &fake.Server{Status: 200, Body: "Dear {{name}},"}
	defer server.Install()()
	got, err := Template("welcome")
	if err != nil || got != "Dear {{name}}," {
		t.Fatalf("Template = %q, %v", got, err)
	}
	if server.LastURL != "https://templates.local/welcome" {
		t.Fatalf("asked %q", server.LastURL)
	}
}

func TestTemplateFallsBackToTheDefault(t *testing.T) {
	server := &fake.Server{Status: 404}
	defer server.Install()()
	got, err := Template("missing")
	if err != nil || got != DefaultTemplate {
		t.Fatalf("Template = %q, %v; want the default and no error", got, err)
	}
}

func TestTemplateAsksOnlyOnce(t *testing.T) {
	down := &fake.Server{Status: 200, FailFirst: 99}
	defer down.Install()()
	if _, err := Template("welcome"); err == nil || down.Calls != 1 {
		t.Fatalf("a dead service answered %v after %d calls; want an error after 1", err, down.Calls)
	}
}
