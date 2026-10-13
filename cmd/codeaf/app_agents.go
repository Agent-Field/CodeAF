package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"

	"github.com/Agent-Field/codeaf/internal/appapi"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// App agents use the canonical wire on a child pipe. The child belongs to this
// app lifetime, and never starts a detached host that could outlive app shutdown.
// Each child receives app credentials over the inherited pipe, never argv or env.
func appAgentOpener(binary string, errorsOut io.Writer) appapi.OpenAgent {
	return appAgentOpenerWithCommand(func(args ...string) *exec.Cmd { return exec.Command(binary, args...) }, errorsOut)
}

func appAgentOpenerWithCommand(commandFor func(...string) *exec.Cmd, errorsOut io.Writer) appapi.OpenAgent {
	return func(ctx context.Context, ask appapi.AgentOpenRequest) (appapi.AgentConnection, error) {
		workspace := ask.Workspace
		if workspace == "" {
			var err error
			workspace, err = os.UserHomeDir()
			if err != nil {
				return appapi.AgentConnection{}, err
			}
		}
		// A saved conversation may already belong to the TUI's host. Join that
		// owner before opening a child, so app reads and renames never fork it.
		if ask.SessionFile != "" {
			if socket, err := enginehost.DialStartingHost(workspace); err == nil {
				client, err := remote.Dial(socket, "local app", remote.Hello{Version: remote.Version, Workspace: workspace, Session: ask.SessionFile, Join: true})
				if err == nil {
					return appAgentConnection(ctx, client, ask)
				}
				_ = socket.Close()
			}
		}
		command := commandFor("engine", "--no-host", "--workspace", workspace)
		command.Stderr = errorsOut
		input, err := command.StdinPipe()
		if err != nil {
			return appapi.AgentConnection{}, err
		}
		output, err := command.StdoutPipe()
		if err != nil {
			input.Close()
			return appapi.AgentConnection{}, err
		}
		closeKeys, err := config.PrepareAppCredentialPipe(command)
		if err != nil {
			input.Close()
			output.Close()
			return appapi.AgentConnection{}, err
		}
		err = command.Start()
		closeKeys()
		if err != nil {
			input.Close()
			output.Close()
			return appapi.AgentConnection{}, err
		}
		pipe := &appAgentPipe{Reader: output, Writer: input, command: command}
		client, err := remote.Dial(pipe, "local app", remote.Hello{Version: remote.Version, Workspace: workspace, Session: ask.SessionFile, New: ask.SessionFile == "", Model: ask.Model, Level: ask.Effort, Launch: &remote.LaunchShape{Interactive: true, DeterministicNames: true}})
		if err != nil {
			pipe.Close()
			return appapi.AgentConnection{}, err
		}
		welcome := client.Welcome()
		if welcome.Launch == nil || !welcome.Launch.DeterministicNames {
			client.Close()
			return appapi.AgentConnection{}, fmt.Errorf("agent engine did not accept deterministic names")
		}
		return appAgentConnection(ctx, client, ask)
	}
}

type appAgentPipe struct {
	io.Reader
	io.Writer
	command *exec.Cmd
	once    sync.Once
}

func (p *appAgentPipe) Close() error {
	p.once.Do(func() {
		if closer, ok := p.Writer.(io.Closer); ok {
			_ = closer.Close()
		}
		if closer, ok := p.Reader.(io.Closer); ok {
			_ = closer.Close()
		}
		_ = p.command.Wait()
	})
	return nil
}

// Settings are applied through the same wire whether the owner is a child or
// an existing TUI host. Detaching and closing the resource remain distinct.
func appAgentConnection(ctx context.Context, client *remote.Client, ask appapi.AgentOpenRequest) (appapi.AgentConnection, error) {
	if ctx.Err() != nil {
		client.Close()
		return appapi.AgentConnection{}, ctx.Err()
	}
	agent := client.Agent()
	if err := agent.EnableAppPermissions(); err != nil {
		client.Close()
		return appapi.AgentConnection{}, err
	}
	usd, err := strconv.ParseFloat(ask.Budget.USD, 64)
	if err != nil {
		client.Close()
		return appapi.AgentConnection{}, err
	}
	if err := agent.SetAppBudget(usd); err != nil {
		client.Close()
		return appapi.AgentConnection{}, err
	}
	posture := map[string]string{"risky": "auto", "everything": "ask", "none": "allow"}[ask.Approvals]
	if posture != "" {
		if err := agent.SetApprovalPosture(posture); err != nil {
			client.Close()
			return appapi.AgentConnection{}, err
		}
	}
	welcome := client.Welcome()
	return appapi.AgentConnection{Engine: agent, SessionFile: welcome.SessionFile, Workspace: welcome.Workspace, Close: func() { _ = client.Close() }, End: agent.Close}, nil
}
