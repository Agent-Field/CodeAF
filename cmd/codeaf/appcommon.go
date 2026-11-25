package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/home"
)

// BindCommonAppType is the person's command to adopt a type already in their
// space. A scoped app authoring turn proposes bindings in its own draft.
func (b *appBackend) BindCommonAppType(p appwire.BindCommonTypeParams) (*appwire.BindCommonTypeResult, error) {
	s, err := b.dataStore("")
	if err != nil {
		return nil, err
	}
	t, err := s.Type(data.Scope{}, p.Type)
	if err != nil {
		return nil, appDataError(err)
	}
	if err := s.BindCommonType(data.CommonTypeKey(p.Common), t.ID); err != nil {
		return nil, appDataError(err)
	}
	return &appwire.BindCommonTypeResult{Common: p.Common, Type: t.Name, TypeID: t.ID}, nil
}

func runAppCommonCommand(root string, args []string) error {
	switch {
	case args[0] == "parts" && len(args) == 1:
		_, err := fmt.Fprintln(os.Stdout, string(schema.KitJSON()))
		return err
	case args[0] == "common-types" && len(args) == 1:
		reply, err := appwire.RunCommonTypeCommand(nil, "common_types", json.RawMessage("{}"))
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(reply)
	case args[0] == "bind-common-type" && len(args) == 2:
		return runAppCommonBind(root, json.RawMessage(args[1]))
	default:
		return fmt.Errorf("usage: codeaf app [--home dir] [parts|common-types|bind-common-type JSON]")
	}
}

func runAppCommonBind(root string, raw json.RawMessage) error {
	root = strings.TrimSpace(root)
	if root == "" {
		root = home.Dir()
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return err
	}
	// Use the engine's home lock so the command cannot race a live writer
	// with an independently cached catalog.
	lock, err := os.OpenFile(filepath.Join(abs, "app.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, true); err != nil {
		if filelock.IsBusy(err) {
			return fmt.Errorf("the desktop engine has this home open; quit it before binding a common type from the command line")
		}
		return err
	}
	backend := &appBackend{data: appDataState{home: abs}}
	defer backend.closeData()
	reply, err := appwire.RunCommonTypeCommand(backend, "bind_common_type", raw)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(reply)
}
