package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// faultMessage is what the user reads when aforge could not keep going. It is
// the whole story: what happened, what it cost them (nothing), and where the
// detail lives. A raw goroutine dump over the alt screen says none of that.
const faultMessage = "aforge hit an internal fault and had to stop. Nothing is lost — the graph is durable, and restarting resumes where it left off. Details: %s\n"

// reportFault writes the stack where it is useful and the sentence where it is
// read, and answers with the process exit code.
func reportFault(stderr io.Writer, detail string, stack []byte) int {
	path := chatLogPath()
	writeFaultLog(path, detail, stack)
	fmt.Fprintf(stderr, faultMessage, displayPath(path))
	return 1
}

func chatLogPath() string {
	return filepath.Join(filepath.Dir(defaultChatDB()), "chat.log")
}

// displayPath prefers the ~ form: it is what the user typed to get here and
// what they will type to read the log.
func displayPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	prefix := home + string(os.PathSeparator)
	if strings.HasPrefix(path, prefix) {
		return "~/" + filepath.ToSlash(strings.TrimPrefix(path, prefix))
	}
	return path
}

// writeFaultLog appends the fault to the same file the TUI already sends the
// standard logger to, so one file answers "what happened" for every fault.
// Best-effort: a process that is already dying must not die twice.
func writeFaultLog(path, detail string, stack []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	fmt.Fprintf(file, "%s fatal fault: %s\n%s\n",
		time.Now().Format("2006/01/02 15:04:05"), detail, stack)
}
