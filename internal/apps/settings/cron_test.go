package settings

import (
	"testing"
	"time"
)

func TestNextCron(t *testing.T) {
	for _, tt := range []struct{ name, cron, at, want string }{
		{"weekly", "0 9 * * 1", "2026-10-13T12:00:00Z", "2026-10-19T09:00:00Z"},
		{"strictly after", "0 9 * * 1", "2026-10-19T09:00:00Z", "2026-10-26T09:00:00Z"},
		{"steps", "*/15 * * * *", "2026-10-13T12:01:42Z", "2026-10-13T12:15:00Z"},
		{"range and list", "5,20 9-11 * * 1-5", "2026-10-13T09:06:00Z", "2026-10-13T09:20:00Z"},
		{"year rollover", "0 0 1 1 *", "2026-12-31T23:59:59Z", "2027-01-01T00:00:00Z"},
		{"leap day", "0 9 29 2 *", "2026-10-13T12:00:00Z", "2028-02-29T09:00:00Z"},
		{"UTC conversion", "0 0 * * *", "2026-10-13T20:01:00-04:00", "2026-10-15T00:00:00Z"},
		{"Sunday alias", "0 9 * * 7", "2026-10-13T12:00:00Z", "2026-10-18T09:00:00Z"},
		{"range step", "0-30/10 * * * *", "2026-10-13T12:11:00Z", "2026-10-13T12:20:00Z"},
		{"day alternatives", "0 9 15 * 1", "2026-10-13T12:00:00Z", "2026-10-15T09:00:00Z"},
		{"invalid", "bad", "2026-10-13T12:00:00Z", ""},
		{"out of bounds", "60 * * * *", "2026-10-13T12:00:00Z", ""},
		{"reverse range", "0 12-9 * * *", "2026-10-13T12:00:00Z", ""},
		{"zero step", "*/0 * * * *", "2026-10-13T12:00:00Z", ""},
		{"impossible", "0 9 31 2 *", "2026-10-13T12:00:00Z", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			at, err := time.Parse(time.RFC3339, tt.at)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := nextCron(tt.cron, at)
			if tt.want == "" {
				if ok {
					t.Fatalf("next = %s", got)
				}
			} else if !ok || got.Format(time.RFC3339) != tt.want {
				t.Fatalf("next = %s, %v; want %s", got.Format(time.RFC3339), ok, tt.want)
			}
		})
	}
}
