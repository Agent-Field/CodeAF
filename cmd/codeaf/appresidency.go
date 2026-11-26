package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"syscall"

	"github.com/Agent-Field/codeaf/internal/apps/connections"
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

func (b *appBackend) AppResidency(p appwire.AppResidencyParams) (connections.ResidencyStatus, error) {
	status, err := b.appConnections().Residency(p.App, p.Type)
	return status, appConnectionError(err)
}

func (b *appBackend) PreviewAppCutover(p connections.CutoverRequest) (data.ResidencyPlan, error) {
	plan, err := b.appConnections().PreviewCutover(context.Background(), p)
	return plan, appConnectionError(err)
}

func (b *appBackend) ApplyAppCutover(p appwire.ApplyCutoverParams) (data.ResidencyPlan, error) {
	plan, err := b.appConnections().ApplyCutover(context.Background(), p.CutoverRequest, p.Confirmation)
	return plan, appConnectionError(err)
}

func appResidencyCommand(b *appBackend, args []string, out io.Writer) error {
	const usage = "usage: codeaf app residency show APP TYPE | preview APP TYPE MODE [--source-quiesced] | apply APP TYPE MODE --confirm TOKEN [--source-quiesced]"
	if len(args) < 3 {
		return errors.New(usage)
	}
	execute := func(method string, p any) error {
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		if b == nil {
			ctx, cancel := context.WithTimeout(context.Background(), appCommandWait)
			defer cancel()
			reply, err := requestAppCommand(ctx, method, raw)
			if err == nil {
				return json.NewEncoder(out).Encode(reply)
			}
			if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ECONNREFUSED) {
				return err
			}
			backend := &appBackend{}
			backend.data.place = data.NewPlace(backend.dataRoot(), data.Options{NoRefresh: true, RequireAgentPrincipal: true})
			defer backend.closeData()
			value, err := appwire.RunAppCommand(backend, method, raw)
			if err != nil {
				return err
			}
			return json.NewEncoder(out).Encode(value)
		}
		value, err := appwire.RunAppCommand(b, method, raw)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(value)
	}
	if args[0] == "show" && len(args) == 3 {
		return execute("residency.show", appwire.AppResidencyParams{App: args[1], Type: args[2]})
	}
	if len(args) < 4 || (args[0] != "preview" && args[0] != "apply") {
		return errors.New(usage)
	}
	request := connections.CutoverRequest{App: args[1], Type: args[2], To: data.ResidencyMode(args[3])}
	flags := commandFlags("residency " + args[0])
	quiesced := flags.Bool("source-quiesced", false, "confirm upstream writers are paused")
	confirmation := flags.String("confirm", "", "confirmation from the exact cut-over preview")
	if err := parseCommandFlags(flags, args[4:]); err != nil {
		return err
	}
	request.SourceQuiesced = *quiesced
	if flags.NArg() != 0 || (args[0] == "apply") != (*confirmation != "") {
		return errors.New(usage)
	}
	if args[0] == "preview" {
		return execute("residency.preview", request)
	}
	return execute("residency.cutover", appwire.ApplyCutoverParams{CutoverRequest: request, Confirmation: *confirmation})
}

func (b *appBackend) planAppQuery(p appwire.DataScope, ir *data.QueryIR) (*data.QueryIR, connections.Source, map[string]string, error) {
	if p.Draft != "" || p.App == "" {
		return ir, connections.NativeSource(), nil, nil
	}
	query, source, versions, err := b.appConnections().PlanQuery(context.Background(), p.App, *ir)
	return &query, source, versions, appConnectionError(err)
}

func (b *appBackend) appQueryRows(s *data.Store, p appwire.DataScope, sc data.Scope, ir *data.QueryIR, after string, limit int) (*data.Page, connections.Source, error) {
	query, source, versions, err := b.planAppQuery(p, ir)
	if err != nil {
		return nil, source, err
	}
	if len(after) != 0 && source.Connection != "" && source.Residency != string(data.ResidencyLive) {
		var ok bool
		after, ok = connections.UnbindCursor(source, after)
		if !ok {
			return nil, source, appwire.Errorf(appwire.CodeBadParams, "the source changed; start again from the first page")
		}
	}
	page, err := s.QueryRows(sc, query, after, limit)
	if err != nil {
		return nil, source, appDataError(err)
	}
	for _, row := range page.Rows {
		if version := versions[row.ID()]; version != "" {
			row["_source_version"] = version
		}
	}
	if err := b.appConnections().AttachVersions(p.App, ir.Type, source, page.Rows); err != nil {
		return nil, source, appConnectionError(err)
	}
	if source.Connection != "" && source.Residency != string(data.ResidencyLive) {
		page.Next = connections.BindCursor(source, page.Next)
	}
	return page, source, nil
}
