package config

import "fmt"

// The factory's retention choice: how many days a finished job stays in the
// running view before it is tidied away. It is kept under the same persisted
// key the app's Settings page has always written, so a profile that already
// holds a choice keeps it.
const (
	KeyAppFactoryDoneDays      = "app.factory.done_days"
	AppDefaultFactoryDoneDays  = 1
	appFactoryDoneDaysMostDays = 3650
)

// AppFactoryDoneDaysAt is the retention in days, or [AppDefaultFactoryDoneDays]
// when none is kept or the kept value is out of range. Zero is a real answer
// (tidy at once), so only a negative or absurd number falls back.
func AppFactoryDoneDaysAt(profileDir string) int {
	if n, ok := persistedInt(profileDir, KeyAppFactoryDoneDays); ok && n >= 0 && n <= appFactoryDoneDaysMostDays {
		return n
	}
	return AppDefaultFactoryDoneDays
}

// WriteAppFactoryDoneDays keeps the retention choice. A value outside 0 to 3650
// is refused before anything is written, so the file never holds a number the
// reader would silently ignore.
func WriteAppFactoryDoneDays(profileDir string, days int) error {
	if days < 0 || days > appFactoryDoneDaysMostDays {
		return fmt.Errorf("done days must be between 0 and %d", appFactoryDoneDaysMostDays)
	}
	return writeProfileValues(profileDir, map[string]any{KeyAppFactoryDoneDays: days})
}
