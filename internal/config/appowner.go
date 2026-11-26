package config

import "strings"

const keyAppOwnerName = "apps.owner_name"

// AppOwnerNameAt returns the name the person supplies for their app drafts.
func AppOwnerNameAt(profileDir string) string {
	name, _ := persistedString(profileDir, keyAppOwnerName)
	return strings.Join(strings.Fields(name), " ")
}
