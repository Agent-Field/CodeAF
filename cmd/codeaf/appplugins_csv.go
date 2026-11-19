package main

import (
	"context"
	"errors"
	"os"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ReadPluginCSV is plugins.read_csv: one read of a connected plugin as CSV,
// for a sheet tab's pull from a connector (the desk's canvas contract,
// §12.13). The read is session's ConnectorReader, which arms that plugin's
// read-only tools and nothing else; this door only finds the model and the
// connections, and turns the reader's refusals into wire codes.
//
// NOTHING HERE LOGS. The answer is a person's rows and an error is a reason;
// neither is written to any log on the way out.
func (b *appBackend) ReadPluginCSV(p appwire.PluginReadCSVParams) (appwire.PluginReadCSVResult, error) {
	manager, err := b.pluginManager()
	if err != nil {
		return appwire.PluginReadCSVResult{}, err
	}
	proc, err := openAppEngineProcess()
	if err != nil {
		return appwire.PluginReadCSVResult{}, appEngineError(err)
	}
	settings := proc.Settings
	// The roles are the person's own, read from their profile; a repository's
	// pins have no say over a read that belongs to no repository.
	home, err := os.UserHomeDir()
	if err != nil {
		home = b.crewProfile()
	}
	source, err := v3RolesSource(home, b.crewProfile())
	if err != nil {
		return appwire.PluginReadCSVResult{}, appEngineError(err)
	}
	reader, err := session.NewConnectorReader(session.ConnectorReaderConfig{
		Model: settings.Model, APIKey: settings.APIKey, BaseURL: settings.BaseURL,
		Sources: settings.Sources, ProfileDir: settings.ProfileDir, RolesSource: source,
		Connect: manager,
	})
	if err != nil {
		return appwire.PluginReadCSVResult{}, appEngineError(err)
	}
	text, err := reader.ReadCSV(context.Background(), p.ID, p.Query, p.MaxBytes)
	if err != nil {
		return appwire.PluginReadCSVResult{}, appPluginCSVError(err)
	}
	return appwire.PluginReadCSVResult{CSV: text}, nil
}

// appPluginCSVError carries a reader's refusal to the wire with its reason as
// the whole message, which the app records as the pull's "why".
func appPluginCSVError(err error) error {
	var refusal *session.ConnectorCSVError
	if !errors.As(err, &refusal) {
		return appEngineError(err)
	}
	code := appwire.CodeEngine
	switch refusal.Code {
	case session.ConnectorCSVTooLarge:
		code = appwire.CodeTooLarge
	case session.ConnectorCSVSignedOut:
		code = appwire.CodeSignedOut
	case session.ConnectorCSVRefused:
		code = appwire.CodeReadRefused
	}
	return &appwire.Error{Code: code, Message: refusal.Why}
}
