// This local conformance peer speaks the exact reactor ABI over framed stdio.
// It is not a relay service and has no listener, storage or authentication.
package main

import (
	"encoding/binary"
	"io"
	"os"

	"github.com/Agent-Field/codeaf/internal/raftabi"
)

func main() {
	if len(os.Args) == 5 && os.Args[1] == "measure" {
		if err := measureRows(os.Args[2], os.Args[3], os.Args[4]); err != nil {
			os.Stderr.WriteString(err.Error() + "\n")
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "rows" {
		if err := rowProof(os.Args[2], os.Stdout); err != nil {
			os.Stderr.WriteString(err.Error() + "\n")
			os.Exit(1)
		}
		return
	}
	r := raftabi.New()
	for {
		var n uint32
		if binary.Read(os.Stdin, binary.LittleEndian, &n) != nil {
			return
		}
		if n == 0 || n > raftabi.MaxBuffer {
			os.Exit(2)
		}
		b := make([]byte, n)
		if _, err := io.ReadFull(os.Stdin, b); err != nil {
			os.Exit(2)
		}
		out := r.Call(b)
		if binary.Write(os.Stdout, binary.LittleEndian, uint32(len(out))) != nil {
			os.Exit(2)
		}
		if _, err := os.Stdout.Write(out); err != nil {
			os.Exit(2)
		}
	}
}
