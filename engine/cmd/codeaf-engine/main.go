package main

import (
 "encoding/json"
 "fmt"
 "io"
 "os"
 "runtime"
)

type health struct {
 Status string `json:"status"`
 Version string `json:"version"`
 Platform string `json:"platform"`
}

func run(args []string, out io.Writer) error {
 if len(args) != 1 || args[0] != "health" {
  return fmt.Errorf("usage: codeaf-engine health")
 }
 return json.NewEncoder(out).Encode(health{"ready", "0.1.0", runtime.GOOS + "/" + runtime.GOARCH})
}

func main() {
 if err := run(os.Args[1:], os.Stdout); err != nil {
  fmt.Fprintln(os.Stderr, err)
  os.Exit(1)
 }
}
