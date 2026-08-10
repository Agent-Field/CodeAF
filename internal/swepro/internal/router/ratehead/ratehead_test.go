package ratehead

import (
	"math"
	"net/http"
	"testing"
)

// src/router/openrouter-rate-headers.ts has no .test.ts of its own — the byte
// parity gate lives in fixtures_test.go. What is left here is the surface the
// fixtures cannot reach: the Date.now() seam's default and restore, and the
// eight date spellings this port deliberately answers differently from bun.

func TestSetNowMSForTestingRestores(t *testing.T) {
	restore := SetNowMSForTesting(func() float64 { return 1234 })
	value := "Wed, 21 Oct 2015 07:28:00 GMT"
	got := parseRetryAfter(&value)
	if got == nil {
		t.Fatalf("expected a parsed retry-after")
	}
	// (1445412480000 - 1234) / 1000
	if want := 1445412478.766; math.Abs(float64(*got)-want) > 1e-6 {
		t.Errorf("pinned clock: want %v got %v", want, float64(*got))
	}
	restore()
	// The default clock must be live again, not the pinned constant.
	if now := nowMS(); now < 1.7e12 {
		t.Errorf("restored clock looks pinned: %v", now)
	}
}

// TestKnownDateDivergences pins the list in jsdate.go's header comment. bun
// parses each of these into a real date; this port answers Invalid Date. If
// one of them ever starts parsing, the comment is stale and has to be edited
// along with this test.
func TestKnownDateDivergences(t *testing.T) {
	diverging := []string{
		"Wed, 21 Oct 2015 07:28:. GMT",
		"Mar, 21 Oct 2015 GMT",
		"Sun Nov 6 08:49:37",
		"Wed, 21 Oct 2015 07:28:00 GMT,",
		"Wed, 21 Oct 2015 07:28:00 GMT.",
		"Oct/21/2015 GMT",
		"Wed, 21 Oct 2015 07:28:00 gmt gmt",
		"Infinity Oct 2015 GMT",
	}
	for _, input := range diverging {
		t.Run(input, func(t *testing.T) {
			if got := parseJSDateMS(input); !math.IsNaN(got) {
				t.Errorf("expected Invalid Date, got %v", got)
			}
		})
	}
}

func TestHeaderGet(t *testing.T) {
	headers := http.Header{}
	headers.Add("X-RateLimit-Limit", "100")
	headers.Add("X-RateLimit-Limit", "200")

	// Headers.get() is case-insensitive and joins a repeated header with ", ".
	if got := headerGet(headers, "x-ratelimit-limit"); got == nil || *got != "100, 200" {
		t.Errorf("case-insensitive join: got %v", got)
	}
	// A missing header is null, which is what stops parseInteger before it
	// ever trims.
	if got := headerGet(headers, "Retry-After"); got != nil {
		t.Errorf("missing header should be null, got %q", *got)
	}
	if got := headerGet(nil, "X-RateLimit-Limit"); got != nil {
		t.Errorf("nil header set should be null, got %q", *got)
	}
}
