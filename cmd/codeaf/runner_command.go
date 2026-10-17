package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// runnerUse is the one sentence every refusal of `runner` ends in.
const runnerUse = "use codeaf runner join <link|url> [--name name] [--token session] [--url factory] [--attach-only]"

// runRunner joins this computer to a factory and, unless --attach-only, runs
// that factory's jobs here until it is stopped.
//
// <link|url> is one of:
//
//   - an access link, `codeaf://factory/join?url=…#token=…`: the person signs
//     in with GitHub (factory_join.go) and the factory's session for THEM is
//     kept in attach.json. The runner works as that person;
//   - a bare link secret, with --url naming the factory (or the factory this
//     machine is already attached to): the same link, spelled in two parts;
//   - a factory address, https://host or http://host:port, with the session
//     the person already holds in --token.
//
// ATTACHING IS THE SAME DOOR `codeaf attach` USES, and so is the file it
// writes: any codeaf on this machine that runs a worker follows attach.json.
// What `join` adds is that THIS process is that worker by default
// ([runFactoryRunner]): one runner per codeaf home holds worker.lock, and a
// second process shares its sessions and waits to take over, so joining twice
// registers one machine, not two.
func runRunner(args []string) error {
	if len(args) == 0 || args[0] != "join" {
		return wrongCall(runnerUse)
	}
	flags := commandFlags("runner join")
	name := flags.String("name", "", "the name this machine goes by on the factory (default its hostname)")
	token := flags.String("token", "", "the session this person already holds on that factory")
	address := flags.String("url", "", "the factory's address, when <link> is only the link's secret")
	attachOnly := flags.Bool("attach-only", false, "write the attachment and return; run no jobs in this process")
	if err := parseCommandFlags(flags, reorder(flags, args[1:])); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return wrongCall(runnerUse)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	attached, err := joinFactory(ctx, flags.Arg(0), *address, *name, strings.TrimSpace(*token), os.Stdout)
	if err != nil {
		return err
	}
	if *attachOnly {
		_, err = fmt.Fprintln(os.Stdout, "attached to "+attached.URL+" as "+attached.Name+"; new codeaf windows use it")
		return err
	}
	return runFactoryRunner(ctx, attached, os.Stdout)
}

// joinFactory resolves target to a factory and the session to work with there,
// and writes attach.json. It answers what it wrote.
func joinFactory(ctx context.Context, target, address, name, session string, out io.Writer) (factoryAttachment, error) {
	target = strings.TrimSpace(target)
	var base string
	switch {
	case strings.HasPrefix(target, "codeaf://"):
		link, err := parseFactoryJoinLink(target)
		if err != nil {
			return factoryAttachment{}, wrongCall(err.Error())
		}
		base = link.base
		if session == "" {
			if session, err = redeemFactoryLink(ctx, link, out); err != nil {
				return factoryAttachment{}, err
			}
		}
	case strings.Contains(target, "://"):
		var err error
		if base, err = factoryURL(target); err != nil {
			return factoryAttachment{}, wrongCall(err.Error())
		}
	default:
		// A bare secret: the factory comes from --url or from where this
		// machine is already attached.
		where := strings.TrimSpace(address)
		if where == "" {
			if a, ok := readFactoryAttachment(); ok {
				where = a.URL
			}
		}
		if where == "" {
			return factoryAttachment{}, wrongCall("a bare link secret needs --url <factory>; " + runnerUse)
		}
		var err error
		if base, err = factoryURL(where); err != nil {
			return factoryAttachment{}, wrongCall(err.Error())
		}
		if session == "" {
			if session, err = redeemFactoryLink(ctx, factoryJoinLink{base: base, secret: target}, out); err != nil {
				return factoryAttachment{}, err
			}
		}
	}
	if name = strings.TrimSpace(name); name == "" {
		name, _ = os.Hostname()
	}
	attached := factoryAttachment{URL: base, Name: name, Token: session}
	if err := writeFactoryAttachment(attached); err != nil {
		return factoryAttachment{}, err
	}
	return attached, nil
}

// runFactoryRunner runs the factory's jobs on this machine until ctx ends:
// this process is the worker host for the attached factory. It is a variable
// so a test of the command's wiring can end it without a model.
var runFactoryRunner = func(ctx context.Context, attached factoryAttachment, out io.Writer) error {
	st, err := store.Open(v3FactoryRoot())
	if err != nil {
		return err
	}
	workspace, _ := os.Getwd()
	sessions := factorySessions(newFactorySessions())
	sessions.SetFactorySession(attached.URL, attached.Token)
	host := &appFactoryHost{ctx: ctx, floor: st, workspace: workspace, profile: config.ProfileDir(), credentials: sessions}
	var me api.Me
	if appFactoryRequest(ctx, factoryTarget{attached.URL, attached.Token}, "GET", "/v1/me", nil, &me) == nil && me.Handle != "" {
		fmt.Fprintf(out, "running %s's jobs for %s on this machine; stop with ctrl-c\n", "@"+strings.TrimPrefix(me.Handle, "@"), attached.URL)
	} else {
		fmt.Fprintf(out, "running jobs for %s on this machine; stop with ctrl-c\n", attached.URL)
	}
	host.workers()
	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
