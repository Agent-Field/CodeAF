package main

import (
 "bytes"
 "encoding/json"
 "testing"
)

func TestHealthContract(t *testing.T) {
 var out bytes.Buffer
 if err := run([]string{"health"}, &out); err != nil { t.Fatal(err) }
 var got health
 if err := json.Unmarshal(out.Bytes(), &got); err != nil { t.Fatal(err) }
 if got.Status != "ready" || got.Version == "" || got.Platform == "" { t.Fatalf("invalid health: %+v", got) }
}

func TestRejectUnknownCommand(t *testing.T) {
 var out bytes.Buffer
 if err := run([]string{"launch"}, &out); err == nil { t.Fatal("expected error") }
 if out.Len() != 0 { t.Fatal("invalid command produced protocol output") }
}
