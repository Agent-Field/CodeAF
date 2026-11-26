package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/appcmd"
	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
)

const appCommandLimit = 3 << 20
const appCommandWait = 5 * time.Minute

type appCommandRequest struct {
	Verb   string          `json:"verb"`
	Params json.RawMessage `json:"params"`
}

type appCommandReply struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  *appwire.Error  `json:"error,omitempty"`
}

func appCommandSocket() (string, error) {
	path, err := factoryRunnerSocket(home.Dir())
	return strings.TrimSuffix(path, ".sock") + ".apps", err
}

// The desktop engine holds the home lock while serving this private command
// socket. Both callers reach the same typed apps.* methods.
func (b *appBackend) serveAppCommands() (func(), error) {
	path, err := appCommandSocket()
	if err != nil {
		return nil, err
	}
	_ = os.Remove(path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		return nil, err
	}
	server := &http.Server{ReadHeaderTimeout: time.Second, WriteTimeout: appCommandWait, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/app" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, appCommandLimit))
		var command appCommandRequest
		if dec.Decode(&command) != nil || dec.Decode(new(any)) != io.EOF {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		value, err := b.runAppCLICommand(r.Context(), command.Verb, command.Params)
		var reply appCommandReply
		if err == nil {
			reply.Result, err = json.Marshal(value)
		}
		if err != nil && !errors.As(err, &reply.Error) {
			reply.Error = &appwire.Error{Code: appwire.CodeEngine, Message: "The app command could not finish."}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(reply)
	})}
	guard.Go("app/commands", func() { _ = server.Serve(listener) })
	return func() {
		_ = server.Close()
		_ = os.Remove(path)
	}, nil
}

func runAppCommand(args []string) error {
	verb := strings.ReplaceAll(args[0], "-", "_")
	params := json.RawMessage("{}")
	switch {
	case verb == "help":
		fmt.Fprintln(os.Stdout, "codeaf app <new|plan|build|check|say|build_state|preview|look|discard|change|kit|parts|common-types|bind-common-type|reference|list|get|screen|versions|mark|presence|show|query|invoke|propose_change|agent_ask|agent_query|agent_grant|agent_duty_add|agent_duty_remove> [JSON|-]")
		fmt.Fprintln(os.Stdout, "codeaf app parts --for <intent>")
		fmt.Fprintln(os.Stdout, "codeaf app <match-candidates|match-scan|match-accept|match-reject|match-merge|match-unmerge> [JSON|-]")
		fmt.Fprintln(os.Stdout, "codeaf app <data-query|data-aggregate|data-types> [JSON|-]")
		fmt.Fprintln(os.Stdout, "codeaf app model-policy FILE.json")
		return nil
	case verb == "kit" && len(args) == 1:
		_, err := fmt.Fprintln(os.Stdout, string(schema.KitJSON()))
		return err
	case verb == "reference" && len(args) == 1:
		_, err := fmt.Fprintln(os.Stdout, builder.Reference())
		return err
	case (verb == "common_types" || verb == "parts") && len(args) == 1:
		reply, err := appwire.RunAppCommand(nil, verb, params)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(reply)
	case verb == "parts" && len(args) == 3 && args[1] == "--for":
		var err error
		params, err = appcmd.PartsArgs(args[2])
		if err != nil {
			return err
		}
	case verb == "look" && len(args) == 3:
		params, _ = json.Marshal(map[string]string{"app": args[1], "screen": args[2]})
	case len(args) > 2:
		return errors.New("usage: codeaf app <verb> [JSON|-]")
	case len(args) == 2 && args[1] == "-":
		raw, err := io.ReadAll(io.LimitReader(os.Stdin, appCommandLimit+1))
		if err != nil {
			return err
		}
		params = raw
	case len(args) == 2:
		params = json.RawMessage(args[1])
	}
	if len(params) > appCommandLimit || !json.Valid(params) {
		return errors.New("app command arguments must be valid JSON")
	}
	path, err := appCommandSocket()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(appCommandRequest{Verb: verb, Params: params})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), appCommandWait)
	defer cancel()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}
	defer transport.CloseIdleConnections()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://apps/app", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		if verb == "bind_common_type" && (errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED)) {
			return runAppCommonBind(params)
		}
		return errors.New("open the desktop app for this profile to use app commands")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("the desktop engine rejected the app command request")
	}
	var reply appCommandReply
	if err := json.NewDecoder(response.Body).Decode(&reply); err != nil {
		return err
	}
	if reply.Error != nil {
		return reply.Error
	}
	_, err = fmt.Fprintln(os.Stdout, string(reply.Result))
	return err
}
