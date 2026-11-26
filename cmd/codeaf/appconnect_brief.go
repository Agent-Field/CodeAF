package main

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/connections"
	"github.com/Agent-Field/codeaf/internal/apps/data"
)

//go:embed samples/daily-brief
var dailyBriefFiles embed.FS

func installDailyBrief(b *appBackend, fixture bool, out io.Writer) error {
	defs := b.AppDefinitions()
	draft, err := defs.NewDraft("")
	if err != nil {
		return err
	}
	defer defs.Discard(draft)
	files, err := fs.Sub(dailyBriefFiles, "samples/daily-brief")
	if err != nil {
		return err
	}
	if err := os.CopyFS(draft.Dir(), files); err != nil {
		return err
	}
	app, err := defs.Promote(draft, "Install Daily brief")
	if err != nil {
		return err
	}
	store, err := b.dataStore("")
	if err != nil {
		return err
	}
	scope := data.Scope{App: app.ID}
	for alias, key := range map[string]data.CommonTypeKey{"message": data.CommonMessage, "meeting": data.CommonEvent} {
		typ, err := store.Type(scope, alias)
		if err != nil {
			return err
		}
		if err := store.BindCommonType(key, typ.ID); err != nil {
			return err
		}
	}
	id := "brief-" + strings.ToLower(app.ID)
	err = b.appConnections().Register(connections.Config{
		ID: id, App: app.ID, Kind: connections.Google, CredentialMode: connections.CredentialPerson, GoogleFixture: fixture,
		Reads: map[string]connections.Read{
			"messages": {
				Type: "message", Table: "gmail", Key: "id", Version: "version", GoogleIndex: true, GoogleQuery: "in:inbox newer_than:1d",
				Fields: map[string]string{"subject": "subject", "sender": "from", "snippet": "snippet", "received": "date"},
			},
			"meetings": {
				Type: "meeting", Table: "calendar", Key: "id", Version: "version",
				Fields: map[string]string{"title": "title", "starts": "start", "ends": "end", "attendees": "attendees"},
			},
		},
	}, "")
	if err != nil {
		return err
	}
	fmt.Fprintln(out, app.ID)
	return nil
}
