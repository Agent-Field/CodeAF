// Command pack writes the archive a packed corpus ships as.
//
// It is run by the //go:generate line beside every packed folder and by
// `make build`, never by hand, and it writes only when the bytes change so an
// unchanged corpus leaves the working tree alone. See internal/packed for the
// format and for why the corpora are packed at all.
//
// Usage: pack -o <archive> <folder>
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/Agent-Field/aforge-v2/internal/packed"
)

func main() {
	out := flag.String("o", "", "archive to write")
	flag.Parse()
	if *out == "" || flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: pack -o <archive> <folder>")
		os.Exit(2)
	}
	archive, err := packed.Pack(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "pack:", err)
		os.Exit(1)
	}
	if existing, err := os.ReadFile(*out); err == nil && bytes.Equal(existing, archive) {
		return
	}
	if err := os.WriteFile(*out, archive, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "pack:", err)
		os.Exit(1)
	}
}
