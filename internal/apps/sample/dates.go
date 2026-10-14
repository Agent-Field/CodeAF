package sample

import (
	"math/rand"
	"strings"
	"time"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// Date words, checked past first: "last due" is past, "issue date" is past,
// "due date" is upcoming. A bare "date" or "day" field records what happened.
var (
	pastDateWords   = []string{"last", "previous", "created", "completed", "received", "birth", "dob", "started", "joined", "signed", "sent", "closed", "paid", "opened", "submitted", "added", "since", "touch", "contacted", "issue", "issued", "invoiced", "billed", "logged", "requested", "delivered", "finished", "shipped", "approved", "cancelled", "hired"}
	futureDateWords = []string{"upcoming", "next", "due", "deadline", "start", "starts", "scheduled", "expires", "expiry", "renewal", "renews", "publish", "publishes", "launch", "ends", "end", "until", "follow", "planned", "event", "pickup", "delivery", "records", "reminder"}
	bareDateWords   = []string{"date", "day", "when", "on", "at"}
)

func dateValue(name string, f *spec.Field, i int, rng *rand.Rand, now time.Time) string {
	semantic := name + " " + f.Label
	days := dayOffset(rng, i, f.Unique, 59) - 29
	switch {
	case hasWord(semantic, pastDateWords...):
		days = -1 - dayOffset(rng, i, f.Unique, 29)
	case hasWord(semantic, futureDateWords...):
		days = 1 + dayOffset(rng, i, f.Unique, 29)
	case onlyWords(semantic, bareDateWords):
		days = -dayOffset(rng, i, f.Unique, 29)
	}
	return formatDate(f, now.AddDate(0, 0, days), i)
}

func formatDate(f *spec.Field, at time.Time, i int) string {
	if f.Type == spec.FieldDate {
		return at.Format("2006-01-02")
	}
	// Datetimes fall in working hours on the half hour; subday offsets keep
	// unique datetime fields distinct even in large samples.
	at = at.Truncate(24 * time.Hour).Add(time.Duration(workHour(i))*time.Hour + time.Duration(i%2*30)*time.Minute)
	if f.Unique {
		at = at.Add(time.Duration(i) * time.Second)
	}
	return at.Format(time.RFC3339Nano)
}

func workHour(i int) int { return workdayStart + i*5%workdayHours }

// onlyWords reports whether every word of text is one of words.
func onlyWords(text string, words []string) bool {
	tokens := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) })
	for _, token := range tokens {
		if !hasWord(token, words...) {
			return false
		}
	}
	return len(tokens) > 0
}

func dayOffset(rng *rand.Rand, i int, unique bool, spread int) int {
	if unique {
		return i % spread
	}
	return rng.Intn(spread)
}
