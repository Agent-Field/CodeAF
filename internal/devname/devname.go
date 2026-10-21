// Package devname is what a person calls this computer.
//
// THE DEFAULT IS THE HOST NAME AND A CHOICE IS ONE SMALL FILE. A person who
// never names the computer has no file and keeps seeing the word their operating
// system already uses; a person who does name it writes one line, and that line
// is the name on every screen of every other device from then on. The name is a
// label and never an identity: nothing is authorized by it, two devices may
// share one (the device list tells them apart by a short id), and renaming
// changes no key.
//
// ONE SOURCE OF TRUTH. The sealed record in the relay's directory, the label
// shown when a device is let in, and "typing from <name>" on a chat window all
// read [Name] here, so they cannot disagree about what this computer is called.
package devname

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Most is the longest name, counted in characters a person sees.
const Most = 32

// fileName is the one line a chosen name is kept in, under the codeaf home.
const fileName = "device-name"

// The reasons a typed name is refused, each a sentence a person can act on.
var (
	ErrEmpty   = errors.New("a device name needs at least one visible character")
	ErrTooLong = errors.New("a device name is at most 32 characters")
	ErrControl = errors.New("a device name cannot hold control characters or line breaks")
)

// Clean is the name a person typed, trimmed, or the reason it cannot be a name.
//
// A BAD NAME IS REFUSED RATHER THAN REPAIRED. The person is looking at the
// name they typed; silently cutting or stripping it would show them a different
// one on the other computers and leave them to wonder why.
func Clean(typed string) (string, error) {
	name := strings.TrimSpace(typed)
	switch {
	case strings.IndexFunc(name, unicode.IsControl) >= 0:
		return "", ErrControl
	case name == "":
		return "", ErrEmpty
	case utf8.RuneCountInString(name) > Most:
		return "", ErrTooLong
	}
	return name, nil
}

// Default is the host name with any domain cut off, so a list of two laptops
// reads `spark` and not `spark.local.example.com`. A machine that cannot say its
// host name is "a device".
func Default() string {
	host, err := os.Hostname()
	if dot := strings.IndexByte(host, '.'); dot > 0 {
		host = host[:dot]
	}
	if name, bad := Clean(host); err == nil && bad == nil {
		return name
	}
	return "a device"
}

// Name is what the codeaf home at dir calls this computer: the name a person
// chose, else the host name. A file that no longer holds a valid name is a name
// that was never chosen.
func Name(dir string) string {
	if chosen, ok := Chosen(dir); ok {
		return Shown(chosen)
	}
	return Default()
}

// mdnsSuffix is what macOS adds to a host name on the local network.
const mdnsSuffix = ".local"

// Shown is a device name as every screen writes it: without the ".local" a Mac
// adds to its host name. A device record sealed by an older build holds the raw
// host name, and the other devices read that record, so the cut is made where a
// name is read as well as where it is made, and the banner of a moved chat can
// never disagree with the device list about what a computer is called.
func Shown(name string) string {
	if len(name) > len(mdnsSuffix) && strings.EqualFold(name[len(name)-len(mdnsSuffix):], mdnsSuffix) {
		return name[:len(name)-len(mdnsSuffix)]
	}
	return name
}

// Chosen is the name a person gave this computer, and false when they never did.
func Chosen(dir string) (string, bool) {
	raw, err := os.ReadFile(path(dir))
	if err != nil {
		return "", false
	}
	name, err := Clean(string(raw))
	return name, err == nil
}

// Set keeps typed as this computer's name and returns the cleaned name. Nothing
// is written for a name that is refused.
func Set(dir, typed string) (string, error) {
	name, err := Clean(typed)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	// Through a temporary file so a crash never leaves half a name.
	temporary := path(dir) + ".new"
	if err := os.WriteFile(temporary, []byte(name+"\n"), 0o600); err != nil {
		return "", err
	}
	return name, os.Rename(temporary, path(dir))
}

func path(dir string) string { return filepath.Join(dir, fileName) }
