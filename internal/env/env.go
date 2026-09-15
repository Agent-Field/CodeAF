// Package env is the one door for environment variables owned by codeaf.
//
// Get and Lookup are the STATIC door: their names are constants known by the
// caller and a foreign name is a programming error. Value and LookupValue are
// the DYNAMIC door: settings tables and key references may contain either a
// codeaf name or a provider/system name, so foreign names pass through exactly
// as os records them. Both doors give a CODEAF_ name the one-release fallback
// to its AFORGE_ spelling when the new spelling is unset or empty. // legacy-name
package env

import (
	"os"
	"strings"
)

const (
	prefix       = "CODEAF_"
	legacyPrefix = "AFORGE_" // legacy-name
)

// Get reads a statically known codeaf variable with its legacy fallback.
func Get(name string) string {
	value, _ := Lookup(name)
	return value
}

// Lookup is Get with the presence bit of the spelling that supplied the value.
func Lookup(name string) (string, bool) {
	requireOwned(name)
	return lookupOwned(name)
}

// Value reads a runtime-supplied name. Foreign variables are read plainly.
func Value(name string) string {
	value, _ := LookupValue(name)
	return value
}

// LookupValue is Value with os.LookupEnv's presence bit.
func LookupValue(name string) (string, bool) {
	if !strings.HasPrefix(name, prefix) {
		return os.LookupEnv(name)
	}
	return lookupOwned(name)
}

// EnvironWithout returns the process environment without the named variables.
// Owned names remove both spellings; foreign names remove only themselves.
func EnvironWithout(names ...string) []string {
	removed := make(map[string]bool, len(names)*2)
	for _, name := range names {
		removed[name] = true
		if strings.HasPrefix(name, prefix) {
			removed[Legacy(name)] = true
		}
	}
	kept := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !removed[name] {
			kept = append(kept, entry)
		}
	}
	return kept
}

// Legacy returns the former spelling of an owned variable.
func Legacy(name string) string {
	requireOwned(name)
	return legacyPrefix + strings.TrimPrefix(name, prefix)
}

func lookupOwned(name string) (string, bool) {
	value, newSet := os.LookupEnv(name)
	if newSet && value != "" {
		return value, true
	}
	if legacy, legacySet := os.LookupEnv(Legacy(name)); legacySet {
		return legacy, true
	}
	return "", newSet
}

func requireOwned(name string) {
	if !strings.HasPrefix(name, prefix) {
		panic("env: static name must start with " + prefix + ": " + name)
	}
}
