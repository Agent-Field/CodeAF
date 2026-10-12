package main

// runRunner joins a computer through the existing attachment door so its windows use the same worker loop.
func runRunner(args []string) error {
	if len(args) == 0 || args[0] != "join" {
		return wrongCall("use codeaf runner join <url> [--name name] [--token session]")
	}
	return runAttach(args[1:])
}
