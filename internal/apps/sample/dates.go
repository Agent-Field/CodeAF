package sample

import (
	"math/rand"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func dateValue(name string, f *spec.Field, i int, rng *rand.Rand, now time.Time) string {
	days := dayOffset(rng, i, f.Unique, 59) - 29
	if hasWord(name+" "+f.Label, "upcoming", "next", "due", "deadline", "start", "starts", "scheduled", "expires", "expiry", "renewal", "renews", "publish", "publishes", "launch", "ends", "end", "until", "follow", "planned", "event") {
		days = 1 + dayOffset(rng, i, f.Unique, 29)
	}
	if hasWord(name+" "+f.Label, "last", "previous", "created", "completed", "received", "birth", "dob", "started", "joined", "signed", "sent", "closed", "paid", "opened", "submitted", "added", "since", "touch", "contacted") {
		days = -1 - dayOffset(rng, i, f.Unique, 29)
	}
	at := now.AddDate(0, 0, days)
	if f.Type == spec.FieldDate {
		return at.Format("2006-01-02")
	}
	// Subday offsets keep unique datetime fields distinct even in large samples.
	return at.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano)
}

func dayOffset(rng *rand.Rand, i int, unique bool, spread int) int {
	if unique {
		return i % spread
	}
	return rng.Intn(spread)
}
