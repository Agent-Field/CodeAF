// codeaf-notices builds the third-party notice carried beside every release.
//
// It is a developer's binary, like cmd/codeaf-changes and
// cmd/codeaf-demo-home, and never a verb on codeaf, so it costs the product
// nothing on the SIZE-BUDGET.
//
// Usage:
//
//	codeaf-notices generate [-o PATH]
//	codeaf-notices check [-o PATH]
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

const defaultNoticesPath = "THIRD-PARTY-NOTICES.md"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "codeaf-notices:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return fmt.Errorf("choose generate or check")
	}
	command := args[0]
	if command != "generate" && command != "check" {
		usage()
		return fmt.Errorf("unknown command %q", command)
	}

	flags := flag.NewFlagSet("codeaf-notices "+command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	output := flags.String("o", "", "the notices file to write or check")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("%s takes flags but no arguments", command)
	}

	root, err := findRepositoryRoot(".")
	if err != nil {
		return err
	}
	path := *output
	if path == "" {
		path = filepath.Join(root, defaultNoticesPath)
	}
	components, err := collectComponents(root, generatorGoEnvironment())
	if err != nil {
		return err
	}
	want := renderNotices(components)

	if command == "generate" {
		if err := os.WriteFile(path, want, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		fmt.Printf("%s: %d third-party components.\n", path, len(components))
		return nil
	}

	got, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if bytes.Equal(got, want) {
		fmt.Printf("%s is current.\n", path)
		return nil
	}
	printDifference(path, got, want)
	return fmt.Errorf("%s would change; run `go run ./cmd/codeaf-notices generate`", path)
}

func usage() {
	fmt.Fprint(os.Stderr, `codeaf-notices — build the notice carried with release binaries

  generate [-o PATH]    write the notices file
  check    [-o PATH]    show whether regeneration would change it

The default PATH is THIRD-PARTY-NOTICES.md at the repository root.
`)
}

// printDifference names the first changed line and shows both spellings. The
// full notice can be large because licence texts are deliberately repeated, so
// a concise answer is more useful here than printing whole displaced sections.
func printDifference(path string, got, want []byte) {
	gotLines := bytes.Split(got, []byte("\n"))
	wantLines := bytes.Split(want, []byte("\n"))
	line := 0
	for line < len(gotLines) && line < len(wantLines) && bytes.Equal(gotLines[line], wantLines[line]) {
		line++
	}
	fmt.Printf("--- %s\n+++ regenerated\n@@ first change at line %d @@\n", path, line+1)
	if line < len(gotLines) {
		fmt.Printf("-%s\n", gotLines[line])
	} else {
		fmt.Println("-<end of file>")
	}
	if line < len(wantLines) {
		fmt.Printf("+%s\n", wantLines[line])
	} else {
		fmt.Println("+<end of file>")
	}
}
