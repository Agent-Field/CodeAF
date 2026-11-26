package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/Agent-Field/codeaf/internal/apps/connections"
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

func (b *appBackend) appConnections() connections.Service {
	return connections.Service{
		Profile: b.crewProfile(),
		Google:  b.pluginManager,
		Store:   func() (*data.Store, error) { return b.dataStore("") },
	}
}

func (b *appBackend) ListAppConnections(p appwire.ConnectionScope) (appwire.ConnectionList, error) {
	if p.App == "" {
		return appwire.ConnectionList{}, appwire.Errorf(appwire.CodeBadParams, "an app is required")
	}
	items, err := b.appConnections().List(p.App)
	return appwire.ConnectionList{Connections: items}, appConnectionError(err)
}

func (b *appBackend) TestAppConnection(p appwire.ConnectionTest) (appwire.ConnectionTestResult, error) {
	err := b.appConnections().Test(context.Background(), p.App, p.Connection)
	return appwire.ConnectionTestResult{OK: err == nil}, appConnectionError(err)
}

func (b *appBackend) ReadAppConnection(p connections.Query) (connections.Page, error) {
	page, err := b.appConnections().Read(context.Background(), p)
	return page, appConnectionError(err)
}

func appConnectionError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, connections.ErrTooLarge):
		return appwire.Errorf(appwire.CodeTooLarge, "%s", err)
	case errors.Is(err, connections.ErrConflict):
		return appwire.Errorf(appwire.CodeConflict, "%s", err)
	case errors.Is(err, connections.ErrSourcePermission), errors.Is(err, connections.ErrPersonCredential):
		return appwire.Errorf(appwire.CodeNotAllowed, "%s", err)
	case errors.Is(err, connections.ErrUnavailable):
		return appwire.Errorf(appwire.CodeUnreachable, "%s", err)
	}
	return appDataError(err)
}

func appConnectionsCommand(b *appBackend, args []string, in io.Reader, out io.Writer) error {
	const usage = "usage: codeaf app connections list APP | test APP CONNECTION | read APP CONNECTION READ [JSON_ARGS] | content APP CONNECTION READ ID | register CONFIG.json | credential APP CONNECTION | bind APP TYPE COMMON | daily-brief [--fixture] | google-binding APP CONNECTION live|fixture | link-google-identities APP CONNECTION READ"
	if len(args) == 0 {
		return errors.New(usage)
	}
	encode := func(value any, err error) error {
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(value)
	}
	switch args[0] {
	case "daily-brief":
		if len(args) == 1 || len(args) == 2 && args[1] == "--fixture" {
			return installDailyBrief(b, len(args) == 2, out)
		}
	case "link-google-identities":
		if len(args) == 4 {
			return b.appConnections().LinkGoogleIdentities(context.Background(), connections.Query{
				App: args[1], Connection: args[2], Read: args[3],
			})
		}
	case "google-binding":
		if len(args) == 4 && (args[3] == "fixture" || args[3] == "live") {
			return b.appConnections().SetGoogleFixture(args[1], args[2], args[3] == "fixture")
		}
	case "list":
		if len(args) == 2 {
			return encode(b.ListAppConnections(appwire.ConnectionScope{App: args[1]}))
		}
	case "test":
		if len(args) == 3 {
			return encode(b.TestAppConnection(appwire.ConnectionTest{App: args[1], Connection: args[2]}))
		}
	case "read":
		if len(args) == 4 || len(args) == 5 {
			query := connections.Query{App: args[1], Connection: args[2], Read: args[3]}
			if len(args) == 5 {
				if err := decodeAppConnectionJSON(bytes.NewBufferString(args[4]), &query.Args); err != nil {
					return errors.New("read arguments must be a JSON object")
				}
			}
			return encode(b.ReadAppConnection(query))
		}
	case "content":
		if len(args) == 5 {
			query := connections.Query{App: args[1], Connection: args[2], Read: args[3]}
			return encode(b.appConnections().ReadGoogleContent(context.Background(), query, args[4], connections.GooglePerson))
		}
	case "register":
		if len(args) == 2 {
			file, err := os.Open(args[1])
			if err != nil {
				return errors.New("the connection declaration could not be opened")
			}
			defer file.Close()
			var cfg connections.Config
			if err := decodeAppConnectionJSON(io.LimitReader(file, 128<<10), &cfg); err != nil {
				return errors.New("the connection declaration must be a JSON object with known fields")
			}
			credential := ""
			if cfg.Kind != connections.Google {
				credential, err = readAppSourceCredential(in)
				if err != nil {
					return err
				}
			}
			return b.appConnections().Register(cfg, credential)
		}
	case "credential":
		if len(args) == 3 {
			credential, err := readAppSourceCredential(in)
			if err != nil {
				return err
			}
			return b.appConnections().SetPersonCredential(context.Background(), args[1], args[2], credential)
		}
	case "bind":
		if len(args) == 4 {
			store, err := b.dataStore("")
			if err != nil {
				return err
			}
			typ, err := store.Type(data.Scope{App: args[1]}, args[2])
			if err != nil {
				return err
			}
			return store.BindCommonType(data.CommonTypeKey(args[3]), typ.ID)
		}
	}
	return errors.New(usage)
}

func readAppSourceCredential(in io.Reader) (string, error) {
	credential, err := io.ReadAll(io.LimitReader(in, connections.MaxCredentialBytes+2))
	if err != nil || len(credential) > connections.MaxCredentialBytes+1 {
		return "", errors.New("could not read one credential line from stdin")
	}
	return string(credential), nil
}

func decodeAppConnectionJSON(in io.Reader, value any) error {
	dec := json.NewDecoder(in)
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(value); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("expected one JSON value")
	}
	return nil
}
