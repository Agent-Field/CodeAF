package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "in")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadJSONLines(t *testing.T) {
	got, err := readParagraphs(write(t, "\n{\"id\":\"a\",\"paragraph\":\"A CRM\",\"family\":\"crm\"}\n\n{\"paragraph\":\"Booking\"}\n"))
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v %v", got, err)
	}
	if got[0] != (para{ID: "a", Paragraph: "A CRM", Family: "crm"}) || got[1].ID != "p02" {
		t.Fatalf("got %+v", got)
	}
}

func TestReadPlainBlocks(t *testing.T) {
	got, err := readParagraphs(write(t, "First app\nover two lines.\n\n\n\nSecond app.\n"))
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v %v", got, err)
	}
	if got[0].ID != "p01" || got[0].Paragraph != "First app over two lines." || got[1].ID != "p02" {
		t.Fatalf("got %+v", got)
	}
}

func TestReadZooPick(t *testing.T) {
	got, err := readParagraphs(write(t, "[clinic-booking] We run a clinic.\n\n[theatre-crew] We stage shows.\n"))
	if err != nil || len(got) != 2 || got[0].ID != "clinic-booking" || got[1].Paragraph != "We stage shows." {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestJobsLineIsNotAParagraph(t *testing.T) {
	got, err := readParagraphs(write(t, "[a] First app.\n\n  jobs: see things; do things\n\n[b] Second.\n"))
	if err != nil || len(got) != 2 || got[0].Jobs != "see things; do things" || got[0].Paragraph != "First app." {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestReadRefusals(t *testing.T) {
	for name, body := range map[string]string{
		"empty":      "  \n\n",
		"bad json":   "{\"id\":",
		"no content": "{\"id\":\"a\"}",
	} {
		if _, err := readParagraphs(write(t, body)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestRawClassifier(t *testing.T) {
	raw := []string{
		"json: cannot unmarshal string into Go value", "unexpected end of JSON input", "bad verb %!d(string=x)",
		"panic: runtime error", "error: screens/x.json invalid", "draft.go:42 failed", "goroutine 7 [running]",
		"Post: context deadline exceeded", "unexpected EOF",
	}
	owner := []string{
		"Reading what you asked for", "Added the Deals screen", "Checking the app", "Your app is ready to preview",
		"Dropped the weekly nudge; it needed a connection you have not set up",
	}
	for _, s := range raw {
		if !looksRaw(s) {
			t.Errorf("should be raw: %q", s)
		}
	}
	for _, s := range owner {
		if looksRaw(s) {
			t.Errorf("should be owner words: %q", s)
		}
	}
	if countRaw(append(raw, owner...)) != len(raw) {
		t.Error("countRaw")
	}
}

func TestRealZooPickShape(t *testing.T) {
	got, err := readParagraphs("testdata/apps.txt")
	if err != nil || len(got) != 2 {
		t.Fatalf("want 2 apps, got %+v %v", got, err)
	}
	if got[0].ID != "coach-packages" || got[0].Jobs == "" || strings.Contains(got[0].Paragraph, "jobs:") {
		t.Fatalf("%+v", got[0])
	}
}
