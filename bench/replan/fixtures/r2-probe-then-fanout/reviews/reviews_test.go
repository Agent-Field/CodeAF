package reviews

import (
	"testing"

	"bloop/shop/fake"
)

func TestCountReadsTheBody(t *testing.T) {
	server := &fake.Server{Status: 200, Body: "17\n"}
	defer server.Install()()
	got, err := Count("mug-1")
	if err != nil || got != 17 {
		t.Fatalf("Count = %d, %v; want 17", got, err)
	}
	if server.LastURL != "https://reviews.local/count/mug-1" {
		t.Fatalf("asked %q", server.LastURL)
	}
}

func TestCountOfAnUnreviewedProductIsZero(t *testing.T) {
	server := &fake.Server{Status: 404}
	defer server.Install()()
	got, err := Count("nope")
	if err != nil || got != 0 {
		t.Fatalf("Count = %d, %v; want 0 and no error", got, err)
	}
}

func TestCountTriesThreeTimes(t *testing.T) {
	flaky := &fake.Server{Status: 200, Body: "3", FailFirst: 2}
	restore := flaky.Install()
	got, err := Count("cup")
	restore()
	if err != nil || got != 3 || flaky.Calls != 3 {
		t.Fatalf("Count = %d, %v after %d calls; want 3 on the third", got, err, flaky.Calls)
	}
	down := &fake.Server{Status: 200, FailFirst: 99}
	defer down.Install()()
	if _, err := Count("cup"); err == nil || down.Calls != 3 {
		t.Fatalf("a dead service answered %v after %d calls; want an error after 3", err, down.Calls)
	}
}
