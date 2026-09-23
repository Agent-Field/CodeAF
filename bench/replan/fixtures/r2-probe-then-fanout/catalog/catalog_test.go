package catalog

import (
	"testing"

	"bloop/shop/fake"
)

func TestTitleReadsTheBody(t *testing.T) {
	server := &fake.Server{Status: 200, Body: "Blue Mug"}
	defer server.Install()()
	got, err := Title("mug-1")
	if err != nil || got != "Blue Mug" {
		t.Fatalf("Title = %q, %v; want Blue Mug", got, err)
	}
	if server.LastURL != "https://catalog.local/items/mug-1/title" {
		t.Fatalf("asked %q", server.LastURL)
	}
}

func TestTitleOfAnUnknownItem(t *testing.T) {
	server := &fake.Server{Status: 404}
	defer server.Install()()
	got, err := Title("nope")
	if err != nil || got != "unknown item" {
		t.Fatalf("Title = %q, %v; want unknown item and no error", got, err)
	}
}

func TestTitleTriesTwice(t *testing.T) {
	flaky := &fake.Server{Status: 200, Body: "Cup", FailFirst: 1}
	restore := flaky.Install()
	got, err := Title("cup")
	restore()
	if err != nil || got != "Cup" || flaky.Calls != 2 {
		t.Fatalf("Title = %q, %v after %d calls; want Cup on the second", got, err, flaky.Calls)
	}
	down := &fake.Server{Status: 200, FailFirst: 99}
	defer down.Install()()
	if _, err := Title("cup"); err == nil || down.Calls != 2 {
		t.Fatalf("a dead service answered %v after %d calls; want an error after 2", err, down.Calls)
	}
}
