package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
)

func TestPhonePairDefaultScopeAndExplicitOverrides(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "defaults", true: "overrides"}[explicit], func(t *testing.T) {
			t.Setenv(home.EnvVar, t.TempDir())
			if err := os.MkdirAll(filepath.Dir(phoneSockPath()), 0700); err != nil {
				t.Fatal(err)
			}
			ln, err := net.Listen("unix", phoneSockPath())
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			received := make(chan appwire.PairClientParams, 1)
			go func() {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				defer c.Close()
				var p appwire.PairClientParams
				_ = json.NewDecoder(c).Decode(&p)
				received <- p
				writePhonePair(c, appwire.ClientGrant{}, nil)
			}()
			input := `{}`
			if explicit {
				input = `{"methods":["asks.answer"],"reads":["asks.list"]}`
			}
			f, err := os.CreateTemp(t.TempDir(), "input")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			_, _ = f.WriteString(input)
			_, _ = f.Seek(0, 0)
			out, err := os.CreateTemp(t.TempDir(), "output")
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			oldIn, oldOut := os.Stdin, os.Stdout
			os.Stdin, os.Stdout = f, out
			defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()
			if err := runPhone([]string{"pair"}); err != nil {
				t.Fatal(err)
			}
			got := <-received
			reads := []string{"agents.entries", "asks.list", "agents.list", "inbox.list", "tasks.list", "spend.today", "crews.list", "crews.get", "crews.items", "crews.log"}
			methods := []string{"agents.create", "agents.send", "agents.stop", "asks.answer", "inbox.answer", "tasks.start", "crews.create", "crews.message", "crews.decide"}
			if explicit {
				reads = []string{"asks.list"}
				methods = []string{"asks.answer"}
			}
			if !slices.Equal(got.Reads, reads) || !slices.Equal(got.Methods, methods) {
				t.Fatalf("scope reads=%v methods=%v; want reads=%v methods=%v", got.Reads, got.Methods, reads, methods)
			}
		})
	}
}
